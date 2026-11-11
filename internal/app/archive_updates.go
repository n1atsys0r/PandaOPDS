package app

import (
	"context"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"pandaopds/internal/eh"
)

const (
	archiveVersionBatchSize = 25
	archiveVersionBatchGap  = 2 * time.Second
)

var archiveVersionToken = regexp.MustCompile(`^[0-9a-fA-F]+$`)

func (s *Server) startArchiveVersionChecks() {
	if s.Settings.ArchiveUpdateIntervalSeconds <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.stopArchiveUpdates = cancel
	s.archiveUpdatesDone = make(chan struct{})
	seconds := min(int64(s.Settings.ArchiveUpdateIntervalSeconds), math.MaxInt64/int64(time.Second))
	interval := max(time.Duration(seconds)*time.Second, archiveVersionBatchGap)
	go func() {
		defer close(s.archiveUpdatesDone)
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				err := s.checkArchiveVersions(ctx)
				if err != nil && ctx.Err() == nil && s.Logger != nil {
					s.Logger.Print("archive update: upstream version scan interrupted")
				}
				timer.Reset(archiveVersionCheckDelay(interval, err))
			}
		}
	}()
}

func archiveVersionCheckDelay(interval time.Duration, err error) time.Duration {
	if err == nil {
		return interval
	}
	delay := max(interval, 5*time.Second)
	var circuit *eh.CircuitOpenError
	if errors.As(err, &circuit) {
		return max(delay, time.Duration(circuit.RetryAfter*float64(time.Second)))
	}
	var upstream *eh.UpstreamError
	if errors.As(err, &upstream) {
		if seconds, e := strconv.ParseInt(upstream.RetryAfter, 10, 64); e == nil && seconds > 0 {
			return max(delay, time.Duration(min(seconds, math.MaxInt64/int64(time.Second)))*time.Second)
		}
		if when, e := http.ParseTime(upstream.RetryAfter); e == nil {
			return max(delay, time.Until(when))
		}
	}
	return delay
}

func (s *Server) checkArchiveVersions(ctx context.Context) error {
	entries := s.Store.ListReady(1, math.MaxInt)
	roots := make(map[eh.GalleryRef]eh.GalleryRef, len(entries))
	refs := make([]eh.GalleryRef, 0, len(entries))
	for _, entry := range entries {
		ref := eh.GalleryRef{GID: entry.ActiveGID, Token: entry.ActiveToken}
		if ref.GID == 0 || ref.Token == "" {
			ref = eh.GalleryRef{GID: entry.GID, Token: entry.Token}
		}
		if _, seen := roots[ref]; !seen {
			refs = append(refs, ref)
			roots[ref] = eh.GalleryRef{GID: entry.GID, Token: entry.Token}
		}
	}
	for start := 0; start < len(refs) && ctx.Err() == nil; start += archiveVersionBatchSize {
		if start > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(archiveVersionBatchGap):
			}
		}
		batch := refs[start:min(start+archiveVersionBatchSize, len(refs))]
		results, err := s.Service.Client.GDataBatch(ctx, batch)
		for _, metadata := range results {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			ref := eh.GalleryRef{GID: metadata.GID, Token: metadata.Token}
			root, ok := roots[ref]
			if !ok {
				continue
			}
			if err := s.Store.UpdateVersionLinks(ref.GID, ref.Token, metadata); err != nil {
				if s.Logger != nil {
					s.Logger.Printf("archive update: could not save version data for gid %d", ref.GID)
				}
				continue
			}
			if s.Settings.ArchiveAutoUpdate && s.Settings.HasIPB() && isArchiveUpdate(ref.GID, metadata.CurrentGID, metadata.CurrentKey) {
				_, _ = s.Archives.StartVersionUpdate(ctx, root.GID, root.Token, metadata.CurrentGID, metadata.CurrentKey, nil)
			}
		}
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func isArchiveUpdate(activeGID, targetGID int, targetToken string) bool {
	return targetGID > 0 && targetGID != activeGID && archiveVersionToken.MatchString(targetToken)
}

func (s *Server) archiveUpdateTarget(gid int, token string) (eh.GalleryRef, bool) {
	activeGID, _ := s.Store.ResolveActive(gid, token)
	targetGID, targetToken := s.Store.CurrentUpdate(gid, token)
	return eh.GalleryRef{GID: targetGID, Token: targetToken}, isArchiveUpdate(activeGID, targetGID, targetToken)
}

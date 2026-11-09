package favorites

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

type Syncer struct {
	Settings  config.Settings
	Service   *eh.Service
	Archive   *archive.Manager
	State     *State
	mu        sync.Mutex
	busy      bool
	lastError string
	cancel    context.CancelFunc
	done      chan struct{}
	trigger   chan struct{}
}

func NewSyncer(cfg config.Settings, service *eh.Service, arch *archive.Manager, state *State) *Syncer {
	return &Syncer{Settings: cfg, Service: service, Archive: arch, State: state, trigger: make(chan struct{}, 1)}
}
func (s *Syncer) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		var ticker *time.Ticker
		var tick <-chan time.Time
		if s.Settings.FavoritesSyncIntervalSeconds > 0 {
			ticker = time.NewTicker(time.Duration(s.Settings.FavoritesSyncIntervalSeconds * float64(time.Second)))
			tick = ticker.C
			defer ticker.Stop()
			_, _ = s.Run(ctx)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick:
				_, _ = s.Run(ctx)
			case <-s.trigger:
				select {
				case <-time.After(3 * time.Second):
				case <-ctx.Done():
					return
				}
				_, _ = s.Run(ctx)
			}
		}
	}()
}
func (s *Syncer) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}
func (s *Syncer) RequestRun() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}
func (s *Syncer) Status() map[string]any {
	s.mu.Lock()
	busy, err := s.busy, s.lastError
	s.mu.Unlock()
	known := s.State.Summary()
	var categories any = s.Settings.FavoritesSyncCategories
	if len(s.Settings.FavoritesSyncCategories) == 0 {
		categories = "all"
	}
	return map[string]any{"enabled": s.Settings.FavoritesSyncIntervalSeconds > 0, "interval_seconds": s.Settings.FavoritesSyncIntervalSeconds, "auto_archive": s.Settings.FavoritesSyncArchive, "categories": categories, "excluded_categories": s.Settings.FavoritesSyncExcludes, "running": busy, "last_error": nilOrString(err), "known_count": known["known_count"], "archived_count": known["archived_count"], "error_count": known["error_count"], "baseline": known["baseline"], "last_run": known["last_run"], "last_ok": known["last_ok"], "scanned_pages": known["scanned_pages"]}
}
func nilOrString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (s *Syncer) Run(ctx context.Context) (map[string]any, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return nil, fmt.Errorf("favorites sync already in progress")
	}
	s.busy = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock() }()
	if !s.Settings.HasIPB() {
		return map[string]any{"ok": false, "skipped": true, "error": "favorites require IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)", "status": s.Status()}, nil
	}
	known := s.State.Known()
	archived := s.State.Archived()
	errs := s.State.Errors()
	favcats := s.State.Favcats()
	baseline := s.State.Baseline()
	newItems := []eh.GalleryListItem{}
	oldCount := 0
	pages := 0
	cursor := ""
	favcatNames := map[int]string{}
	for pages < s.Settings.FavoritesSyncMaxPages {
		page, e := s.Service.Favorites(ctx, cursor, "")
		if e != nil {
			s.mu.Lock()
			s.lastError = e.Error()
			s.mu.Unlock()
			ok := false
			_ = s.State.Update(nil, nil, nil, nil, nil, nil, &ok, &pages)
			return nil, e
		}
		pages++
		for id, name := range page.FavcatMap {
			favcatNames[id] = name
		}
		for _, item := range page.Galleries {
			k := fmt.Sprintf("%d:%s", item.GID, item.Token)
			if known[k] {
				oldCount++
			} else {
				known[k] = true
				newItems = append(newItems, item)
			}
			if item.Favcat != nil {
				favcats[k] = *item.Favcat
			}
		}
		if page.NextGID == "" || oldCount >= s.Settings.FavoritesSyncMatchThreshold {
			break
		}
		cursor = page.NextGID
	}
	isBaseline := !baseline
	newArchived := map[string]bool{}
	for k, v := range archived {
		newArchived[k] = v
	}
	if !isBaseline && s.Settings.FavoritesSyncArchive {
		for _, item := range newItems {
			if !s.inScope(item.Favcat) {
				continue
			}
			k := fmt.Sprintf("%d:%s", item.GID, item.Token)
			if archived[k] {
				continue
			}
			if meta := s.Archive.Store.Get(item.GID, item.Token); meta != nil {
				if meta["status"] == "ready" && strings.EqualFold(fmt.Sprint(meta["or"]), "res") && canonicalTier(s.Settings.ArchiveQuality) == "org" {
					_, e := s.Archive.Start(ctx, item.GID, item.Token, "org", true)
					if e != nil {
						errs[k] = e.Error()
					} else {
						newArchived[k] = true
					}
				}
				continue
			}
			if _, e := s.Archive.Start(ctx, item.GID, item.Token, s.Settings.ArchiveQuality, false); e != nil {
				errs[k] = e.Error()
			} else {
				newArchived[k] = true
			}
		}
	}
	ok := true
	var basePtr *bool
	if isBaseline {
		basePtr = &ok
	}
	_ = s.State.Update(known, newArchived, errs, favcats, favcatNames, basePtr, &ok, &pages)
	s.mu.Lock()
	s.lastError = ""
	s.mu.Unlock()
	return map[string]any{"ok": true, "baseline": isBaseline, "new_count": len(newItems), "archived_count": len(newArchived), "scanned_pages": pages, "status": s.Status()}, nil
}
func (s *Syncer) inScope(cat *int) bool {
	if cat == nil {
		return true
	}
	allow := len(s.Settings.FavoritesSyncCategories) == 0
	for _, id := range s.Settings.FavoritesSyncCategories {
		if *cat == id {
			allow = true
		}
	}
	for _, id := range s.Settings.FavoritesSyncExcludes {
		if *cat == id {
			return false
		}
	}
	return allow
}
func canonicalTier(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "original", "org":
		return "org"
	case "resample", "resized", "res":
		return "res"
	}
	return strings.ToLower(t)
}
func (s *Syncer) ParsedFavcat(key string) *int {
	v, ok := s.State.Favcats()[key]
	if !ok {
		return nil
	}
	return &v
}
func ParseKey(key string) (int, string, bool) {
	a, b, ok := strings.Cut(key, ":")
	if !ok {
		return 0, "", false
	}
	id, e := strconv.Atoi(a)
	return id, b, e == nil
}

package app

import (
	"context"
	"net/http"
	"time"

	"pandaopds/internal/migration"
)

type ArchiveScanJob struct {
	ID          uint64 `json:"id"`
	Status      string `json:"status"`
	Imported    int    `json:"imported"`
	Skipped     int    `json:"skipped"`
	Conflicts   int    `json:"conflicts"`
	Error       string `json:"error,omitempty"`
	StartedAt   int64  `json:"started_at,omitempty"`
	CompletedAt int64  `json:"completed_at,omitempty"`
}

func (s *Server) archiveScanStatus(w http.ResponseWriter, _ *http.Request) {
	s.archiveScanMu.RLock()
	status := s.archiveScan
	s.archiveScanMu.RUnlock()
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) startArchiveScan(w http.ResponseWriter, _ *http.Request) {
	s.archiveScanMu.Lock()
	if s.archiveScan.Status == "running" {
		status := s.archiveScan
		s.archiveScanMu.Unlock()
		writeJSON(w, http.StatusAccepted, status)
		return
	}
	s.archiveScanNextID++
	job := ArchiveScanJob{ID: s.archiveScanNextID, Status: "running", StartedAt: time.Now().Unix()}
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	s.archiveScan, s.archiveScanDone = job, done
	s.archiveScanCancel = cancel
	s.archiveScanMu.Unlock()
	writeJSON(w, http.StatusAccepted, job)

	go func() {
		report, err := s.Documents.Scan(ctx)
		if err == nil && s.Database != nil {
			_, err = migration.LinkLegacyDocuments(s.Database, s.Settings.ArchiveDir)
		}
		completed := time.Now().Unix()
		result := ArchiveScanJob{ID: job.ID, StartedAt: job.StartedAt, CompletedAt: completed}
		if err != nil {
			result.Status, result.Error = "error", err.Error()
		} else if found, ok := report["documents_found"].(int); ok {
			result.Imported = found
			result.Status = "success"
		} else {
			result.Status = "success"
		}
		s.ArchiveScanReady = err == nil
		if err == nil && s.Subscriptions != nil {
			s.Subscriptions.EnableDispatch()
		}
		s.archiveScanMu.Lock()
		s.archiveScan = result
		s.archiveScanCancel = nil
		cancel()
		close(done)
		s.archiveScanMu.Unlock()
	}()
}

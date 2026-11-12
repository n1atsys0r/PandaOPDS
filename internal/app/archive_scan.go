package app

import (
	"context"
	"net/http"
	"time"
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
		report, err := s.Store.ScanLegacySidecarsContext(ctx)
		completed := time.Now().Unix()
		result := ArchiveScanJob{ID: job.ID, Imported: report.State + report.Gallery + report.Cover, Skipped: report.Skipped, Conflicts: report.Conflicts, StartedAt: job.StartedAt, CompletedAt: completed}
		if err != nil {
			result.Status, result.Error = "error", err.Error()
		} else if result.Imported > 0 {
			result.Status = "success"
		} else if result.Conflicts > 0 {
			result.Status = "conflict"
		} else if result.Skipped > 0 {
			result.Status = "skipped"
		} else {
			result.Status = "success"
		}
		s.archiveScanMu.Lock()
		s.archiveScan = result
		s.archiveScanCancel = nil
		cancel()
		close(done)
		s.archiveScanMu.Unlock()
	}()
}

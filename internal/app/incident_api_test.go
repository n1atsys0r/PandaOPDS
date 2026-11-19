package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pandaopds/internal/config"
	"pandaopds/internal/recorder"
)

func TestIncidentAPIs(t *testing.T) {
	settings := config.Settings{
		CacheDir:          t.TempDir(),
		ArchiveDir:        t.TempDir(),
		ArchiveStateDir:   t.TempDir(),
		PublicBaseURL:     "http://example.com",
		PageURLTTLSeconds: 300,
	}
	server, err := New(settings, nil)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	handler := server.Handler()

	// 1. Initial GET /api/incidents
	req := httptest.NewRequest(http.MethodGet, "/api/incidents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var initialResp struct {
		Range          string                   `json:"range"`
		Summary        recorder.SummaryStats    `json:"summary"`
		Activity       []recorder.ActivitySlot  `json:"activity"`
		FailureCauses  []recorder.BreakdownItem `json:"failure_causes"`
		RequestSources []recorder.BreakdownItem `json:"request_sources"`
		Pulse          []recorder.PulseSlot     `json:"pulse"`
		History        []recorder.IncidentSummary `json:"history"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &initialResp); err != nil {
		t.Fatalf("unmarshal incidents response: %v", err)
	}
	if len(initialResp.Activity) != 24 {
		t.Fatalf("expected 24 activity slots for 24h, got %d", len(initialResp.Activity))
	}
	if len(initialResp.FailureCauses) != 5 {
		t.Fatalf("expected 5 failure causes, got %d", len(initialResp.FailureCauses))
	}

	// 1b. Test GET /api/incidents?range=7d (42 activity slots, 4h per slot)
	req7d := httptest.NewRequest(http.MethodGet, "/api/incidents?range=7d", nil)
	w7d := httptest.NewRecorder()
	handler.ServeHTTP(w7d, req7d)
	if w7d.Code != http.StatusOK {
		t.Fatalf("expected 200 for 7d, got %d", w7d.Code)
	}
	var resp7d struct {
		Activity []recorder.ActivitySlot `json:"activity"`
	}
	if err := json.Unmarshal(w7d.Body.Bytes(), &resp7d); err != nil {
		t.Fatalf("unmarshal 7d response: %v", err)
	}
	if len(resp7d.Activity) != 42 {
		t.Fatalf("expected 42 activity slots for 7d, got %d", len(resp7d.Activity))
	}

	// 2. Trigger circuit trip via server
	server.Service.Throttle.Circuit.TripWithDetail("exceed", 10*time.Minute, "509 Image quota exceeded", "GET", "/image/123/page/1")

	// Verify active incident exists
	req = httptest.NewRequest(http.MethodGet, "/api/incidents", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var trippedResp struct {
		ActiveIncident *recorder.Incident         `json:"active_incident"`
		History        []recorder.IncidentSummary `json:"history"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &trippedResp); err != nil {
		t.Fatalf("unmarshal incidents response: %v", err)
	}
	if trippedResp.ActiveIncident == nil {
		t.Fatal("expected active incident")
	}
	if trippedResp.ActiveIncident.Reason != "exceed" {
		t.Fatalf("expected exceed reason, got %s", trippedResp.ActiveIncident.Reason)
	}
	incidentID := trippedResp.ActiveIncident.ID

	// 3. GET /api/incidents/{id}
	req = httptest.NewRequest(http.MethodGet, "/api/incidents/"+incidentID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for incident detail, got %d: %s", w.Code, w.Body.String())
	}
	var detail recorder.Incident
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail response: %v", err)
	}
	if detail.ID != incidentID || detail.TriggerError != "509 Image quota exceeded" {
		t.Fatalf("unexpected detail: %+v", detail)
	}

	// 4. POST /api/circuit/reset
	req = httptest.NewRequest(http.MethodPost, "/api/circuit/reset", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for circuit reset, got %d: %s", w.Code, w.Body.String())
	}

	// Verify circuit is now closed
	if snap := server.Service.Throttle.Circuit.Snapshot(); snap.State != "closed" {
		t.Fatalf("expected closed circuit snapshot, got %+v", snap)
	}
}

func TestStandbyAndUpstreamFiltering(t *testing.T) {
	settings := config.Settings{
		CacheDir:          t.TempDir(),
		ArchiveDir:        t.TempDir(),
		ArchiveStateDir:   t.TempDir(),
		PublicBaseURL:     "http://example.com",
		PageURLTTLSeconds: 300,
	}
	server, err := New(settings, nil)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	handler := server.Handler()

	// 1. Dashboard standby requests (polling /api/status, /api/incidents, /health, /favicon.ico)
	standbyPaths := []string{
		"/api/status",
		"/api/incidents",
		"/api/config",
		"/api/home",
		"/health",
		"/favicon.ico",
	}

	for _, p := range standbyPaths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Ring buffer should have 0 traces recorded because none are upstream-related
	if count := server.Tracker.RingBuffer().Count(); count != 0 {
		t.Fatalf("expected 0 traces in standby, got %d", count)
	}

	// 2. Upstream-related requests (e.g. /image/123/abc/thumb)
	req := httptest.NewRequest(http.MethodGet, "/image/123/abc/thumb", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Ring buffer should now record this upstream request
	if count := server.Tracker.RingBuffer().Count(); count != 1 {
		t.Fatalf("expected 1 trace recorded for upstream request, got %d", count)
	}

	traces := server.Tracker.RingBuffer().Recent(1)
	if traces[0].Kind != "thumb" || traces[0].Path != "/image/123/abc/thumb" {
		t.Fatalf("unexpected trace recorded: %+v", traces[0])
	}
}

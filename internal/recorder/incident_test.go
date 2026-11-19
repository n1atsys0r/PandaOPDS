package recorder

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTraceRingBuffer(t *testing.T) {
	buf := NewTraceRingBuffer(5)
	if buf.Count() != 0 {
		t.Fatalf("expected 0 count, got %d", buf.Count())
	}

	for i := 1; i <= 7; i++ {
		buf.Add(RequestTrace{
			Method: "GET",
			Path:   "/test",
			Status: 200,
		})
	}

	if buf.Count() != 5 {
		t.Fatalf("expected 5 count, got %d", buf.Count())
	}

	recent := buf.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 items, got %d", len(recent))
	}
	// The last items added had IDs 5, 6, 7
	if recent[0].ID != 5 || recent[2].ID != 7 {
		t.Fatalf("expected IDs 5..7, got %d..%d", recent[0].ID, recent[2].ID)
	}
}

func TestIncidentLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "incidents.json")

	tracker := NewTracker(cachePath, 100)

	// Add some requests before trip
	for i := 0; i < 5; i++ {
		tracker.RecordTrace(RequestTrace{
			Method: "GET",
			Path:   "/image/123/page/1",
			Kind:   "image",
			Status: 200,
		})
	}

	// Trigger circuit breaker
	trigger := RequestTrace{
		Method: "GET",
		Path:   "/image/123/page/6",
		Kind:   "image",
		Status: 509,
	}
	tracker.OnCircuitTrip("exceed", 10*time.Minute, trigger, "509 Image limit exceeded")

	active := tracker.ActiveIncident()
	if active == nil {
		t.Fatal("expected active incident")
	}
	if active.Reason != "exceed" || active.State != "ongoing" {
		t.Fatalf("unexpected active state: %+v", active)
	}
	if len(active.TracesBefore) != 5 {
		t.Fatalf("expected 5 traces before, got %d", len(active.TracesBefore))
	}

	// Block some requests during circuit open
	for i := 0; i < 3; i++ {
		tracker.OnRequestBlocked(RequestTrace{
			Method: "GET",
			Path:   "/image/123/page/7",
			Status: 503,
		})
	}

	active = tracker.ActiveIncident()
	if active.BlockedCount != 3 {
		t.Fatalf("expected 3 blocked requests, got %d", active.BlockedCount)
	}
	if len(active.TracesDuring) != 3 {
		t.Fatalf("expected 3 traces during, got %d", len(active.TracesDuring))
	}

	// Reset circuit
	tracker.OnCircuitReset()
	if tracker.ActiveIncident() != nil {
		t.Fatal("expected no active incident after reset")
	}

	list := tracker.ListIncidents(10)
	if len(list) != 1 {
		t.Fatalf("expected 1 incident in history, got %d", len(list))
	}
	if list[0].State != "reset" {
		t.Fatalf("expected reset state, got %s", list[0].State)
	}

	// Verify persistence
	tracker2 := NewTracker(cachePath, 100)
	list2 := tracker2.ListIncidents(10)
	if len(list2) != 1 || list2[0].ID != list[0].ID {
		t.Fatalf("persisted incident mismatch: %+v", list2)
	}
}

func TestPulseGeneration(t *testing.T) {
	tracker := NewTracker("", 100)

	// Add an incident
	tracker.OnCircuitTrip("banned", 5*time.Minute, RequestTrace{
		Method: "GET",
		Path:   "/opds/v2.0",
		Status: 503,
	}, "IP banned")

	slots := tracker.Pulse(5, 30)
	if len(slots) != 10 {
		t.Fatalf("expected 10 slots, got %d", len(slots))
	}

	// The current slot should show circuit_open
	lastSlot := slots[9]
	if lastSlot.Status != "circuit_open" || lastSlot.Reason != "banned" {
		t.Fatalf("expected circuit_open status, got %+v", lastSlot)
	}
}

func TestUpstreamErrorIncident(t *testing.T) {
	tracker := NewTracker("", 100)

	// Trigger 3 consecutive 502 upstream errors
	for i := 0; i < 3; i++ {
		tracker.OnUpstreamError(502, "E-Hentai fatal server error", RequestTrace{
			Method: "GET",
			Path:   "/gallery/123",
			Status: 502,
		})
	}

	list := tracker.ListIncidents(10)
	if len(list) != 1 {
		t.Fatalf("expected 1 upstream_error incident, got %d", len(list))
	}
	if list[0].Type != "upstream_error" || list[0].Reason != "upstream_502" {
		t.Fatalf("unexpected incident: %+v", list[0])
	}
}

func TestDashboardOverview(t *testing.T) {
	tracker := NewTracker("", 100)

	// Test 24h range -> 24 slots (1h each)
	stats24h := tracker.DashboardOverview("24h")
	if len(stats24h.Activity) != 24 {
		t.Fatalf("expected 24 activity slots for 24h, got %d", len(stats24h.Activity))
	}

	// Test 7d range -> 42 slots (4h each)
	stats7d := tracker.DashboardOverview("7d")
	if len(stats7d.Activity) != 42 {
		t.Fatalf("expected 42 activity slots for 7d, got %d", len(stats7d.Activity))
	}
}

func TestArchiveErrorIncident(t *testing.T) {
	tracker := NewTracker("", 100)

	// User triggers an archive operation that fails with 403 Insufficient GP
	tr := RequestTrace{
		Method: "POST",
		Path:   "/api/archive/12345/start",
		Kind:   "archive",
		Status: 403,
		Error:  "ArchiverUnavailableError: Your Star Membership does not qualify for free archive downloads",
	}
	tracker.RecordTrace(tr)
	tracker.OnUpstreamError(403, tr.Error, tr)

	// 1. Verify incident history has this error
	list := tracker.ListIncidents(10)
	if len(list) != 1 {
		t.Fatalf("expected 1 incident recorded, got %d", len(list))
	}
	if list[0].Reason != "archiver_unavailable" {
		t.Fatalf("expected archiver_unavailable, got %s", list[0].Reason)
	}
	if list[0].TriggerError != tr.Error {
		t.Fatalf("expected trigger error to match, got %s", list[0].TriggerError)
	}

	// 2. Verify dashboard breakdown
	overview := tracker.DashboardOverview("24h")
	if overview.Summary.Failures != 1 {
		t.Fatalf("expected 1 failure in summary, got %d", overview.Summary.Failures)
	}

	var foundUpstreamError bool
	for _, cause := range overview.FailureCauses {
		if cause.Key == "upstream_error" && cause.Count == 1 {
			foundUpstreamError = true
			break
		}
	}
	if !foundUpstreamError {
		t.Fatalf("expected upstream_error to have count 1, got: %+v", overview.FailureCauses)
	}

	var foundArchiveSource bool
	for _, src := range overview.RequestSources {
		if src.Key == "archive" && src.Count == 1 {
			foundArchiveSource = true
			break
		}
	}
	if !foundArchiveSource {
		t.Fatalf("expected archive source to have count 1, got: %+v", overview.RequestSources)
	}
}

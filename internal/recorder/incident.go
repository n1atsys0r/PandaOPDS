package recorder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Incident captures a circuit breaker trip or significant upstream error event.
type Incident struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`   // "circuit_breaker" | "upstream_error"
	Reason       string         `json:"reason"` // "banned" | "exceed" | "upstream_502" | "upstream_503"
	State        string         `json:"state"`  // "ongoing" | "recovered" | "reset"
	StartedAt    time.Time      `json:"started_at"`
	RecoveredAt  *time.Time     `json:"recovered_at,omitempty"`
	DurationSec  float64        `json:"duration_sec"`
	CooldownSec  float64        `json:"cooldown_sec"`
	TriggerTrace RequestTrace   `json:"trigger_trace"`
	TriggerError string         `json:"trigger_error"`
	BlockedCount int            `json:"blocked_count"`
	TracesBefore []RequestTrace `json:"traces_before"`
	TracesDuring []RequestTrace `json:"traces_during"`
	TracesAfter  []RequestTrace `json:"traces_after"`
}

// IncidentSummary provides an overview of an incident for listing.
type IncidentSummary struct {
	ID             string     `json:"id"`
	Type           string     `json:"type"`
	Reason         string     `json:"reason"`
	State          string     `json:"state"`
	StartedAt      time.Time  `json:"started_at"`
	RecoveredAt    *time.Time `json:"recovered_at,omitempty"`
	DurationSec    float64    `json:"duration_sec"`
	CooldownSec    float64    `json:"cooldown_sec"`
	BlockedCount   int        `json:"blocked_count"`
	TriggerSummary string     `json:"trigger_summary"`
	TriggerPath    string     `json:"trigger_path"`
	TriggerError   string     `json:"trigger_error,omitempty"`
}

// PulseSlot represents one segment in the activity sparkline/pulse strip.
type PulseSlot struct {
	Time         string `json:"time"`   // "15:04"
	Minute       string `json:"minute"` // for backwards compatibility
	Timestamp    int64  `json:"timestamp"`
	Status       string `json:"status"` // "ok" | "idle" | "warning" | "error" | "circuit_open"
	TotalCount   int    `json:"total_count"`
	ErrorCount   int    `json:"error_count"`
	BlockedCount int    `json:"blocked_count"`
	Reason       string `json:"reason,omitempty"`
}

// ActivitySlot represents one bar in the TRAWL-style request activity timeline.
type ActivitySlot struct {
	Label        string `json:"label"` // e.g. "昨天 18时"
	Timestamp    int64  `json:"timestamp"`
	SuccessCount int    `json:"success_count"`
	FailureCount int    `json:"failure_count"`
	BlockedCount int    `json:"blocked_count"`
	TotalCount   int    `json:"total_count"`
	Status       string `json:"status"` // "ok" | "idle" | "warning" | "error" | "circuit_open"
	Reason       string `json:"reason,omitempty"`
}

// BreakdownItem represents an item in failure causes or request sources.
type BreakdownItem struct {
	Key        string  `json:"key"`
	Label      string  `json:"label"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// SummaryStats contains high-level KPIs like total requests, success rate, etc.
type SummaryStats struct {
	TotalRequests int64   `json:"total_requests"`
	SuccessRate   float64 `json:"success_rate"`
	Failures      int64   `json:"failures"`
	AvgDurationMs int64   `json:"avg_duration_ms"`
}

// DashboardStats is the comprehensive response for the TRAWL-style post-mortem view.
type DashboardStats struct {
	Range          string            `json:"range"`
	Summary        SummaryStats      `json:"summary"`
	Activity       []ActivitySlot    `json:"activity"`
	FailureCauses  []BreakdownItem   `json:"failure_causes"`
	RequestSources []BreakdownItem   `json:"request_sources"`
	History        []IncidentSummary `json:"history"`
	ActiveIncident *Incident         `json:"active_incident,omitempty"`
	Pulse          []PulseSlot       `json:"pulse"` // for backwards compatibility
}

// Tracker manages recent request traces and records incident history.
type Tracker struct {
	mu           sync.RWMutex
	ring         *TraceRingBuffer
	active       *Incident
	history      []*Incident
	cacheFile    string
	maxHistory   int
	lastErrTime  time.Time
	errCountSpan int
}

// NewTracker creates an incident tracker with the specified disk cache file.
func NewTracker(cacheFile string, bufferCapacity int) *Tracker {
	if bufferCapacity <= 0 {
		bufferCapacity = 1000
	}
	t := &Tracker{
		ring:       NewTraceRingBuffer(bufferCapacity),
		history:    make([]*Incident, 0, 50),
		cacheFile:  cacheFile,
		maxHistory: 50,
	}
	if cacheFile != "" {
		t.load()
	}
	return t
}

// RingBuffer returns the underlying trace buffer.
func (t *Tracker) RingBuffer() *TraceRingBuffer {
	return t.ring
}

// RecordTrace adds a request trace to the buffer.
func (t *Tracker) RecordTrace(trace RequestTrace) RequestTrace {
	saved := t.ring.Add(trace)

	// If there is an active incident and it is currently recovering, collect after-traces
	t.mu.Lock()
	if t.active == nil && len(t.history) > 0 {
		last := t.history[0]
		if last.RecoveredAt != nil && time.Since(*last.RecoveredAt) < 2*time.Minute && len(last.TracesAfter) < 10 {
			last.TracesAfter = append(last.TracesAfter, saved)
		}
	}
	t.mu.Unlock()

	return saved
}

// OnCircuitTrip records a circuit trip event and captures before-traces.
func (t *Tracker) OnCircuitTrip(reason string, cooldown time.Duration, trigger RequestTrace, triggerErr string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	id := fmt.Sprintf("inc_%d", now.Unix())

	// Grab up to 30 recent traces from ring buffer before trip
	before := t.ring.Recent(30)

	inc := &Incident{
		ID:           id,
		Type:         "circuit_breaker",
		Reason:       reason,
		State:        "ongoing",
		StartedAt:    now,
		CooldownSec:  cooldown.Seconds(),
		TriggerTrace: trigger,
		TriggerError: triggerErr,
		BlockedCount: 0,
		TracesBefore: before,
		TracesDuring: make([]RequestTrace, 0, 30),
		TracesAfter:  make([]RequestTrace, 0, 10),
	}

	t.active = inc
	t.history = append([]*Incident{inc}, t.history...)
	if len(t.history) > t.maxHistory {
		t.history = t.history[:t.maxHistory]
	}

	t.saveLocked()
}

// OnCircuitRecovered marks the active circuit incident as recovered.
func (t *Tracker) OnCircuitRecovered() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.active == nil {
		return
	}

	now := time.Now()
	t.active.RecoveredAt = &now
	t.active.DurationSec = now.Sub(t.active.StartedAt).Seconds()
	t.active.State = "recovered"

	// Collect immediate after-traces
	t.active.TracesAfter = t.ring.Recent(5)
	t.active = nil

	t.saveLocked()
}

// OnCircuitReset marks the active circuit incident as manually reset.
func (t *Tracker) OnCircuitReset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.active == nil {
		return
	}

	now := time.Now()
	t.active.RecoveredAt = &now
	t.active.DurationSec = now.Sub(t.active.StartedAt).Seconds()
	t.active.State = "reset"

	t.active.TracesAfter = t.ring.Recent(5)
	t.active = nil

	t.saveLocked()
}

// OnRequestBlocked registers a request that was blocked by an open circuit.
func (t *Tracker) OnRequestBlocked(trace RequestTrace) {
	trace.IsBlocked = true
	t.ring.Add(trace)

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.active != nil {
		t.active.BlockedCount++
		if len(t.active.TracesDuring) < 30 {
			t.active.TracesDuring = append(t.active.TracesDuring, trace)
		}
	}
}

// OnUpstreamError tracks serious upstream failures (5xx, 429, 403) and records an incident for post-mortem inspection.
func (t *Tracker) OnUpstreamError(status int, detail string, trigger RequestTrace) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	// Check if this error is already part of an ongoing circuit breaker
	if t.active != nil {
		return
	}

	if status < 400 || status == 404 {
		return
	}

	reason := fmt.Sprintf("upstream_%d", status)
	detailLower := strings.ToLower(detail)
	if status == 509 || strings.Contains(detailLower, "quota") || strings.Contains(detailLower, "509") || strings.Contains(detailLower, "exceed") {
		reason = "exceed"
	} else if strings.Contains(detailLower, "banned") || (status == 503 && strings.Contains(detailLower, "ban")) {
		reason = "banned"
	} else if status == 403 || strings.Contains(detailLower, "star membership") || strings.Contains(detailLower, "insufficient gp") || strings.Contains(detailLower, "cannot be archived") {
		reason = "archiver_unavailable"
	} else if status == 504 || strings.Contains(detailLower, "timeout") {
		reason = "timeout"
	}

	// De-duplicate rapid duplicate errors within 15 seconds of the same path & reason
	if len(t.history) > 0 {
		last := t.history[0]
		if last.Reason == reason && last.TriggerTrace.Path == trigger.Path && now.Sub(last.StartedAt) < 15*time.Second {
			return
		}
	}

	id := fmt.Sprintf("inc_%d", now.UnixNano())
	before := t.ring.Recent(20)
	recoveredAt := now
	duration := float64(trigger.DurationMs) / 1000.0

	inc := &Incident{
		ID:           id,
		Type:         "upstream_error",
		Reason:       reason,
		State:        "recovered",
		StartedAt:    now,
		RecoveredAt:  &recoveredAt,
		DurationSec:  duration,
		TriggerTrace: trigger,
		TriggerError: detail,
		TracesBefore: before,
		TracesDuring: make([]RequestTrace, 0),
		TracesAfter:  make([]RequestTrace, 0),
	}

	t.history = append([]*Incident{inc}, t.history...)
	if len(t.history) > t.maxHistory {
		t.history = t.history[:t.maxHistory]
	}
	t.saveLocked()
}

// ActiveIncident returns the currently ongoing incident, or nil if none.
func (t *Tracker) ActiveIncident() *Incident {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.active == nil {
		return nil
	}
	// Return a copy
	cp := *t.active
	return &cp
}

// ListIncidents returns a list of incident summaries.
func (t *Tracker) ListIncidents(limit int) []IncidentSummary {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit > len(t.history) {
		limit = len(t.history)
	}

	out := make([]IncidentSummary, 0, limit)
	for i := 0; i < limit; i++ {
		inc := t.history[i]
		sum := fmt.Sprintf("%s %s", inc.TriggerTrace.Method, inc.TriggerTrace.Path)
		if inc.TriggerError != "" {
			sum += fmt.Sprintf(" (%s)", inc.TriggerError)
		}
		out = append(out, IncidentSummary{
			ID:             inc.ID,
			Type:           inc.Type,
			Reason:         inc.Reason,
			State:          inc.State,
			StartedAt:      inc.StartedAt,
			RecoveredAt:    inc.RecoveredAt,
			DurationSec:    inc.DurationSec,
			CooldownSec:    inc.CooldownSec,
			BlockedCount:   inc.BlockedCount,
			TriggerSummary: sum,
			TriggerPath:    inc.TriggerTrace.Path,
			TriggerError:   inc.TriggerError,
		})
	}
	return out
}

// GetIncident returns full details for a given incident ID.
func (t *Tracker) GetIncident(id string) (*Incident, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, inc := range t.history {
		if inc.ID == id {
			cp := *inc
			return &cp, true
		}
	}
	return nil, false
}

// Pulse generates a status strip over the past N hours (default 24 hours, 30-minute slots -> 48 slots).
func (t *Tracker) Pulse(hours, slotMinutes int) []PulseSlot {
	if hours <= 0 {
		hours = 24
	}
	if slotMinutes <= 0 {
		slotMinutes = 30
	}
	totalSlots := (hours * 60) / slotMinutes
	if totalSlots <= 0 {
		totalSlots = 48
	}

	now := time.Now()
	slotDuration := time.Duration(slotMinutes) * time.Minute
	nowSlot := now.Truncate(slotDuration)

	slots := make([]PulseSlot, totalSlots)
	traces := t.ring.Recent(t.ring.Count())

	t.mu.RLock()
	activeInc := t.active
	history := make([]*Incident, len(t.history))
	copy(history, t.history)
	t.mu.RUnlock()

	for i := 0; i < totalSlots; i++ {
		offset := totalSlots - 1 - i
		slotStart := nowSlot.Add(-time.Duration(offset) * slotDuration)
		slotEnd := slotStart.Add(slotDuration)

		timeStr := slotStart.Format("15:04")
		slot := PulseSlot{
			Time:      timeStr,
			Minute:    timeStr,
			Timestamp: slotStart.Unix(),
			Status:    "idle",
		}

		// Check if any incident overlaps this slot
		var overlappingIncident *Incident
		if activeInc != nil && (activeInc.StartedAt.Before(slotEnd)) {
			overlappingIncident = activeInc
		} else {
			for _, inc := range history {
				if inc.StartedAt.Before(slotEnd) {
					if inc.RecoveredAt == nil || inc.RecoveredAt.After(slotStart) {
						overlappingIncident = inc
						break
					}
				}
			}
		}

		if overlappingIncident != nil {
			if overlappingIncident.Type == "circuit_breaker" {
				slot.Status = "circuit_open"
				slot.Reason = overlappingIncident.Reason
			} else {
				slot.Status = "error"
				slot.Reason = overlappingIncident.Reason
			}
		}

		// Count traces in slot
		for _, tr := range traces {
			if (tr.Timestamp.Equal(slotStart) || tr.Timestamp.After(slotStart)) && tr.Timestamp.Before(slotEnd) {
				slot.TotalCount++
				if tr.IsBlocked {
					slot.BlockedCount++
				}
				if tr.Status >= 400 && !tr.IsBlocked {
					slot.ErrorCount++
				}
			}
		}

		if slot.Status == "idle" {
			if slot.ErrorCount > 0 {
				slot.Status = "error"
			} else if slot.TotalCount > 0 {
				slot.Status = "ok"
			}
		}

		slots[i] = slot
	}

	return slots
}

// DashboardOverview generates TRAWL-style aggregated stats for 24h or 7d.
func (t *Tracker) DashboardOverview(timeRange string) DashboardStats {
	if timeRange != "7d" {
		timeRange = "24h"
	}

	now := time.Now()
	var totalSlots int
	var slotDuration time.Duration
	var spanDuration time.Duration
	var is7d bool
	var nowSlot time.Time

	if timeRange == "7d" {
		// Group 4 hours per slot (6 slots per day, 42 slots in 7 days).
		// Gives actionable temporal resolution across days rather than smashing 24 hours into 1 bar.
		slotHours := 4
		slotsPerDay := 24 / slotHours
		totalSlots = 7 * slotsPerDay // 42 slots
		slotDuration = time.Duration(slotHours) * time.Hour
		spanDuration = 7 * 24 * time.Hour
		is7d = true
		nowSlot = truncateToLocalHours(now, slotHours)
	} else {
		// 1 hour per slot, 24 slots across 24 hours.
		slotHours := 1
		totalSlots = 24
		slotDuration = time.Duration(slotHours) * time.Hour
		spanDuration = 24 * time.Hour
		is7d = false
		nowSlot = truncateToLocalHours(now, slotHours)
	}

	since := now.Add(-spanDuration)

	activity := make([]ActivitySlot, totalSlots)
	traces := t.ring.Recent(t.ring.Count())

	t.mu.RLock()
	activeInc := t.active
	history := make([]*Incident, len(t.history))
	copy(history, t.history)
	t.mu.RUnlock()

	var totalRequests int64
	var totalFailures int64
	var totalDurationMs int64
	var durationSamples int64

	sourcesMap := map[string]int{
		"image":   0,
		"opds":    0,
		"archive": 0,
	}

	failureCausesMap := map[string]int{
		"exceed":         0,
		"banned":         0,
		"upstream_error": 0,
		"timeout":        0,
		"shielded":       0,
	}

	for i := 0; i < totalSlots; i++ {
		offset := totalSlots - 1 - i
		slotStart := nowSlot.Add(-time.Duration(offset) * slotDuration)
		slotEnd := slotStart.Add(slotDuration)

		slot := ActivitySlot{
			Label:     formatActivityLabel(slotStart, now, is7d),
			Timestamp: slotStart.Unix(),
			Status:    "idle",
		}

		var overlappingIncident *Incident
		if activeInc != nil && activeInc.StartedAt.Before(slotEnd) {
			overlappingIncident = activeInc
		} else {
			for _, inc := range history {
				if inc.StartedAt.Before(slotEnd) {
					if inc.RecoveredAt == nil || inc.RecoveredAt.After(slotStart) {
						overlappingIncident = inc
						break
					}
				}
			}
		}

		if overlappingIncident != nil {
			if overlappingIncident.Type == "circuit_breaker" {
				slot.Status = "circuit_open"
				slot.Reason = overlappingIncident.Reason
			} else {
				slot.Status = "error"
				slot.Reason = overlappingIncident.Reason
			}
		}

		for _, tr := range traces {
			if (tr.Timestamp.Equal(slotStart) || tr.Timestamp.After(slotStart)) && tr.Timestamp.Before(slotEnd) {
				slot.TotalCount++
				totalRequests++
				if tr.DurationMs > 0 {
					totalDurationMs += tr.DurationMs
					durationSamples++
				}

				if tr.Kind == "image" || tr.Kind == "thumb" || tr.Kind == "stream" {
					sourcesMap["image"]++
				} else if tr.Kind == "opds" {
					sourcesMap["opds"]++
				} else if tr.Kind == "archive" || tr.Kind == "favorite" || tr.Kind == "rating" {
					sourcesMap["archive"]++
				}

				if tr.IsBlocked {
					slot.BlockedCount++
					failureCausesMap["shielded"]++
					totalFailures++
				} else if tr.Status >= 400 {
					slot.FailureCount++
					totalFailures++
					errLower := strings.ToLower(tr.Error)
					if tr.Status == 509 || strings.Contains(errLower, "quota") || strings.Contains(errLower, "509") || strings.Contains(errLower, "exceed") {
						failureCausesMap["exceed"]++
					} else if strings.Contains(errLower, "banned") || (tr.Status == 503 && strings.Contains(errLower, "ban")) {
						failureCausesMap["banned"]++
					} else if tr.Status == 504 || strings.Contains(errLower, "timeout") {
						failureCausesMap["timeout"]++
					} else {
						failureCausesMap["upstream_error"]++
					}
				} else {
					slot.SuccessCount++
				}
			}
		}

		if slot.Status == "idle" {
			if slot.FailureCount > 0 {
				slot.Status = "error"
			} else if slot.SuccessCount > 0 {
				slot.Status = "ok"
			}
		}

		activity[i] = slot
	}

	for _, inc := range history {
		if inc.StartedAt.After(since) {
			if inc.Reason == "exceed" && failureCausesMap["exceed"] == 0 {
				failureCausesMap["exceed"]++
			} else if inc.Reason == "banned" && failureCausesMap["banned"] == 0 {
				failureCausesMap["banned"]++
			} else if (strings.HasPrefix(inc.Reason, "upstream_") || inc.Reason == "archiver_unavailable") && failureCausesMap["upstream_error"] == 0 {
				failureCausesMap["upstream_error"]++
			}
		}
	}

	var successRate float64 = 100.0
	if totalRequests > 0 {
		sr := float64(totalRequests-totalFailures) / float64(totalRequests) * 100.0
		if sr < 0 {
			sr = 0
		}
		successRate = float64(int(sr*10)) / 10
	}

	var avgDuration int64 = 0
	if durationSamples > 0 {
		avgDuration = totalDurationMs / durationSamples
	}

	failureCauses := []BreakdownItem{
		{Key: "exceed", Label: "图片配额超限 (509)", Count: failureCausesMap["exceed"]},
		{Key: "banned", Label: "IP 临时封禁 (503)", Count: failureCausesMap["banned"]},
		{Key: "upstream_error", Label: "上游与业务异常 (5xx/403)", Count: failureCausesMap["upstream_error"]},
		{Key: "timeout", Label: "请求网络超时", Count: failureCausesMap["timeout"]},
		{Key: "shielded", Label: "熔断拦截保护", Count: failureCausesMap["shielded"]},
	}
	var totalFailCount int
	for _, item := range failureCauses {
		totalFailCount += item.Count
	}
	for i := range failureCauses {
		if totalFailCount > 0 {
			failureCauses[i].Percentage = float64(int(float64(failureCauses[i].Count)/float64(totalFailCount)*1000)) / 10
		}
	}

	requestSources := []BreakdownItem{
		{Key: "image", Label: "图片与阅读流", Count: sourcesMap["image"]},
		{Key: "opds", Label: "OPDS 目录与搜索", Count: sourcesMap["opds"]},
		{Key: "archive", Label: "画册归档与同步", Count: sourcesMap["archive"]},
	}
	var totalSourceCount int
	for _, item := range requestSources {
		totalSourceCount += item.Count
	}
	for i := range requestSources {
		if totalSourceCount > 0 {
			requestSources[i].Percentage = float64(int(float64(requestSources[i].Count)/float64(totalSourceCount)*1000)) / 10
		}
	}

	pulseSlots := t.Pulse(24, 30)

	return DashboardStats{
		Range: timeRange,
		Summary: SummaryStats{
			TotalRequests: totalRequests,
			SuccessRate:   successRate,
			Failures:      totalFailures,
			AvgDurationMs: avgDuration,
		},
		Activity:       activity,
		FailureCauses:  failureCauses,
		RequestSources: requestSources,
		History:        t.ListIncidents(30),
		ActiveIncident: t.ActiveIncident(),
		Pulse:          pulseSlots,
	}
}

func formatActivityLabel(slotTime time.Time, now time.Time, is7d bool) string {
	yNow, mNow, dNow := now.Date()
	ySlot, mSlot, dSlot := slotTime.Date()

	if is7d {
		startHour := slotTime.Hour()
		endHour := startHour + 4
		if ySlot == yNow && mSlot == mNow && dSlot == dNow {
			return fmt.Sprintf("今天 %02d-%02d时", startHour, endHour)
		}
		yesterday := now.AddDate(0, 0, -1)
		yYest, mYest, dYest := yesterday.Date()
		if ySlot == yYest && mSlot == mYest && dSlot == dYest {
			return fmt.Sprintf("昨天 %02d-%02d时", startHour, endHour)
		}
		return fmt.Sprintf("%02d日 %02d-%02d时", slotTime.Day(), startHour, endHour)
	}

	if ySlot == yNow && mSlot == mNow && dSlot == dNow {
		return fmt.Sprintf("今天 %02d时", slotTime.Hour())
	}
	yesterday := now.AddDate(0, 0, -1)
	yYest, mYest, dYest := yesterday.Date()
	if ySlot == yYest && mSlot == mYest && dSlot == dYest {
		return fmt.Sprintf("昨天 %02d时", slotTime.Hour())
	}
	return fmt.Sprintf("%02d日 %02d时", slotTime.Day(), slotTime.Hour())
}

func truncateToLocalHours(t time.Time, hours int) time.Time {
	y, m, d := t.Date()
	if hours <= 1 {
		return time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
	}
	hour := (t.Hour() / hours) * hours
	return time.Date(y, m, d, hour, 0, 0, 0, t.Location())
}

func (t *Tracker) saveLocked() {
	if t.cacheFile == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(t.cacheFile), 0755)

	data, err := json.MarshalIndent(t.history, "", "  ")
	if err != nil {
		return
	}

	tmp := t.cacheFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err == nil {
		_ = os.Rename(tmp, t.cacheFile)
	}
}

func (t *Tracker) load() {
	if t.cacheFile == "" {
		return
	}
	data, err := os.ReadFile(t.cacheFile)
	if err != nil {
		return
	}
	var loaded []*Incident
	if err := json.Unmarshal(data, &loaded); err == nil {
		t.history = loaded
		// If the most recent one was ongoing when persisted, mark it as recovered
		if len(t.history) > 0 && t.history[0].State == "ongoing" {
			t.history[0].State = "recovered"
			now := time.Now()
			t.history[0].RecoveredAt = &now
		}
	}
}

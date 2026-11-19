package recorder

import (
	"sync"
	"time"
)

// RequestTrace captures lightweight metadata for a single request.
type RequestTrace struct {
	ID         int64     `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Kind       string    `json:"kind,omitempty"` // "html", "api", "image", "thumb", "client"
	Status     int       `json:"status"`
	DurationMs int64     `json:"duration_ms"`
	ClientIP   string    `json:"client_ip,omitempty"`
	UserAgent  string    `json:"user_agent,omitempty"`
	GalleryID  string    `json:"gallery_id,omitempty"`
	IsBlocked  bool      `json:"is_blocked,omitempty"` // Blocked by circuit breaker
	Error      string    `json:"error,omitempty"`
}

// TraceRingBuffer is a fixed-size, thread-safe ring buffer for request traces.
type TraceRingBuffer struct {
	mu       sync.RWMutex
	capacity int
	entries  []RequestTrace
	head     int
	count    int
	seq      int64
}

// NewTraceRingBuffer creates a new ring buffer with the given capacity.
func NewTraceRingBuffer(capacity int) *TraceRingBuffer {
	if capacity <= 0 {
		capacity = 1000
	}
	return &TraceRingBuffer{
		capacity: capacity,
		entries:  make([]RequestTrace, capacity),
	}
}

// Add appends a new trace into the ring buffer.
func (b *TraceRingBuffer) Add(t RequestTrace) RequestTrace {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	t.ID = b.seq
	if t.Timestamp.IsZero() {
		t.Timestamp = time.Now()
	}
	b.entries[b.head] = t
	b.head = (b.head + 1) % b.capacity
	if b.count < b.capacity {
		b.count++
	}
	return t
}

// Recent returns up to n most recent traces in chronological order (oldest first).
func (b *TraceRingBuffer) Recent(n int) []RequestTrace {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n <= 0 || n > b.count {
		n = b.count
	}
	if n == 0 {
		return []RequestTrace{}
	}
	out := make([]RequestTrace, n)
	start := (b.head - n + b.capacity) % b.capacity
	for i := 0; i < n; i++ {
		idx := (start + i) % b.capacity
		out[i] = b.entries[idx]
	}
	return out
}

// Count returns the number of traces currently held in buffer.
func (b *TraceRingBuffer) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}

package eh

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"pandaopds/internal/config"
)

type RequestKind string

const (
	KindHTML  RequestKind = "html"
	KindAPI   RequestKind = "api"
	KindImage RequestKind = "image"
	KindThumb RequestKind = "thumb"
)

type CircuitSnapshot struct {
	State     string  `json:"state"`
	Reason    string  `json:"reason,omitempty"`
	Remaining float64 `json:"remaining"`
	Cooldown  float64 `json:"cooldown"`
}
type CircuitOpenError struct {
	Reason     string
	RetryAfter float64
}

func (e *CircuitOpenError) Error() string { return "circuit open: " + e.Reason }

type CircuitBreaker struct {
	mu              sync.Mutex
	opened          time.Time
	reason          string
	cooldown        time.Duration
	defaultCooldown time.Duration
	onTrip          func(reason string, cooldown time.Duration, detail, method, target string)
	onReset         func()
	onRecovered     func()
}

type CircuitCallbacks struct {
	OnTrip      func(reason string, cooldown time.Duration, detail, method, target string)
	OnReset     func()
	OnRecovered func()
}

func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{defaultCooldown: 10 * time.Minute, cooldown: 10 * time.Minute}
}
func (c *CircuitBreaker) SetCallbacks(cb CircuitCallbacks) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onTrip = cb.OnTrip
	c.onReset = cb.OnReset
	c.onRecovered = cb.OnRecovered
}
func (c *CircuitBreaker) Trip(reason string, cooldown time.Duration) {
	c.TripWithDetail(reason, cooldown, reason, "", "")
}
func (c *CircuitBreaker) TripWithDetail(reason string, cooldown time.Duration, detail, method, target string) {
	c.mu.Lock()
	c.reason = reason
	c.opened = time.Now()
	if cooldown > 0 {
		c.cooldown = cooldown
	} else {
		c.cooldown = c.defaultCooldown
	}
	cb := c.onTrip
	cd := c.cooldown
	c.mu.Unlock()
	if cb != nil {
		cb(reason, cd, detail, method, target)
	}
}
func (c *CircuitBreaker) Reset() {
	c.mu.Lock()
	c.reason = ""
	c.opened = time.Time{}
	c.cooldown = c.defaultCooldown
	cb := c.onReset
	c.mu.Unlock()
	if cb != nil {
		cb()
	}
}
func (c *CircuitBreaker) Check() error {
	c.mu.Lock()
	if c.reason == "" {
		c.mu.Unlock()
		return nil
	}
	left := c.cooldown - time.Since(c.opened)
	if left <= 0 {
		c.reason = ""
		c.opened = time.Time{}
		c.cooldown = c.defaultCooldown
		cb := c.onRecovered
		c.mu.Unlock()
		if cb != nil {
			cb()
		}
		return nil
	}
	reason := c.reason
	c.mu.Unlock()
	return &CircuitOpenError{Reason: reason, RetryAfter: left.Seconds()}
}
func (c *CircuitBreaker) Snapshot() CircuitSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reason == "" {
		return CircuitSnapshot{State: "closed", Cooldown: c.cooldown.Seconds()}
	}
	left := c.cooldown - time.Since(c.opened)
	if left < 0 {
		left = 0
	}
	return CircuitSnapshot{State: "open", Reason: c.reason, Remaining: float64(int(left.Seconds()*10)) / 10, Cooldown: c.cooldown.Seconds()}
}

type Throttle struct {
	cfg      config.Settings
	Circuit  *CircuitBreaker
	pools    map[RequestKind]chan struct{}
	mu       sync.Mutex
	htmlLock *contextMutex
	lastHTML time.Time
	counts   map[RequestKind]int64
}

func NewThrottle(cfg config.Settings) *Throttle {
	return &Throttle{cfg: cfg, Circuit: NewCircuitBreaker(), pools: map[RequestKind]chan struct{}{KindHTML: make(chan struct{}, positive(cfg.MaxConcurrency, 5)), KindAPI: make(chan struct{}, positive(cfg.MaxConcurrency, 5)), KindImage: make(chan struct{}, positive(cfg.ImageMaxConcurrency, 5)), KindThumb: make(chan struct{}, positive(cfg.ThumbMaxConcurrency, 25))}, htmlLock: newContextMutex(), counts: map[RequestKind]int64{}}
}
func positive(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}
func (t *Throttle) Acquire(ctx context.Context, kind RequestKind) error {
	if err := t.Circuit.Check(); err != nil {
		return err
	}
	pool := t.pools[kind]
	if pool == nil {
		pool = t.pools[KindHTML]
	}
	select {
	case pool <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	failed := true
	defer func() {
		if failed {
			<-pool
		}
	}()
	if kind == KindHTML && t.cfg.HTMLIntervalSeconds > 0 {
		// Admission happens first, so HTML spacing is enforced at dispatch while
		// still respecting the configured concurrency cap. The serialized wait
		// prevents a burst when several callers reach the interval together.
		if err := t.htmlLock.Lock(ctx); err != nil {
			return err
		}
		wait := time.Until(t.lastHTML.Add(time.Duration(t.cfg.HTMLIntervalSeconds * float64(time.Second))))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				t.htmlLock.Unlock()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if err := ctx.Err(); err != nil {
			t.htmlLock.Unlock()
			return err
		}
		if err := t.Circuit.Check(); err != nil {
			t.htmlLock.Unlock()
			return err
		}
		t.lastHTML = time.Now()
		t.htmlLock.Unlock()
	} else if err := ctx.Err(); err != nil {
		return err
	} else if err := t.Circuit.Check(); err != nil {
		return err
	}
	t.mu.Lock()
	t.counts[kind]++
	t.mu.Unlock()
	failed = false
	return nil
}
func (t *Throttle) Release(kind RequestKind) {
	pool := t.pools[kind]
	if pool == nil {
		pool = t.pools[KindHTML]
	}
	select {
	case <-pool:
	default:
	}
}
func (t *Throttle) Counts() map[string]int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string]int64{}
	for k, v := range t.counts {
		out[string(k)] = v
	}
	return out
}
func (t *Throttle) TripFromBodyWithDetail(body string, kind RequestKind, host, method, target string) {
	x := strings.TrimSpace(body)
	if strings.HasPrefix(x, "Your IP address") || strings.HasPrefix(x, "This IP address") ||
		((strings.Contains(x, "Your IP address") || strings.Contains(x, "This IP address")) && (strings.Contains(x, "banned") || strings.Contains(x, "temporarily banned"))) {
		t.Circuit.TripWithDetail("banned", time.Duration(t.cfg.BannedCooldownSeconds*float64(time.Second)), "E-Hentai IP address is banned", method, target)
		log.Printf("upstream circuit opened: reason=banned kind=%s host=%s", kind, host)
		return
	}
	if strings.HasPrefix(x, "You have exceeded your image") || strings.Contains(x, "exceeded your image") {
		t.Circuit.TripWithDetail("exceed", time.Duration(t.cfg.ExceedCooldownSeconds*float64(time.Second)), "image limit exceeded", method, target)
		log.Printf("upstream circuit opened: reason=exceed kind=%s host=%s", kind, host)
	}
}
func (t *Throttle) TripFromBody(body string, kind RequestKind, host string) {
	t.TripFromBodyWithDetail(body, kind, host, "", "")
}
func (t *Throttle) WaitError() error {
	if err := t.Circuit.Check(); err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}

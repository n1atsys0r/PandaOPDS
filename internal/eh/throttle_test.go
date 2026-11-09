package eh

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"pandaopds/internal/config"
)

func TestConcurrentHTMLThrottleMaintainsSpacing(t *testing.T) {
	const interval = 40 * time.Millisecond
	throttle := NewThrottle(config.Settings{MaxConcurrency: 5, HTMLIntervalSeconds: interval.Seconds()})
	const callers = 6
	var mu sync.Mutex
	times := make([]time.Time, 0, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := throttle.Acquire(context.Background(), KindHTML); err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			mu.Lock()
			times = append(times, time.Now())
			mu.Unlock()
			throttle.Release(KindHTML)
		}()
	}
	close(start)
	wg.Wait()
	if len(times) != callers {
		t.Fatalf("admitted %d callers, want %d", len(times), callers)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	for i := 1; i < len(times); i++ {
		gap := times[i].Sub(times[i-1])
		if gap < interval-10*time.Millisecond {
			t.Fatalf("HTML dispatch gap %s at index %d, want at least %s", gap, i, interval-10*time.Millisecond)
		}
	}
}

func TestCanceledHTMLSpacingWaitReleasesConcurrencySlot(t *testing.T) {
	throttle := NewThrottle(config.Settings{MaxConcurrency: 2, HTMLIntervalSeconds: 300})
	if err := throttle.Acquire(context.Background(), KindHTML); err != nil {
		t.Fatal(err)
	}
	defer throttle.Release(KindHTML)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := throttle.Acquire(ctx, KindHTML); err == nil {
		t.Fatal("Acquire succeeded after context cancellation")
	}
	if len(throttle.pools[KindHTML]) != 1 {
		t.Fatalf("canceled wait left %d concurrency slots occupied, want 1", len(throttle.pools[KindHTML]))
	}
}

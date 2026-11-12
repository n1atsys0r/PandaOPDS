package archive

import (
	"archive/zip"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultZipPoolSize    = 8
	defaultZipPoolIdleTTL = 3 * time.Minute
)

type zipPoolEntry struct {
	path     string
	reader   *zip.ReadCloser
	lastUsed time.Time
	refs     int
	retired  bool
}

type zipOpenCall struct {
	done   chan struct{}
	reader *zip.ReadCloser
	err    error
}

// zipPool keeps a small bounded set of recently read archive ZIP readers open
// so sequential page reads avoid re-opening and re-parsing the Central
// Directory on every page, which is especially expensive over FUSE/cloud mounts.
type zipPool struct {
	mu         sync.Mutex
	maxEntries int
	idleTTL    time.Duration
	now        func() time.Time
	entries    map[string]*zipPoolEntry
	opening    map[string]*zipOpenCall
	reapTimer  *time.Timer
	closed     bool
}

func newZipPool(maxEntries int, idleTTL time.Duration) *zipPool {
	if maxEntries <= 0 {
		maxEntries = defaultZipPoolSize
	}
	if idleTTL <= 0 {
		idleTTL = defaultZipPoolIdleTTL
	}
	return &zipPool{
		maxEntries: maxEntries,
		idleTTL:    idleTTL,
		now:        time.Now,
		entries:    map[string]*zipPoolEntry{},
		opening:    map[string]*zipOpenCall{},
	}
}

func (p *zipPool) acquire(path string) (*zip.ReadCloser, func(), bool, error) {
	clean := filepath.Clean(path)
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			r, err := zip.OpenReader(clean)
			if err != nil {
				return nil, nil, false, err
			}
			return r, func() { _ = r.Close() }, false, nil
		}

		now := p.now()
		expired := p.pruneExpiredLocked(now)

		if e, ok := p.entries[clean]; ok && !e.retired {
			e.refs++
			e.lastUsed = now
			p.mu.Unlock()
			closeReaders(expired)
			return e.reader, p.makeRelease(e), true, nil
		}

		if call, ok := p.opening[clean]; ok {
			p.mu.Unlock()
			closeReaders(expired)
			<-call.done
			if call.err != nil {
				return nil, nil, false, call.err
			}
			// Re-acquire so ref counting and invalidation rules stay unified.
			continue
		}

		call := &zipOpenCall{done: make(chan struct{})}
		p.opening[clean] = call
		p.mu.Unlock()
		closeReaders(expired)

		r, err := zip.OpenReader(clean)

		p.mu.Lock()
		delete(p.opening, clean)
		call.reader = r
		call.err = err
		close(call.done)

		if err != nil {
			p.mu.Unlock()
			return nil, nil, false, err
		}

		if p.closed {
			p.mu.Unlock()
			return r, func() { _ = r.Close() }, false, nil
		}

		if existing, ok := p.entries[clean]; ok && !existing.retired {
			existing.refs++
			existing.lastUsed = p.now()
			p.mu.Unlock()
			_ = r.Close()
			return existing.reader, p.makeRelease(existing), false, nil
		}

		evicted := p.evictForInsertLocked()
		e := &zipPoolEntry{
			path:     clean,
			reader:   r,
			lastUsed: p.now(),
			refs:     1,
		}
		p.entries[clean] = e
		p.scheduleReapLocked()
		p.mu.Unlock()
		if evicted != nil {
			_ = evicted.Close()
		}
		return r, p.makeRelease(e), false, nil
	}
}

func (p *zipPool) makeRelease(e *zipPoolEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			var toClose *zip.ReadCloser
			p.mu.Lock()
			e.refs--
			e.lastUsed = p.now()
			if (e.retired || p.closed) && e.refs <= 0 {
				toClose = e.reader
				e.reader = nil
			} else if !p.closed && len(p.entries) > 0 {
				p.scheduleReapLocked()
			}
			p.mu.Unlock()
			if toClose != nil {
				_ = toClose.Close()
			}
		})
	}
}

func (p *zipPool) invalidate(path string) {
	if path == "" {
		return
	}
	clean := filepath.Clean(path)
	var toClose *zip.ReadCloser
	p.mu.Lock()
	if e, ok := p.entries[clean]; ok {
		delete(p.entries, clean)
		e.retired = true
		if e.refs <= 0 {
			toClose = e.reader
			e.reader = nil
		}
	}
	p.mu.Unlock()
	if toClose != nil {
		_ = toClose.Close()
	}
}

func (p *zipPool) close() {
	var toClose []*zip.ReadCloser
	p.mu.Lock()
	p.closed = true
	if p.reapTimer != nil {
		p.reapTimer.Stop()
		p.reapTimer = nil
	}
	for k, e := range p.entries {
		delete(p.entries, k)
		e.retired = true
		if e.refs <= 0 && e.reader != nil {
			toClose = append(toClose, e.reader)
			e.reader = nil
		}
	}
	p.mu.Unlock()
	for _, r := range toClose {
		_ = r.Close()
	}
}

func (p *zipPool) reapIdle() {
	var toClose []*zip.ReadCloser
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	toClose = p.pruneExpiredLocked(p.now())
	if len(p.entries) > 0 {
		p.scheduleReapLocked()
	} else {
		p.reapTimer = nil
	}
	p.mu.Unlock()
	for _, r := range toClose {
		_ = r.Close()
	}
}

func (p *zipPool) scheduleReapLocked() {
	if p.closed || p.reapTimer != nil {
		return
	}
	p.reapTimer = time.AfterFunc(p.idleTTL, p.reapIdle)
}

func (p *zipPool) pruneExpiredLocked(now time.Time) []*zip.ReadCloser {
	var toClose []*zip.ReadCloser
	for k, e := range p.entries {
		if e.refs <= 0 && now.Sub(e.lastUsed) >= p.idleTTL {
			delete(p.entries, k)
			e.retired = true
			if e.reader != nil {
				toClose = append(toClose, e.reader)
				e.reader = nil
			}
		}
	}
	return toClose
}

func (p *zipPool) evictForInsertLocked() *zip.ReadCloser {
	if len(p.entries) < p.maxEntries {
		return nil
	}
	var lru *zipPoolEntry
	for _, e := range p.entries {
		if e.refs > 0 {
			continue
		}
		if lru == nil || e.lastUsed.Before(lru.lastUsed) {
			lru = e
		}
	}
	if lru == nil {
		// All cached entries are actively in use; retire the oldest one so it
		// closes as soon as its in-flight read finishes.
		for _, e := range p.entries {
			if lru == nil || e.lastUsed.Before(lru.lastUsed) {
				lru = e
			}
		}
	}
	if lru != nil {
		delete(p.entries, lru.path)
		lru.retired = true
		if lru.refs <= 0 && lru.reader != nil {
			r := lru.reader
			lru.reader = nil
			return r
		}
	}
	return nil
}

func closeReaders(readers []*zip.ReadCloser) {
	for _, r := range readers {
		if r != nil {
			_ = r.Close()
		}
	}
}

package eh

import (
	"container/heap"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type cached[T any] struct {
	value   T
	expires time.Time
}

func lookup[T any](m map[string]cached[T], key string) (T, bool) {
	v, ok := m[key]
	if !ok || time.Now().After(v.expires) {
		var zero T
		delete(m, key)
		return zero, false
	}
	return v.value, true
}
func put[T any](m map[string]cached[T], key string, value T, ttl time.Duration) {
	m[key] = cached[T]{value: value, expires: time.Now().Add(ttl)}
}

type DiskCache struct {
	dir      string
	maxBytes int64
	enabled  bool
	mu       sync.Mutex
	loaded   bool
	index    diskCacheIndex
}

type diskCacheEntry struct {
	path     string
	size     int64
	modTime  time.Time
	accessed time.Time
}

type diskCacheHeap struct {
	items     []diskCacheEntry
	positions map[string]int
	byAccess  bool
}

type diskCacheIndex struct {
	entries map[string]diskCacheEntry
	lru     diskCacheHeap
	expiry  diskCacheHeap
	total   int64
}

func newDiskCacheIndex() diskCacheIndex {
	return diskCacheIndex{
		entries: map[string]diskCacheEntry{},
		lru:     diskCacheHeap{positions: map[string]int{}, byAccess: true},
		expiry:  diskCacheHeap{positions: map[string]int{}},
	}
}

func (h diskCacheHeap) Len() int { return len(h.items) }
func (h diskCacheHeap) Less(i, j int) bool {
	first, second := h.items[i].modTime, h.items[j].modTime
	if h.byAccess {
		first, second = h.items[i].accessed, h.items[j].accessed
	}
	if first.Equal(second) {
		return h.items[i].path < h.items[j].path
	}
	return first.Before(second)
}
func (h diskCacheHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
	h.positions[h.items[i].path] = i
	h.positions[h.items[j].path] = j
}
func (h *diskCacheHeap) Push(item any) {
	entry := item.(diskCacheEntry)
	h.positions[entry.path] = len(h.items)
	h.items = append(h.items, entry)
}
func (h *diskCacheHeap) Pop() any {
	last := len(h.items) - 1
	entry := h.items[last]
	h.items[last] = diskCacheEntry{}
	h.items = h.items[:last]
	delete(h.positions, entry.path)
	return entry
}
func (h *diskCacheHeap) set(entry diskCacheEntry) {
	if i, ok := h.positions[entry.path]; ok {
		h.items[i] = entry
		heap.Fix(h, i)
		return
	}
	heap.Push(h, entry)
}
func (h *diskCacheHeap) remove(path string) bool {
	i, ok := h.positions[path]
	if !ok {
		return false
	}
	heap.Remove(h, i)
	return true
}
func (i *diskCacheIndex) set(entry diskCacheEntry) {
	if previous, ok := i.entries[entry.path]; ok {
		i.total -= previous.size
	}
	i.entries[entry.path] = entry
	i.total += entry.size
	i.lru.set(entry)
	i.expiry.set(entry)
}
func (i *diskCacheIndex) remove(path string) (diskCacheEntry, bool) {
	entry, ok := i.entries[path]
	if !ok {
		return diskCacheEntry{}, false
	}
	delete(i.entries, path)
	i.total -= entry.size
	i.lru.remove(path)
	i.expiry.remove(path)
	return entry, true
}

const imageCacheTTL = 7 * 24 * time.Hour
const ArchiveStagingDirName = "archive-staging"

func isArchiveStagingPath(cacheDir, path string) bool {
	return filepath.Clean(path) == filepath.Join(filepath.Clean(cacheDir), ArchiveStagingDirName)
}

func NewDiskCache(dir string, maxGB float64, enabled bool) *DiskCache {
	max := int64(maxGB * 1024 * 1024 * 1024)
	d := &DiskCache{dir: dir, maxBytes: max, enabled: enabled, index: newDiskCacheIndex()}
	if enabled {
		d.mu.Lock()
		d.loadIndexLocked()
		d.mu.Unlock()
	}
	return d
}
func (d *DiskCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(d.dir, h[:2], h[2:])
}
func (d *DiskCache) Get(key string) ([]byte, bool) {
	if !d.enabled {
		return nil, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadIndexLocked()
	p := d.path(key)
	st, err := os.Stat(p)
	if os.IsNotExist(err) {
		d.index.remove(p)
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	entry := diskCacheEntry{path: p, size: st.Size(), modTime: st.ModTime(), accessed: st.ModTime()}
	if cached, ok := d.index.entries[p]; ok && cached.modTime.Equal(entry.modTime) && cached.size == entry.size {
		entry.accessed = cached.accessed
	}
	if time.Since(entry.modTime) > imageCacheTTL {
		err := os.Remove(p)
		if err == nil || os.IsNotExist(err) {
			d.index.remove(p)
		} else {
			d.index.set(entry)
		}
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			d.index.remove(p)
		}
		return nil, false
	}
	now := time.Now()
	_ = os.Chtimes(p, now, st.ModTime())
	entry.accessed = now
	d.index.set(entry)
	return b, true
}
func (d *DiskCache) Put(key string, b []byte) {
	if !d.enabled {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadIndexLocked()
	p := d.path(key)
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	if os.Rename(tmp, p) != nil {
		_ = os.Remove(tmp)
		return
	}
	modTime := time.Now()
	if st, err := os.Stat(p); err == nil {
		modTime = st.ModTime()
	}
	d.index.set(diskCacheEntry{path: p, size: int64(len(b)), modTime: modTime, accessed: modTime})
	d.pruneLocked(time.Now())
}
func (d *DiskCache) Delete(key string) {
	if !d.enabled {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadIndexLocked()
	p := d.path(key)
	err := os.Remove(p)
	if err == nil || os.IsNotExist(err) {
		d.index.remove(p)
	}
}
func (d *DiskCache) Clear() error {
	if !d.enabled {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadIndexLocked()
	entries, err := os.ReadDir(d.dir)
	if os.IsNotExist(err) {
		d.index = newDiskCacheIndex()
		d.loaded = false
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if isArchiveStagingPath(d.dir, filepath.Join(d.dir, entry.Name())) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(d.dir, entry.Name())); err != nil {
			d.index = newDiskCacheIndex()
			d.loaded = false
			d.loadIndexLocked()
			return err
		}
	}
	d.index = newDiskCacheIndex()
	d.loaded = true
	return nil
}
func (d *DiskCache) Stats() map[string]any {
	if !d.enabled {
		var bytes int64
		count := 0
		_ = filepath.WalkDir(d.dir, func(path string, e fs.DirEntry, err error) error {
			if err == nil && e.IsDir() && isArchiveStagingPath(d.dir, path) {
				return filepath.SkipDir
			}
			if err == nil && !e.IsDir() {
				if st, x := e.Info(); x == nil {
					count++
					bytes += st.Size()
				}
			}
			return nil
		})
		return map[string]any{"entries": count, "bytes": bytes, "enabled": false, "max_bytes": d.maxBytes}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadIndexLocked()
	return map[string]any{"entries": len(d.index.entries), "bytes": d.index.total, "enabled": true, "max_bytes": d.maxBytes}
}

func (d *DiskCache) loadIndexLocked() {
	if d.loaded {
		return
	}
	if d.dir == "" {
		d.loaded = true
		return
	}
	if err := os.MkdirAll(d.dir, 0o755); err != nil {
		return
	}
	d.loaded = true
	d.index = newDiskCacheIndex()
	_ = filepath.WalkDir(d.dir, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && entry.IsDir() && isArchiveStagingPath(d.dir, p) {
			return filepath.SkipDir
		}
		if walkErr != nil || entry.IsDir() || strings.HasSuffix(entry.Name(), ".tmp") {
			return nil
		}
		st, err := entry.Info()
		if err != nil {
			return nil
		}
		d.index.set(diskCacheEntry{path: p, size: st.Size(), modTime: st.ModTime(), accessed: st.ModTime()})
		return nil
	})
	d.pruneLocked(time.Now())
}

func (d *DiskCache) pruneLocked(now time.Time) {
	if d.maxBytes <= 0 {
		return
	}
	for len(d.index.expiry.items) > 0 {
		oldest := d.index.expiry.items[0]
		if now.Sub(oldest.modTime) <= imageCacheTTL {
			break
		}
		if err := os.Remove(oldest.path); err != nil && !os.IsNotExist(err) {
			return
		}
		d.index.remove(oldest.path)
	}
	for d.index.total > d.maxBytes && len(d.index.lru.items) > 0 {
		oldest := d.index.lru.items[0]
		if err := os.Remove(oldest.path); err != nil && !os.IsNotExist(err) {
			return
		}
		d.index.remove(oldest.path)
	}
}

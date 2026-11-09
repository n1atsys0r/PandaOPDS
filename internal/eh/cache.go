package eh

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
}

const imageCacheTTL = 7 * 24 * time.Hour

func NewDiskCache(dir string, maxGB float64, enabled bool) *DiskCache {
	max := int64(maxGB * 1024 * 1024 * 1024)
	return &DiskCache{dir: dir, maxBytes: max, enabled: enabled}
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
	p := d.path(key)
	b, e := os.ReadFile(p)
	if e != nil {
		return nil, false
	}
	if st, err := os.Stat(p); err == nil {
		if time.Since(st.ModTime()) > imageCacheTTL {
			_ = os.Remove(p)
			return nil, false
		}
		_ = os.Chtimes(p, time.Now(), st.ModTime())
	}
	return b, true
}
func (d *DiskCache) Put(key string, b []byte) {
	if !d.enabled {
		return
	}
	d.mu.Lock()
	p := d.path(key)
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		d.mu.Unlock()
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		d.mu.Unlock()
		return
	}
	if os.Rename(tmp, p) != nil {
		_ = os.Remove(tmp)
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	d.prune()
}
func (d *DiskCache) Clear() error {
	if !d.enabled {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	entries, err := os.ReadDir(d.dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(d.dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
func (d *DiskCache) Stats() map[string]any {
	var bytes int64
	count := 0
	_ = filepath.WalkDir(d.dir, func(_ string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if st, x := e.Info(); x == nil {
				count++
				bytes += st.Size()
			}
		}
		return nil
	})
	return map[string]any{"entries": count, "bytes": bytes, "enabled": d.enabled, "max_bytes": d.maxBytes}
}
func (d *DiskCache) prune() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.maxBytes <= 0 {
		return
	}
	type file struct {
		p    string
		size int64
		at   time.Time
	}
	files := []file{}
	var total int64
	_ = filepath.WalkDir(d.dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && !strings.HasSuffix(e.Name(), ".tmp") {
			if st, x := e.Info(); x == nil {
				if time.Since(st.ModTime()) > imageCacheTTL {
					_ = os.Remove(p)
					return nil
				}
				total += st.Size()
				files = append(files, file{p, st.Size(), st.ModTime()})
			}
		}
		return nil
	})
	if total <= d.maxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for _, f := range files {
		if total <= d.maxBytes {
			break
		}
		_ = os.Remove(f.p)
		total -= f.size
	}
}

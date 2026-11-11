package eh

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDiskCacheUsesPythonShardAndClearKeepsRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	cache := NewDiskCache(root, 1, true)
	key := "2753175-abcdef-1"
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	want := filepath.Join(root, hash[:2], hash[2:])
	if got := cache.path(key); got != want {
		t.Fatalf("sharded path=%q, want %q", got, want)
	}
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("cached bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := cache.Get(key); !ok || string(got) != "cached bytes" {
		t.Fatalf("cross-written cache read = %q, %v", got, ok)
	}
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Fatalf("cache mount root removed: stat=%v err=%v", st, err)
	}
}

func TestDiskCacheIgnoresArchiveStaging(t *testing.T) {
	dir := t.TempDir()
	stage := filepath.Join(dir, "archive-staging", "1", "aa", "archive.part")
	if err := os.MkdirAll(filepath.Dir(stage), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stage, []byte("partial archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := NewDiskCache(dir, 1, true)
	if got := cache.Stats(); got["entries"] != 0 || got["bytes"] != int64(0) {
		t.Fatalf("staging counted as image cache: %v", got)
	}
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(stage); err != nil || string(b) != "partial archive" {
		t.Fatalf("image cache clear removed staging data: %q, %v", b, err)
	}
	disabled := NewDiskCache(dir, 1, false)
	if got := disabled.Stats(); got["entries"] != 0 || got["bytes"] != int64(0) {
		t.Fatalf("disabled image cache stats counted staging: %v", got)
	}
}

func TestDiskCacheExpiresSevenDayEntries(t *testing.T) {
	cache := NewDiskCache(t.TempDir(), 1, true)
	key := "1:token:1"
	cache.Put(key, []byte("old"))
	path := cache.path(key)
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get(key); ok {
		t.Fatal("expired item was served")
	}
}

func TestDiskCacheIndexEvictsLRUAndTracksOverwriteDeleteClear(t *testing.T) {
	cache := newByteLimitedDiskCache(t.TempDir(), 8)
	cache.Put("a", []byte("aaaa"))
	time.Sleep(10 * time.Millisecond)
	cache.Put("b", []byte("bbbb"))
	aModTime, err := os.Stat(cache.path("a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("expected cache hit for recently used entry")
	}
	after, err := os.Stat(cache.path("a"))
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(aModTime.ModTime()) {
		t.Fatalf("cache hit changed mtime used for TTL: before=%v after=%v", aModTime.ModTime(), after.ModTime())
	}
	cache.Put("c", []byte("cccc"))
	if _, ok := cache.Get("b"); ok {
		t.Fatal("least recently used entry was not evicted")
	}
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("recently used entry was evicted")
	}
	if got := cache.Stats(); got["entries"] != 2 || got["bytes"] != int64(8) {
		t.Fatalf("capacity stats after eviction=%v, want 2 entries / 8 bytes", got)
	}

	cache.Put("c", []byte("xy"))
	if got := cache.Stats(); got["entries"] != 2 || got["bytes"] != int64(6) {
		t.Fatalf("stats after overwrite=%v, want 2 entries / 6 bytes", got)
	}
	cache.Delete("a")
	if got := cache.Stats(); got["entries"] != 1 || got["bytes"] != int64(2) {
		t.Fatalf("stats after delete=%v, want 1 entry / 2 bytes", got)
	}
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	if got := cache.Stats(); got["entries"] != 0 || got["bytes"] != int64(0) {
		t.Fatalf("stats after clear=%v, want empty cache", got)
	}
	cache.Put("before-external-clear", []byte("x"))
	if err := os.RemoveAll(cache.dir); err != nil {
		t.Fatal(err)
	}
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	if got := cache.Stats(); got["entries"] != 0 || got["bytes"] != int64(0) {
		t.Fatalf("stats after external root removal=%v, want empty cache", got)
	}
}

func TestDiskCacheRestartIndexesExistingFilesExpiresAndEnforcesCapacity(t *testing.T) {
	root := t.TempDir()
	cache := newByteLimitedDiskCache(root, 0)
	now := time.Now()
	for _, key := range []string{"expired", "old", "middle", "new"} {
		cache.Put(key, []byte("12"))
	}
	mtimes := map[string]time.Time{
		"expired": now.Add(-8 * 24 * time.Hour),
		"old":     now.Add(-4 * 24 * time.Hour),
		"middle":  now.Add(-3 * 24 * time.Hour),
		"new":     now.Add(-2 * 24 * time.Hour),
	}
	for key, modified := range mtimes {
		path := cache.path(key)
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}

	reopened := newByteLimitedDiskCache(root, 4)
	if got := reopened.Stats(); got["entries"] != 2 || got["bytes"] != int64(4) {
		t.Fatalf("reopened stats=%v, want 2 entries / 4 bytes", got)
	}
	for _, key := range []string{"expired", "old"} {
		if _, ok := reopened.Get(key); ok {
			t.Errorf("reopened cache retained expired/LRU key %q", key)
		}
	}
	for _, key := range []string{"middle", "new"} {
		if _, ok := reopened.Get(key); !ok {
			t.Errorf("reopened cache lost recent key %q", key)
		}
	}
}

func TestDiskCacheConcurrentIndexOperations(t *testing.T) {
	cache := newByteLimitedDiskCache(t.TempDir(), 1<<20)
	const workers, entries = 8, 80
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := range entries {
				key := fmt.Sprintf("%d:%d", worker, i)
				cache.Put(key, []byte("x"))
				if _, ok := cache.Get(key); !ok {
					t.Errorf("worker %d entry %d missed after put", worker, i)
				}
				if i%4 == 0 {
					cache.Delete(key)
				}
				_ = cache.Stats()
			}
		}(worker)
	}
	wg.Wait()
	wantEntries := workers * (entries - entries/4)
	if got := cache.Stats(); got["entries"] != wantEntries || got["bytes"] != int64(wantEntries) {
		t.Fatalf("concurrent final stats=%v, want %d entries / %d bytes", got, wantEntries, wantEntries)
	}
}

func newByteLimitedDiskCache(root string, maxBytes int64) *DiskCache {
	return NewDiskCache(root, float64(maxBytes)/(1024*1024*1024), true)
}

func BenchmarkDiskCachePutIndexedEntries(b *testing.B) {
	for _, existing := range []int{100, 10000} {
		b.Run(fmt.Sprintf("existing_%d", existing), func(b *testing.B) {
			root := b.TempDir()
			seeder := NewDiskCache(root, 10, false)
			for i := 0; i < existing; i++ {
				path := seeder.path(fmt.Sprintf("seed-%d", i))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
					b.Fatal(err)
				}
			}
			cache := NewDiskCache(root, 10, true)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cache.Put(fmt.Sprintf("write-%d", i), []byte("thumbnail bytes"))
			}
		})
	}
}

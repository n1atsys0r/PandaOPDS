package archive

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func writeTestZip(t *testing.T, path string, pages map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i < len(pages); i++ {
		name := fmt.Sprintf("%03d.jpg", i+1)
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(pages[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestZipPoolReusesEvictsAndRespectsActiveRefs(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "1.zip")
	p2 := filepath.Join(dir, "2.zip")
	p3 := filepath.Join(dir, "3.zip")
	writeTestZip(t, p1, map[string]string{"001.jpg": "one-1", "002.jpg": "one-2"})
	writeTestZip(t, p2, map[string]string{"001.jpg": "two-1"})
	writeTestZip(t, p3, map[string]string{"001.jpg": "three-1"})

	now := time.Unix(1700000000, 0)
	pool := newZipPool(2, time.Minute)
	pool.now = func() time.Time { return now }
	defer pool.close()

	r1, rel1, reused, err := pool.acquire(p1)
	if err != nil || reused {
		t.Fatalf("first acquire p1: reused=%v err=%v", reused, err)
	}
	// Hold rel1 active while opening p2 and p3; p2 is idle so p2 should be evicted, not active p1.
	now = now.Add(time.Second)
	_, rel2, _, err := pool.acquire(p2)
	if err != nil {
		t.Fatal(err)
	}
	rel2()

	now = now.Add(time.Second)
	_, rel3, _, err := pool.acquire(p3)
	if err != nil {
		t.Fatal(err)
	}
	rel3()

	// p1 is still held active, so reading from r1 must still succeed.
	if data, ok, err := readZipPage(r1, 1); err != nil || !ok || string(data) != "one-2" {
		t.Fatalf("active reader failed after eviction: ok=%v err=%v data=%q", ok, err, data)
	}
	rel1()

	// Next acquire of p1 should reuse the cached entry without reopening.
	now = now.Add(time.Second)
	_, rel1b, reused, err := pool.acquire(p1)
	if err != nil || !reused {
		t.Fatalf("expected p1 cache hit, reused=%v err=%v", reused, err)
	}
	rel1b()

	// Advance past idleTTL and verify idle entries are pruned.
	now = now.Add(2 * time.Minute)
	pool.reapIdle()
	pool.mu.Lock()
	remaining := len(pool.entries)
	pool.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected 0 entries after idle reap, got %d", remaining)
	}
}

func TestStoreGetPagePoolInvalidatesOnPublishAndRemove(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Upsert(777, "tok", map[string]any{"status": "ready", "title": "Pooled"}); err != nil {
		t.Fatal(err)
	}
	_, pubPath, err := store.ArchivePublishPath(777, "tok")
	if err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, pubPath, map[string]string{"001.jpg": "v1-page1", "002.jpg": "v1-page2"})
	if err := store.PublishArchive(777, "tok", pubPath); err != nil {
		t.Fatal(err)
	}

	// Concurrent reads on the same archive should share the pooled reader safely.
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			page := idx % 2
			want := fmt.Sprintf("v1-page%d", page+1)
			b, ok, err := store.GetPage(777, "tok", page)
			if err != nil || !ok || string(b) != want {
				t.Errorf("concurrent GetPage(%d): ok=%v err=%v got=%q want=%q", page, ok, err, b, want)
			}
		}(i)
	}
	wg.Wait()

	// Re-publishing the archive must invalidate the pooled reader and serve updated bytes.
	writeTestZip(t, pubPath, map[string]string{"001.jpg": "v2-page1"})
	if err := store.PublishArchive(777, "tok", pubPath); err != nil {
		t.Fatal(err)
	}
	b, ok, err := store.GetPage(777, "tok", 0)
	if err != nil || !ok || string(b) != "v2-page1" {
		t.Fatalf("after republish GetPage(0): ok=%v err=%v got=%q", ok, err, b)
	}

	if !store.Remove(777, "tok") {
		t.Fatal("expected store.Remove to succeed")
	}
	store.zipPool.mu.Lock()
	entries := len(store.zipPool.entries)
	store.zipPool.mu.Unlock()
	if entries != 0 {
		t.Fatalf("expected pool to be empty after Remove, got %d", entries)
	}
}

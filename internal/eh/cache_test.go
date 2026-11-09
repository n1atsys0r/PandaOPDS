package eh

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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

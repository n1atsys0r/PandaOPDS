package archive

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pandaopds/internal/eh"
)

func writeStoreTestZIP(t *testing.T, path, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFlatShelfSwitchKeepsOriginalAndVersionStreams(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(101, "original", map[string]any{"status": "ready", "title": "Old title", "quality": "Original", "or": "org", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(101, "original"), "old.jpg", "old-page")
	if err := store.WriteMetadata(101, "original", map[string]any{"gid": 101, "token": "original", "title": "Old title", "saved_at": float64(10)}); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteCover(101, "original", []byte("old-cover")); err != nil {
		t.Fatal(err)
	}
	if err := store.CanAddVersion(101, "original", 202, "new-version"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(202, "new-version", map[string]any{"root_gid": 101, "root_token": "original", "is_version_update": true, "status": "ready", "title": "New title", "quality": "Original", "or": "org", "page_count": 1, "metadata_at": float64(20)}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(202, "new-version"), "new.jpg", "new-page")
	if err := store.ActivateVersion(101, "original", 202, "new-version", map[string]any{"gid": 202, "token": "new-version", "title": "New title", "saved_at": float64(20)}, []byte("new-cover"), true); err != nil {
		t.Fatal(err)
	}

	if gid, token := store.ResolveActive(101, "original"); gid != 202 || token != "new-version" {
		t.Fatalf("active identity=(%d,%q), want (202,new-version)", gid, token)
	}
	if count := store.ReadyCount(); count != 1 {
		t.Fatalf("ready shelves=%d, want 1", count)
	}
	listing := store.ListReady(1, 10)
	if len(listing) != 1 || listing[0].GID != 101 || listing[0].Token != "original" || listing[0].ActiveGID != 202 || listing[0].ActiveToken != "new-version" || listing[0].Title != "New title" || listing[0].PageCount != 1 {
		t.Fatalf("unexpected active shelf listing: %#v", listing)
	}
	oldPage, ok, err := store.GetPage(101, "original", 0)
	if err != nil || !ok || string(oldPage) != "old-page" {
		t.Fatalf("original stream page=%q ok=%t err=%v", oldPage, ok, err)
	}
	newPage, ok, err := store.GetPage(202, "new-version", 0)
	if err != nil || !ok || string(newPage) != "new-page" {
		t.Fatalf("active stream page=%q ok=%t err=%v", newPage, ok, err)
	}
	if _, ok := store.GetMetadataSnapshot(101, "original"); ok {
		t.Fatal("original identity returned the active sidecar without explicit resolution")
	}
	if snapshot, ok := store.GetMetadataSnapshot(202, "new-version"); !ok || snapshot["title"] != "New title" {
		t.Fatalf("active metadata snapshot=%#v found=%t", snapshot, ok)
	}
	if cover, ok := store.Cover(202, "new-version"); !ok || string(cover) != "new-cover" {
		t.Fatalf("active cover=%q found=%t", cover, ok)
	}
	for _, path := range []string{
		filepath.Join(root, "101", "meta.json"),
		filepath.Join(root, "101", "metadata.json"),
		filepath.Join(root, "101", "cover.jpg"),
		filepath.Join(root, "101", "101.zip"),
		filepath.Join(root, "101", "202.zip"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected flat archive file %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "101", "new-version", metaName)); !os.IsNotExist(err) {
		t.Fatalf("flat update created a per-version meta file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "202", metaName)); !os.IsNotExist(err) {
		t.Fatalf("flat update created a separate shelf root for target: %v", err)
	}

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if gid, token := restarted.ResolveActive(101, "original"); gid != 202 || token != "new-version" {
		t.Fatalf("reloaded active identity=(%d,%q)", gid, token)
	}
	oldPage, ok, err = restarted.GetPage(101, "original", 0)
	if err != nil || !ok || string(oldPage) != "old-page" {
		t.Fatalf("reloaded original stream=%q ok=%t err=%v", oldPage, ok, err)
	}
}

func TestUpdateLinksOnlyWritesForCurrentActiveIdentity(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(11, "root-token", map[string]any{"status": "ready"}); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteMetadata(11, "root-token", map[string]any{"gid": 11, "token": "root-token", "title": "Stable", "rating": float64(4)}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateVersionLinks(11, "root-token", eh.GalleryMetadata{GID: 12, Token: "next-token", CurrentGID: 13, CurrentKey: "latest-token", Title: "Wrong version"}); err != nil {
		t.Fatal(err)
	}
	snapshot, found := store.GetMetadataSnapshot(11, "root-token")
	if !found || snapshot["title"] != "Stable" || snapshot["current_gid"] != nil {
		t.Fatalf("mismatched active gdata modified snapshot: %#v found=%t", snapshot, found)
	}
	if err := store.UpdateVersionLinks(11, "root-token", eh.GalleryMetadata{GID: 11, Token: "root-token", CurrentGID: 13, CurrentKey: "latest-token", Title: "New title"}); err != nil {
		t.Fatal(err)
	}
	snapshot, found = store.GetMetadataSnapshot(11, "root-token")
	if !found || snapshot["title"] != "Stable" || snapshot["current_gid"] != float64(13) || snapshot["current_key"] != "latest-token" || snapshot["rating"] != float64(4) {
		t.Fatalf("version link update changed other snapshot fields: %#v found=%t", snapshot, found)
	}
}

func TestFlatRootRejectsConflictingTokenBeforeCreatingShelf(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(44, "one", map[string]any{"status": "ready"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CanStart(44, "two"); err == nil {
		t.Fatal("same gid with a conflicting token passed archive preflight")
	}
	if _, err := store.Upsert(44, "two", map[string]any{"status": "pending"}); err == nil {
		t.Fatal("same gid with a conflicting token created another shelf")
	}
}

func TestActivationJournalFinishesAfterRestartWithoutMixingSidecars(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(70, "root", map[string]any{"status": "ready", "title": "Old", "or": "org", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(70, "root"), "old.jpg", "old")
	if err := store.WriteMetadata(70, "root", map[string]any{"gid": 70, "token": "root", "title": "Old"}); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteCover(70, "root", []byte("old-cover")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(71, "next", map[string]any{"root_gid": 70, "root_token": "root", "is_version_update": true, "status": "ready", "title": "New", "or": "org", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(71, "next"), "new.jpg", "new")
	newSnapshot := map[string]any{"gid": 71, "token": "next", "title": "New"}
	newSidecar, err := json.MarshalIndent(newSnapshot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	rec := store.rootLocked(70, "root")
	metaPath := filepath.Join(rec.dir, metadataName)
	coverPath := filepath.Join(rec.dir, coverName)
	if err = store.writeAtomic(metaPath+".next", newSidecar); err != nil {
		t.Fatal(err)
	}
	if err = store.writeAtomic(coverPath+".next", []byte("new-cover")); err != nil {
		t.Fatal(err)
	}
	rec.meta["activation_pending"] = map[string]any{"old_gid": 70, "old_token": "root", "target_gid": 71, "target_token": "next"}
	if err = store.persistLocked(rec); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(metaPath, metaPath+".previous"); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(metaPath+".next", metaPath); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(coverPath, coverPath+".previous"); err != nil {
		t.Fatal(err)
	}
	store.mu.Unlock()

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if gid, token := restarted.ResolveActive(70, "root"); gid != 71 || token != "next" {
		t.Fatalf("recovered active version=(%d,%q), want (71,next)", gid, token)
	}
	snapshot, ok := restarted.GetMetadataSnapshot(71, "next")
	if !ok || snapshot["title"] != "New" {
		t.Fatalf("recovered metadata=%#v found=%t", snapshot, ok)
	}
	cover, ok := restarted.Cover(71, "next")
	if !ok || string(cover) != "new-cover" {
		t.Fatalf("recovered cover=%q found=%t", cover, ok)
	}
	old, oldOK, err := restarted.GetPage(70, "root", 0)
	if err != nil || !oldOK || string(old) != "old" {
		t.Fatalf("recovered original ZIP page=%q found=%t err=%v", old, oldOK, err)
	}
}

func TestNoCoverActivationClearsStaleStagedCover(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(80, "root", map[string]any{"status": "ready", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(80, "root"), "old.jpg", "old")
	if err := store.WriteMetadata(80, "root", map[string]any{"gid": 80, "token": "root", "title": "Old"}); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteCover(80, "root", []byte("old-cover")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(81, "next", map[string]any{"root_gid": 80, "root_token": "root", "is_version_update": true, "status": "ready", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(81, "next"), "new.jpg", "new")
	recDir := store.dir(80, "root")
	if err := os.WriteFile(filepath.Join(recDir, coverName+".next"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateVersion(80, "root", 81, "next", map[string]any{"gid": 81, "token": "next", "title": "New"}, nil, false); err != nil {
		t.Fatal(err)
	}
	if cover, ok := store.Cover(81, "next"); ok {
		t.Fatalf("no-cover activation exposed stale cover bytes: %q", cover)
	}
}

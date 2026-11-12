package archive

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	oldArchive := filepath.Join(root, "101", zipName)
	if _, err := os.Stat(oldArchive); !os.IsNotExist(err) {
		t.Fatalf("previous ZIP should be removed after activation: %v", err)
	}
	if count := store.PageCount(101, "original"); count != 0 {
		t.Fatalf("removed original stream still reports %d pages", count)
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
	targetDir := filepath.Join(root, "202")
	for _, path := range []string{
		filepath.Join(root, "101", stateName),
		filepath.Join(root, "101", galleryName),
		filepath.Join(root, "101", "cover.jpg"),
		filepath.Join(targetDir, stateName),
		filepath.Join(targetDir, galleryName),
		filepath.Join(targetDir, "cover.jpg"),
		filepath.Join(targetDir, zipName),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected flat archive file %s: %v", path, err)
		}
	}
	var targetMeta map[string]any
	b, err := os.ReadFile(filepath.Join(targetDir, stateName))
	if err != nil || json.Unmarshal(b, &targetMeta) != nil || toInt(targetMeta["root_gid"]) != 101 || toString(targetMeta["root_token"]) != "original" {
		t.Fatalf("target physical version metadata does not retain original shelf identity: %#v err=%v", targetMeta, err)
	}

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if gid, token := restarted.ResolveActive(101, "original"); gid != 202 || token != "new-version" {
		t.Fatalf("reloaded active identity=(%d,%q)", gid, token)
	}
	if _, err := os.Stat(oldArchive); !os.IsNotExist(err) {
		t.Fatalf("restart recreated the removed previous ZIP: %v", err)
	}
	newPage, ok, err = restarted.GetPage(202, "new-version", 0)
	if err != nil || !ok || string(newPage) != "new-page" {
		t.Fatalf("reloaded active stream=%q ok=%t err=%v", newPage, ok, err)
	}
	if err := restarted.CanAddVersion(101, "original", 303, "newest"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Upsert(303, "newest", map[string]any{"root_gid": 101, "root_token": "original", "is_version_update": true, "status": "ready", "title": "Newest", "quality": "Original", "or": "org", "page_count": 1, "metadata_at": float64(30)}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, restarted.archivePath(303, "newest"), "newest.jpg", "newest-page")
	if err := restarted.ActivateVersion(101, "original", 303, "newest", map[string]any{"gid": 303, "token": "newest", "title": "Newest", "saved_at": float64(30)}, []byte("newest-cover"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "202", zipName)); !os.IsNotExist(err) {
		t.Fatalf("second switch did not remove the prior active ZIP: %v", err)
	}
	for _, path := range []string{filepath.Join(root, "202", stateName), filepath.Join(root, "202", galleryName), filepath.Join(root, "202", coverName)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("second switch removed prior active sidecar %s: %v", path, err)
		}
	}
	if count := restarted.PageCount(202, "new-version"); count != 0 {
		t.Fatalf("prior active stream still reports %d pages after ZIP cleanup", count)
	}
	if _, err := restarted.Upsert(400, "independent", map[string]any{"status": "ready", "title": "Independent"}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, restarted.archivePath(400, "independent"), "independent.jpg", "keep")
	if !restarted.Remove(101, "original") {
		t.Fatal("removing the shelf root failed")
	}
	if _, err := os.Stat(filepath.Join(root, "202")); !os.IsNotExist(err) {
		t.Fatalf("shelf deletion left an owned physical version directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "400", stateName)); err != nil {
		t.Fatalf("shelf deletion removed an independent gid directory: %v", err)
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
	// Preserve historical filenames for this existing root while creating a
	// new physical version with state.json and gallery.json.
	if err := os.Rename(filepath.Join(root, "70", stateName), filepath.Join(root, "70", metaName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "70", galleryName), filepath.Join(root, "70", metadataName)); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	rootRec := store.rootLocked(70, "root")
	rootRec.stateFileName = metaName
	rootVersions := versionRecords(rootRec.meta)
	rootVersions[key(70, "root")]["state_file"] = metaName
	rootVersions[key(70, "root")]["gallery_file"] = metadataName
	if err := store.persistLocked(rootRec); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	store.mu.Unlock()
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteMetadata(70, "root", map[string]any{"gid": 70, "token": "root", "title": "Old refreshed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "70", metaName)); err != nil {
		t.Fatalf("reloaded legacy root state filename was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "70", metadataName)); err != nil {
		t.Fatalf("reloaded legacy gallery filename was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "70", stateName)); !os.IsNotExist(err) {
		t.Fatalf("legacy root was migrated to the new state filename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "70", galleryName)); !os.IsNotExist(err) {
		t.Fatalf("legacy root was migrated to the new gallery filename: %v", err)
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
	targetDir := store.dir(71, "next")
	store.mu.Lock()
	rec := store.rootLocked(70, "root")
	metaPath := filepath.Join(targetDir, galleryName)
	coverPath := filepath.Join(targetDir, coverName)
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
	if err = os.Rename(metaPath+".next", metaPath); err != nil {
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
	if _, err := os.Stat(filepath.Join(root, "70", zipName)); !os.IsNotExist(err) {
		t.Fatalf("startup did not retry previous ZIP cleanup: %v", err)
	}
	oldSidecar, err := os.ReadFile(filepath.Join(root, "70", metadataName))
	if err != nil || !strings.Contains(string(oldSidecar), "Old") {
		t.Fatalf("cleanup removed or changed previous metadata sidecar: %s err=%v", oldSidecar, err)
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
	targetDir := store.dir(81, "next")
	if err := os.WriteFile(filepath.Join(targetDir, coverName+".next"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateVersion(80, "root", 81, "next", map[string]any{"gid": 81, "token": "next", "title": "New"}, nil, false); err != nil {
		t.Fatal(err)
	}
	if cover, ok := store.Cover(81, "next"); ok {
		t.Fatalf("no-cover activation exposed stale cover bytes: %q", cover)
	}
	if cover, err := os.ReadFile(filepath.Join(store.dir(80, "root"), coverName)); err != nil || string(cover) != "old-cover" {
		t.Fatalf("activation removed the previous version cover sidecar: %q err=%v", cover, err)
	}
}

func TestCleanupRetriesAfterCommitAndPreservesSourceWhenTargetZIPIsMissing(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(120, "root", map[string]any{"status": "ready", "title": "Old", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(120, "root"), "old.jpg", "old")
	if err := store.WriteMetadata(120, "root", map[string]any{"gid": 120, "token": "root", "title": "Old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(121, "next", map[string]any{"root_gid": 120, "root_token": "root", "is_version_update": true, "status": "ready", "title": "New", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(121, "next"), "new.jpg", "new")
	if err := store.ActivateVersion(120, "root", 121, "next", map[string]any{"gid": 121, "token": "next", "title": "New"}, nil, false); err != nil {
		t.Fatal(err)
	}
	previousPath, activePath := store.archivePath(120, "root"), store.archivePath(121, "next")
	writeStoreTestZIP(t, previousPath, "old.jpg", "old")
	if err := os.Remove(activePath); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	rec := store.rootLocked(120, "root")
	rec.meta["cleanup_previous_zip"] = map[string]any{"gid": 120, "token": "root", "target_gid": 121, "target_token": "next"}
	if err := store.persistLocked(rec); err != nil {
		t.Fatal(err)
	}
	store.mu.Unlock()

	interrupted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(previousPath); err != nil {
		t.Fatalf("cleanup removed the source while active ZIP was missing: %v", err)
	}
	if interrupted.Get(120, "root")["active_gid"] != 121 {
		t.Fatal("restart lost the committed active pointer")
	}
	writeStoreTestZIP(t, activePath, "new.jpg", "new")
	retried, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(previousPath); !os.IsNotExist(err) {
		t.Fatalf("startup cleanup did not remove source ZIP after target recovery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "120", stateName)); err != nil {
		t.Fatalf("source identity metadata was removed with its ZIP: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "120", galleryName)); err != nil {
		t.Fatalf("source snapshot sidecar was removed with its ZIP: %v", err)
	}
	if gid, token := retried.ResolveActive(120, "root"); gid != 121 || token != "next" {
		t.Fatalf("active identity after cleanup retry=(%d,%q)", gid, token)
	}
}

func TestRemoveRefusesVersionDirectoryWithDifferentOwner(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(130, "root", map[string]any{"status": "ready"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(131, "next", map[string]any{"root_gid": 130, "root_token": "root", "is_version_update": true, "status": "ready"}); err != nil {
		t.Fatal(err)
	}
	foreignMeta := map[string]any{"gid": 131, "token": "next", "root_gid": 131, "root_token": "next", "active_gid": 131, "active_token": "next", "layout": versionDirLayout, "status": "ready"}
	b, err := json.Marshal(foreignMeta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "131", stateName), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if store.Remove(130, "root") {
		t.Fatal("remove deleted a version directory whose owner no longer matched the shelf")
	}
	if _, err := os.Stat(filepath.Join(root, "130", stateName)); err != nil {
		t.Fatalf("failed ownership check removed the original shelf: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "131", stateName)); err != nil {
		t.Fatalf("failed ownership check removed the foreign directory: %v", err)
	}
}

func TestCatalogCreatesUnarchivedWithoutTouchingReadyOrVersions(t *testing.T) {
	dataDir := t.TempDir()
	stateDir := t.TempDir()
	store, err := NewStoreWithState(dataDir, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	created, err := store.Catalog(301, "tok301", map[string]any{"title": "Fav Gallery", "created_at": float64(100)}, map[string]any{
		"title": "Fav Gallery", "category": "Manga", "filecount": 18, "catalog_source": "favorites_list",
	})
	if err != nil || !created {
		t.Fatalf("Catalog(301) created=%v err=%v", created, err)
	}
	if store.ReadyCount() != 0 || len(store.ListReady(1, 20)) != 0 {
		t.Fatalf("unarchived catalog entry leaked into ReadyCount/ListReady")
	}
	if got := store.Get(301, "tok301"); got == nil || got["status"] != "unarchived" {
		t.Fatalf("Get(301)=%#v, want status=unarchived", got)
	}
	if len(store.Entries()) != 1 {
		t.Fatalf("Entries()=%d, want 1", len(store.Entries()))
	}
	if err := store.CanStart(301, "tok301"); err != nil {
		t.Fatalf("CanStart on unarchived catalog entry failed: %v", err)
	}

	// Ready archive & active updated version must never be downgraded or duplicated.
	if _, err := store.Upsert(302, "root302", map[string]any{"status": "ready", "title": "Root", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(302, "root302"), "p1.jpg", "img")
	if _, err := store.Upsert(303, "ver303", map[string]any{"root_gid": 302, "root_token": "root302", "is_version_update": true, "status": "ready", "title": "V2", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(303, "ver303"), "p1.jpg", "img")
	if err := store.ActivateVersion(302, "root302", 303, "ver303", map[string]any{"gid": 303, "token": "ver303", "title": "V2"}, []byte("c"), true); err != nil {
		t.Fatal(err)
	}

	created, err = store.Catalog(303, "ver303", map[string]any{"title": "V2 Fav"}, map[string]any{"title": "V2 Fav"})
	if err != nil || created {
		t.Fatalf("Catalog on active version returned created=%v err=%v", created, err)
	}
	if got := store.Get(302, "root302"); got["status"] != "ready" {
		t.Fatalf("Catalog altered existing shelf status: %#v", got)
	}
	if len(store.Entries()) != 2 {
		t.Fatalf("Entries()=%d, want 2 (no duplicate card for updated version)", len(store.Entries()))
	}
}

func TestResetToUnarchivedCleansVersionPayloadsAndRetainsRootCatalog(t *testing.T) {
	dataDir := t.TempDir()
	stateDir := t.TempDir()
	store, err := NewStoreWithState(dataDir, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(401, "root", map[string]any{"status": "ready", "title": "Root", "or": "org", "quality": "Original", "page_count": 2}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(401, "root"), "p1.jpg", "old")
	if err := store.WriteMetadata(401, "root", map[string]any{"gid": 401, "token": "root", "title": "Root", "filecount": 2, "saved_at": float64(10)}); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteCover(401, "root", []byte("root-cover")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(402, "next", map[string]any{"root_gid": 401, "root_token": "root", "is_version_update": true, "status": "ready", "title": "V2", "or": "org", "quality": "Original", "page_count": 3}); err != nil {
		t.Fatal(err)
	}
	writeStoreTestZIP(t, store.archivePath(402, "next"), "p1.jpg", "new")
	if err := store.ActivateVersion(401, "root", 402, "next", map[string]any{"gid": 402, "token": "next", "title": "V2", "filecount": 3}, []byte("new-cover"), true); err != nil {
		t.Fatal(err)
	}

	if !store.ResetToUnarchived(401, "root") {
		t.Fatal("ResetToUnarchived returned false")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "402.zip")); !os.IsNotExist(err) {
		t.Fatalf("version ZIP was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "402")); !os.IsNotExist(err) {
		t.Fatalf("version state directory was not removed: %v", err)
	}
	if store.ReadyCount() != 0 {
		t.Fatalf("ReadyCount=%d, want 0", store.ReadyCount())
	}
	got := store.Get(401, "root")
	if got == nil || got["status"] != "unarchived" || toInt(got["active_gid"]) != 401 || toInt(got["page_count"]) != 0 || toString(got["or"]) != "" {
		t.Fatalf("reset entry=%#v", got)
	}
	if cover, ok := store.Cover(401, "root"); !ok || string(cover) != "root-cover" {
		t.Fatalf("root cover=%q ok=%v", cover, ok)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewStoreWithState(dataDir, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if got := restarted.Get(401, "root"); got == nil || got["status"] != "unarchived" {
		t.Fatalf("restarted reset entry=%#v", got)
	}
}

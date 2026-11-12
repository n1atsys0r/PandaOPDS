package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestScanLegacySidecarsPublishesReadyEntryAndIsRepeatable(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	sourceDir := filepath.Join(dataRoot, "501")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{"gid":501,"token":"scan-token","status":"ready","title":"Scanned","layout":"flat","page_count":1,"archive_size":100}`
	if err := os.WriteFile(filepath.Join(sourceDir, stateName), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(sourceDir, "501.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("page.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("page")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, galleryName), []byte(`{"title":"Scanned"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	report, err := store.ScanLegacySidecarsContext(context.Background())
	if err != nil {
		t.Fatalf("scan: report=%+v err=%v", report, err)
	}
	if report.State != 1 || report.Gallery != 1 {
		t.Fatalf("first scan report=%+v", report)
	}
	entries := store.ListReady(1, 20)
	if len(entries) != 1 || entries[0].GID != 501 || entries[0].PageCount != 1 {
		t.Fatalf("ready entries=%+v", entries)
	}
	if got := store.Get(501, "scan-token"); got == nil {
		t.Fatal("scanned entry is not indexed in the live store")
	}

	report, err = store.ScanLegacySidecars()
	if err != nil {
		t.Fatalf("repeat scan: report=%+v err=%v", report, err)
	}
	if report.State+report.Gallery+report.Cover != 0 || report.Skipped < 2 {
		t.Fatalf("repeat scan report=%+v", report)
	}
	if entries := store.ListReady(1, 20); len(entries) != 1 {
		t.Fatalf("repeat scan duplicated entry: %+v", entries)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.ScanLegacySidecars(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent scan: %v", err)
	}
	if entries := store.ListReady(1, 20); len(entries) != 1 {
		t.Fatalf("concurrent scan duplicated entry: %+v", entries)
	}
	// A prior CLI migration intentionally leaves source sidecars on the ZIP
	// volume. Once split storage deletes the payload and local state, scanning
	// those stale sidecars must not recreate the shelf.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(zipPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stateRoot, "501", stateName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stateRoot, "501", galleryName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stateRoot, "index.snapshot")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	store, err = NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.ScanLegacySidecars(); err != nil {
		t.Fatalf("scan after deletion: %v", err)
	}
	if entries := store.ListReady(1, 20); len(entries) != 0 {
		t.Fatalf("deleted archive was resurrected: %+v", entries)
	}
}

func TestImportSidecarsPreservesArchiveAndIsRepeatable(t *testing.T) {
	archiveRoot, stateRoot := t.TempDir(), t.TempDir()
	for name, data := range map[string]string{"42/state.json": `{"gid":42,"token":"abc"}`, "42/gallery.json": `{"title":"x"}`, "42/cover.jpg": "cover", "42/archive.zip": "zip"} {
		path := filepath.Join(archiveRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if name == "42/archive.zip" {
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			w, _ := zw.Create("page.jpg")
			_, _ = w.Write([]byte("page"))
			_ = zw.Close()
			_ = f.Close()
			continue
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	report, err := ImportSidecars(archiveRoot, stateRoot)
	if err != nil || report.State != 1 || report.Gallery != 1 || report.Cover != 1 {
		t.Fatalf("import report=%+v err=%v", report, err)
	}
	if _, err := zip.OpenReader(filepath.Join(archiveRoot, "42", "archive.zip")); err != nil {
		t.Fatalf("source ZIP changed: %v", err)
	}
	report, err = ImportSidecars(archiveRoot, stateRoot)
	if err != nil || report.Skipped != 3 || report.Conflicts != 0 {
		t.Fatalf("repeat import report=%+v err=%v", report, err)
	}
}

func TestImportArchiveMovesNestedPayloadToFlatPath(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	writeMigrationZip(t, filepath.Join(dataRoot, "401", "401.zip"), "page-1.jpg")
	writeMigrationSidecar(t, filepath.Join(dataRoot, "401", stateName), `{"gid":401,"token":"root-token","status":"ready","layout":"flat","page_count":99,"archive_size":999,"versions":{"401:root-token":{"gid":401,"token":"root-token","status":"ready","storage_dir":"."}}}`)
	writeMigrationSidecar(t, filepath.Join(dataRoot, "401", galleryName), `{"title":"legacy"}`)

	report, err := ImportArchive(dataRoot, stateRoot, dataRoot)
	if err != nil {
		t.Fatalf("import report=%+v err=%v", report, err)
	}
	if report.Payloads != 1 || report.Conflicts != 0 {
		t.Fatalf("migration report=%+v", report)
	}
	assertFileMissing(t, filepath.Join(dataRoot, "401", "401.zip"))
	if _, err := zip.OpenReader(filepath.Join(dataRoot, "401.zip")); err != nil {
		t.Fatalf("flat ZIP missing or invalid: %v", err)
	}
	local := readJSON(t, filepath.Join(stateRoot, "401", stateName))
	if local["payload_layout"] != "flat-gid" || toInt(local["page_count"]) != 1 || toInt64(local["archive_size"]) == 0 {
		t.Fatalf("local state did not track flat payload: %#v", local)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "401", stateName)); err != nil {
		t.Fatalf("source sidecar was not preserved: %v", err)
	}
	store, err := NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.PageCount(401, "root-token") != 1 {
		t.Fatalf("reloaded page count=%d", store.PageCount(401, "root-token"))
	}
	if _, ok, err := store.GetPage(401, "root-token", 0); err != nil || !ok {
		t.Fatalf("flat payload unavailable after reload: ok=%v err=%v", ok, err)
	}
}

func TestImportArchiveMigratesEveryVersionAndPreservesActiveIdentity(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	rootState := `{"gid":410,"token":"root-token","root_gid":410,"root_token":"root-token","active_gid":420,"active_token":"active-token","layout":"versioned","status":"ready","versions":{"410:root-token":{"gid":410,"token":"root-token","storage_dir":".","status":"ready"},"420:active-token":{"gid":420,"token":"active-token","storage_dir":"420","status":"ready","is_version_update":true}}}`
	writeMigrationSidecar(t, filepath.Join(dataRoot, "410", stateName), rootState)
	writeMigrationSidecar(t, filepath.Join(dataRoot, "420", stateName), `{"gid":420,"token":"active-token","root_gid":410,"root_token":"root-token","status":"ready","storage_dir":"420"}`)
	writeMigrationZip(t, filepath.Join(dataRoot, "410", zipName), "root-page.jpg")
	writeMigrationZip(t, filepath.Join(dataRoot, "420", zipName), "active-page.jpg")

	report, err := ImportArchive(dataRoot, stateRoot, dataRoot)
	if err != nil {
		t.Fatalf("import report=%+v err=%v", report, err)
	}
	if report.Payloads != 2 || report.Conflicts != 0 {
		t.Fatalf("migration report=%+v", report)
	}
	for _, gid := range []int{410, 420} {
		if _, err := zip.OpenReader(filepath.Join(dataRoot, strconv.Itoa(gid)+".zip")); err != nil {
			t.Fatalf("flat version %d missing: %v", gid, err)
		}
		assertFileMissing(t, filepath.Join(dataRoot, strconv.Itoa(gid), zipName))
	}
	local := readJSON(t, filepath.Join(stateRoot, "410", stateName))
	if local["payload_layout"] != "flat-gid" || toInt(local["active_gid"]) != 420 || local["active_token"] != "active-token" {
		t.Fatalf("root identity/active version changed: %#v", local)
	}
	versions := versionRecords(local)
	if toInt(versions["410:root-token"]["page_count"]) != 1 || toInt(versions["420:active-token"]["page_count"]) != 1 {
		t.Fatalf("per-version cache not updated: %#v", versions)
	}
	activeState := readJSON(t, filepath.Join(stateRoot, "420", stateName))
	if toInt(activeState["page_count"]) != 1 || toInt(activeState["root_gid"]) != 410 {
		t.Fatalf("version sidecar cache/owner=%#v", activeState)
	}
}

func TestImportArchiveMigratesOldFlatVersionsSharingOneSidecar(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	root := `{"gid":455,"token":"old-root","root_gid":455,"root_token":"old-root","active_gid":456,"active_token":"old-active","layout":"flat","status":"ready","versions":{"455:old-root":{"gid":455,"token":"old-root","storage_dir":".","state_file":"state.json","status":"ready"},"456:old-active":{"gid":456,"token":"old-active","storage_dir":".","state_file":"state.json","status":"ready"}}}`
	writeMigrationSidecar(t, filepath.Join(dataRoot, "455", stateName), root)
	writeMigrationZip(t, filepath.Join(dataRoot, "455", "455.zip"), "old-root.jpg")
	writeMigrationZip(t, filepath.Join(dataRoot, "455", "456.zip"), "old-active.jpg")
	report, err := ImportArchive(dataRoot, stateRoot, dataRoot)
	if err != nil {
		t.Fatalf("import report=%+v err=%v", report, err)
	}
	if report.Payloads != 2 || report.Conflicts != 0 {
		t.Fatalf("old flat migration report=%+v", report)
	}
	for _, gid := range []int{455, 456} {
		if _, err := zip.OpenReader(filepath.Join(dataRoot, strconv.Itoa(gid)+".zip")); err != nil {
			t.Fatalf("flat payload %d missing: %v", gid, err)
		}
	}
	assertFileMissing(t, filepath.Join(dataRoot, "455", "455.zip"))
	assertFileMissing(t, filepath.Join(dataRoot, "455", "456.zip"))
	local := readJSON(t, filepath.Join(stateRoot, "455", stateName))
	if toInt(versionRecords(local)["456:old-active"]["page_count"]) != 1 {
		t.Fatalf("active version cache not updated: %#v", local)
	}
}

func TestImportArchiveRejectsAliasedVersionSources(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	root := `{"gid":457,"token":"alias-root","root_gid":457,"root_token":"alias-root","active_gid":458,"active_token":"alias-active","layout":"versioned","status":"ready","versions":{"457:alias-root":{"gid":457,"token":"alias-root","storage_dir":".","status":"ready"},"458:alias-active":{"gid":458,"token":"alias-active","storage_dir":".","status":"ready"}}}`
	writeMigrationSidecar(t, filepath.Join(dataRoot, "457", stateName), root)
	writeMigrationZip(t, filepath.Join(dataRoot, "457", zipName), "shared.jpg")
	report, err := ImportArchive(dataRoot, stateRoot, dataRoot)
	if err != nil {
		t.Fatalf("import report=%+v err=%v", report, err)
	}
	if report.Payloads != 0 || report.Conflicts == 0 {
		t.Fatalf("aliased payloads were not reported as conflict: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "457", zipName)); err != nil {
		t.Fatalf("shared source ZIP was changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "457.zip")); !os.IsNotExist(err) {
		t.Fatalf("aliased root ZIP was migrated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "458.zip")); !os.IsNotExist(err) {
		t.Fatalf("aliased version ZIP was migrated: %v", err)
	}
}

func TestImportArchiveAllowsRetiredOriginalZIPMissingAfterVersionSwitch(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	writeMigrationSidecar(t, filepath.Join(dataRoot, "450", stateName), `{"gid":450,"token":"root-old","root_gid":450,"root_token":"root-old","active_gid":451,"active_token":"active-new","layout":"versioned","status":"ready","versions":{"450:root-old":{"gid":450,"token":"root-old","storage_dir":".","status":"ready"},"451:active-new":{"gid":451,"token":"active-new","storage_dir":"451","status":"ready"}}}`)
	writeMigrationSidecar(t, filepath.Join(dataRoot, "451", stateName), `{"gid":451,"token":"active-new","root_gid":450,"root_token":"root-old","status":"ready","storage_dir":"451"}`)
	writeMigrationZip(t, filepath.Join(dataRoot, "451", zipName), "active.jpg")

	report, err := ImportArchive(dataRoot, stateRoot, dataRoot)
	if err != nil {
		t.Fatalf("import report=%+v err=%v", report, err)
	}
	if report.Payloads != 1 || report.Conflicts != 0 {
		t.Fatalf("retired-original migration report=%+v", report)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "451.zip")); err != nil {
		t.Fatalf("active flat payload missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "450", zipName)); !os.IsNotExist(err) {
		t.Fatalf("unexpected retired ZIP appeared: %v", err)
	}
	root := readJSON(t, filepath.Join(stateRoot, "450", stateName))
	if root["payload_layout"] != "flat-gid" || toInt(root["active_gid"]) != 451 {
		t.Fatalf("switched shelf identity changed: %#v", root)
	}
}

func TestImportArchiveFlatCollisionPreservesNestedPayload(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	writeMigrationZip(t, filepath.Join(dataRoot, "430", zipName), "nested.jpg")
	writeMigrationSidecar(t, filepath.Join(dataRoot, "430", stateName), `{"gid":430,"token":"collision-token","status":"ready"}`)
	nested, err := os.ReadFile(filepath.Join(dataRoot, "430", zipName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "430.zip"), nested, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dataRoot, "430.zip"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := ImportArchive(dataRoot, stateRoot, dataRoot)
	if err != nil {
		t.Fatalf("import report=%+v err=%v", report, err)
	}
	if report.Payloads != 0 || report.Conflicts == 0 {
		t.Fatalf("collision report=%+v", report)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "430", zipName)); err != nil {
		t.Fatalf("nested payload lost: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dataRoot, "430.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unowned flat collision was overwritten")
	}
	local := readJSON(t, filepath.Join(stateRoot, "430", stateName))
	if local["payload_layout"] == "flat-gid" {
		t.Fatal("conflicted shelf was pointed at unrelated flat payload")
	}
}

func TestImportArchiveMigratesExistingLocalRecordWithoutReplacingMetadata(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	writeMigrationZip(t, filepath.Join(dataRoot, "435", zipName), "local.jpg")
	writeMigrationSidecar(t, filepath.Join(dataRoot, "435", stateName), `{"gid":435,"token":"local-token","status":"ready","title":"old source"}`)
	writeMigrationSidecar(t, filepath.Join(stateRoot, "435", stateName), `{"gid":435,"token":"local-token","root_gid":435,"root_token":"local-token","active_gid":435,"active_token":"local-token","status":"ready","title":"newer local metadata","layout":"versioned","versions":{"435:local-token":{"gid":435,"token":"local-token","storage_dir":".","status":"ready","title":"version metadata"}}}`)

	report, err := ImportArchive(dataRoot, stateRoot, dataRoot)
	if err != nil {
		t.Fatalf("import report=%+v err=%v", report, err)
	}
	if report.Payloads != 1 || report.Conflicts == 0 {
		t.Fatalf("expected preserved-sidecar conflict and migrated payload: %+v", report)
	}
	local := readJSON(t, filepath.Join(stateRoot, "435", stateName))
	if local["title"] != "newer local metadata" || local["payload_layout"] != "flat-gid" {
		t.Fatalf("migration replaced local metadata or missed path update: %#v", local)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "435.zip")); err != nil {
		t.Fatalf("matching local shelf ZIP not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "435", zipName)); !os.IsNotExist(err) {
		t.Fatalf("nested ZIP still present: %v", err)
	}
}

func TestImportArchiveResumesAfterRenameBeforeStateCommit(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	source := filepath.Join(dataRoot, "440", zipName)
	writeMigrationZip(t, source, "resume.jpg")
	writeMigrationSidecar(t, filepath.Join(dataRoot, "440", stateName), `{"gid":440,"token":"resume-token","status":"ready"}`)
	if _, err := ImportSidecars(dataRoot, stateRoot, dataRoot); err != nil {
		t.Fatal(err)
	}
	store, err := NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	plans, blocked := buildPayloadMigrationPlans(store, dataRoot, stateRoot)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if blocked != 0 || len(plans) != 1 {
		t.Fatalf("plans=%d blocked=%d", len(plans), blocked)
	}
	plan := plans[0]
	if err := writeDirtyMarkerAt(stateRoot); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(plan.journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicImport(plan.journalPath, b); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, filepath.Join(dataRoot, "440.zip")); err != nil {
		t.Fatal(err)
	}

	report, err := ImportArchive(dataRoot, stateRoot, dataRoot)
	if err != nil {
		t.Fatalf("resume report=%+v err=%v", report, err)
	}
	if _, err := os.Stat(plan.journalPath); !os.IsNotExist(err) {
		t.Fatalf("journal not cleared after commit: %v", err)
	}
	assertFileMissing(t, source)
	if local := readJSON(t, filepath.Join(stateRoot, "440", stateName)); local["payload_layout"] != "flat-gid" || toInt(local["page_count"]) != 1 {
		t.Fatalf("resumed state not committed: %#v", local)
	}
}

func writeMigrationZip(t *testing.T, path, entry string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("page")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeMigrationSidecar(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s still exists: %v", path, err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestImportInvalidatesCleanSnapshotForNewEntries(t *testing.T) {
	dataRoot, stateRoot, peerStateRoot := t.TempDir(), t.TempDir(), t.TempDir()
	store, err := NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(peerStateRoot, "55", stateName)
	if err := os.MkdirAll(filepath.Dir(state), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte(`{"gid":55,"token":"new","status":"ready","layout":"versioned","payload_layout":"flat-gid","root_gid":55,"root_token":"new","active_gid":55,"active_token":"new","versions":{"55:new":{"gid":55,"token":"new","storage_dir":".","status":"ready"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dataRoot, "55.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("p.jpg")
	_, _ = w.Write([]byte("p"))
	_ = zw.Close()
	_ = f.Close()
	if _, err := ImportSidecars(peerStateRoot, stateRoot, dataRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, ".dirty")); err != nil {
		t.Fatalf("import did not invalidate the clean snapshot: %v", err)
	}
	reloaded, err := NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if got := reloaded.Get(55, "new"); got == nil || got["status"] != "ready" {
		t.Fatalf("imported archive not loaded: %v", got)
	}
	if count := reloaded.PageCount(55, "new"); count != 1 {
		t.Fatalf("imported ZIP page count=%d, want 1", count)
	}
}

func TestImportRejectsInvalidOrConflictingGroupBeforeSidecars(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	write := func(root, rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(source, "70/state.json", `{bad`)
	write(source, "70/cover.jpg", "invalid-state-cover")
	write(source, "71/state.json", `{"gid":71,"token":"source"}`)
	write(source, "71/gallery.json", `{"title":"source"}`)
	write(target, "71/state.json", `{"gid":71,"token":"different"}`)
	report, err := ImportSidecars(source, target)
	if err != nil {
		t.Fatal(err)
	}
	if report.Skipped != 2 || report.Conflicts != 2 || report.State != 0 || report.Gallery != 0 || report.Cover != 0 {
		t.Fatalf("report=%+v", report)
	}
	for _, rel := range []string{"70/cover.jpg", "71/gallery.json"} {
		if _, err := os.Stat(filepath.Join(target, rel)); !os.IsNotExist(err) {
			t.Fatalf("sidecar %s committed despite invalid/conflicting state: %v", rel, err)
		}
	}
}

func TestSnapshotFallbackUsesAuthoritativeLocalState(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "dirty"} {
		t.Run(mode, func(t *testing.T) {
			dataRoot, stateRoot := t.TempDir(), t.TempDir()
			store, err := NewStoreWithState(dataRoot, stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Upsert(88, "auth", map[string]any{"status": "pending"}); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(stateRoot, "88", stateName)
			if err := os.WriteFile(statePath, []byte(`{"gid":88,"token":"auth","root_gid":88,"root_token":"auth","active_gid":88,"active_token":"auth","layout":"versioned","status":"ready","page_count":6,"archive_size":123,"versions":{"88:auth":{"gid":88,"token":"auth","storage_dir":".","status":"ready","page_count":6,"archive_size":123}}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			snapshotPath := filepath.Join(stateRoot, "index.snapshot")
			switch mode {
			case "missing":
				if err := os.Remove(snapshotPath); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(snapshotPath, []byte("not-json"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "dirty":
				if err := writeDirtyMarkerAt(stateRoot); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := NewStoreWithState(dataRoot, stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			items := reopened.ListReady(1, 10)
			if len(items) != 1 || items[0].PageCount != 6 {
				t.Fatalf("state fallback listing=%#v", items)
			}
			if got := reopened.Stats()["bytes"]; got != int64(123) {
				t.Fatalf("state fallback bytes=%v", got)
			}
		})
	}
}

func TestCleanSnapshotListsWithoutArchiveVolume(t *testing.T) {
	stateRoot := t.TempDir()
	initialDataRoot := filepath.Join(t.TempDir(), "archives")
	store, err := NewStoreWithState(initialDataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(89, "cached", map[string]any{"status": "ready", "page_count": 4, "archive_size": 987}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initialDataRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStoreWithState(initialDataRoot, stateRoot)
	if err != nil {
		t.Fatalf("startup touched unavailable archive root: %v", err)
	}
	defer reopened.Close()
	if got := reopened.ListReady(1, 10); len(got) != 1 || got[0].PageCount != 4 {
		t.Fatalf("cached list=%#v", got)
	}
	if got := reopened.Stats()["bytes"]; got != int64(987) {
		t.Fatalf("cached stats bytes=%v", got)
	}
}

func TestSplitStorePublishesZIPInDataRootAndStateInStateRoot(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	s, err := NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(42, "abc", map[string]any{"status": "pending"}); err != nil {
		t.Fatal(err)
	}
	if s.archivePath(42, "abc") != filepath.Join(dataRoot, "42.zip") {
		t.Fatalf("ZIP path %q", s.archivePath(42, "abc"))
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "42", stateName)); err != nil {
		t.Fatalf("state not in state root: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "index.snapshot")); err != nil {
		t.Fatalf("index snapshot missing: %v", err)
	}
	reopened, err := NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Get(42, "abc"); got == nil || got["status"] != "pending" {
		t.Fatalf("snapshot reload=%v", got)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestImportedPayloadLayoutsKeepTheirOriginalZIPPaths(t *testing.T) {
	for _, tc := range []struct{ name, relState, relZIP, state string }{
		{"versioned", "31/state.json", "31/archive.zip", `{"gid":31,"token":"v","layout":"versioned","root_gid":31,"root_token":"v","active_gid":31,"active_token":"v","versions":{"31:v":{"gid":31,"token":"v","storage_dir":"."}}}`},
		{"flat", "32/meta.json", "32/32.zip", `{"gid":32,"token":"f","layout":"flat","root_gid":32,"root_token":"f","active_gid":32,"active_token":"f"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataRoot, stateRoot := t.TempDir(), t.TempDir()
			writePath := filepath.Join(dataRoot, tc.relState)
			if err := os.MkdirAll(filepath.Dir(writePath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(writePath, []byte(tc.state), 0o600); err != nil {
				t.Fatal(err)
			}
			zipPath := filepath.Join(dataRoot, tc.relZIP)
			if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(zipPath)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			w, _ := zw.Create("p.jpg")
			_, _ = w.Write([]byte("p"))
			_ = zw.Close()
			_ = f.Close()
			if _, err := ImportSidecars(dataRoot, stateRoot); err != nil {
				t.Fatal(err)
			}
			store, err := NewStoreWithState(dataRoot, stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			gid := 31
			token := "v"
			if tc.name == "flat" {
				gid, token = 32, "f"
			}
			if got := store.archivePath(gid, token); got != zipPath {
				t.Fatalf("payload path=%q, want %q", got, zipPath)
			}
		})
	}
}

func TestSplitStoreRefusesUnownedFlatCollisionAndDeletesOnlyOwnedZIP(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	unowned := filepath.Join(dataRoot, "99.zip")
	if err := os.WriteFile(unowned, []byte("owned by someone else"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CanStart(99, "x"); err == nil {
		t.Fatal("CanStart accepted an unowned flat ZIP")
	}
	if _, err := s.Upsert(99, "x", map[string]any{"status": "pending"}); err == nil {
		t.Fatal("Upsert accepted an unowned flat ZIP")
	}
	if _, err := s.Upsert(100, "y", map[string]any{"status": "pending"}); err != nil {
		t.Fatal(err)
	}
	owned := s.archivePath(100, "y")
	if err := os.WriteFile(owned, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !s.Remove(100, "y") {
		t.Fatal("Remove failed")
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("owned payload remains: %v", err)
	}
	if _, err := os.Stat(unowned); err != nil {
		t.Fatalf("unowned payload was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "keep.txt")); err != nil {
		t.Fatalf("flat payload root content was removed: %v", err)
	}
}

func TestSplitPublishValidatesBeforeReplacingExistingZIP(t *testing.T) {
	dataRoot, stateRoot := t.TempDir(), t.TempDir()
	s, err := NewStoreWithState(dataRoot, stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Upsert(91, "publish", map[string]any{"status": "ready", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	target, publish, err := s.ArchivePublishPath(91, "publish")
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(dataRoot, "91.zip") || publish != target+".publish.tmp" {
		t.Fatalf("publish paths target=%q temp=%q", target, publish)
	}
	writeStoreTestZIP(t, target, "old.jpg", "old")
	if err := os.WriteFile(publish, []byte("invalid zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.PublishArchive(91, "publish", publish); err == nil {
		t.Fatal("invalid archive publication succeeded")
	}
	r, err := zip.OpenReader(target)
	if err != nil {
		t.Fatalf("old ZIP was lost: %v", err)
	}
	defer r.Close()
	if len(r.File) != 1 {
		t.Fatalf("old ZIP was replaced: entries=%d", len(r.File))
	}
}

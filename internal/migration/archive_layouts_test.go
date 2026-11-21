package migration

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pandaopds/internal/archive"
	"pandaopds/internal/document"
	"pandaopds/internal/storage"
)

var fixturePNG = []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}

type layoutVersion struct {
	gid                                   int
	token, storageDir, archivePath, title string
	cover                                 []byte
}

func writeLayoutFixture(t *testing.T, dataRoot, stateRoot string, rootGID int, rootToken, layout, payloadLayout string, versions []layoutVersion) {
	t.Helper()
	vm := map[string]any{}
	for _, v := range versions {
		vm[fmt.Sprintf("%d:%s", v.gid, v.token)] = map[string]any{"gid": v.gid, "token": v.token, "status": "ready", "storage_dir": v.storageDir, "state_file": "state.json", "gallery_file": "gallery.json", "archive_path": v.archivePath, "page_count": 1, "archive_size": int64(100), "or": "org"}
	}
	state := map[string]any{"gid": rootGID, "token": rootToken, "root_gid": rootGID, "root_token": rootToken, "active_gid": versions[len(versions)-1].gid, "active_token": versions[len(versions)-1].token, "status": "ready", "layout": layout, "versions": vm}
	if payloadLayout != "" {
		state["payload_layout"] = payloadLayout
	}
	rootDir := filepath.Join(stateRoot, strconv.Itoa(rootGID))
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(rootDir, "state.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	for _, v := range versions {
		dir := rootDir
		if v.storageDir != "" && v.storageDir != "." {
			dir = filepath.Join(stateRoot, v.storageDir)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		gallery, _ := json.Marshal(map[string]any{"gid": v.gid, "token": v.token, "title": v.title, "title_jpn": "作品", "category": "Manga", "uploader": "uploader", "cover_url": "https://example.test/cover.png", "page_count": 1, "rating": 4.5, "posted": 1700000000, "language": "English", "tags": map[string]any{"artist": []string{"tag"}}, "extension": map[string]any{"legacy": "keep"}})
		if err := os.WriteFile(filepath.Join(dir, "gallery.json"), gallery, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), v.cover, 0600); err != nil {
			t.Fatal(err)
		}
		if v.gid != rootGID {
			vs, _ := json.Marshal(map[string]any{"gid": v.gid, "token": v.token, "root_gid": rootGID, "root_token": rootToken, "layout": layout, "storage_dir": v.storageDir, "status": "ready"})
			if err := os.WriteFile(filepath.Join(dir, "state.json"), vs, 0600); err != nil {
				t.Fatal(err)
			}
		}
		zipPath := filepath.Join(dataRoot, filepath.FromSlash(v.archivePath))
		if err := os.MkdirAll(filepath.Dir(zipPath), 0755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		w, err := zw.Create("001.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(fixturePNG); err != nil {
			t.Fatal(err)
		}
		if err = zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func writeLayoutZIP(t *testing.T, path, entry string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
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
	if _, err = w.Write(fixturePNG); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestImportLayoutsLinkDocumentsAndRestartReadOnly(t *testing.T) {
	base := t.TempDir()
	dataRoot, stateRoot := filepath.Join(base, "files"), filepath.Join(base, "state")
	if err := os.MkdirAll(dataRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateRoot, 0755); err != nil {
		t.Fatal(err)
	}
	versions := []layoutVersion{
		{51, "flat-gid", ".", "51.zip", "flat root", bytes.Repeat([]byte{1}, 8)},
		{52, "flat-gid-v2", "52", "52.zip", "flat version", bytes.Repeat([]byte{2}, 8)},
		{61, "flat", ".", "61.zip", "flat", bytes.Repeat([]byte{3}, 8)},
		{71, "nested", ".", "71/archive.zip", "nested root", bytes.Repeat([]byte{4}, 8)},
		{72, "nested-v2", "72", "72/archive.zip", "nested version", bytes.Repeat([]byte{5}, 8)},
	}
	writeLayoutFixture(t, dataRoot, stateRoot, 51, "flat-gid", "versioned", "flat-gid", versions[:2])
	writeLayoutFixture(t, dataRoot, stateRoot, 61, "flat", "flat", "", versions[2:3])
	writeLayoutFixture(t, dataRoot, stateRoot, 71, "nested", "versioned", "", versions[3:])
	before := map[string][]byte{}
	_ = filepath.WalkDir(stateRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(stateRoot, path)
			b, e := os.ReadFile(path)
			if e == nil {
				before[rel] = b
			}
		}
		return nil
	})
	zipBefore := map[string][]byte{}
	for _, v := range versions {
		p := filepath.Join(dataRoot, filepath.FromSlash(v.archivePath))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		zipBefore[v.archivePath] = b
	}
	db, err := storage.Open(filepath.Join(base, "config", "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ImportLegacyArchive(db, dataRoot, stateRoot); err != nil {
		t.Fatal(err)
	}
	var versionCount, coverCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM archive_versions`).Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM covers`).Scan(&coverCount); err != nil {
		t.Fatal(err)
	}
	if versionCount != len(versions) || coverCount != len(versions) {
		t.Fatalf("imported versions=%d covers=%d", versionCount, coverCount)
	}
	for _, v := range versions {
		var rel, hash string
		var size, mtime int64
		if err = db.QueryRow(`SELECT archive_path,source_size,source_mtime_ns,source_sha256 FROM archive_versions av JOIN publications p ON p.id=av.publication_id WHERE p.gid=? AND p.token=?`, v.gid, v.token).Scan(&rel, &size, &mtime, &hash); err != nil {
			t.Fatal(err)
		}
		if rel != v.archivePath || size <= 0 || mtime <= 0 || hash != "" {
			t.Fatalf("source evidence gid=%d path=%q size=%d mtime=%d sha=%q", v.gid, rel, size, mtime, hash)
		}
	}
	// An earlier eager import can have source hashes while a new candidate
	// row has not been read; matching stat evidence remains sufficient to link.
	if _, err = db.Exec(`UPDATE archive_versions SET source_sha256='legacy-eager-hash' WHERE publication_id=(SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?)`, versions[0].gid, strings.ToLower(versions[0].token)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO progressions(target_type,target_id,gid,token,modified,device_json,progression,written) VALUES('eh','51/flat-gid',51,'flat-gid','2026-11-20T00:00:00Z','{}',0.6,12)`); err != nil {
		t.Fatal(err)
	}
	docs, err := document.New(db, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	var flatDoc string
	if err = db.QueryRow(`SELECT id FROM documents WHERE archive_path='61.zip'`).Scan(&flatDoc); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO progressions(target_type,target_id,modified,device_json,progression,written) VALUES('document',?,'2026-11-20T00:00:00Z','{}',0.25,5)`, flatDoc); err != nil {
		t.Fatal(err)
	}
	linked, err := LinkLegacyDocuments(db, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if linked != len(versions) {
		t.Fatalf("linked=%d want %d", linked, len(versions))
	}
	if again, err := LinkLegacyDocuments(db, dataRoot); err != nil || again != 0 {
		t.Fatalf("link retry=%d err=%v", again, err)
	}
	var linkedCount, ehTarget int
	if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE association_source='legacy_sidecar'`).Scan(&linkedCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE progression_target='eh'`).Scan(&ehTarget); err != nil {
		t.Fatal(err)
	}
	if linkedCount != len(versions) || ehTarget != len(versions)-1 {
		t.Fatalf("linked docs=%d EH target=%d", linkedCount, ehTarget)
	}
	var localTarget string
	if err = db.QueryRow(`SELECT progression_target FROM documents WHERE archive_path='51.zip'`).Scan(&localTarget); err != nil {
		t.Fatal(err)
	}
	if localTarget != "eh" {
		t.Fatalf("legacy EH progress target=%s", localTarget)
	}
	if err = db.QueryRow(`SELECT progression_target FROM documents WHERE id=?`, flatDoc).Scan(&localTarget); err != nil || localTarget != "document" {
		t.Fatalf("local progression target=%q err=%v", localTarget, err)
	}
	var localProgress float64
	var written int64
	if err = db.QueryRow(`SELECT progression,written FROM progressions WHERE target_type='document' AND target_id=?`, flatDoc).Scan(&localProgress, &written); err != nil || localProgress != .25 || written != 5 {
		t.Fatalf("local progression changed value=%v written=%d err=%v", localProgress, written, err)
	}
	store, err := archive.NewStoreWithSQLite(dataRoot, stateRoot, db.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, ok, err := store.GetPage(51, "flat-gid", 0)
	if err != nil || !ok || !bytes.Equal(page, fixturePNG) {
		t.Fatalf("old flat-gid page ok=%v err=%v", ok, err)
	}
	page, ok, err = store.GetPage(61, "flat", 0)
	if err != nil || !ok || !bytes.Equal(page, fixturePNG) {
		t.Fatalf("old flat page ok=%v err=%v", ok, err)
	}
	page, ok, err = store.GetPage(72, "nested-v2", 0)
	if err != nil || !ok || !bytes.Equal(page, fixturePNG) {
		t.Fatalf("old nested version page ok=%v err=%v", ok, err)
	}
	for _, v := range versions {
		b, err := os.ReadFile(filepath.Join(dataRoot, filepath.FromSlash(v.archivePath)))
		if err != nil || !bytes.Equal(b, zipBefore[v.archivePath]) {
			t.Fatalf("source ZIP changed gid=%d err=%v", v.gid, err)
		}
		var c []byte
		if err = db.QueryRow(`SELECT c.data FROM covers c JOIN publications p ON p.id=c.publication_id WHERE p.gid=? AND p.token=?`, v.gid, v.token).Scan(&c); err != nil || !bytes.Equal(c, v.cover) {
			t.Fatalf("cover changed gid=%d err=%v", v.gid, err)
		}
	}
	_ = filepath.WalkDir(stateRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(stateRoot, path)
			b, e := os.ReadFile(path)
			if e == nil {
				old, ok := before[rel]
				if !ok || !bytes.Equal(old, b) {
					t.Errorf("legacy sidecar changed: %s", rel)
				}
			}
		}
		return nil
	})
}

func TestImportLegacyArchiveRejectsMalformedSourceAndDoesNotMarkComplete(t *testing.T) {
	base := t.TempDir()
	files, state := filepath.Join(base, "files"), filepath.Join(base, "state")
	if err := os.MkdirAll(filepath.Join(state, "broken"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(files, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "broken", "state.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ImportLegacyArchive(db, files, state); err == nil {
		t.Fatal("malformed legacy state silently imported")
	}
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM legacy_imports WHERE name='archive_sidecars'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("import marker=%d err=%v", n, err)
	}
}

func TestImportLegacyArchiveRejectsConflictingIdentitiesWithoutMarker(t *testing.T) {
	base := t.TempDir()
	files, state := filepath.Join(base, "files"), filepath.Join(base, "state")
	for _, dir := range []string{files, filepath.Join(state, "510"), filepath.Join(state, "511")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"gid":510,"token":"same-token","root_gid":510,"root_token":"same-token","active_gid":510,"active_token":"same-token","layout":"versioned","versions":{"510:same-token":{"gid":510,"token":"same-token","storage_dir":"."}}}`
	for _, dir := range []string{filepath.Join(state, "510"), filepath.Join(state, "511")} {
		if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ImportLegacyArchive(db, files, state); err == nil {
		t.Fatal("conflicting source identities silently imported")
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM legacy_imports WHERE name='archive_sidecars'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("completion marker=%d err=%v", count, err)
	}
}

func TestLegacyDownloadStatesImportAsReviewOrFailedWithoutQueueing(t *testing.T) {
	base := t.TempDir()
	files, state := filepath.Join(base, "files"), filepath.Join(base, "state")
	if err := os.MkdirAll(files, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "600"), 0755); err != nil {
		t.Fatal(err)
	}
	versions := map[string]any{
		"600:pendingtoken": map[string]any{"gid": 600, "token": "pendingtoken", "status": "downloading", "storage_dir": ".", "download_url": "https://example.test/pending.zip", "or": "res", "error": "network interrupted"},
		"601:failedtoken":  map[string]any{"gid": 601, "token": "failedtoken", "status": "failed", "storage_dir": "601", "download_url": "https://example.test/failed.zip", "quality": "org", "error": "old download error"},
		"602:upgradetoken": map[string]any{"gid": 602, "token": "upgradetoken", "status": "ready", "storage_dir": "602", "is_version_update": true, "download_url": "https://example.test/upgrade.zip", "or": "res"},
		"603:readytoken":   map[string]any{"gid": 603, "token": "readytoken", "status": "ready", "storage_dir": "603", "or": "org"},
	}
	root := map[string]any{"gid": 600, "token": "pendingtoken", "root_gid": 600, "root_token": "pendingtoken", "active_gid": 600, "active_token": "pendingtoken", "status": "downloading", "layout": "versioned", "versions": versions}
	body, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(state, "600", "state.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ImportLegacyArchive(db, files, state); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM download_jobs`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("legacy jobs=%d err=%v", count, err)
	}
	rows, err := db.Query(`SELECT gid,status,stage,quality,intent_json,error FROM download_jobs ORDER BY gid`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]struct{ status, stage, quality, intent, error string }{}
	for rows.Next() {
		var gid int
		var x struct{ status, stage, quality, intent, error string }
		if err = rows.Scan(&gid, &x.status, &x.stage, &x.quality, &x.intent, &x.error); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		got[gid] = x
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if x := got[600]; x.status != "needs_review" || x.stage != "needs_review" || x.quality != "res" || x.error != "network interrupted" || !strings.Contains(x.intent, "pending.zip") {
		t.Fatalf("pending migration=%+v", x)
	}
	if x := got[601]; x.status != "failed" || x.error != "old download error" || x.quality != "org" {
		t.Fatalf("failed migration=%+v", x)
	}
	if x := got[602]; x.status != "needs_review" || !strings.Contains(x.intent, "is_version_update") {
		t.Fatalf("upgrade migration=%+v", x)
	}
	var queued int
	if err = db.QueryRow(`SELECT COUNT(*) FROM download_jobs WHERE status IN ('queued','running')`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("import queued/running jobs=%d err=%v", queued, err)
	}
	if err = ImportLegacyArchive(db, files, state); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM download_jobs`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("reimport duplicated jobs count=%d err=%v", count, err)
	}
	var sources int
	if err = db.QueryRow(`SELECT COUNT(*) FROM download_job_sources WHERE source='legacy_import'`).Scan(&sources); err != nil || sources != 3 {
		t.Fatalf("legacy job sources=%d err=%v", sources, err)
	}
}

func TestLegacyDownloadImportRetriesAcrossWorkTransactionsWithoutDuplicates(t *testing.T) {
	base := t.TempDir()
	files, state := filepath.Join(base, "files"), filepath.Join(base, "state")
	if err := os.MkdirAll(files, 0755); err != nil {
		t.Fatal(err)
	}
	for _, gid := range []int{609, 610} {
		dir := filepath.Join(state, strconv.Itoa(gid))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		token := fmt.Sprintf("token%d", gid)
		meta := map[string]any{"gid": gid, "token": token, "root_gid": gid, "root_token": token, "active_gid": gid, "active_token": token, "status": "downloading", "layout": "versioned", "versions": map[string]any{fmt.Sprintf("%d:%s", gid, token): map[string]any{"gid": gid, "token": token, "status": "zipping", "storage_dir": ".", "download_url": "https://example.test/pending.zip", "or": "org"}}}
		b, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "state.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER fail_legacy_job BEFORE INSERT ON download_jobs WHEN NEW.gid=610 BEGIN SELECT RAISE(ABORT,'injected second work failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = ImportLegacyArchive(db, files, state); err == nil {
		t.Fatal("expected injected partial import error")
	}
	var jobs int
	if err = db.QueryRow(`SELECT COUNT(*) FROM download_jobs`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("partial jobs=%d err=%v", jobs, err)
	}
	var marker int
	if err = db.QueryRow(`SELECT COUNT(*) FROM legacy_imports WHERE name='archive_sidecars'`).Scan(&marker); err != nil || marker != 0 {
		t.Fatalf("partial marker=%d err=%v", marker, err)
	}
	if _, err = db.Exec(`DROP TRIGGER fail_legacy_job`); err != nil {
		t.Fatal(err)
	}
	if err = ImportLegacyArchive(db, files, state); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM download_jobs`).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatalf("retried jobs=%d err=%v", jobs, err)
	}
	var sources int
	if err = db.QueryRow(`SELECT COUNT(*) FROM download_job_sources WHERE source='legacy_import'`).Scan(&sources); err != nil || sources != 2 {
		t.Fatalf("retried sources=%d err=%v", sources, err)
	}
}

func TestLinkLegacyDocumentsRejectsChangedPayloadManualConflictAndPathCollision(t *testing.T) {
	for _, mode := range []string{"changed", "manual", "path_collision"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			files, state := filepath.Join(base, "files"), filepath.Join(base, "state")
			if err := os.MkdirAll(files, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(state, 0755); err != nil {
				t.Fatal(err)
			}
			writeLayoutFixture(t, files, state, 88, "legacy", "versioned", "", []layoutVersion{{88, "legacy", ".", "88/archive.zip", "old", bytes.Repeat([]byte{8}, 8)}})
			db, err := storage.Open(filepath.Join(base, "db.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = ImportLegacyArchive(db, files, state); err != nil {
				t.Fatal(err)
			}
			if mode == "path_collision" {
				var workID int64
				if err = db.QueryRow(`SELECT id FROM archive_works WHERE gid=88 AND token='legacy'`).Scan(&workID); err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,created_at,updated_at)VALUES('eh',889,'different','{}','2026-11-20','2026-11-20')`); err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec(`INSERT INTO archive_versions(publication_id,work_id,status,quality,archive_path,state_path,state_json)SELECT id,?,'ready','org','88/archive.zip','88/state.json','{}' FROM publications WHERE provider='eh' AND gid=889`, workID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "changed" {
				writeLayoutZIP(t, filepath.Join(files, "88", "archive.zip"), "replacement.png")
			}
			docs, err := document.New(db, files)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = docs.Scan(t.Context()); err != nil {
				t.Fatal(err)
			}
			if mode == "manual" {
				var id string
				if err = db.QueryRow(`SELECT id FROM documents WHERE archive_path='88/archive.zip'`).Scan(&id); err != nil {
					t.Fatal(err)
				}
				if err = docs.Associate(t.Context(), id, 900, "manual-token"); err != nil {
					t.Fatal(err)
				}
			}
			_, linkErr := LinkLegacyDocuments(db, files)
			if mode == "path_collision" && linkErr == nil {
				t.Fatal("linking accepted ambiguous path identity")
			}
			if mode != "path_collision" && linkErr != nil {
				t.Fatalf("nonfatal changed/conflicting source blocked scan linking: %v", linkErr)
			}
			var marker int
			if err = db.QueryRow(`SELECT COUNT(*) FROM legacy_imports WHERE name=?`, archiveDocumentLinksMarker).Scan(&marker); err != nil || marker != 0 {
				if mode == "path_collision" && (err != nil || marker != 0) {
					t.Fatalf("link marker=%d err=%v", marker, err)
				}
			}
			if mode != "path_collision" && (err != nil || marker != 1) {
				t.Fatalf("link marker=%d err=%v", marker, err)
			}
			var unlinked int
			if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE association_source='legacy_sidecar'`).Scan(&unlinked); err != nil || unlinked != 0 {
				t.Fatalf("partial associations persisted=%d err=%v", unlinked, err)
			}
		})
	}
}

func TestLinkLegacyDocumentsSkipsReadyPayloadMissingAtImport(t *testing.T) {
	base := t.TempDir()
	files := filepath.Join(base, "archive")
	states := filepath.Join(base, "state")
	versions := []layoutVersion{
		{gid: 991, token: "present", storageDir: "991", archivePath: "991/present.zip", title: "Present", cover: fixturePNG},
		{gid: 992, token: "stale", storageDir: "992", archivePath: "992/stale.zip", title: "Stale", cover: fixturePNG},
	}
	writeLayoutFixture(t, files, states, 991, "present", "nested", "nested", versions)
	if err := os.Remove(filepath.Join(files, "992", "stale.zip")); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(base, "config", "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ImportLegacyArchive(db, files, states); err != nil {
		t.Fatal(err)
	}
	docs, err := document.New(db, files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	linked, err := LinkLegacyDocuments(db, files)
	if err != nil {
		t.Fatalf("link should tolerate a payload absent at source import: %v", err)
	}
	if linked != 1 {
		t.Fatalf("linked=%d want 1", linked)
	}
	var linkedCount, staleDocs int
	if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE association_source='legacy_sidecar'`).Scan(&linkedCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE archive_path='992/stale.zip'`).Scan(&staleDocs); err != nil {
		t.Fatal(err)
	}
	if linkedCount != 1 || staleDocs != 0 {
		t.Fatalf("linked=%d stale documents=%d", linkedCount, staleDocs)
	}
	var details string
	if err = db.QueryRow(`SELECT details_json FROM legacy_imports WHERE name=?`, archiveDocumentLinksMarker).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(details, `"missing_at_import":1`) {
		t.Fatalf("link marker omits missing history: %s", details)
	}
	if retry, err := LinkLegacyDocuments(db, files); err != nil || retry != 0 {
		t.Fatalf("retry linked=%d err=%v", retry, err)
	}
}

func TestFlatGIDLegacyMetadataReconcilesRepeatedlyWithoutRebindingReplacement(t *testing.T) {
	base := t.TempDir()
	files, states := filepath.Join(base, "archive"), filepath.Join(base, "state")
	const gid = 1726517
	const token = "abc123"
	if err := os.MkdirAll(filepath.Join(states, strconv.Itoa(gid)), 0755); err != nil {
		t.Fatal(err)
	}
	writeLayoutZIP(t, filepath.Join(files, fmt.Sprintf("%d.zip", gid)), "001.png")
	state := fmt.Sprintf(`{"gid":%d,"token":"%s","root_gid":%d,"root_token":"%s","payload_layout":"flat-gid","status":"ready","archive_size":14243223,"page_count":24,"or":"res"}`, gid, token, gid, token)
	if err := os.WriteFile(filepath.Join(states, strconv.Itoa(gid), "state.json"), []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
	gallery := fmt.Sprintf(`{"gid":%d,"token":"%s","title":"Flat legacy","page_count":24}`, gid, token)
	if err := os.WriteFile(filepath.Join(states, strconv.Itoa(gid), "gallery.json"), []byte(gallery), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(states, strconv.Itoa(gid), "cover.jpg"), fixturePNG, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(base, "config", "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ImportLegacyArchive(db, files, states); err != nil {
		t.Fatal(err)
	}
	var path string
	var sourceSize, sourceMTime int64
	if err = db.QueryRow(`SELECT archive_path,source_size,source_mtime_ns FROM archive_versions av JOIN publications p ON p.id=av.publication_id WHERE p.gid=? AND p.token=?`, gid, token).Scan(&path, &sourceSize, &sourceMTime); err != nil {
		t.Fatal(err)
	}
	if path != fmt.Sprintf("%d.zip", gid) || sourceSize <= 0 || sourceMTime <= 0 {
		t.Fatalf("legacy flat path/evidence path=%q size=%d mtime=%d", path, sourceSize, sourceMTime)
	}
	// An early reconciliation must remain retryable after the explicit scan.
	if linked, err := LinkLegacyDocuments(db, files); err != nil || linked != 0 {
		t.Fatalf("pre-scan reconciliation linked=%d err=%v", linked, err)
	}
	if diagnostics, err := LegacyDocumentLinkDiagnostics(db); err != nil || diagnostics["no_document"] != 1 {
		t.Fatalf("pre-scan diagnostics=%v err=%v", diagnostics, err)
	}
	docs, err := document.New(db, files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if linked, err := LinkLegacyDocuments(db, files); err != nil || linked != 1 {
		t.Fatalf("post-scan reconciliation linked=%d err=%v", linked, err)
	}
	var oldID string
	if err = db.QueryRow(`SELECT id FROM documents WHERE archive_path=? AND status='available'`, path).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	var source, progressionTarget string
	if err = db.QueryRow(`SELECT association_source,progression_target FROM documents WHERE id=?`, oldID).Scan(&source, &progressionTarget); err != nil || source != "legacy_sidecar" || progressionTarget != "eh" {
		t.Fatalf("legacy link source=%q target=%q err=%v", source, progressionTarget, err)
	}
	if linked, err := LinkLegacyDocuments(db, files); err != nil || linked != 0 {
		t.Fatalf("idempotent reconciliation linked=%d err=%v", linked, err)
	}
	if err = docs.Associate(t.Context(), oldID, 0, ""); err != nil {
		t.Fatal(err)
	}
	if linked, err := LinkLegacyDocuments(db, files); err != nil || linked != 0 {
		t.Fatalf("reconciliation restored a manually removed association: linked=%d err=%v", linked, err)
	}
	var associationSource, currentTarget string
	var currentPub sql.NullInt64
	if err = db.QueryRow(`SELECT publication_id,association_source,progression_target FROM documents WHERE id=?`, oldID).Scan(&currentPub, &associationSource, &currentTarget); err != nil {
		t.Fatal(err)
	}
	if currentPub.Valid || associationSource != "manual" || currentTarget != "eh" {
		t.Fatalf("manual unlink/frozen progression changed pub=%v source=%q target=%q", currentPub, associationSource, currentTarget)
	}
	if err = os.Chtimes(filepath.Join(files, path), time.Now(), time.Now().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if linked, err := LinkLegacyDocuments(db, files); err != nil || linked != 0 {
		t.Fatalf("replacement reconciliation linked=%d err=%v", linked, err)
	}
	var newID string
	if err = db.QueryRow(`SELECT id FROM documents WHERE archive_path=? AND status='available'`, path).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	if newID == oldID {
		t.Fatal("changed signature reused the previous document UUID")
	}
	var newSource sql.NullString
	var oldStatus string
	if err = db.QueryRow(`SELECT association_source FROM documents WHERE id=?`, newID).Scan(&newSource); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT status FROM documents WHERE id=?`, oldID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if newSource.Valid || oldStatus != "missing" {
		t.Fatalf("replacement inherited association source=%q old status=%q", newSource.String, oldStatus)
	}
	diagnostics, err := LegacyDocumentLinkDiagnostics(db)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics["stale_mtime"] != 1 || diagnostics["linked"] != 0 {
		t.Fatalf("replacement link diagnostics=%v", diagnostics)
	}
}

func TestLegacyImportDoesNotOpenArchivePayloads(t *testing.T) {
	base := t.TempDir()
	files := filepath.Join(base, "archive")
	states := filepath.Join(base, "state")
	versions := []layoutVersion{{gid: 1201, token: "readonly", storageDir: "1201", archivePath: "1201/archive.zip", title: "Read only", cover: fixturePNG}}
	writeLayoutFixture(t, files, states, 1201, "readonly", "nested", "nested", versions)
	zipPath := filepath.Join(files, "1201", "archive.zip")
	if err := os.Chmod(zipPath, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(zipPath, 0600) })
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ImportLegacyArchive(db, files, states); err != nil {
		t.Fatalf("metadata import attempted to read archive payload: %v", err)
	}
	var size int64
	var hash string
	if err = db.QueryRow(`SELECT source_size,source_sha256 FROM archive_versions`).Scan(&size, &hash); err != nil || size <= 0 || hash != "" {
		t.Fatalf("source stat size=%d hash=%q err=%v", size, hash, err)
	}
	if err = ImportLegacyArchive(db, files, states); err != nil {
		t.Fatalf("idempotent import opened payload: %v", err)
	}
}

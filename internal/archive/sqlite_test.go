package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pandaopds/internal/storage"
)

var sqliteTestPNG = []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}

func openSQLiteArchiveTest(t *testing.T) (*Store, *storage.DB, string, string) {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "archive")
	state := filepath.Join(base, "config", "archive-state")
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(base, "config", "panda.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewStoreWithSQLite(data, state, db.DB)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(); _ = db.Close() })
	return s, db, data, state
}

func writeSQLiteZIP(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("page.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteArchiveMetadataCoverActivationResetAndRestart(t *testing.T) {
	s, db, data, _ := openSQLiteArchiveTest(t)
	if _, err := s.Upsert(71, "root-token", map[string]any{"status": "ready", "title": "root", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	rootZIP := s.archivePath(71, "root-token")
	writeSQLiteZIP(t, rootZIP, "root-page")
	metadata := map[string]any{"gid": 71, "token": "root-token", "title": "Original title", "title_jpn": "原題", "category": "Manga", "uploader": "artist", "cover_url": "https://example.test/c.png", "page_count": 9, "rating": 4.5, "posted": 1700000000, "language": "English", "tags": map[string]any{"artist": []any{"tag"}}, "extra": map[string]any{"keep": true}}
	if err := s.WriteMetadata(71, "root-token", metadata); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteCover(71, "root-token", sqliteTestPNG); err != nil {
		t.Fatal(err)
	}
	var title, titleJPN, category, uploader, coverURL, language, tags string
	var pageCount int
	var rating float64
	if err := db.QueryRow(`SELECT title,title_jpn,category,uploader,cover_url,page_count,rating,language,tags_json FROM publications WHERE gid=71 AND token='root-token'`).Scan(&title, &titleJPN, &category, &uploader, &coverURL, &pageCount, &rating, &language, &tags); err != nil {
		t.Fatal(err)
	}
	if title != "Original title" || titleJPN != "原題" || category != "Manga" || uploader != "artist" || coverURL != "https://example.test/c.png" || pageCount != 9 || rating != 4.5 || language != "English" || tags == "[]" {
		t.Fatalf("structured publication fields: %q %q %q %q %q %d %.1f %q %s", title, titleJPN, category, uploader, coverURL, pageCount, rating, language, tags)
	}
	var cover []byte
	var mime string
	if err := db.QueryRow(`SELECT data,mime FROM covers c JOIN publications p ON p.id=c.publication_id WHERE p.gid=71 AND p.token='root-token'`).Scan(&cover, &mime); err != nil {
		t.Fatal(err)
	}
	if string(cover) != string(sqliteTestPNG) || mime != "image/png" {
		t.Fatalf("cover mime=%q bytes=%d", mime, len(cover))
	}
	if _, err := s.Upsert(72, "new-token", map[string]any{"root_gid": 71, "root_token": "root-token", "is_version_update": true, "status": "ready", "title": "new", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	newZIP := s.archivePath(72, "new-token")
	writeSQLiteZIP(t, newZIP, "new-page")
	newSnapshot := map[string]any{"gid": 72, "token": "new-token", "title": "Activated title", "category": "Doujinshi", "page_count": 1, "rating": 4.9, "tags": map[string]any{"artist": []any{"new-tag"}}}
	if err := s.ActivateVersion(71, "root-token", 72, "new-token", newSnapshot, sqliteTestPNG, true); err != nil {
		t.Fatal(err)
	}
	if gid, token := s.ResolveActive(71, "root-token"); gid != 72 || token != "new-token" {
		t.Fatalf("active=(%d,%s)", gid, token)
	}
	if _, err := os.Stat(rootZIP); !os.IsNotExist(err) {
		t.Fatalf("old zip remains after valid activation: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := storage.Open(filepath.Join(filepath.Dir(data), "config", "panda.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	restarted, err := NewStoreWithSQLite(data, filepath.Join(filepath.Dir(data), "config", "archive-state"), db2.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	page, ok, err := restarted.GetPage(72, "new-token", 0)
	if err != nil || !ok || string(page) != "new-page" {
		t.Fatalf("restart page=%q ok=%v err=%v", page, ok, err)
	}
	meta, err := restarted.ReadMetadata(72, "new-token")
	if err != nil || meta["title"] != "Activated title" || meta["extra"] != nil {
		t.Fatalf("restart metadata=%v err=%v", meta, err)
	}
	cover, ok = restarted.Cover(72, "new-token")
	if !ok || string(cover) != string(sqliteTestPNG) {
		t.Fatalf("restart cover bytes=%d ok=%v", len(cover), ok)
	}
	if err := restarted.ResetToUnarchivedWithError(71, "root-token"); err != nil {
		t.Fatal(err)
	}
	if st := restarted.Get(71, "root-token"); st == nil || st["status"] != "unarchived" {
		t.Fatalf("reset state=%v", st)
	}
	if _, err := os.Stat(newZIP); !os.IsNotExist(err) {
		t.Fatalf("reset left payload: %v", err)
	}
	var suppressed int
	if err:=db2.QueryRow(`SELECT COUNT(*) FROM download_suppressions WHERE gid IN (71,72)`).Scan(&suppressed);err!=nil||suppressed!=2{t.Fatalf("reset suppressions=%d err=%v",suppressed,err)}
	if _, err := os.Stat(filepath.Join(data, "71", stateName)); !os.IsNotExist(err) {
		t.Fatalf("SQLite mode wrote legacy sidecar: %v", err)
	}
}

func TestSQLiteUpsertAndActivateRollbackMemoryOnDBFailure(t *testing.T) {
	s, db, _, _ := openSQLiteArchiveTest(t)
	if _, err := db.Exec(`CREATE TRIGGER fail_archive_version BEFORE INSERT ON archive_versions BEGIN SELECT RAISE(ABORT,'injected version insert'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(81, "failed", map[string]any{"status": "ready"}); err == nil {
		t.Fatal("expected injected Upsert failure")
	}
	if s.rootLocked(81, "failed") != nil || len(s.ListReady(1, 10)) != 0 {
		t.Fatal("failed Upsert remained visible in memory")
	}
	var works int
	if err := db.QueryRow(`SELECT COUNT(*) FROM archive_works WHERE gid=81`).Scan(&works); err != nil || works != 0 {
		t.Fatalf("failed Upsert DB rows=%d err=%v", works, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_archive_version`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(82, "root", map[string]any{"status": "ready", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeSQLiteZIP(t, s.archivePath(82, "root"), "old")
	if _, err := s.Upsert(83, "next", map[string]any{"root_gid": 82, "root_token": "root", "is_version_update": true, "status": "ready", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeSQLiteZIP(t, s.archivePath(83, "next"), "new")
	if _, err := db.Exec(`CREATE TRIGGER fail_activation_finalize BEFORE UPDATE ON archive_works WHEN new.active_gid=83 BEGIN SELECT RAISE(ABORT,'injected finalize'); END`); err != nil {
		t.Fatal(err)
	}
	err := s.ActivateVersion(82, "root", 83, "next", map[string]any{"gid": 83, "token": "next", "title": "new"}, []byte("cover"), true)
	if err == nil {
		t.Fatal("expected activation finalization failure")
	}
	if gid, token := s.ResolveActive(82, "root"); gid != 82 || token != "root" {
		t.Fatalf("failed activation claimed active=(%d,%s)", gid, token)
	}
	var raw string
	if err = db.QueryRow(`SELECT raw_state_json FROM archive_works WHERE gid=82`).Scan(&raw); err != nil || !strings.Contains(raw, "activation_pending") {
		t.Fatalf("pending activation not durable raw=%s err=%v", raw, err)
	}
	if _, err = db.Exec(`DROP TRIGGER fail_activation_finalize`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(s.DataRoot)
	db2, err := storage.Open(filepath.Join(base, "config", "panda.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	restarted, err := NewStoreWithSQLite(s.DataRoot, filepath.Join(base, "config", "archive-state"), db2.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if gid, token := restarted.ResolveActive(82, "root"); gid != 83 || token != "next" {
		t.Fatalf("recovered activation=(%d,%s)", gid, token)
	}
	if _, err = os.Stat(filepath.Join(s.DataRoot, "82.zip")); !os.IsNotExist(err) {
		t.Fatalf("recovery left old zip: %v", err)
	}
}

func TestSQLiteDeleteIntentRecoversPartialDeleteAndProtectsReplacement(t *testing.T) {
	s, db, data, _ := openSQLiteArchiveTest(t)
	if _, err := s.Upsert(91, "root", map[string]any{"status": "ready", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeSQLiteZIP(t, s.archivePath(91, "root"), "root")
	if _, err := s.Upsert(92, "next", map[string]any{"root_gid": 91, "root_token": "root", "is_version_update": true, "status": "ready", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	nextPath := s.archivePath(92, "next")
	writeSQLiteZIP(t, nextPath, "next")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, row := range []struct{ id, path string }{{"doc91", "91.zip"}, {"doc92", "92.zip"}} {
		if _, err := db.Exec(`INSERT INTO documents(id,archive_path,format,status,created_at,updated_at) VALUES(?,?,'zip','available',?,?)`, row.id, row.path, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_delete_progress BEFORE UPDATE ON archive_delete_payloads WHEN old.archive_path='91.zip' BEGIN SELECT RAISE(ABORT,'injected delete progress'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveWithError(91, "root"); err == nil {
		t.Fatal("expected deletion progress failure")
	}
	if state := s.Get(91, "root"); state == nil || state["status"] != "ready" {
		t.Fatalf("failed partial delete mutated shelf state: %v", state)
	}
	if _, err := s.Upsert(93, "later-version", map[string]any{"root_gid": 91, "root_token": "root", "is_version_update": true, "status": "ready"}); err == nil {
		t.Fatal("accepted a new path while the fixed deletion intent was pending")
	}
	if _, err := os.Stat(filepath.Join(data, "91.zip")); !os.IsNotExist(err) {
		t.Fatalf("first payload should have been removed: %v", err)
	}
	if _, err := os.Stat(nextPath); err != nil {
		t.Fatalf("remaining payload removed after first failure: %v", err)
	}
	var suppression int
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_suppressions WHERE gid IN (91,92)`).Scan(&suppression); err != nil || suppression != 2 {
		t.Fatalf("suppression rows=%d err=%v", suppression, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_delete_progress`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(data)
	db2, err := storage.Open(filepath.Join(base, "config", "panda.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	restarted, err := NewStoreWithSQLite(data, filepath.Join(base, "config", "archive-state"), db2.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.Get(91, "root") != nil {
		t.Fatal("removed work returned after recovery")
	}
	for _, id := range []string{"doc91", "doc92"} {
		var status string
		if err := db2.QueryRow(`SELECT status FROM documents WHERE id=?`, id).Scan(&status); err != nil || status != "missing" {
			t.Fatalf("deleted payload document %s status=%q err=%v", id, status, err)
		}
	}
	if _, err := os.Stat(nextPath); !os.IsNotExist(err) {
		t.Fatalf("recovery left pending version: %v", err)
	}
	var intentStatus string
	if err := db2.QueryRow(`SELECT status FROM archive_delete_intents ORDER BY created_at DESC LIMIT 1`).Scan(&intentStatus); err != nil || intentStatus != "complete" {
		t.Fatalf("intent status=%q err=%v", intentStatus, err)
	}
}

func TestSQLiteDeleteIntentPreservesReplacementForReview(t *testing.T) {
	s, db, data, _ := openSQLiteArchiveTest(t)
	if _, err := s.Upsert(101, "root", map[string]any{"status": "ready"}); err != nil {
		t.Fatal(err)
	}
	path := s.archivePath(101, "root")
	writeSQLiteZIP(t, path, "old")
	if _, err := db.Exec(`CREATE TRIGGER fail_delete_progress BEFORE UPDATE ON archive_delete_payloads BEGIN SELECT RAISE(ABORT,'injected delete progress'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveWithError(101, "root"); err == nil {
		t.Fatal("expected injected failure")
	}
	writeSQLiteZIP(t, path, "new replacement")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(data)
	db2, err := storage.Open(filepath.Join(base, "config", "panda.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	restarted, err := NewStoreWithSQLite(data, filepath.Join(base, "config", "archive-state"), db2.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		t.Fatalf("replacement file was removed: %v", err)
	}
	var status string
	if err := db2.QueryRow(`SELECT status FROM archive_delete_intents ORDER BY created_at DESC LIMIT 1`).Scan(&status); err != nil || status != "needs_review" {
		t.Fatalf("intent status=%q err=%v", status, err)
	}
}

func TestSQLiteRemoveRefusesSymlinkedPayloadComponents(t *testing.T) {
	s, _, data, _ := openSQLiteArchiveTest(t)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "evil.zip")
	writeSQLiteZIP(t, outsideFile, "valuable")
	if err := os.Symlink(outside, filepath.Join(data, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := s.Upsert(111, "root", map[string]any{"status": "ready", "archive_path": "linked/evil.zip"}); err != nil {
		t.Fatal(err)
	}
	err := s.RemoveWithError(111, "root")
	if err == nil {
		t.Fatal("removed a symlinked archive component")
	}
	if _, err = os.Stat(outsideFile); err != nil {
		t.Fatalf("outside payload was touched: %v", err)
	}
	var intents int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM archive_delete_intents WHERE root_gid=111`).Scan(&intents); err != nil || intents != 0 {
		t.Fatalf("unsafe path created delete intent=%d err=%v", intents, err)
	}
}

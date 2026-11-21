package document

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"pandaopds/internal/storage"
)

var onePixelPNG = []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}

func writeArchive(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, b := range entries {
		w, e := zw.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(b); e != nil {
			t.Fatal(e)
		}
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func solidPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, c)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "cfg", "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	root := filepath.Join(dir, "archive")
	if err = os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	s, err := New(db, root)
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

func TestScanRegistersCandidateAndFirstReadBuildsPageIndex(t *testing.T) {
	s, root := newTestStore(t)
	writeArchive(t, filepath.Join(root, "unknown.cbz"), map[string][]byte{"10.png": onePixelPNG, "2.png": onePixelPNG})
	opens := 0
	s.payloadOpenHook = func(string) { opens++ }
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "complete" {
		t.Fatalf("scan result=%v", result)
	}
	items, total, err := s.List(context.Background(), 1, 20, "", "false")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("documents=%d total=%d", len(items), total)
	}
	if items[0].GID != nil || items[0].PageCount != 0 || items[0].SHA256 != "" || opens != 0 {
		t.Fatalf("document=%+v", items[0])
	}
	page, mime, err := s.OpenPage(context.Background(), items[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" || len(page) == 0 {
		t.Fatalf("mime=%q bytes=%d", mime, len(page))
	}
	indexed, err := s.ListByID(context.Background(), items[0].ID)
	if err != nil || indexed.PageCount != 2 || indexed.SHA256 != "" {
		t.Fatalf("lazy index changed identity/hash: %+v err=%v", indexed, err)
	}
	before := opens
	if _, err = s.PageMIMEs(context.Background(), indexed.ID); err != nil || opens != before+1 {
		t.Fatalf("cached index verification should stat the current file without rebuilding it: opens=%d before=%d err=%v", opens, before, err)
	}
}

type failRangeReaderAt struct {
	raw       []byte
	failStart int64
	failEnd   int64
}

func (r failRangeReaderAt) ReadAt(p []byte, off int64) (int, error) {
	end := off + int64(len(p))
	if off < r.failEnd && end > r.failStart {
		return 0, syscall.EIO
	}
	if off >= int64(len(r.raw)) {
		return 0, io.EOF
	}
	n := copy(p, r.raw[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestLazyIndexReadFailureDoesNotPersistPartialIndexAndCanRetry(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "network.cbz")
	var raw bytes.Buffer
	zw := zip.NewWriter(&raw)
	for _, name := range []string{"1.png", "2.png"} {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(onePixelPNG); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	// Locate the second member's stored bytes using its central-directory
	// record (the streaming writer uses data descriptors in local headers).
	var failStart, failEnd int64
	for off := 0; off+46 <= raw.Len(); off++ {
		b := raw.Bytes()[off:]
		if binary.LittleEndian.Uint32(b[:4]) != 0x02014b50 {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint16(b[28:30]))
		extraLen := int(binary.LittleEndian.Uint16(b[30:32]))
		commentLen := int(binary.LittleEndian.Uint16(b[32:34]))
		if off+46+nameLen+extraLen+commentLen > raw.Len() {
			continue
		}
		name := string(raw.Bytes()[off+46 : off+46+nameLen])
		if name != "2.png" {
			continue
		}
		localOffset := int(binary.LittleEndian.Uint32(b[42:46]))
		local := raw.Bytes()[localOffset:]
		localNameLen := int(binary.LittleEndian.Uint16(local[26:28]))
		localExtraLen := int(binary.LittleEndian.Uint16(local[28:30]))
		failStart = int64(localOffset + 30 + localNameLen + localExtraLen)
		failEnd = failStart + int64(binary.LittleEndian.Uint32(b[20:24]))
		break
	}
	if failEnd <= failStart {
		t.Fatal("could not locate second ZIP member")
	}
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstID(t, s)
	s.zipReaderHook = func(_ *os.File, size int64) (*zip.Reader, error) {
		return zip.NewReader(failRangeReaderAt{raw: raw.Bytes(), failStart: failStart, failEnd: failEnd}, size)
	}
	if _, _, err := s.EnsurePageIndex(context.Background(), id); ValidationReason(err) != "io_error" || !errors.Is(err, syscall.EIO) || ValidationCauseCode(err) != "io_error" {
		t.Fatalf("expected retryable I/O error preserving EIO, got %v cause=%s", err, ValidationCauseCode(err))
	}
	var status string
	var pageCount, indexed int
	if err := s.db.QueryRow(`SELECT status,page_count FROM documents WHERE id=?`, id).Scan(&status, &pageCount); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM document_pages WHERE document_id=?`, id).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if status != "available" || pageCount != 0 || indexed != 0 {
		t.Fatalf("partial read changed candidate status=%s page_count=%d indexed=%d", status, pageCount, indexed)
	}
	s.zipReaderHook = nil
	indexedDoc, pages, err := s.EnsurePageIndex(context.Background(), id)
	if err != nil || indexedDoc.PageCount != 2 || len(pages) != 2 {
		t.Fatalf("explicit retry failed doc=%+v pages=%d err=%v", indexedDoc, len(pages), err)
	}
}

func TestRegisterPublishedBindsNewDocumentWithoutScanningOtherPaths(t *testing.T) {
	s, root := newTestStore(t)
	otherPath := filepath.Join(root, "other.zip")
	targetPath := filepath.Join(root, "target.cbz")
	writeArchive(t, otherPath, map[string][]byte{"1.png": onePixelPNG})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var otherID string
	if err := s.db.QueryRow(`SELECT id FROM documents WHERE archive_path='other.zip'`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(otherPath); err != nil {
		t.Fatal(err)
	}
	writeArchive(t, targetPath, map[string][]byte{"1.png": onePixelPNG})
	registered, err := s.RegisterPublished(context.Background(), "target.cbz", 321, "AbCd0123", "download_job")
	if err != nil {
		t.Fatal(err)
	}
	if registered.GID == nil || *registered.GID != 321 || registered.Token != "abcd0123" || registered.AssociationSource != "download_job" {
		t.Fatalf("registered identity=%+v", registered)
	}
	if registered.ProgressionTarget != "eh" || registered.ProgressionGID != 321 || registered.ProgressionToken != "abcd0123" {
		t.Fatalf("new document progression target=%+v", registered)
	}
	other, err := s.ListByID(context.Background(), otherID)
	if err != nil || other.Status != "available" {
		t.Fatalf("registration scanned/marked another row: %+v err=%v", other, err)
	}
	var pageCount int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM document_pages WHERE document_id=?`, registered.ID).Scan(&pageCount); err != nil || pageCount != 0 || registered.PageCount != 0 || registered.SHA256 != "" {
		t.Fatalf("page index=%d err=%v", pageCount, err)
	}
}

func TestRegisterPublishedPreservesExistingLocalProgressionAndAssociation(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "existing.zip")
	writeArchive(t, path, map[string][]byte{"1.png": onePixelPNG})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	doc, err := s.ListByID(context.Background(), mustFirstID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO progressions(target_type,target_id,modified,device_json,progression,written) VALUES('document',?,'2026-11-20T00:00:00Z','{}',0.75,9)`, doc.ID); err != nil {
		t.Fatal(err)
	}
	registered, err := s.RegisterPublished(context.Background(), "existing.zip", 654, "feed1234", "download_job")
	if err != nil {
		t.Fatal(err)
	}
	if registered.ID != doc.ID || registered.ProgressionTarget != "document" || registered.ProgressionGID != 0 {
		t.Fatalf("existing local target changed: %+v", registered)
	}
	var progression float64
	var written int64
	if err = s.db.QueryRow(`SELECT progression,written FROM progressions WHERE target_type='document' AND target_id=?`, doc.ID).Scan(&progression, &written); err != nil || progression != 0.75 || written != 9 {
		t.Fatalf("local progression overwritten: value=%v written=%d err=%v", progression, written, err)
	}
	if registered.GID == nil || *registered.GID != 654 {
		t.Fatalf("trusted download did not bind EH identity: %+v", registered)
	}
}

func TestRegisterPublishedRejectsManualConflictAndSymlink(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "manual.zip")
	writeArchive(t, path, map[string][]byte{"1.png": onePixelPNG})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	doc, err := s.ListByID(context.Background(), mustFirstID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Associate(context.Background(), doc.ID, 100, "manual123"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RegisterPublished(context.Background(), "manual.zip", 200, "download12", "download_job"); !errors.Is(err, ErrAssociationConflict) {
		t.Fatalf("manual association conflict err=%v", err)
	}
	still, err := s.ListByID(context.Background(), doc.ID)
	if err != nil || still.GID == nil || *still.GID != 100 || still.AssociationSource != "manual" {
		t.Fatalf("manual identity changed: %+v err=%v", still, err)
	}
	if err := os.Symlink(path, filepath.Join(root, "linked.cbz")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err = s.RegisterPublished(context.Background(), "linked.cbz", 300, "link1234", "download_job"); err == nil {
		t.Fatal("registered symlink path")
	}
}

func TestSamePathReplacementKeepsIdentityAssociationAndProgress(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "book.zip")
	writeArchive(t, path, map[string][]byte{"1.png": solidPNG(t, color.NRGBA{R: 255, A: 255})})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListByID(context.Background(), mustFirstID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Associate(context.Background(), first.ID, 123, "AbCd"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO progressions(target_type,target_id,modified,device_json,progression,written) VALUES('document',?,'2026-11-20T00:00:00Z','{}',0.75,9)`, first.ID); err != nil {
		t.Fatal(err)
	}
	writeArchive(t, path, map[string][]byte{"1.png": solidPNG(t, color.NRGBA{B: 255, A: 255}), "2.png": onePixelPNG})
	if _, err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.List(context.Background(), 1, 20, "", "true")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != first.ID || rows[0].GID == nil || *rows[0].GID != 123 {
		t.Fatalf("same-path identity/association changed: %+v", rows)
	}
	current, err := s.ListByID(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "available" || current.GID == nil || current.PageCount != 0 || current.SHA256 != "" {
		t.Fatalf("path update did not preserve identity while invalidating cache: %+v", current)
	}
	var progress float64
	var written int64
	if err = s.db.QueryRow(`SELECT progression,written FROM progressions WHERE target_type='document' AND target_id=?`, first.ID).Scan(&progress, &written); err != nil || progress != .75 || written != 9 {
		t.Fatalf("same-path progress changed value=%v written=%d err=%v", progress, written, err)
	}
	page, _, err := s.OpenPage(context.Background(), first.ID, 0)
	if err != nil || !bytes.Equal(page, solidPNG(t, color.NRGBA{B: 255, A: 255})) {
		t.Fatalf("replacement page read failed or served stale bytes, err=%v", err)
	}
}

func TestMissingHistoricalIDUsesCanonicalAvailableDocumentForReadAndDelete(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "same.zip")
	writeArchive(t, path, map[string][]byte{"1.png": onePixelPNG})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	oldID := mustFirstID(t, s)
	var size, mtime int64
	if err := s.db.QueryRow(`SELECT size,mtime_ns FROM documents WHERE id=?`, oldID).Scan(&size, &mtime); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE documents SET status='missing' WHERE id=?`, oldID); err != nil {
		t.Fatal(err)
	}
	canonicalID := "canonical-history-id"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`INSERT INTO documents(id,archive_path,format,size,mtime_ns,status,content_sha256,title,page_count,progression_target,created_at,updated_at)VALUES(?,'same.zip','zip',?,?,'available','', 'same',0,'document',?,?)`, canonicalID, size, mtime, now, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Associate(context.Background(), canonicalID, 777, "canonical-token"); err != nil {
		t.Fatal(err)
	}
	var pubID int64
	if err := s.db.QueryRow(`SELECT publication_id FROM documents WHERE id=?`, canonicalID).Scan(&pubID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO covers(publication_id,mime,data,sha256,source,updated_at)VALUES(?,?,?,?,?,?)`, pubID, "image/png", onePixelPNG, "fixture", "legacy_sidecar", now); err != nil {
		t.Fatal(err)
	}
	if d, err := s.Get(context.Background(), oldID); err != nil || d.ID != canonicalID {
		t.Fatalf("GET did not resolve canonical id: %+v err=%v", d, err)
	}
	page, mime, err := s.OpenPage(context.Background(), oldID, 0)
	if err != nil || mime != "image/png" || !bytes.Equal(page, onePixelPNG) {
		t.Fatalf("historical id read failed mime=%q err=%v", mime, err)
	}
	if profile, err := s.PageMIMEProfile(context.Background(), oldID); err != nil || profile != "image/png" {
		t.Fatalf("historical id MIME profile=%q err=%v", profile, err)
	}
	if cover, coverMIME, ok, coverErr := s.Cover(context.Background(), oldID); coverErr != nil || !ok || coverMIME != "image/png" || !bytes.Equal(cover, onePixelPNG) {
		t.Fatalf("historical id cover failed ok=%v mime=%q err=%v", ok, coverMIME, coverErr)
	}
	if err = s.Delete(context.Background(), oldID); err != nil {
		t.Fatalf("delete through historical id failed: %v", err)
	}
	var canonicalStatus, oldStatus string
	if err = s.db.QueryRow(`SELECT status FROM documents WHERE id=?`, canonicalID).Scan(&canonicalStatus); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT status FROM documents WHERE id=?`, oldID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if canonicalStatus != "missing" || oldStatus != "missing" {
		t.Fatalf("wrong records marked missing canonical=%q old=%q", canonicalStatus, oldStatus)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still exists after delete: %v", err)
	}
}

func TestIncompleteScanDoesNotMarkDocumentsMissing(t *testing.T) {
	s, root := newTestStore(t)
	writeArchive(t, filepath.Join(root, "book.zip"), map[string][]byte{"1.png": onePixelPNG})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstID(t, s)
	_ = os.Remove(filepath.Join(root, "book.zip"))
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Scan(context.Background()); err == nil {
		t.Fatal("expected failed scan")
	}
	rows, _, err := s.List(context.Background(), 1, 20, "available", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("missing file should not appear in live list: %+v", rows)
	}
	if got, err := s.ListByID(context.Background(), id); err != nil || got.Status != "available" {
		t.Fatalf("failed scan mutated status: %+v err=%v", got, err)
	}
}

func TestPurgeMissingRecordDoesNotDeletePayloadOrSharedPublication(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "book.zip")
	writeArchive(t, path, map[string][]byte{"1.png": onePixelPNG})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstID(t, s)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO progressions(target_type,target_id,modified,device_json,progression,written) VALUES('document',?,'2026-11-20T00:00:00Z','{}',0.5,1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,document_id,created_at,updated_at) VALUES('job-1','eh',42,'token','original','complete','complete',?,'2026-11-20T00:00:00Z','2026-11-20T00:00:00Z')`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.PurgeMissing(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListByID(context.Background(), id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("purged record still present: err=%v", err)
	}
	var progressCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM progressions WHERE target_type='document' AND target_id=?`, id).Scan(&progressCount); err != nil || progressCount != 1 {
		t.Fatalf("progression history count=%d err=%v", progressCount, err)
	}
	var jobDocument sql.NullString
	if err := s.db.QueryRow(`SELECT document_id FROM download_jobs WHERE id='job-1'`).Scan(&jobDocument); err != nil || jobDocument.Valid {
		t.Fatalf("download history was not detached: document_id=%v err=%v", jobDocument, err)
	}
	if err := s.PurgeMissing(context.Background(), id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second purge err=%v", err)
	}
}

func TestPurgeMissingRefusesRestoredPayload(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "restored.zip")
	writeArchive(t, path, map[string][]byte{"1.png": onePixelPNG})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstID(t, s)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeArchive(t, path, map[string][]byte{"1.png": onePixelPNG})
	if err := s.PurgeMissing(context.Background(), id); err == nil {
		t.Fatal("purged stale row while its payload had been restored")
	}
}

func TestListAllIncludesEveryStatusAndFillsPageAfterStaleFiles(t *testing.T) {
	s, root := newTestStore(t)
	for i := 0; i < 25; i++ {
		writeArchive(t, filepath.Join(root, fmt.Sprintf("%02d.zip", i)), map[string][]byte{"1.png": onePixelPNG})
	}
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := os.Remove(filepath.Join(root, fmt.Sprintf("%02d.zip", i))); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := s.List(context.Background(), 1, 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if total != 25 || len(items) != 10 {
		t.Fatalf("live page items=%d total=%d", len(items), total)
	}
	for _, item := range items {
		if item.Status != "available" {
			t.Fatalf("stale item returned: %+v", item)
		}
	}
	if _, err := s.db.Exec(`UPDATE documents SET status='invalid' WHERE archive_path=?`, "24.zip"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE documents SET status='missing' WHERE archive_path IN ('00.zip','01.zip','02.zip','03.zip')`); err != nil {
		t.Fatal(err)
	}
	all, allTotal, err := s.List(context.Background(), 1, 50, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if allTotal != 25 || len(all) != 25 {
		t.Fatalf("all items=%d total=%d", len(all), allTotal)
	}
	seen := map[string]bool{}
	for _, item := range all {
		seen[item.Status] = true
	}
	if !seen["available"] || !seen["missing"] || !seen["invalid"] {
		t.Fatalf("all statuses not included: %v", seen)
	}
}

func TestScanRegistersCandidatesAndLazyReadMarksInvalidArchives(t *testing.T) {
	s, root := newTestStore(t)
	writeArchive(t, filepath.Join(root, "plain.zip"), map[string][]byte{"readme.txt": []byte("hello")})
	if err := os.Symlink(filepath.Join(root, "plain.zip"), filepath.Join(root, "link.zip")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, total, err := s.List(context.Background(), 1, 20, "available", "")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("candidate not registered: %d", total)
	}
	items, _, err := s.List(context.Background(), 1, 20, "available", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.OpenPage(context.Background(), items[0].ID, 0); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("lazy read error=%v", err)
	}
	invalid, invalidTotal, err := s.List(context.Background(), 1, 20, "invalid", "")
	if err != nil || invalidTotal != 1 || len(invalid) != 1 {
		t.Fatalf("diagnostic invalid docs=%d total=%d err=%v", len(invalid), invalidTotal, err)
	}
	if report, err := s.Scan(context.Background()); err != nil || report["documents_found"] != 1 {
		t.Fatalf("ordinary scan should restore the available candidate without opening it: report=%v err=%v", report, err)
	}
	if _, total, err = s.List(context.Background(), 1, 20, "available", ""); err != nil || total != 1 {
		t.Fatalf("candidate missing after ordinary scan: total=%d err=%v", total, err)
	}
	if _, _, err = s.OpenPage(context.Background(), items[0].ID, 0); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("bad format should still fail on explicit read: %v", err)
	}
	if err = os.Remove(filepath.Join(root, "plain.zip")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = s.db.QueryRow(`SELECT status FROM documents WHERE id=?`, items[0].ID).Scan(&status); err != nil || status != "missing" {
		t.Fatalf("invalid document disappearance status=%q err=%v", status, err)
	}
	writeArchive(t, filepath.Join(root, "plain.zip"), map[string][]byte{"readme.txt": []byte("hello")})
	if _, err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var returnedID, returnedStatus string
	if err = s.db.QueryRow(`SELECT id,status FROM documents WHERE archive_path='plain.zip' AND status='available'`).Scan(&returnedID, &returnedStatus); err != nil || returnedID != items[0].ID || returnedStatus != "available" {
		t.Fatalf("returned path did not restore stable candidate id=%q status=%q err=%v", returnedID, returnedStatus, err)
	}
}

func TestArchiveMetadataExplicitEHURLAssociatesWithEvidence(t *testing.T) {
	s, root := newTestStore(t)
	writeArchive(t, filepath.Join(root, "declared.cbz"), map[string][]byte{"1.png": onePixelPNG, "ComicInfo.xml": []byte(`<ComicInfo><Web>https://e-hentai.org/g/456/abcdef0123/</Web></ComicInfo>`)})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstID(t, s)
	if _, _, err := s.OpenPage(context.Background(), id, 0); err != nil {
		t.Fatal(err)
	}
	items, _, err := s.List(context.Background(), 1, 10, "available", "")
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if items[0].GID == nil || *items[0].GID != 456 || items[0].Token != "abcdef0123" || items[0].AssociationSource != "archive_metadata" {
		t.Fatalf("association=%+v", items[0])
	}
}

func TestScanSignatureShortcutAndDeepRecheck(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "book.zip")
	writeArchive(t, path, map[string][]byte{"1.png": solidPNG(t, color.NRGBA{R: 255, A: 255})})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := mustFirstID(t, s)
	old, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := solidPNG(t, color.NRGBA{B: 255, A: 255})
	writeArchive(t, path, map[string][]byte{"1.png": replacement})
	if err = os.Chtimes(path, old.ModTime(), old.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if id := mustFirstID(t, s); id != before {
		t.Fatalf("signature shortcut unexpectedly changed identity: %s -> %s", before, id)
	}
	if _, err = s.ScanDeep(context.Background()); err != nil {
		t.Fatal(err)
	}
	before = mustFirstID(t, s)
	var deepHash string
	if err = s.db.QueryRow(`SELECT content_sha256 FROM documents WHERE id=?`, before).Scan(&deepHash); err != nil || deepHash == "" {
		t.Fatalf("explicit deep scan did not compute hash=%q err=%v", deepHash, err)
	}
	old, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement = solidPNG(t, color.NRGBA{G: 255, A: 255})
	writeArchive(t, path, map[string][]byte{"1.png": replacement})
	if err = os.Chtimes(path, old.ModTime(), old.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ScanDeep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if id := mustFirstID(t, s); id != before {
		t.Fatal("deep scan changed path identity")
	}
}

func TestCachedPageIndexRefreshesChangedPathAndRejectsMissingOrSymlink(t *testing.T) {
	for _, mode := range []string{"changed", "missing", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			s, root := newTestStore(t)
			path := filepath.Join(root, "cached.cbz")
			writeArchive(t, path, map[string][]byte{"1.png": onePixelPNG})
			if _, err := s.Scan(context.Background()); err != nil {
				t.Fatal(err)
			}
			id := mustFirstID(t, s)
			if _, _, err := s.OpenPage(context.Background(), id, 0); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "changed":
				writeArchive(t, path, map[string][]byte{"1.png": solidPNG(t, color.NRGBA{G: 255, A: 255})})
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "other.cbz")
				writeArchive(t, outside, map[string][]byte{"1.png": onePixelPNG})
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			_, _, err := s.EnsurePageIndex(context.Background(), id)
			if mode == "changed" {
				if err != nil {
					t.Fatalf("changed same-path file should rebuild index: %v", err)
				}
				page, _, readErr := s.OpenPage(context.Background(), id, 0)
				if readErr != nil || !bytes.Equal(page, solidPNG(t, color.NRGBA{G: 255, A: 255})) {
					t.Fatalf("changed file read failed: %v", readErr)
				}
				return
			}
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("expected typed validation error, got %v", err)
			}
			want := map[string]string{"missing": "file_missing", "symlink": "unsafe_path"}[mode]
			if validation.Reason != want {
				t.Fatalf("reason=%q want %q err=%v", validation.Reason, want, err)
			}
		})
	}
}

func TestSecureOpenRejectsSymlinkTraversal(t *testing.T) {
	s, root := newTestStore(t)
	outside := t.TempDir()
	writeArchive(t, filepath.Join(outside, "book.zip"), map[string][]byte{"1.png": onePixelPNG})
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := secureOpen(root, "link/book.zip"); err == nil {
		t.Fatal("symlink path was followed")
	}
	_ = s
}

func TestDuplicateZipEntryNamesUseCentralDirectoryIndex(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "duplicate.cbz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, pixels := range []color.NRGBA{{R: 255, A: 255}, {B: 255, A: 255}} {
		w, e := zw.Create("page.png")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(solidPNG(t, pixels)); e != nil {
			t.Fatal(e)
		}
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstID(t, s)
	first, _, err := s.OpenPage(context.Background(), id, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.OpenPage(context.Background(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("same-name entries resolved to the same page")
	}
}

func TestDeleteHookFailureAndFilesystemFailurePreserveSafeState(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "nested", "book.zip")
	writeArchive(t, path, map[string][]byte{"1.png": onePixelPNG})
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstID(t, s)
	var err error
	if _, err := s.ListByID(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	blocked := errors.New("suppression write failed")
	s.SetDeleteHooks(func(Document) error { return blocked }, func(Document) error { return nil })
	if err = s.Delete(context.Background(), id); !errors.Is(err, blocked) {
		t.Fatalf("delete err=%v", err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatalf("file removed despite suppression failure: %v", err)
	}
	rolledBack := false
	s.SetDeleteHooks(func(Document) error {
		if err := os.Rename(path, path+".saved"); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(path, "child"), []byte("x"), 0600)
	}, func(Document) error { rolledBack = true; return nil })
	if err = s.Delete(context.Background(), id); err == nil {
		t.Fatal("expected filesystem deletion failure")
	}
	if !rolledBack {
		t.Fatal("suppression rollback was not called")
	}
	current, err := s.ListByID(context.Background(), id)
	if err != nil || current.Status != "available" {
		t.Fatalf("document state=%+v err=%v", current, err)
	}
}

func TestDeleteRefusesSameStatPayloadReplacementWhenDeepHashExists(t *testing.T) {
	s, root := newTestStore(t)
	path := filepath.Join(root, "hashed.cbz")
	writeArchive(t, path, map[string][]byte{"1.png": onePixelPNG})
	if _, err := s.ScanDeep(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstID(t, s)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body[10] ^= 0x40
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(context.Background(), id); err == nil {
		t.Fatal("delete accepted same-stat replacement")
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatalf("replacement was removed: %v", err)
	}
}

func mustFirstID(t *testing.T, s *Store) string {
	t.Helper()
	items, _, err := s.List(context.Background(), 1, 1, "available", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one doc, got %d", len(items))
	}
	return items[0].ID
}

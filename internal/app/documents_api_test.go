package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pandaopds/internal/config"
	"pandaopds/internal/document"
	"pandaopds/internal/feed"
	"pandaopds/internal/migration"
	"pandaopds/internal/progression"
	"pandaopds/internal/storage"
)

func TestLocalDocumentHTTPAndProgressionRoutes(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(archiveRoot, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(archiveRoot, "local.cbz"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	entry, err := zw.Create("page1.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	docs, err := document.New(db, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.ScanDeep(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _, err := docs.List(context.Background(), 1, 10, "available", "")
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	id := items[0].ID
	mux := http.NewServeMux()
	RegisterDocumentRoutes(mux, docs, config.Settings{PSEPageBase: 1, PublicBaseURL: "https://reader.example/prefix"})
	list := httptest.NewRecorder()
	mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/documents?page=1&per_page=10&linked=unlinked", nil))
	if list.Code != 200 {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body)
	}
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err = json.Unmarshal(list.Body.Bytes(), &listed); err != nil || len(listed.Items) != 1 {
		t.Fatalf("list=%s err=%v", list.Body, err)
	}
	manifest := httptest.NewRecorder()
	mux.ServeHTTP(manifest, httptest.NewRequest(http.MethodGet, "/opds/v2.0/documents/"+id+"/manifest", nil))
	if manifest.Code != 200 || !strings.Contains(manifest.Body.String(), "/prefix/stream/documents/") || !strings.Contains(manifest.Body.String(), "image/png") || !strings.Contains(manifest.Body.String(), "/opds/progression/documents/") {
		t.Fatalf("manifest status=%d body=%s", manifest.Code, manifest.Body)
	}
	opdsFeed, opdsTotal, err := DocumentArchiveOPDSFeed(context.Background(), docs, config.Settings{PSEPageBase: 1, PublicBaseURL: "https://reader.example/prefix"}, 1, 100)
	if err != nil || opdsTotal != 1 {
		t.Fatalf("OPDS archive feed total=%d err=%v", opdsTotal, err)
	}
	pubs := opdsFeed["publications"].([]map[string]any)
	if len(pubs) != 1 || !strings.Contains(pubs[0]["metadata"].(map[string]any)["identifier"].(string), id) {
		t.Fatalf("OPDS feed publications=%v", pubs)
	}
	atomFeed, atomTotal, err := DocumentArchiveAtomFeed(context.Background(), docs, config.Settings{PSEPageBase: 1}, 1, 100)
	if err != nil || atomTotal != 1 || !strings.Contains(string(atomFeed), "urn:pandaopds:document:"+id) || !strings.Contains(string(atomFeed), "/stream/documents/"+id+"/page/{pageNumber}") {
		t.Fatalf("Atom archive feed total=%d err=%v body=%s", atomTotal, err, atomFeed)
	}
	page := httptest.NewRecorder()
	mux.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/stream/documents/"+id+"/page/1", nil))
	if page.Code != 200 || page.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("page status=%d type=%s body=%s", page.Code, page.Header().Get("Content-Type"), page.Body)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := `{"modified":"` + now + `","device":{"id":"urn:uuid:test","name":"reader"},"progression":0.5,"references":[],"title":"local"}`
	put := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/opds/progression/documents/"+id, strings.NewReader(body))
		mux.ServeHTTP(w, req)
		return w
	}
	created := put(payload)
	if created.Code != 201 {
		t.Fatalf("progression PUT status=%d body=%s", created.Code, created.Body)
	}
	retry := put(payload)
	if retry.Code != 200 {
		t.Fatalf("progression retry status=%d body=%s", retry.Code, retry.Body)
	}
	stale := put(strings.Replace(payload, now, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), 1))
	if stale.Code != 409 {
		t.Fatalf("stale progression status=%d body=%s", stale.Code, stale.Body)
	}
	if _, err = docs.DB().Exec(`UPDATE documents SET progression_target='eh',progression_gid=456,progression_token='frozen' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = docs.DB().Exec(`INSERT INTO progressions(target_type,target_id,gid,token,modified,device_json,progression,references_json,written)VALUES('eh','456/frozen',456,'frozen',?,? ,0.8,'[]',?)`, now, `{"id":"urn:uuid:other","name":"other"}`, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err = docs.Associate(context.Background(), id, 999, "relinked"); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/documents/"+id, nil))
	if get.Code != 200 {
		t.Fatalf("document GET status=%d body=%s", get.Code, get.Body)
	}
	var current map[string]any
	if err = json.Unmarshal(get.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current["progression_url"] != "/opds/progression/456/frozen" || current["gid"] != float64(999) {
		t.Fatalf("relinked document=%v", current)
	}
	var localProgress, ehProgress float64
	if err = docs.DB().QueryRow(`SELECT progression FROM progressions WHERE target_type='document' AND target_id=?`, id).Scan(&localProgress); err != nil {
		t.Fatal(err)
	}
	if err = docs.DB().QueryRow(`SELECT progression FROM progressions WHERE target_type='eh' AND target_id='456/frozen'`).Scan(&ehProgress); err != nil {
		t.Fatal(err)
	}
	if localProgress != 0.5 || ehProgress != 0.8 {
		t.Fatalf("progressions overwritten: local=%v EH=%v", localProgress, ehProgress)
	}
	if err = os.Remove(filepath.Join(archiveRoot, "local.cbz")); err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	purge := httptest.NewRecorder()
	mux.ServeHTTP(purge, httptest.NewRequest(http.MethodDelete, "/api/documents/"+id+"/record", nil))
	if purge.Code != http.StatusNoContent {
		t.Fatalf("purge status=%d body=%s", purge.Code, purge.Body)
	}
	var count int
	if err = docs.DB().QueryRow(`SELECT COUNT(*) FROM progressions WHERE target_type='document' AND target_id=?`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("local progression should be retained count=%d err=%v", count, err)
	}
	if err = docs.DB().QueryRow(`SELECT COUNT(*) FROM progressions WHERE target_type='eh' AND target_id='456/frozen'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("EH progression removed count=%d err=%v", count, err)
	}
}

func TestManifestReadsChangedPathAndAllowsExplicitInvalidRetry(t *testing.T) {
	base := t.TempDir()
	archiveRoot := filepath.Join(base, "archive")
	if err := os.MkdirAll(archiveRoot, 0755); err != nil {
		t.Fatal(err)
	}
	valid, err := os.Create(filepath.Join(archiveRoot, "stale.cbz"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(valid)
	entry, err := zw.Create("page.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = valid.Close(); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(archiveRoot, "bad.zip")
	if err = os.WriteFile(badPath, []byte("not a zip"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	docs, err := document.New(db, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterDocumentRoutes(mux, docs, config.Settings{PSEPageBase: 1})
	var stale string
	if err = db.QueryRow(`SELECT id FROM documents WHERE archive_path='stale.cbz'`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	oldInfo, err := os.Stat(filepath.Join(archiveRoot, "stale.cbz"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(filepath.Join(archiveRoot, "stale.cbz"), oldInfo.ModTime().Add(time.Second), oldInfo.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opds/v2.0/documents/"+stale+"/manifest", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("changed-path manifest status=%d body=%s", response.Code, response.Body)
	}
	var manifest map[string]any
	if err = json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	metadata, _ := manifest["metadata"].(map[string]any)
	if metadata["numberOfPages"] != float64(1) {
		t.Fatalf("changed-path index not available: %v", metadata)
	}
	for _, path := range []string{"/api/documents/" + stale, "/stream/documents/" + stale + "/page/1"} {
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("stale endpoint %s status=%d body=%s err=%v", path, response.Code, response.Body, err)
		}
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE id=? AND status='available' AND page_count=1`, stale).Scan(&count); err != nil || count != 1 {
		t.Fatalf("same-path document not refreshed count=%d err=%v", count, err)
	}

	var badID string
	if err = db.QueryRow(`SELECT id FROM documents WHERE archive_path='bad.zip'`).Scan(&badID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if i == 1 {
			if _, err = db.Exec(`UPDATE documents SET updated_at='2000-01-01T00:00:00Z' WHERE id=?`, badID); err != nil {
				t.Fatal(err)
			}
		}
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opds/v2.0/documents/"+badID+"/manifest", nil))
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid retry %d status=%d body=%s", i, response.Code, response.Body)
		}
		var body map[string]any
		if err = json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["reason"] != "invalid_archive" {
			t.Fatalf("invalid retry body=%v err=%v", body, err)
		}
	}
	var updated string
	if err = db.QueryRow(`SELECT updated_at FROM documents WHERE id=?`, badID).Scan(&updated); err != nil || updated == "2000-01-01T00:00:00Z" {
		t.Fatalf("invalid archive retry did not revisit the payload: updated=%q err=%v", updated, err)
	}
	var status string
	if err = db.QueryRow(`SELECT status FROM documents WHERE id=?`, badID).Scan(&status); err != nil || status != "invalid" {
		t.Fatalf("invalid status=%q err=%v", status, err)
	}
}

func TestLegacyArchiveLinkKeepsEHProgressAndSQLiteCoverOnDocumentHTTP(t *testing.T) {
	base := t.TempDir()
	files, state := filepath.Join(base, "archive"), filepath.Join(base, "state")
	if err := os.MkdirAll(filepath.Join(state, "444"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(files, "444"), 0755); err != nil {
		t.Fatal(err)
	}
	stateJSON := `{"gid":444,"token":"ab1234","root_gid":444,"root_token":"ab1234","active_gid":444,"active_token":"ab1234","status":"ready","layout":"versioned","versions":{"444:ab1234":{"gid":444,"token":"ab1234","status":"ready","storage_dir":".","state_file":"state.json","gallery_file":"gallery.json","archive_path":"444/archive.zip","page_count":1}}}`
	if err := os.WriteFile(filepath.Join(state, "444", "state.json"), []byte(stateJSON), 0600); err != nil {
		t.Fatal(err)
	}
	gallery := `{"gid":444,"token":"ab1234","title":"Imported title","category":"Manga","page_count":1,"rating":4.2}`
	if err := os.WriteFile(filepath.Join(state, "444", "gallery.json"), []byte(gallery), 0600); err != nil {
		t.Fatal(err)
	}
	cover := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}
	if err := os.WriteFile(filepath.Join(state, "444", "cover.jpg"), cover, 0600); err != nil {
		t.Fatal(err)
	}
	zf, err := os.Create(filepath.Join(files, "444", "archive.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	entry, err := zw.Create("page.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write(cover); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = zf.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(base, "config", "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = migration.ImportLegacyArchive(db, files, state); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO progressions(target_type,target_id,gid,token,modified,device_json,progression,title,references_json,written) VALUES('eh','444/ab1234',444,'ab1234','2026-11-20T00:00:00Z','{"id":"https://reader.test","name":"Reader"}',0.7,'Imported title','[]',11)`); err != nil {
		t.Fatal(err)
	}
	docs, err := document.New(db, files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	linked, err := migration.LinkLegacyDocuments(db, files)
	if err != nil || linked != 1 {
		t.Fatalf("linked=%d err=%v", linked, err)
	}
	id := mustFirstDocumentID(t, docs)
	progressStore, err := progression.OpenDB(db, filepath.Join(base, "legacy-progression.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer progressStore.Close()
	server := &Server{Progression: progressStore}
	mux := http.NewServeMux()
	RegisterDocumentRoutes(mux, docs, config.Settings{})
	mux.HandleFunc("/opds/progression/{gid}/{token}", server.progressionHandler)
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/documents/"+id, nil))
	if get.Code != 200 {
		t.Fatalf("document get status=%d body=%s", get.Code, get.Body)
	}
	var body map[string]any
	if err = json.Unmarshal(get.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["association_source"] != "legacy_sidecar" || body["progression_url"] != "/opds/progression/444/ab1234" {
		t.Fatalf("linked API document=%v", body)
	}
	thumb := httptest.NewRecorder()
	mux.ServeHTTP(thumb, httptest.NewRequest(http.MethodGet, "/image/documents/"+id+"/thumb", nil))
	if thumb.Code != 200 || !strings.HasPrefix(thumb.Header().Get("Content-Type"), "image/png") || string(thumb.Body.Bytes()) != string(cover) {
		t.Fatalf("imported cover status=%d type=%q bytes=%d", thumb.Code, thumb.Header().Get("Content-Type"), thumb.Body.Len())
	}
	progress := httptest.NewRecorder()
	mux.ServeHTTP(progress, httptest.NewRequest(http.MethodGet, "/opds/progression/444/ab1234", nil))
	if progress.Code != 200 {
		t.Fatalf("legacy progression status=%d body=%s", progress.Code, progress.Body)
	}
	var p map[string]any
	if err = json.Unmarshal(progress.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p["progression"] != 0.7 || p["title"] != "Imported title" {
		t.Fatalf("legacy progression=%v", p)
	}
}

func mustFirstDocumentID(t *testing.T, docs *document.Store) string {
	t.Helper()
	items, _, err := docs.List(context.Background(), 1, 10, "available", "")
	if err != nil || len(items) != 1 {
		t.Fatalf("documents=%d err=%v", len(items), err)
	}
	return items[0].ID
}

func TestArchiveFeedSummariesOmitReadingOrderAndAdvertiseImageStreams(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "archive")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	var jpg bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Black)
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	makeZip := func(name string, entries map[string][]byte) {
		t.Helper()
		f, err := os.Create(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		for entry, data := range entries {
			w, e := zw.Create(entry)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = w.Write(data); e != nil {
				t.Fatal(e)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	makeZip("homogeneous.cbz", map[string][]byte{"1.png": []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}, "2.png": []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}})
	makeZip("mixed.cbz", map[string][]byte{"1.png": []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}, "2.jpg": jpg.Bytes()})
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	docs, err := document.New(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.ScanDeep(context.Background()); err != nil {
		t.Fatal(err)
	}
	feedDoc, total, err := DocumentArchiveOPDSFeed(context.Background(), docs, config.Settings{}, 1, 10)
	if err != nil || total != 2 {
		t.Fatalf("feed total=%d err=%v", total, err)
	}
	pubs := feedDoc["publications"].([]map[string]any)
	for _, pub := range pubs {
		if _, ok := pub["readingOrder"]; ok {
			t.Fatal("catalog summary contains full readingOrder")
		}
		id := strings.TrimPrefix(pub["metadata"].(map[string]any)["identifier"].(string), "urn:pandaopds:document:")
		full, err := docs.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		summaryType := ""
		for _, link := range pub["links"].([]map[string]any) {
			if link["rel"] == "http://vaemendis.net/opds-pse/stream" {
				summaryType = link["type"].(string)
			}
		}
		if full.Title == "homogeneous" && summaryType != "image/png" {
			t.Fatalf("homogeneous stream MIME=%q", summaryType)
		}
		if full.Title == "mixed" && summaryType != "image/*" {
			t.Fatalf("mixed stream MIME=%q", summaryType)
		}
		manifest := LocalDocumentPublication(context.Background(), full, docs, config.Settings{})
		reading := manifest["readingOrder"].([]map[string]any)
		types := map[string]bool{}
		for _, page := range reading {
			types[page["type"].(string)] = true
		}
		wantJPG := full.Title == "mixed"
		if len(reading) != 2 || !types["image/png"] || types["image/jpeg"] != wantJPG {
			t.Fatalf("manifest readingOrder=%v", reading)
		}
	}
	atom, _, err := DocumentArchiveAtomFeed(context.Background(), docs, config.Settings{}, 1, 10)
	if err != nil || !strings.Contains(string(atom), `type="image/png"`) || !strings.Contains(string(atom), `type="image/*"`) {
		t.Fatalf("Atom stream MIME types missing: err=%v body=%s", err, atom)
	}
}

func TestManifestRejectsCorruptCandidateAndRecordsInvalid(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(archiveRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveRoot, "broken.cbz"), []byte("not a zip"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	docs, err := document.New(db, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstDocumentID(t, docs)
	mux := http.NewServeMux()
	RegisterDocumentRoutes(mux, docs, config.Settings{})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opds/v2.0/documents/"+id+"/manifest", nil))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("manifest status=%d body=%s", response.Code, response.Body.String())
	}
	doc, err := docs.ListByID(context.Background(), id)
	if err != nil || doc.Status != "invalid" {
		t.Fatalf("document after failed index=%+v err=%v", doc, err)
	}
}

func TestArchiveSummariesDoNotIndexUnopenedCandidates(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	writeTestArchive(t, filepath.Join(archiveRoot, "candidate.cbz"), "page")
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	docs, err := document.New(db, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	feedDoc, _, err := DocumentArchiveOPDSFeed(context.Background(), docs, config.Settings{}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	pub := feedDoc["publications"].([]map[string]any)[0]
	for _, link := range pub["links"].([]map[string]any) {
		if link["rel"] == feed.RelThumb {
			t.Fatal("unindexed candidate summary advertises an automatic cover request")
		}
	}
	var pages int
	if err = db.QueryRow(`SELECT COUNT(*) FROM document_pages`).Scan(&pages); err != nil || pages != 0 {
		t.Fatalf("summary built page index=%d err=%v", pages, err)
	}
	if _, _, err = DocumentArchiveAtomFeed(context.Background(), docs, config.Settings{}, 1, 20); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM document_pages`).Scan(&pages); err != nil || pages != 0 {
		t.Fatalf("Atom summary built page index=%d err=%v", pages, err)
	}
}

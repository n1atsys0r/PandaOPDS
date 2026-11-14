package app

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

type archiveOnlyTransport struct{}

func (archiveOnlyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("archive OPDS regression must not contact upstream")
}

func TestArchivedOPDSKeepsRootRoutesAndUsesActiveVersionAfterRestart(t *testing.T) {
	t.Run("existing grouped layout", func(t *testing.T) {
		root := t.TempDir()
		prepareGroupedArchive(t, root)
		checkArchivedOPDSAfterRestart(t, root, true)
	})
	t.Run("per version directories", func(t *testing.T) {
		root := t.TempDir()
		prepareUpdatedArchive(t, root)
		checkArchivedOPDSAfterRestart(t, root, false)
	})
}

func prepareGroupedArchive(t *testing.T, root string) {
	t.Helper()
	rootDir := filepath.Join(root, "100")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestArchive(t, filepath.Join(rootDir, "100.zip"), "old-page")
	writeTestArchive(t, filepath.Join(rootDir, "200.zip"), "active-page")
	rootMeta := map[string]any{
		"gid": 100, "token": "aaaa", "root_gid": 100, "root_token": "aaaa",
		"active_gid": 200, "active_token": "bbbb", "layout": "flat", "status": "ready",
		"title": "Active title", "page_count": 1, "created_at": 1700000000,
		"versions": map[string]any{
			"100:aaaa": map[string]any{"gid": 100, "token": "aaaa", "status": "ready", "title": "Original title", "page_count": 1},
			"200:bbbb": map[string]any{"gid": 200, "token": "bbbb", "status": "ready", "title": "Active title", "page_count": 1},
		},
	}
	writeTestJSON(t, filepath.Join(rootDir, "meta.json"), rootMeta)
	writeTestJSON(t, filepath.Join(rootDir, "metadata.json"), map[string]any{
		"gid": 200, "token": "bbbb", "title": "Active title", "title_jpn": "Active title",
		"category": "Manga", "posted": 1700000000, "tags": map[string]any{},
	})
}

func checkArchivedOPDSAfterRestart(t *testing.T, root string, hasOldZIP bool) {
	t.Helper()
	// Reopening from disk exercises the same identity and active-version mapping
	// that a process restart sees.
	store, err := archive.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{EHSite: "e-hentai", PSEPageBase: 1, PageURLTTLSeconds: 60, CacheDir: filepath.Join(t.TempDir(), "cache")}
	service := eh.NewService(settings, archiveOnlyTransport{})
	defer service.Close()
	service.AttachArchive(store)
	s := &Server{Settings: settings, Service: service, Store: store, mux: http.NewServeMux()}
	s.register()

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d body=%s", path, w.Code, w.Body.String())
		}
		return w
	}

	atom := get("/opds/v1.2/archives").Body.String()
	for _, want := range []string{"urn:ehentai:gallery:100:aaaa", "/opds/v1.2/gallery/100/aaaa/chapters", "/stream/200/bbbb/page/{pageNumber}", "pse:count=\"1\"", "https://e-hentai.org/g/200/bbbb/"} {
		if !strings.Contains(atom, want) {
			t.Fatalf("OPDS 1.2 archive shelf missing %q: %s", want, atom)
		}
	}

	var shelf20 map[string]any
	if err := json.Unmarshal(get("/opds/v2.0/archives").Body.Bytes(), &shelf20); err != nil {
		t.Fatal(err)
	}
	publications := shelf20["publications"].([]any)
	if len(publications) != 1 {
		t.Fatalf("expected one shelf entry, got %d", len(publications))
	}
	pub := publications[0].(map[string]any)
	if got := pub["metadata"].(map[string]any)["identifier"]; got != "urn:ehentai:gallery:100:aaaa" {
		t.Fatalf("OPDS 2.0 archive identifier = %v", got)
	}
	if got := pub["metadata"].(map[string]any)["title"]; got != "Active title" {
		t.Fatalf("OPDS 2.0 archive title = %v", got)
	}
	if got := pub["links"].([]any)[0].(map[string]any)["href"]; got != "/opds/v2.0/gallery/100/aaaa/manifest" {
		t.Fatalf("archive manifest link = %v", got)
	}
	manifest := get("/opds/v2.0/gallery/100/aaaa/manifest")
	if got := manifest.Header().Get("Content-Type"); got != "application/divina+json" {
		t.Fatalf("manifest content type = %q", got)
	}
	var manifestDoc map[string]any
	if err := json.Unmarshal(manifest.Body.Bytes(), &manifestDoc); err != nil {
		t.Fatal(err)
	}
	if manifestDoc["@context"] != "https://readium.org/webpub-manifest/context.jsonld" {
		t.Fatalf("manifest context = %#v", manifestDoc["@context"])
	}
	if got := manifestDoc["metadata"].(map[string]any)["conformsTo"].([]any)[0]; got != "https://readium.org/webpub-manifest/profiles/divina" {
		t.Fatalf("Divina conformance = %v", got)
	}
	legacyPublication := get("/opds/v2.0/gallery/100/aaaa/publication")
	if got := legacyPublication.Header().Get("Content-Type"); got != "application/opds+json" {
		t.Fatalf("legacy publication content type = %q", got)
	}
	var legacyPublicationDoc map[string]any
	if err := json.Unmarshal(legacyPublication.Body.Bytes(), &legacyPublicationDoc); err != nil {
		t.Fatal(err)
	}
	if legacyPublicationDoc["@context"] != nil || legacyPublicationDoc["context"] == nil || legacyPublicationDoc["metadata"].(map[string]any)["conformsTo"] != nil {
		t.Fatalf("legacy publication changed Divina context contract: %#v", legacyPublicationDoc)
	}
	manifestLinks := manifestDoc["links"].([]any)
	if len(manifestLinks) != 3 {
		t.Fatalf("manifest must have exactly self, alternate, progression links: %#v", manifestLinks)
	}
	wantLinks := []struct{ rel, href, typ string }{
		{"self", "/opds/v2.0/gallery/100/aaaa/manifest", "application/divina+json"},
		{"alternate", "https://e-hentai.org/g/200/bbbb/", "text/html"},
		{"http://opds-spec.org/progression", "/opds/progression/200/bbbb", "application/opds-progression+json"},
	}
	for i, want := range wantLinks {
		link := manifestLinks[i].(map[string]any)
		if link["rel"] != want.rel || link["href"] != want.href || link["type"] != want.typ {
			t.Fatalf("manifest link %d = %#v, want rel=%q href=%q type=%q", i, link, want.rel, want.href, want.typ)
		}
	}
	manifestOrder := manifestDoc["readingOrder"].([]any)
	firstManifestPage := manifestOrder[0].(map[string]any)["href"].(string)
	if got := get(firstManifestPage).Body.String(); got != "active-page" {
		t.Fatalf("manifest readingOrder resource %q returned %q", firstManifestPage, got)
	}

	chapters := get("/opds/v1.2/gallery/100/aaaa/chapters").Body.String()
	for _, want := range []string{"urn:ehentai:gallery:100:aaaa", "/stream/200/bbbb/page/{pageNumber}", "pse:count=\"1\""} {
		if !strings.Contains(chapters, want) {
			t.Fatalf("old-identity chapters feed missing %q: %s", want, chapters)
		}
	}

	var detailDoc map[string]any
	if err := json.Unmarshal(get("/opds/v2.0/gallery/100/aaaa").Body.Bytes(), &detailDoc); err != nil {
		t.Fatal(err)
	}
	if got := detailDoc["links"].([]any)[0].(map[string]any)["href"]; got != "/opds/v2.0/gallery/100/aaaa" {
		t.Fatalf("detail self link = %v", got)
	}
	detail := detailDoc["publications"].([]any)[0].(map[string]any)
	if got := detail["metadata"].(map[string]any)["identifier"]; got != "urn:ehentai:gallery:100:aaaa" {
		t.Fatalf("detail identifier = %v", got)
	}
	if got := detail["metadata"].(map[string]any)["title"]; got != "Active title" {
		t.Fatalf("detail title = %v", got)
	}
	selfFound := false
	for _, rawLink := range detail["links"].([]any) {
		link := rawLink.(map[string]any)
		if link["rel"] == "self" && link["href"] == "/opds/v2.0/gallery/100/aaaa/manifest" && link["type"] == "application/divina+json" {
			selfFound = true
		}
	}
	if !selfFound {
		t.Fatalf("publication links lost root self URL: %#v", detail["links"])
	}
	order := detail["readingOrder"].([]any)
	if len(order) != 1 || order[0].(map[string]any)["href"] != "/stream/200/bbbb/page/1" {
		t.Fatalf("readingOrder = %#v", order)
	}
	if got := detail["images"].([]any)[0].(map[string]any)["href"]; got != "/image/200/bbbb/thumb" {
		t.Fatalf("cover link = %v", got)
	}

	expectedPages := map[string]string{"/stream/200/bbbb/page/1": "active-page"}
	if hasOldZIP {
		expectedPages["/stream/100/aaaa/page/1"] = "old-page"
	}
	for path, want := range expectedPages {
		response := get(path)
		if got := response.Body.String(); got != want {
			t.Fatalf("GET %s returned %q, want %q", path, got, want)
		}
	}
}

func prepareUpdatedArchive(t *testing.T, root string) {
	t.Helper()
	store, err := archive.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(100, "aaaa", map[string]any{"status": "ready", "title": "Original title", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	oldZIP := filepath.Join(root, "100", "archive.zip")
	writeTestArchive(t, oldZIP, "old-page")
	if err := store.WriteMetadata(100, "aaaa", map[string]any{"gid": 100, "token": "aaaa", "title": "Original title"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(200, "bbbb", map[string]any{"root_gid": 100, "root_token": "aaaa", "is_version_update": true, "status": "ready", "title": "Active title", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeTestArchive(t, filepath.Join(root, "200", "archive.zip"), "active-page")
	snapshot := map[string]any{"gid": 200, "token": "bbbb", "title": "Active title", "category": "Manga", "posted": 1700000000}
	if err := store.ActivateVersion(100, "aaaa", 200, "bbbb", snapshot, []byte("active-cover"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldZIP); !os.IsNotExist(err) {
		t.Fatalf("previous ZIP should be removed after activation, got %v", err)
	}
}

func activeStreamLink(t *testing.T, raw any, want string) {
	t.Helper()
	for _, value := range raw.([]any) {
		if link, ok := value.(map[string]any); ok && link["href"] == want {
			return
		}
	}
	t.Fatalf("link list %#v does not contain %q", raw, want)
}

func writeTestArchive(t *testing.T, path, page string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entry, err := w.Create("0001.jpg")
	if err == nil {
		_, err = io.WriteString(entry, page)
	}
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

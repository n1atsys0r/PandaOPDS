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
	root := filepath.Join(t.TempDir(), "archives")
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
	pub := shelf20["publications"].([]any)[0].(map[string]any)
	if got := pub["metadata"].(map[string]any)["identifier"]; got != "urn:ehentai:gallery:100:aaaa" {
		t.Fatalf("OPDS 2.0 archive identifier = %v", got)
	}
	if got := pub["metadata"].(map[string]any)["title"]; got != "Active title" {
		t.Fatalf("OPDS 2.0 archive title = %v", got)
	}
	activeStreamLink(t, pub["links"], "/stream/200/bbbb/page/{pageNumber}")

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
	activeStreamLink(t, detail["links"], "/stream/200/bbbb/page/{pageNumber}")
	selfFound := false
	for _, rawLink := range detail["links"].([]any) {
		link := rawLink.(map[string]any)
		if link["rel"] == "self" && link["href"] == "/opds/v2.0/gallery/100/aaaa/publication" {
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

	// Resource URLs remain version-specific: the old URL still reads the old
	// archive, and the active URL reads the newly selected archive.
	for path, want := range map[string]string{"/stream/100/aaaa/page/1": "old-page", "/stream/200/bbbb/page/1": "active-page"} {
		response := get(path)
		if got := response.Body.String(); got != want {
			t.Fatalf("GET %s returned %q, want %q", path, got, want)
		}
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

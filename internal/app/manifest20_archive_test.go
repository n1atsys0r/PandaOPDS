package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/document"
	"pandaopds/internal/eh"
	"pandaopds/internal/feed"
	"pandaopds/internal/storage"
)

const fixtureGalleryGID = 4115099
const fixtureGalleryToken = "8ece1a7eb7"

// galleryDetailFixtureTransport serves the recorded detail page for one
// gallery, so manifest tests can assert on real upstream-derived metadata.
type galleryDetailFixtureTransport struct{ body []byte }

func (f galleryDetailFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(string(f.body))), Request: r}, nil
}

// associatedArchiveServer builds a server whose gallery is backed by an
// associated local ZIP whose page count differs from upstream metadata.
func associatedArchiveServer(t *testing.T, transport http.RoundTripper) (*Server, string) {
	t.Helper()
	fixture, err := os.ReadFile("../../tests/fixtures/detail_page.html")
	if err != nil {
		t.Fatal(err)
	}
	if transport == nil {
		transport = galleryDetailFixtureTransport{body: fixture}
	}
	base := t.TempDir()
	archiveRoot := filepath.Join(base, "archive")
	writeTestArchive(t, filepath.Join(archiveRoot, "archived.cbz"), "page")
	db, err := storage.Open(filepath.Join(base, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	docs, err := document.New(db, archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.ScanDeep(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := mustFirstDocumentID(t, docs)
	if err = docs.Associate(context.Background(), id, fixtureGalleryGID, fixtureGalleryToken); err != nil {
		t.Fatal(err)
	}
	store, err := archive.NewStoreWithSQLite(archiveRoot, filepath.Join(base, "state"), db.DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	settings := config.Settings{
		EHSite: "e-hentai", PSEPageBase: 1, CommentsEnabled: true,
		TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0,
		PageURLTTLSeconds: 60, CacheDir: filepath.Join(base, "cache"),
		ImageProxyHosts: []string{"ehgt.org"},
	}
	service := eh.NewService(settings, transport)
	t.Cleanup(func() { service.Close() })
	service.AttachArchive(store)
	server := &Server{Settings: settings, Service: service, Store: store, Documents: docs}
	return server, id
}

func manifestRequest(gid int, token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery/"+strconv.Itoa(gid)+"/"+token+"/manifest", nil)
	r.SetPathValue("gid", strconv.Itoa(gid))
	r.SetPathValue("token", token)
	return r
}

func decodeManifest(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("manifest status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != feed.MIMEDivina {
		t.Fatalf("manifest content-type=%q, want %q", got, feed.MIMEDivina)
	}
	var doc map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &doc); err != nil {
		t.Fatalf("manifest body: %v (%s)", err, recorder.Body.String())
	}
	return doc
}

// An archived gallery with a readable local ZIP must still expose upstream
// detail metadata (comments, tags, favorites) while pages come from the file.
func TestArchiveManifestKeepsUpstreamMetadataAndLocalPages(t *testing.T) {
	server, _ := associatedArchiveServer(t, nil)
	recorder := httptest.NewRecorder()
	server.manifest20(recorder, manifestRequest(fixtureGalleryGID, fixtureGalleryToken))
	doc := decodeManifest(t, recorder)
	metadata := doc["metadata"].(map[string]any)

	reviews, ok := metadata["x:reviews"].([]any)
	if !ok || len(reviews) == 0 {
		t.Fatalf("archived manifest dropped upstream reviews: %v", metadata)
	}
	if _, ok := metadata["subject"]; !ok {
		t.Fatalf("archived manifest dropped upstream subjects: %v", metadata)
	}
	if got := metadata["title"]; got == "archived" || got == nil {
		t.Fatalf("archived manifest kept the ZIP file name instead of the gallery title: %v", got)
	}

	reading := doc["readingOrder"].([]any)
	if len(reading) != 1 {
		t.Fatalf("readingOrder pages=%d, want the local file's 1 page", len(reading))
	}
	if got, want := metadata["numberOfPages"], float64(1); got != want {
		t.Fatalf("numberOfPages=%v, want the local file's %v pages", got, want)
	}
	href := reading[0].(map[string]any)["href"].(string)
	if want := "/stream/4115099/8ece1a7eb7/page/1"; href != want {
		t.Fatalf("reading href=%q, want local ZIP route %q", href, want)
	}
	if got := reading[0].(map[string]any)["type"]; got != "image/png" {
		t.Fatalf("reading type=%v, want the local page MIME image/png", got)
	}
	conformsTo, _ := metadata["conformsTo"].([]any)
	if len(conformsTo) != 1 || conformsTo[0] != feed.DivinaProfile {
		t.Fatalf("conformsTo=%v", metadata["conformsTo"])
	}
}

// When upstream is unavailable the manifest must fall back to the local
// snapshot so an archived gallery stays readable.
func TestArchiveManifestFallsBackToLocalSnapshotWhenUpstreamFails(t *testing.T) {
	server, _ := associatedArchiveServer(t, unavailableGalleryTransport{})
	recorder := httptest.NewRecorder()
	server.manifest20(recorder, manifestRequest(fixtureGalleryGID, fixtureGalleryToken))
	doc := decodeManifest(t, recorder)
	metadata := doc["metadata"].(map[string]any)

	if got, want := metadata["identifier"], "urn:ehentai:gallery:4115099:8ece1a7eb7"; got != want {
		t.Fatalf("fallback identifier=%v, want %q", got, want)
	}
	if got, want := metadata["numberOfPages"], float64(1); got != want {
		t.Fatalf("fallback numberOfPages=%v, want %v", got, want)
	}
	reading := doc["readingOrder"].([]any)
	if len(reading) != 1 || reading[0].(map[string]any)["href"] != "/stream/4115099/8ece1a7eb7/page/1" {
		t.Fatalf("fallback readingOrder=%v", reading)
	}
}

// A gallery without an associated ZIP keeps the plain upstream behaviour and
// still fails when upstream is unavailable.
func TestManifestWithoutLocalArchiveKeepsUpstreamErrors(t *testing.T) {
	base := t.TempDir()
	settings := config.Settings{EHSite: "e-hentai", PSEPageBase: 1, TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, CacheDir: filepath.Join(base, "cache")}
	service := eh.NewService(settings, unavailableGalleryTransport{})
	defer service.Close()
	server := &Server{Settings: settings, Service: service}
	recorder := httptest.NewRecorder()
	server.manifest20(recorder, manifestRequest(123, "abc"))
	if recorder.Code == http.StatusOK {
		t.Fatalf("manifest unexpectedly succeeded without upstream or local archive: %s", recorder.Body.String())
	}
}

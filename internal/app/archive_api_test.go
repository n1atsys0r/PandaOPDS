package app

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/eh"
)

func TestArchiveListCoverUsesDecimalGalleryID(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	const gid = 1234567
	const token = "abcdef1234"
	if err := os.WriteFile(settings.FavoritesSyncState, []byte(`{"known":["1234567:abcdef1234"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := server.Store.Upsert(gid, token, map[string]any{"status": "ready"}); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(settings.ArchiveDir, fmt.Sprintf("%d.zip", gid))
	writeTestArchiveZIP(t, zipPath)
	if _, err := server.Documents.Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Documents.RegisterPublished(t.Context(), filepath.Base(zipPath), gid, token, "download_job"); err != nil {
		t.Fatal(err)
	}
	coverBytes := []byte("\xff\xd8\xff\xe0local cover bytes")
	if err := server.Store.WriteCover(gid, token, coverBytes); err != nil {
		t.Fatal(err)
	}

	listResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/archive", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("archive list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var payload struct {
		Entries []struct {
			CoverURL    string `json:"cover_url"`
			IsFavorited bool   `json:"is_favorited"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Entries) != 1 {
		t.Fatalf("archive list returned %d entries, want 1", len(payload.Entries))
	}
	entry := payload.Entries[0]
	coverResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(coverResponse, httptest.NewRequest(http.MethodGet, entry.CoverURL, nil))
	if coverResponse.Code != http.StatusOK {
		t.Fatalf("cover response status=%d for emitted URL %q body=%q", coverResponse.Code, entry.CoverURL, coverResponse.Body.String())
	}
	if entry.CoverURL != "/api/publications/eh/1234567/abcdef1234/cover" {
		t.Fatalf("cover URL=%q, want decimal gallery ID", entry.CoverURL)
	}
	if !strings.Contains(coverResponse.Header().Get("Content-Type"), "image/jpeg") || string(coverResponse.Body.Bytes()) != string(coverBytes) {
		t.Fatalf("cover response status=%d content-type=%q body=%q", coverResponse.Code, coverResponse.Header().Get("Content-Type"), coverResponse.Body.String())
	}
	if !entry.IsFavorited {
		t.Fatal("archive entry was not matched to its favorites key")
	}
}

type staticRoundTripper func(*http.Request) (*http.Response, error)

func (f staticRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUnarchivedEntryStatusAndFavoriteRemoveCleanup(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	settings.IPBMemberID = "1"
	settings.IPBPassHash = "hash"
	if err := os.WriteFile(settings.FavoritesSyncState, []byte(`{"known":["7001:aa7001","7002:bb7002"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.Service.Client = eh.NewClient(settings, staticRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<html>ok</html>")), Request: r}, nil
	}), server.Service.Throttle)

	if _, err := server.Store.Catalog(7001, "aa7001", map[string]any{"title": "Unarchived Fav"}, map[string]any{"title": "Unarchived Fav", "filecount": 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Store.Upsert(7002, "bb7002", map[string]any{"status": "ready", "title": "Ready Fav"}); err != nil {
		t.Fatal(err)
	}

	statusRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusRes, httptest.NewRequest(http.MethodGet, "/api/archive/7001/aa7001", nil))
	if statusRes.Code != http.StatusOK || !strings.Contains(statusRes.Body.String(), `"unarchived"`) {
		t.Fatalf("GET unarchived status=%d body=%s", statusRes.Code, statusRes.Body.String())
	}
	absentRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(absentRes, httptest.NewRequest(http.MethodGet, "/api/archive/9999/deadbeef00", nil))
	if absentRes.Code != http.StatusNotFound {
		t.Fatalf("GET unknown status=%d, want 404", absentRes.Code)
	}

	removeReq := httptest.NewRequest(http.MethodPost, "/api/favorites", strings.NewReader(`{"action":"remove","items":[{"gid":7001,"token":"aa7001"},{"gid":7002,"token":"bb7002"}]}`))
	removeReq.Header.Set("Content-Type", "application/json")
	removeRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(removeRes, removeReq)
	if removeRes.Code != http.StatusOK {
		t.Fatalf("remove favorite status=%d body=%s", removeRes.Code, removeRes.Body.String())
	}
	if server.Store.Get(7001, "aa7001") == nil {
		t.Fatal("favorite removal mutated archive state")
	}
	list := httptest.NewRecorder()
	server.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/archive", nil))
	if list.Code != 200 || strings.Contains(list.Body.String(), `"gid":7001`) {
		t.Fatalf("archive API exposed a non-file catalog record: %d %s", list.Code, list.Body.String())
	}
	if server.Store.Get(7002, "bb7002") == nil {
		t.Fatal("ready archive entry should remain after favorite removal")
	}
}

func TestArchiveCoverLazyFetchesAndPersistsThumbOnDemand(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	settings.ImageProxyHosts = []string{"ehgt.org"}
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := server.Store.Catalog(7003, "cc7003", map[string]any{"title": "Lazy Cover"}, map[string]any{"title": "Lazy Cover", "thumb": "https://ehgt.org/t/7003.jpg"}); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(settings.ArchiveDir, "7003.zip")
	writeTestArchiveZIP(t, zipPath)
	if _, err := server.Documents.Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	registered, err := server.Documents.RegisterPublished(t.Context(), "7003.zip", 7003, "cc7003", "download_job")
	if err != nil {
		t.Fatal(err)
	}

	var thumbFetches int
	coverBytes := []byte("\xff\xd8\xff\xe0lazy-cover")
	server.Service.Client = eh.NewClient(settings, staticRoundTripper(func(r *http.Request) (*http.Response, error) {
		thumbFetches++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader(string(coverBytes))), Request: r}, nil
	}), server.Service.Throttle)

	listRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRes, httptest.NewRequest(http.MethodGet, "/api/archive", nil))
	if listRes.Code != 200 || !strings.Contains(listRes.Body.String(), `"/image/documents/`+registered.ID+`/thumb"`) {
		t.Fatalf("archive list missing lazy cover_url: status=%d body=%s", listRes.Code, listRes.Body.String())
	}

	// First request fetches upstream and persists cover.jpg.
	coverRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(coverRes, httptest.NewRequest(http.MethodGet, "/api/archive/7003/cc7003/cover", nil))
	if coverRes.Code != 200 || string(coverRes.Body.Bytes()) != string(coverBytes) || thumbFetches != 1 {
		t.Fatalf("first cover fetch status=%d fetches=%d", coverRes.Code, thumbFetches)
	}
	// Second request hits persisted local cover.jpg with zero upstream requests.
	coverRes2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(coverRes2, httptest.NewRequest(http.MethodGet, "/api/archive/7003/cc7003/cover", nil))
	if coverRes2.Code != 200 || string(coverRes2.Body.Bytes()) != string(coverBytes) || thumbFetches != 1 {
		t.Fatalf("second cover fetch status=%d fetches=%d (want 1)", coverRes2.Code, thumbFetches)
	}
	publicationCover := httptest.NewRecorder()
	server.Handler().ServeHTTP(publicationCover, httptest.NewRequest(http.MethodGet, "/api/publications/eh/7003/cc7003/cover", nil))
	if publicationCover.Code != 200 || string(publicationCover.Body.Bytes()) != string(coverBytes) || thumbFetches != 1 {
		t.Fatalf("cached publication cover status=%d fetches=%d", publicationCover.Code, thumbFetches)
	}
}

func writeTestArchiveZIP(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("01.png")
	if err != nil {
		t.Fatal(err)
	}
	imageBytes, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/gXcAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(imageBytes); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFavoriteRemovalDoesNotDeleteArchiveAndArchiveDeleteDoesNotUnsubscribe(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	settings.IPBMemberID = "1"
	settings.IPBPassHash = "hash"
	if err := os.WriteFile(settings.FavoritesSyncState, []byte(`{"known":["9001:aa9001"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	zipPath := filepath.Join(settings.ArchiveDir, "9001.zip")
	writeTestArchiveZIP(t, zipPath)
	if _, err = server.Documents.Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = server.Documents.RegisterPublished(t.Context(), "9001.zip", 9001, "aa9001", "download_job"); err != nil {
		t.Fatal(err)
	}
	remove := httptest.NewRequest(http.MethodPost, "/api/favorites", strings.NewReader(`{"action":"remove","gid":9001,"token":"aa9001"}`))
	remove.Header.Set("Content-Type", "application/json")
	removeRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(removeRes, remove)
	if removeRes.Code != 200 {
		t.Fatalf("favorite remove=%d %s", removeRes.Code, removeRes.Body.String())
	}
	if _, err = os.Stat(zipPath); err != nil {
		t.Fatalf("favorite removal deleted archive: %v", err)
	}
	if _, _, err = server.Documents.List(t.Context(), 1, 20, "available", ""); err != nil {
		t.Fatal(err)
	}
	deleteRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRes, httptest.NewRequest(http.MethodDelete, "/api/archive/9001/aa9001", nil))
	if deleteRes.Code != 200 {
		t.Fatalf("archive delete=%d %s", deleteRes.Code, deleteRes.Body.String())
	}
	if _, err = os.Stat(zipPath); !os.IsNotExist(err) {
		t.Fatalf("archive delete retained ZIP: %v", err)
	}
	var suppressed int
	if err = server.Database.QueryRow(`SELECT COUNT(*) FROM download_suppressions WHERE provider='eh' AND gid=9001 AND token='aa9001'`).Scan(&suppressed); err != nil || suppressed != 1 {
		t.Fatalf("suppression=%d err=%v", suppressed, err)
	}
	memberships, err := server.Subscriptions.Store.FavoriteMemberships(t.Context(), server.FavoriteSubscriptionID)
	_, stillMember := memberships["9001:aa9001"]
	if err != nil || !stillMember {
		t.Fatalf("favorite membership lost after archive delete: memberships=%v err=%v", memberships, err)
	}
}

func TestFavoriteCategoriesParsesAfterNavbarNoselAndFallsBackToSyncState(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	settings.IPBMemberID = "1"
	settings.IPBPassHash = "hash"
	if err := os.WriteFile(settings.FavoritesSyncState, []byte(`{"favcat_map":{"0":"SavedZero","1":"SavedOne"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	// Upstream returns navbar <div id="nb" class="nosel"> before the real category picker.
	server.Service.Client = eh.NewClient(settings, staticRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := `<!doctype html><html><body>
		<div id="nb" class="nosel"><a href="/">Front</a></div>
		<div class="nosel"><div class="fp" onclick="popUp('https://e-hentai.org/gallerypopups.php?favcat=0')"><div>0</div><div></div><div>仮</div></div></div>
		</body></html>`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}), server.Service.Throttle)

	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/favorites/categories", nil))
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"仮"`) {
		t.Fatalf("categories=%d body=%s", res.Code, res.Body.String())
	}

	// When upstream returns empty categories (no picker), fall back to Favorites.State.FavcatMap().
	_ = server.Service.ClearCache()
	server.Service.Client = eh.NewClient(settings, staticRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`<html><body><div id="nb" class="nosel"></div></body></html>`)), Request: r}, nil
	}), server.Service.Throttle)
	fallbackRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(fallbackRes, httptest.NewRequest(http.MethodGet, "/api/favorites/categories", nil))
	if fallbackRes.Code != 200 || !strings.Contains(fallbackRes.Body.String(), `"SavedZero"`) {
		t.Fatalf("fallback categories=%d body=%s", fallbackRes.Code, fallbackRes.Body.String())
	}
}

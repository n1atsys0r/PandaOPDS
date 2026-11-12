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
	coverBytes := []byte("local cover bytes")
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
	if entry.CoverURL != "/api/archive/1234567/abcdef1234/cover" {
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
	if server.Store.Get(7001, "aa7001") != nil {
		t.Fatal("unarchived catalog entry was not cleaned up after favorite removal")
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

	var thumbFetches int
	coverBytes := []byte("\xff\xd8\xff\xe0lazy-cover")
	server.Service.Client = eh.NewClient(settings, staticRoundTripper(func(r *http.Request) (*http.Response, error) {
		thumbFetches++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader(string(coverBytes))), Request: r}, nil
	}), server.Service.Throttle)

	if _, err := server.Store.Catalog(7003, "cc7003", map[string]any{"title": "Lazy Cover"}, map[string]any{"title": "Lazy Cover", "thumb": "https://ehgt.org/t/7003.jpg"}); err != nil {
		t.Fatal(err)
	}

	listRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRes, httptest.NewRequest(http.MethodGet, "/api/archive", nil))
	if listRes.Code != 200 || !strings.Contains(listRes.Body.String(), `"/api/archive/7003/cc7003/cover"`) {
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
	w, err := zw.Create("01.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("img")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteArchiveRetainsUnarchivedForFavoritedAndRemovesNonFavorited(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	settings.IPBMemberID = "1"
	settings.IPBPassHash = "hash"
	if err := os.WriteFile(settings.FavoritesSyncState, []byte(`{"known":["9001:aa9001","9002:bb9002","9004:dd9004"],"archived":["9001:aa9001"],"errors":{"9002:bb9002":"old err"}}`), 0o600); err != nil {
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

	// 9001: favorited ready archive
	if _, err := server.Store.Upsert(9001, "aa9001", map[string]any{"status": "ready", "or": "org", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeTestArchiveZIP(t, filepath.Join(settings.ArchiveDir, "9001.zip"))
	if err := server.Store.WriteMetadata(9001, "aa9001", map[string]any{"gid": 9001, "token": "aa9001", "title": "Fav Ready", "filecount": 10}); err != nil {
		t.Fatal(err)
	}

	// 9002: favorited failed archive with staging partial
	if _, err := server.Store.Upsert(9002, "bb9002", map[string]any{"status": "failed", "error": "boom"}); err != nil {
		t.Fatal(err)
	}
	stagePart := filepath.Join(settings.CacheDir, "archive-staging", "9002", "bb9002", "archive.part")
	if err := os.MkdirAll(filepath.Dir(stagePart), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagePart, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 9003 -> 9004: shelf updated to 9004 where 9004 is in favorites known
	if _, err := server.Store.Upsert(9003, "cc9003", map[string]any{"status": "ready", "or": "org", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeTestArchiveZIP(t, filepath.Join(settings.ArchiveDir, "9003.zip"))
	if _, err := server.Store.Upsert(9004, "dd9004", map[string]any{"root_gid": 9003, "root_token": "cc9003", "is_version_update": true, "status": "ready", "or": "org", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeTestArchiveZIP(t, filepath.Join(settings.ArchiveDir, "9004.zip"))
	if err := server.Store.ActivateVersion(9003, "cc9003", 9004, "dd9004", map[string]any{"gid": 9004, "token": "dd9004", "title": "V2"}, []byte("c"), true); err != nil {
		t.Fatal(err)
	}

	// 9005: non-favorited ready archive
	if _, err := server.Store.Upsert(9005, "ee9005", map[string]any{"status": "ready", "or": "org", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeTestArchiveZIP(t, filepath.Join(settings.ArchiveDir, "9005.zip"))

	// 9006: non-favorited ready archive -> user adds to favorites -> then deletes archive
	if _, err := server.Store.Upsert(9006, "ff9006", map[string]any{"status": "ready", "or": "org", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	writeTestArchiveZIP(t, filepath.Join(settings.ArchiveDir, "9006.zip"))

	// Delete 9001 (favorited) -> demotes to unarchived, deletes ZIP, clears archived flag, keeps known
	del9001 := httptest.NewRecorder()
	server.Handler().ServeHTTP(del9001, httptest.NewRequest(http.MethodDelete, "/api/archive/9001/aa9001", nil))
	if del9001.Code != 200 || !strings.Contains(del9001.Body.String(), `"retained_catalog":true`) {
		t.Fatalf("DELETE 9001=%d %s", del9001.Code, del9001.Body.String())
	}
	if _, err := os.Stat(filepath.Join(settings.ArchiveDir, "9001.zip")); !os.IsNotExist(err) {
		t.Fatalf("9001.zip still exists: %v", err)
	}
	if got := server.Store.Get(9001, "aa9001"); got == nil || got["status"] != "unarchived" {
		t.Fatalf("9001 after delete=%#v, want unarchived", got)
	}
	if server.Favorites.State.Archived()["9001:aa9001"] || !server.Favorites.State.Known()["9001:aa9001"] {
		t.Fatalf("9001 favorites tracking=%v known=%v", server.Favorites.State.Archived(), server.Favorites.State.Known())
	}

	// Removing 9001 from favorites now auto-deletes the unarchived record
	rem9001 := httptest.NewRequest(http.MethodPost, "/api/favorites", strings.NewReader(`{"action":"remove","gid":9001,"token":"aa9001"}`))
	rem9001.Header.Set("Content-Type", "application/json")
	rem9001Res := httptest.NewRecorder()
	server.Handler().ServeHTTP(rem9001Res, rem9001)
	if rem9001Res.Code != 200 || server.Store.Get(9001, "aa9001") != nil {
		t.Fatalf("9001 still present after favorite remove: code=%d store=%#v", rem9001Res.Code, server.Store.Get(9001, "aa9001"))
	}

	// Clear failed 9002 (favorited) -> removes staging part, clears error, demotes to unarchived
	del9002 := httptest.NewRecorder()
	server.Handler().ServeHTTP(del9002, httptest.NewRequest(http.MethodDelete, "/api/archive/9002/bb9002", nil))
	if del9002.Code != 200 || !strings.Contains(del9002.Body.String(), `"retained_catalog":true`) {
		t.Fatalf("DELETE 9002=%d %s", del9002.Code, del9002.Body.String())
	}
	if _, err := os.Stat(stagePart); !os.IsNotExist(err) {
		t.Fatalf("9002 staging part survived: %v", err)
	}
	if got := server.Store.Get(9002, "bb9002"); got == nil || got["status"] != "unarchived" {
		t.Fatalf("9002 after clear=%#v", got)
	}
	if server.Favorites.State.Errors()["9002:bb9002"] != "" {
		t.Fatalf("9002 favorite error not cleared: %v", server.Favorites.State.Errors())
	}

	// Delete 9003 (version 9004 is favorited) -> retains 9003 as unarchived
	del9003 := httptest.NewRecorder()
	server.Handler().ServeHTTP(del9003, httptest.NewRequest(http.MethodDelete, "/api/archive/9003/cc9003", nil))
	if del9003.Code != 200 || !strings.Contains(del9003.Body.String(), `"retained_catalog":true`) {
		t.Fatalf("DELETE 9003=%d %s", del9003.Code, del9003.Body.String())
	}
	if got := server.Store.Get(9003, "cc9003"); got == nil || got["status"] != "unarchived" {
		t.Fatalf("9003 after delete=%#v", got)
	}

	// Delete 9005 (not favorited) -> removes completely
	del9005 := httptest.NewRecorder()
	server.Handler().ServeHTTP(del9005, httptest.NewRequest(http.MethodDelete, "/api/archive/9005/ee9005", nil))
	if del9005.Code != 200 || !strings.Contains(del9005.Body.String(), `"retained_catalog":false`) {
		t.Fatalf("DELETE 9005=%d %s", del9005.Code, del9005.Body.String())
	}
	if server.Store.Get(9005, "ee9005") != nil {
		t.Fatal("9005 non-favorited archive was not removed")
	}

	// 9006: add to favorites first, then delete archive -> retained as unarchived until removed from favorites
	add9006 := httptest.NewRequest(http.MethodPost, "/api/favorites", strings.NewReader(`{"action":"add","gid":9006,"token":"ff9006","favcat":0}`))
	add9006.Header.Set("Content-Type", "application/json")
	add9006Res := httptest.NewRecorder()
	server.Handler().ServeHTTP(add9006Res, add9006)
	if add9006Res.Code != 200 {
		t.Fatalf("add 9006 favorite=%d %s", add9006Res.Code, add9006Res.Body.String())
	}
	del9006 := httptest.NewRecorder()
	server.Handler().ServeHTTP(del9006, httptest.NewRequest(http.MethodDelete, "/api/archive/9006/ff9006", nil))
	if del9006.Code != 200 || !strings.Contains(del9006.Body.String(), `"retained_catalog":true`) {
		t.Fatalf("DELETE 9006=%d %s", del9006.Code, del9006.Body.String())
	}
	if got := server.Store.Get(9006, "ff9006"); got == nil || got["status"] != "unarchived" {
		t.Fatalf("9006 after delete=%#v, want unarchived", got)
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

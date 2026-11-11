package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"pandaopds/internal/config"
)

func TestRootWebUIUsesEmbeddedAssetFromAnyWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "PandaOPDS") {
		t.Fatalf("embedded web UI response status=%d body=%q", response.Code, response.Body.String())
	}
	script := regexp.MustCompile(`src="(/webui-assets/assets/[^"/]+\.js)"`).FindStringSubmatch(response.Body.String())
	if len(script) != 2 {
		t.Fatalf("embedded web UI does not reference a bundled script: %q", response.Body.String())
	}
	asset := httptest.NewRecorder()
	server.Handler().ServeHTTP(asset, httptest.NewRequest(http.MethodGet, script[1], nil))
	if asset.Code != http.StatusOK || !strings.Contains(asset.Header().Get("Content-Type"), "javascript") || asset.Body.Len() == 0 {
		t.Fatalf("embedded script status=%d type=%q length=%d", asset.Code, asset.Header().Get("Content-Type"), asset.Body.Len())
	}
}

func loadTestSettings(base string) config.Settings {
	return config.Settings{
		EHSite: "e-hentai", CacheDir: filepath.Join(base, "cache"), ArchiveDir: filepath.Join(base, "archives"),
		HomeConfigPath: filepath.Join(base, "home.toml"), FavoritesSyncState: filepath.Join(base, "favorites.json"),
		MyTagsState: filepath.Join(base, "mytags.json"), TagTranslationState: filepath.Join(base, "translations.json"),
		ImageCacheEnabled: false, FavoritesSyncIntervalSeconds: 0,
	}
}

func TestWebUIAuthBehavior(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	settings.AuthUsername = "admin"
	settings.AuthPassword = "password123"
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for /favicon.ico, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for /api/status, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("expected no WWW-Authenticate header on /api/status, got %q", rec.Header().Get("WWW-Authenticate"))
	}

	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for /opds/v1.2, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("expected WWW-Authenticate header on /opds/v1.2 for OPDS reader clients")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.SetBasicAuth("admin", "password123")
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated /api/status, got %d", rec.Code)
	}
}

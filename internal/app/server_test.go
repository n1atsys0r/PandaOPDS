package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
}

func loadTestSettings(base string) config.Settings {
	return config.Settings{
		EHSite: "e-hentai", CacheDir: filepath.Join(base, "cache"), ArchiveDir: filepath.Join(base, "archives"),
		HomeConfigPath: filepath.Join(base, "home.toml"), FavoritesSyncState: filepath.Join(base, "favorites.json"),
		MyTagsState: filepath.Join(base, "mytags.json"), TagTranslationState: filepath.Join(base, "translations.json"),
		ImageCacheEnabled: false, FavoritesSyncIntervalSeconds: 0,
	}
}

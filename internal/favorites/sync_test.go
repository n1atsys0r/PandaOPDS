package favorites

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

type favoriteReplayTransport func(*http.Request) (*http.Response, error)

func (f favoriteReplayTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFavoritesFirstRunBaselineAndDuplicateDoNotAutoArchive(t *testing.T) {
	cfg := config.Settings{EHSite: "e-hentai", IPBMemberID: "1", IPBPassHash: "hash", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, ListCacheTTLSeconds: 600, PageURLTTLSeconds: 3600, FavoritesSyncArchive: true, FavoritesSyncMaxPages: 2, FavoritesSyncMatchThreshold: 5, FavoritesSyncState: filepath.Join(t.TempDir(), "favorites_sync.json"), CacheDir: t.TempDir()}
	service := eh.NewService(cfg, favoriteReplayTransport(func(r *http.Request) (*http.Response, error) {
		body := `<html></html>`
		if r.URL.Path == "/favorites.php" {
			body = `<div class="gl1e"><a href="https://e-hentai.org/g/123456/abcdef/" class="glink">Gallery</a><div class="cs">Manga</div></div>`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}))
	defer service.Close()
	s := NewSyncer(cfg, service, nil, NewState(cfg.FavoritesSyncState))
	first, err := s.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first["baseline"] != true || s.State.Summary()["known_count"] != 1 || s.State.Summary()["archived_count"] != 0 {
		t.Fatalf("first scan did not establish non-archiving baseline: result=%#v status=%#v", first, s.Status())
	}
	second, err := s.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second["new_count"] != 0 || s.State.Summary()["archived_count"] != 0 {
		t.Fatalf("duplicate scan changed baseline/archive state: result=%#v status=%#v", second, s.Status())
	}
}

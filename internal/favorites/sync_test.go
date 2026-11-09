package favorites

import (
	"context"
	"fmt"
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

func TestFavoritesSyncFetchesFreshSortedPagesAndStopsAtKnownStreak(t *testing.T) {
	const cursor = "2753175-1786365950"
	const cursor2 = "3000000-1786366000"
	pages := []string{
		favoritesSyncHTML([]favoritesSyncGallery{
			{10, "aa10"}, {11, "aa11"}, {20, "bb20"}, {12, "aa12"}, {13, "aa13"}, {21, "bb21"},
		}, cursor),
		favoritesSyncHTML([]favoritesSyncGallery{
			{10, "aa10"}, {11, "aa11"}, {20, "bb20"}, {12, "aa12"}, {13, "aa13"},
			{22, "bb22"}, {14, "aa14"}, {15, "aa15"},
		}, cursor2),
		favoritesSyncHTML([]favoritesSyncGallery{
			{16, "aa16"}, {99, "bb99"},
		}, ""),
		favoritesSyncHTML([]favoritesSyncGallery{
			{30, "bb30"}, {10, "aa10"}, {11, "aa11"}, {31, "bb31"}, {12, "aa12"}, {13, "aa13"},
		}, "3200000-1786366200"),
		favoritesSyncHTML([]favoritesSyncGallery{
			{31, "bb31"}, {14, "aa14"}, {15, "aa15"}, {16, "aa16"}, {99, "bb98"},
		}, ""),
		favoritesSyncHTML(nil, "4000000-1786366100"),
	}
	var favoritesRequests int
	var cursors, inlineSets []string
	cfg := config.Settings{
		EHSite: "e-hentai", IPBMemberID: "1", IPBPassHash: "hash", TimeoutSeconds: 2,
		HTMLIntervalSeconds: 0, Retries: 0, ListCacheTTLSeconds: 600, FavoritesSyncMaxPages: 5,
		FavoritesSyncMatchThreshold: 3, FavoritesSyncState: filepath.Join(t.TempDir(), "favorites.json"),
		CacheDir: t.TempDir(), ImageCacheEnabled: false,
	}
	service := eh.NewService(cfg, favoriteReplayTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`<html>session</html>`)), Request: r}, nil
		}
		if r.URL.Path != "/favorites.php" {
			return nil, fmt.Errorf("unexpected upstream path %s", r.URL.Path)
		}
		if favoritesRequests >= len(pages) {
			return nil, fmt.Errorf("unexpected favorites request %d", favoritesRequests+1)
		}
		query := r.URL.Query()
		cursors = append(cursors, query.Get("next"))
		inlineSets = append(inlineSets, query.Get("inline_set"))
		favoritesRequests++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(pages[favoritesRequests-1])), Request: r}, nil
	}))
	defer service.Close()
	state := NewState(cfg.FavoritesSyncState)
	baseline := true
	knownAtStart := map[string]bool{
		"10:aa10": true, "11:aa11": true, "12:aa12": true, "13:aa13": true,
		"14:aa14": true, "15:aa15": true, "16:aa16": true,
	}
	if err := state.Update(knownAtStart, nil, nil, nil, nil, &baseline, nil, nil); err != nil {
		t.Fatal(err)
	}
	syncer := NewSyncer(cfg, service, nil, state)

	first, err := syncer.Run(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if first["new_count"] != 3 || first["scanned_pages"] != 3 {
		t.Fatalf("first sync result=%#v known=%v, want three unique new keys over three pages", first, state.Known())
	}
	known := state.Known()
	for _, key := range []string{"10:aa10", "11:aa11", "12:aa12", "13:aa13", "14:aa14", "15:aa15", "16:aa16", "20:bb20", "21:bb21", "22:bb22"} {
		if !known[key] {
			t.Errorf("successful scan did not record %q", key)
		}
	}
	if known["99:bb99"] {
		t.Error("sync did not carry the known streak across pages or continued after threshold")
	}
	if favoritesRequests != 3 || len(cursors) != 3 || cursors[0] != "" || cursors[1] != cursor || cursors[2] != cursor2 {
		t.Fatalf("first scan request cursors=%v requests=%d, want empty and both opaque cursors", cursors, favoritesRequests)
	}
	second, err := syncer.Run(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second["new_count"] != 2 || second["scanned_pages"] != 2 {
		t.Fatalf("second sync result=%#v, want fresh two-page response with two unique new keys", second)
	}
	known = state.Known()
	if !known["30:bb30"] || !known["31:bb31"] || known["99:bb98"] {
		t.Fatalf("second sync did not use fresh response or stopped too late: known=%v", known)
	}
	if favoritesRequests != 5 || cursors[3] != "" || cursors[4] != "3200000-1786366200" {
		t.Fatalf("second sync requests=%d cursors=%v, want another uncached scan with opaque cursors", favoritesRequests, cursors)
	}

	third, err := syncer.Run(context.Background())
	if err != nil {
		t.Fatalf("empty-page sync: %v", err)
	}
	if third["new_count"] != 0 || third["scanned_pages"] != 1 || favoritesRequests != 6 {
		t.Fatalf("empty-page sync result=%#v requests=%d, want one empty page and no cursor fetch", third, favoritesRequests)
	}
	for _, inlineSet := range inlineSets {
		if inlineSet != "fs_f dm_e" {
			t.Errorf("sync inline_set=%q, want fs_f dm_e", inlineSet)
		}
	}
}

type favoritesSyncGallery struct {
	gid   int
	token string
}

func favoritesSyncHTML(galleries []favoritesSyncGallery, next string) string {
	var body strings.Builder
	body.WriteString(`<!doctype html><html><body>`)
	for _, gallery := range galleries {
		fmt.Fprintf(&body, `<div class="gl1e"><a class="glink" href="/g/%d/%s/">Gallery %d</a><div class="cs">Manga</div></div>`, gallery.gid, gallery.token, gallery.gid)
	}
	if next != "" {
		fmt.Fprintf(&body, `<a id="unext" href="/favorites.php?next=%s">Next</a>`, next)
	}
	body.WriteString(`</body></html>`)
	return body.String()
}

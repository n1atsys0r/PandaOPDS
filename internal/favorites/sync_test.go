package favorites

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/archive"
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

func TestFavoritesSyncCatalogsIntoArchiveStateWithoutExtraAPIOrCoverRequests(t *testing.T) {
	temp := t.TempDir()
	cfg := config.Settings{
		EHSite: "e-hentai", IPBMemberID: "1", IPBPassHash: "hash", TimeoutSeconds: 2,
		HTMLIntervalSeconds: 0, Retries: 0, ListCacheTTLSeconds: 600, PageURLTTLSeconds: 3600,
		FavoritesSyncArchive: false, FavoritesSyncMaxPages: 5, FavoritesSyncMatchThreshold: 5,
		FavoritesSyncState: filepath.Join(temp, "favorites_sync.json"),
		CacheDir:           filepath.Join(temp, "cache"),
		ArchiveDir:         filepath.Join(temp, "archives"),
		ArchiveStateDir:    filepath.Join(temp, "config", "archive"),
	}
	var archiverPosts, extraRequests int
	service := eh.NewService(cfg, favoriteReplayTransport(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/":
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`<html>session</html>`)), Request: r}, nil
		case r.URL.Path == "/favorites.php":
			body := `<!doctype html><html><body>
				<div class="gl1e">
					<a class="glink" href="https://e-hentai.org/g/123456/abcdef/">Cataloged Fav</a>
					<div class="cs">Doujinshi</div>
					<img src="https://ehgt.org/t/123456.jpg"/>
					<div id="posted_123456" title="Fav 0">2026-01-02 15:04</div>
					<div>24 pages</div>
				</div>
			</body></html>`
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		case r.URL.Path == "/archiver.php":
			archiverPosts++
			return nil, fmt.Errorf("unexpected archiver call")
		default:
			extraRequests++
			return nil, fmt.Errorf("unexpected extra request %s", r.URL.String())
		}
	}))
	defer service.Close()

	store, err := archive.NewStoreWithState(cfg.ArchiveDir, cfg.ArchiveStateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := archive.NewManager(cfg, service.Client, service, store)
	defer manager.Close()

	syncer := NewSyncer(cfg, service, manager, NewState(cfg.FavoritesSyncState))
	res, err := syncer.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res["cataloged_count"] != 1 || archiverPosts != 0 || extraRequests != 0 {
		t.Fatalf("res=%#v archiverPosts=%d extraRequests=%d", res, archiverPosts, extraRequests)
	}
	entry := store.Get(123456, "abcdef")
	if entry == nil || entry["status"] != "unarchived" {
		t.Fatalf("cataloged entry=%#v, want status=unarchived", entry)
	}
	if store.ReadyCount() != 0 {
		t.Fatalf("ReadyCount=%d, want 0", store.ReadyCount())
	}
	snap, err := store.ReadMetadata(123456, "abcdef")
	if err != nil || snap["title"] != "Cataloged Fav" || snap["filecount"] != float64(24) || snap["thumb"] != "https://ehgt.org/t/123456.jpg" {
		t.Fatalf("seeded metadata=%#v err=%v", snap, err)
	}
}

func TestFavoritesSyncBackfillsKnownWithoutAutoArchivingAndArchivesOnlyNew(t *testing.T) {
	temp := t.TempDir()
	cfg := config.Settings{
		EHSite: "e-hentai", IPBMemberID: "1", IPBPassHash: "hash", TimeoutSeconds: 2,
		HTMLIntervalSeconds: 0, Retries: 0, ListCacheTTLSeconds: 600, PageURLTTLSeconds: 3600,
		FavoritesSyncArchive: true, ArchiveQuality: "original",
		FavoritesSyncMaxPages: 5, FavoritesSyncMatchThreshold: 1,
		FavoritesSyncState: filepath.Join(temp, "favorites_sync.json"),
		CacheDir:           filepath.Join(temp, "cache"),
		ArchiveDir:         filepath.Join(temp, "archives"),
		ArchiveStateDir:    filepath.Join(temp, "config", "archive"),
	}
	var archiverGIDs []string
	service := eh.NewService(cfg, favoriteReplayTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/":
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`<html>session</html>`)), Request: r}, nil
		case "/favorites.php":
			next := r.URL.Query().Get("next")
			if next == "" {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(favoritesSyncHTML([]favoritesSyncGallery{{10, "aa10"}, {20, "bb20"}}, "10-100"))), Request: r}, nil
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(favoritesSyncHTML([]favoritesSyncGallery{{11, "aa11"}}, ""))), Request: r}, nil
		case "/archiver.php":
			gid := r.URL.Query().Get("gid")
			if r.Method == http.MethodPost {
				archiverGIDs = append(archiverGIDs, gid)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`<html><body>Cannot be archived</body></html>`)), Request: r}, nil
			}
			body := `<html><body><form action="archiver.php" method="post"><input type="hidden" name="dltype" value="org"/><input type="submit" name="dlcheck" value="Download Original Archive"/></form><div>Download Cost: Free!</div><div>Estimated Size: 10 MiB</div></body></html>`
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		default:
			return nil, fmt.Errorf("unexpected path %s", r.URL.String())
		}
	}))
	defer service.Close()

	store, err := archive.NewStoreWithState(cfg.ArchiveDir, cfg.ArchiveStateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := archive.NewManager(cfg, service.Client, service, store)
	defer manager.Close()

	state := NewState(cfg.FavoritesSyncState)
	baseline := true
	if err := state.Update(map[string]bool{"10:aa10": true, "11:aa11": true}, nil, nil, nil, nil, &baseline, nil, nil); err != nil {
		t.Fatal(err)
	}
	syncer := NewSyncer(cfg, service, manager, state)
	res, err := syncer.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res["scanned_pages"] != 2 || res["cataloged_count"] != 3 {
		t.Fatalf("backfill result=%#v, want scanned_pages=2 cataloged_count=3", res)
	}
	if len(archiverGIDs) != 1 || archiverGIDs[0] != "20" {
		t.Fatalf("archiver submitted gids=%v, want only [20]", archiverGIDs)
	}
	if store.Get(10, "aa10")["status"] != "unarchived" || store.Get(11, "aa11")["status"] != "unarchived" {
		t.Fatalf("backfilled known entries should stay unarchived: 10=%#v 11=%#v", store.Get(10, "aa10"), store.Get(11, "aa11"))
	}
	if !state.CatalogBackfilled() {
		t.Fatal("expected catalog_backfilled to be marked true")
	}
}

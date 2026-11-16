package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/eh"
	"pandaopds/internal/home"
)

type homePreviewTransport func(*http.Request) (*http.Response, error)

func (f homePreviewTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func homePreviewResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func newHomePreviewServer(t *testing.T, homeTOML string, transport http.RoundTripper) *Server {
	t.Helper()
	base := t.TempDir()
	configPath := filepath.Join(base, "home.toml")
	if err := os.WriteFile(configPath, []byte(homeTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{EHSite: "e-hentai", HomeConfigPath: configPath, CacheDir: filepath.Join(base, "cache"), PublicBaseURL: "https://reader.example", ListCacheTTLSeconds: 60, HTMLIntervalSeconds: 0, Retries: 0, TimeoutSeconds: 3}
	service := eh.NewService(settings, transport)
	store, err := archive.NewStore(filepath.Join(base, "archives"))
	if err != nil {
		service.Close()
		t.Fatal(err)
	}
	service.AttachArchive(store)
	t.Cleanup(func() { _ = service.Close() })
	return &Server{Settings: settings, Service: service, Store: store}
}

const twoPreviewSections = `[[section]]
kind = "publication"
group = true
title = "Popular preview"
type = "preset"
query = "popular"
count = 1

[[section]]
kind = "publication"
group = true
title = "Search preview"
type = "search"
query = "slow-query"
count = 1

[[section]]
kind = "navigation"
group = true
title = "Browse"
type = "preset"
query = "latest"
`

func TestRootV20FetchesPublicationPreviewsConcurrentlyInOrder(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{}, 2)
	transport := homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" && r.URL.Query().Get("f_search") == "" {
			return homePreviewResponse(r, `<html>session</html>`), nil
		}
		started <- r.URL.Path + "?" + r.URL.RawQuery
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		if r.URL.Path == "/popular" {
			return homePreviewResponse(r, `<div class="gl1e"><a class="glink" href="/g/101/aaa/">Popular Item</a></div>`), nil
		}
		return homePreviewResponse(r, `<div class="gl1e"><a class="glink" href="/g/202/bbb/">Search Item</a></div>`), nil
	})
	server := newHomePreviewServer(t, twoPreviewSections, transport)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.rootV20(response, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil))
		close(done)
	}()

	paths := make([]string, 0, 2)
	for len(paths) < 2 {
		select {
		case p := <-started:
			paths = append(paths, p)
		case <-time.After(time.Second):
			// Release any serialized request too, then let the handler finish
			// before reporting the concurrency regression.
			release <- struct{}{}
			release <- struct{}{}
			<-done
			t.Fatalf("only %d homepage previews started concurrently: %v", len(paths), paths)
		}
	}
	if (paths[0] != "/popular?inline_set=dm_e" && paths[1] != "/popular?inline_set=dm_e") || (!strings.HasPrefix(paths[0], "/?f_search=slow-query") && !strings.HasPrefix(paths[1], "/?f_search=slow-query")) {
		release <- struct{}{}
		release <- struct{}{}
		<-done
		t.Fatalf("unexpected preview requests: %v", paths)
	}
	release <- struct{}{}
	release <- struct{}{}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("homepage did not complete after previews were released")
	}
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), body)
	}
	var doc struct {
		Groups []map[string]json.RawMessage
	}
	if err := json.Unmarshal(response.Body.Bytes(), &doc); err != nil || len(doc.Groups) != 3 {
		t.Fatalf("expected three independent groups: %v, %s", err, body)
	}
	for i, group := range doc.Groups {
		if i < 2 && (group["publications"] == nil || group["navigation"] != nil) ||
			i == 2 && (group["navigation"] == nil || group["publications"] != nil) ||
			group["groups"] != nil || group["facets"] != nil {
			t.Fatalf("group %d contains an unexpected collection: %s", i, body)
		}
	}

	popularIndex, searchIndex := strings.Index(body, "Popular Item"), strings.Index(body, "Search Item")
	if popularIndex < 0 || searchIndex < 0 || popularIndex >= searchIndex || !strings.Contains(body, `"title": "Browse"`) {
		t.Fatalf("previews/navigation missing or reordered: %s", body)
	}
}

func TestRootV20ReturnsPartialHomepageWithoutCachingAfterPreviewDeadline(t *testing.T) {
	slowCanceled := make(chan struct{})
	transport := homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" && r.URL.Query().Get("f_search") == "" {
			return homePreviewResponse(r, `<html>session</html>`), nil
		}
		if r.URL.Path == "/popular" {
			return homePreviewResponse(r, `<div class="gl1e"><a class="glink" href="/g/101/aaa/">Fast Item</a></div>`), nil
		}
		<-r.Context().Done()
		close(slowCanceled)
		return nil, r.Context().Err()
	})
	server := newHomePreviewServer(t, twoPreviewSections, transport)
	response := httptest.NewRecorder()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil).WithContext(ctx)
	server.rootV20(response, request)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("homepage did not honor cancellation promptly: %s", elapsed)
	}
	select {
	case <-slowCanceled:
	case <-time.After(time.Second):
		t.Fatal("unfinished preview request did not observe cancellation")
	}
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), body)
	}
	if !strings.Contains(body, "Fast Item") || strings.Contains(body, "Search Item") || !strings.Contains(body, `"title": "Browse"`) {
		t.Fatalf("partial success or navigation was lost: %s", body)
	}
}

func TestRootV20FacetsReuseRootSourceAndFallback(t *testing.T) {
	calls := 0
	homeTOML := `[[section]]
kind="publication"
type="search"
query="language:chinese"
title="Chinese"
count=1

[[section]]
kind="facets"
type="search"
query="language:chinese"
title="Category"
`
	server := newHomePreviewServer(t, homeTOML, homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.HasPrefix(r.URL.Path, "/") && r.URL.Query().Get("f_search") == "" {
			return homePreviewResponse(r, `<html>session</html>`), nil
		}
		return homePreviewResponse(r, `<div class="gl1e"><a class="glink" href="/g/101/aaa/">Chinese Item</a></div>`), nil
	}))
	server.Settings.IPBMemberID = "123"
	server.Settings.IPBPassHash = "abc"
	server.Settings.Facets = []config.Facet{{Name: "Doujinshi", Mask: 1021}, {Name: "Manga", Mask: 1019}}
	rr := httptest.NewRecorder()
	server.rootV20(rr, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"title": "Category"`) || !strings.Contains(rr.Body.String(), `f_cats=1021`) || !strings.Contains(rr.Body.String(), `query=language%3Achinese`) {
		t.Fatalf("facets/root source missing: %s", rr.Body.String())
	}
	if calls != 2 {
		t.Fatalf("facet rendering fetched extra data: %d requests", calls)
	}

	empty := newHomePreviewServer(t, "", homePreviewTransport(func(r *http.Request) (*http.Response, error) { return nil, context.Canceled }))
	fallback := httptest.NewRecorder()
	empty.rootV20(fallback, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil))
	if fallback.Code != http.StatusOK || !strings.Contains(fallback.Body.String(), `"title": "Latest"`) {
		t.Fatalf("empty layout fallback missing: %s", fallback.Body.String())
	}
}

func TestRootV20NavigationGroupWithMultipleItems(t *testing.T) {
	homeTOML := `[[section]]
kind = "navigation"
group = true
title = "Rankings Group"

[[section.items]]
title = "Month"
type = "preset"
query = "toplist:month"

[[section.items]]
title = "Year"
type = "preset"
query = "toplist:year"
`
	server := newHomePreviewServer(t, homeTOML, homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		return homePreviewResponse(r, `<html>session</html>`), nil
	}))
	rr := httptest.NewRecorder()
	server.rootV20(rr, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var doc struct {
		Groups []struct {
			Metadata struct {
				Title string `json:"title"`
			} `json:"metadata"`
			Navigation []struct {
				Title string `json:"title"`
				Href  string `json:"href"`
			} `json:"navigation"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("failed to decode JSON: %v, body=%s", err, rr.Body.String())
	}
	if len(doc.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(doc.Groups))
	}
	grp := doc.Groups[0]
	if grp.Metadata.Title != "Rankings Group" {
		t.Fatalf("expected group title 'Rankings Group', got %q", grp.Metadata.Title)
	}
	if len(grp.Navigation) != 2 {
		t.Fatalf("expected 2 navigation items in group, got %d", len(grp.Navigation))
	}
	if grp.Navigation[0].Title != "Month" || !strings.Contains(grp.Navigation[0].Href, "period=month") {
		t.Fatalf("item 0 mismatch: %+v", grp.Navigation[0])
	}
	if grp.Navigation[1].Title != "Year" || !strings.Contains(grp.Navigation[1].Href, "period=year") {
		t.Fatalf("item 1 mismatch: %+v", grp.Navigation[1])
	}
}

func TestAutoFacetsAndGalleryConsistency(t *testing.T) {
	// A publication page without explicit kind="facets" section
	homeTOML := `[[section]]
kind="publication"
type="preset"
query="watched"
title="Watched"
count=1
`
	server := newHomePreviewServer(t, homeTOML, homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		return homePreviewResponse(r, `<div class="gl1e"><a class="glink" href="/g/101/aaa/">Watched Item</a></div>`), nil
	}))
	server.Settings.IPBMemberID = "123"
	server.Settings.IPBPassHash = "abc"
	server.Settings.Facets = []config.Facet{{Name: "Doujinshi", Mask: 1021}, {Name: "Manga", Mask: 1019}}

	// 1. rootV20 automatically generates facets
	rr := httptest.NewRecorder()
	server.rootV20(rr, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("rootV20 status=%d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"title": "Filter"`) || !strings.Contains(body, `f_cats=1021`) || !strings.Contains(body, `query=watched`) {
		t.Fatalf("auto-derived facets missing or malformed: %s", body)
	}

	// 2. gallery20 with query=watched and f_cats=1021 preserves query and sets rel="self" on Doujinshi
	galleryReq := httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery?query=watched&f_cats=1021", nil)
	galleryRR := httptest.NewRecorder()
	server.gallery20(galleryRR, galleryReq)
	if galleryRR.Code != http.StatusOK {
		t.Fatalf("gallery20 status=%d", galleryRR.Code)
	}
	gBody := galleryRR.Body.String()
	if !strings.Contains(gBody, `"title": "Filter"`) || !strings.Contains(gBody, `query=watched`) {
		t.Fatalf("gallery20 facets missing for watched: %s", gBody)
	}
	var galleryDoc struct {
		Facets []struct {
			Links []struct {
				Title string `json:"title"`
				Href  string `json:"href"`
				Rel   string `json:"rel"`
			} `json:"links"`
		} `json:"facets"`
	}
	if err := json.Unmarshal(galleryRR.Body.Bytes(), &galleryDoc); err != nil || len(galleryDoc.Facets) == 0 {
		t.Fatalf("failed to parse gallery facets: %v", err)
	}
	doujinLinkFound := false
	for _, l := range galleryDoc.Facets[0].Links {
		if l.Title == "Doujinshi" {
			doujinLinkFound = true
			if l.Rel != "self" {
				t.Fatalf("expected Doujinshi to have rel='self', got %q", l.Rel)
			}
			if !strings.Contains(l.Href, "f_cats=1021") || !strings.Contains(l.Href, "query=watched") {
				t.Fatalf("expected Doujinshi href to contain f_cats and query, got %q", l.Href)
			}
		}
	}
	if !doujinLinkFound {
		t.Fatalf("Doujinshi link not found in gallery facets: %+v", galleryDoc.Facets)
	}

	// 3. gallery20 with search within watched (query=watched touhou)
	searchWatchedReq := httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery?query=watched+touhou&f_cats=1021", nil)
	searchWatchedRR := httptest.NewRecorder()
	server.gallery20(searchWatchedRR, searchWatchedReq)
	if searchWatchedRR.Code != http.StatusOK {
		t.Fatalf("gallery20 search-watched status=%d", searchWatchedRR.Code)
	}
	swBody := searchWatchedRR.Body.String()
	if !strings.Contains(swBody, "Watched — touhou") {
		t.Fatalf("expected title with search term, got %s", swBody)
	}
	if !strings.Contains(swBody, "query=watched+touhou") && !strings.Contains(swBody, "query=watched%20touhou") {
		t.Fatalf("expected facets in watched search to retain query, got %s", swBody)
	}
}

func TestPageWithCustomFacetSetAndGalleryParams(t *testing.T) {
	homeTOML := `[[page]]
id = "home"
title = "Home"
mode = "publications"

  [[page.section]]
  kind = "publication"
  title = "东方Project"
  type = "search"
  query = "touhou"
  count = 10

  [[page.section]]
  kind = "facets"
  title = "深度筛选"

    [[page.section.items]]
    title = "全部"

    [[page.section.items]]
    title = "4星汉化同人"
    categories = ["Doujinshi"]
    rating = 4
    expunged = true
    disable_filters = ["language"]
    query = "language:chinese"
`
	server := newHomePreviewServer(t, homeTOML, homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		return homePreviewResponse(r, `<div class="gl1e"><a class="glink" href="/g/101/aaa/">Touhou Item</a></div>`), nil
	}))
	if _, err := home.Load(server.Settings.HomeConfigPath); err != nil {
		t.Fatalf("home.Load error: %v", err)
	}
	server.Settings.Facets = []config.Facet{{Name: "Doujinshi", Mask: 1021}, {Name: "Manga", Mask: 1019}}

	// 1. GET /opds/v2.0 renders custom facets with deep parameters
	rec := httptest.NewRecorder()
	server.rootV20(rec, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("rootV20 status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var doc struct {
		Facets []struct {
			Metadata map[string]string `json:"metadata"`
			Links    []struct {
				Title string `json:"title"`
				Href  string `json:"href"`
				Rel   string `json:"rel"`
			} `json:"links"`
		} `json:"facets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || len(doc.Facets) != 1 {
		t.Fatalf("failed to decode root facets: %v, body=%s", err, rec.Body.String())
	}
	if doc.Facets[0].Metadata["title"] != "深度筛选" {
		t.Fatalf("expected facet group title '深度筛选', got %q", doc.Facets[0].Metadata["title"])
	}
	links := doc.Facets[0].Links
	if len(links) != 2 {
		t.Fatalf("expected 2 facet links, got %d", len(links))
	}
	// Link 0: 全部 -> /opds/v2.0/gallery?query=touhou
	if links[0].Title != "全部" || !strings.Contains(links[0].Href, "query=touhou") {
		t.Fatalf("unexpected link 0: %+v", links[0])
	}
	// Link 1: 4星汉化同人 -> includes f_cats=1021, advsearch=1, f_srdd=4, f_sh=on, f_sfl=on, query=touhou language:chinese
	l1 := links[1]
	if l1.Title != "4星汉化同人" {
		t.Fatalf("unexpected link 1 title: %q", l1.Title)
	}
	for _, expected := range []string{"f_cats=1021", "advsearch=1", "f_srdd=4", "f_sh=on", "f_sfl=on", "query=touhou+language%3Achinese"} {
		if !strings.Contains(l1.Href, expected) {
			t.Errorf("link 1 href %q missing %q", l1.Href, expected)
		}
	}

	// 2. GET gallery with these query parameters forwards them and preserves in self & next
	gReq := httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery?query=touhou+language%3Achinese&f_cats=1021&advsearch=1&f_srdd=4&f_sh=on", nil)
	gRec := httptest.NewRecorder()
	server.gallery20(gRec, gReq)
	if gRec.Code != http.StatusOK {
		t.Fatalf("gallery20 status=%d", gRec.Code)
	}
	gBody := gRec.Body.String()
	for _, expected := range []string{"f_cats=1021", "advsearch=1", "f_srdd=4", "f_sh=on"} {
		if !strings.Contains(gBody, expected) {
			t.Errorf("gallery response missing %q in self/next links: %s", expected, gBody)
		}
	}
}

func TestPageWithReferencedFacetSet(t *testing.T) {
	homeTOML := `
[[facet_set]]
id = "manga_favs"
title = "单行本精选"

[[facet_set.items]]
title = "全部单行本"
categories = ["Manga"]

[[facet_set.items]]
title = "5星中文单行本"
categories = ["Manga"]
rating = 5
query = "language:chinese"

[[page]]
id = "home"
title = "Home"
mode = "publications"

[[page.section]]
kind = "publication"
type = "search"
query = "tag:romance"

[[page.section]]
kind = "facets"
facet_set = "manga_favs"
`
	server := newHomePreviewServer(t, homeTOML, homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		return homePreviewResponse(r, `<div class="gl1e"><a class="glink" href="/g/101/aaa/">Item 1</a></div>`), nil
	}))
	server.Settings.IPBMemberID = "123"
	server.Settings.IPBPassHash = "abc"

	rr := httptest.NewRecorder()
	server.rootV20(rr, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("rootV20 status=%d: %s", rr.Code, rr.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	facets, ok := doc["facets"].([]any)
	if !ok || len(facets) != 1 {
		t.Fatalf("expected 1 facet group from referenced set, got %v", doc["facets"])
	}
	fg := facets[0].(map[string]any)
	meta := fg["metadata"].(map[string]any)
	if meta["title"] != "单行本精选" {
		t.Fatalf("expected title '单行本精选', got %v", meta["title"])
	}
	links := fg["links"].([]any)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	l1 := links[1].(map[string]any)
	if l1["title"] != "5星中文单行本" {
		t.Fatalf("unexpected link 1: %+v", l1)
	}
	href := l1["href"].(string)
	for _, expected := range []string{"f_cats=1019", "advsearch=1", "f_srdd=5", "query=tag%3Aromance+language%3Achinese"} {
		if !strings.Contains(href, expected) {
			t.Errorf("referenced facet href %q missing %q", href, expected)
		}
	}
}

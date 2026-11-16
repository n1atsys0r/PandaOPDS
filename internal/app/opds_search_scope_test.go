package app

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
	"pandaopds/internal/feed"
	"pandaopds/internal/home"
)

type scopedSearchTransport struct {
	mu       sync.Mutex
	requests []*url.URL
}

func (t *scopedSearchTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	copyURL := *r.URL
	t.requests = append(t.requests, &copyURL)
	t.mu.Unlock()
	body := `<!doctype html><html><body><a id="unext" href="/?next=123-456">Next</a></body></html>`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

func (t *scopedSearchTransport) requestsWithSearch(term string) []*url.URL {
	t.mu.Lock()
	defer t.mu.Unlock()
	var found []*url.URL
	for _, requestURL := range t.requests {
		if requestURL.Query().Get("f_search") == term {
			copyURL := *requestURL
			found = append(found, &copyURL)
		}
	}
	return found
}

func newScopedSearchTestServer(t *testing.T) (*Server, *scopedSearchTransport) {
	t.Helper()
	transport := &scopedSearchTransport{}
	settings := config.Settings{
		EHSite:              "e-hentai",
		TimeoutSeconds:      2,
		HTMLIntervalSeconds: 0,
		Retries:             0,
		CacheDir:            filepath.Join(t.TempDir(), "cache"),
		PublicBaseURL:       "https://reader.example",
	}
	service := eh.NewService(settings, transport)
	t.Cleanup(func() { _ = service.Close() })
	return &Server{Settings: settings, Service: service}, transport
}

func findLinkByRel(t *testing.T, doc map[string]any, rel string) string {
	t.Helper()
	links, ok := doc["links"].([]any)
	if !ok {
		t.Fatalf("document links missing: %#v", doc["links"])
	}
	for _, raw := range links {
		link := raw.(map[string]any)
		if link["rel"] == rel {
			return link["href"].(string)
		}
	}
	t.Fatalf("link rel=%q not found", rel)
	return ""
}

func TestScopedSearchLinksAndOpenSearchTemplates(t *testing.T) {
	s, _ := newScopedSearchTestServer(t)

	watched12 := httptest.NewRecorder()
	s.gallery12(watched12, httptest.NewRequest(http.MethodGet, "/opds/v1.2/gallery?query=watched", nil))
	if watched12.Code != http.StatusOK || !strings.Contains(watched12.Body.String(), `href="https://reader.example/opds/v1.2/search.xml?query=watched"`) {
		t.Fatalf("v1.2 watched search link missing: status=%d body=%s", watched12.Code, watched12.Body.String())
	}
	watchedDescription := httptest.NewRecorder()
	s.searchXML12(watchedDescription, httptest.NewRequest(http.MethodGet, "/opds/v1.2/search.xml?query=watched", nil))
	if !strings.Contains(watchedDescription.Body.String(), `template="https://reader.example/opds/v1.2/gallery?query=watched&amp;f_search={searchTerms}"`) {
		t.Fatalf("v1.2 watched template missing: %s", watchedDescription.Body.String())
	}

	favorites12 := httptest.NewRecorder()
	s.gallery12(favorites12, httptest.NewRequest(http.MethodGet, "/opds/v1.2/gallery?query=favorites&favcat=4", nil))
	if favorites12.Code != http.StatusOK || !strings.Contains(favorites12.Body.String(), `href="https://reader.example/opds/v1.2/search.xml?favcat=4&amp;query=favorites"`) {
		t.Fatalf("v1.2 favorites search link missing: status=%d body=%s", favorites12.Code, favorites12.Body.String())
	}
	favoritesDescription := httptest.NewRecorder()
	s.searchXML12(favoritesDescription, httptest.NewRequest(http.MethodGet, "/opds/v1.2/search.xml?query=favorites&favcat=4", nil))
	if !strings.Contains(favoritesDescription.Body.String(), `template="https://reader.example/opds/v1.2/gallery?query=favorites&amp;favcat=4&amp;f_search={searchTerms}"`) {
		t.Fatalf("v1.2 favorites template missing: %s", favoritesDescription.Body.String())
	}
	favoritesAll12 := httptest.NewRecorder()
	s.gallery12(favoritesAll12, httptest.NewRequest(http.MethodGet, "/opds/v1.2/gallery?query=favorites", nil))
	if !strings.Contains(favoritesAll12.Body.String(), `href="https://reader.example/opds/v1.2/search.xml?favcat=all&amp;query=favorites"`) {
		t.Fatalf("v1.2 all-favorites search link missing: %s", favoritesAll12.Body.String())
	}
	globalDescription := httptest.NewRecorder()
	s.searchXML12(globalDescription, httptest.NewRequest(http.MethodGet, "/opds/v1.2/search.xml", nil))
	if !strings.Contains(globalDescription.Body.String(), `template="https://reader.example/opds/v1.2/gallery?query={searchTerms}"`) {
		t.Fatalf("global v1.2 template changed: %s", globalDescription.Body.String())
	}

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/opds/v2.0/gallery?query=watched", "https://reader.example/opds/v2.0/gallery?query=watched&f_search={searchTerms}"},
		{"/opds/v2.0/gallery?query=favorites", "https://reader.example/opds/v2.0/gallery?query=favorites&favcat=all&f_search={searchTerms}"},
		{"/opds/v2.0/gallery?query=favorites&favcat=4", "https://reader.example/opds/v2.0/gallery?query=favorites&favcat=4&f_search={searchTerms}"},
		{"/opds/v2.0/gallery?query=ordinary", "https://reader.example/opds/v2.0/gallery?query={searchTerms}"},
	} {
		rec := httptest.NewRecorder()
		s.gallery20(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("v2.0 feed status=%d body=%s", rec.Code, rec.Body.String())
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if got := findLinkByRel(t, doc, "search"); got != tc.want {
			t.Errorf("v2.0 search href=%q, want %q", got, tc.want)
		}
	}
}

func TestScopedSearchUsesUpstreamPagesAndRetainsFavoriteContext(t *testing.T) {
	s, transport := newScopedSearchTestServer(t)
	requests := []struct {
		path string
		want string
	}{
		{"/opds/v1.2/gallery?query=watched&f_search=blue%20sky", "blue sky"},
		{"/opds/v2.0/gallery?query=watched&f_search=green%20field", "green field"},
		{"/opds/v1.2/gallery?query=favorites&favcat=all&f_search=white%20album", "white album"},
		{"/opds/v2.0/gallery?query=favorites&favcat=2&f_search=blue%20period", "blue period"},
	}
	for i, tc := range requests {
		rec := httptest.NewRecorder()
		if i%2 == 0 {
			s.gallery12(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		} else {
			s.gallery20(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("feed request %q status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		upstream := transport.requestsWithSearch(tc.want)
		if len(upstream) == 0 {
			t.Fatalf("no upstream request carried f_search=%q", tc.want)
		}
		got := upstream[len(upstream)-1]
		if strings.Contains(tc.path, "query=watched") && got.Path != "/watched" {
			t.Errorf("watched query %q used upstream path %q", tc.path, got.Path)
		}
		if i == 0 {
			var atom feed.AtomFeed
			if err := xml.Unmarshal(rec.Body.Bytes(), &atom); err != nil {
				t.Fatal(err)
			}
			for _, link := range atom.Links {
				if link.Rel != "self" && link.Rel != "next" {
					continue
				}
				parsed, err := url.Parse(link.Href)
				if err != nil || parsed.Query().Get("f_search") != tc.want {
					t.Errorf("v1.2 watched %s link lost search term: %s", link.Rel, link.Href)
				}
			}
		}
		if strings.Contains(tc.path, "query=favorites") {
			if got.Path != "/favorites.php" || got.Query().Get("favcat") == "" {
				t.Errorf("favorites query %q used upstream URL %s", tc.path, got)
			}
			if strings.Contains(tc.path, "favcat=all") && got.Query().Get("favcat") != "all" {
				t.Errorf("all favorites upstream URL=%s, want favcat=all", got)
			}
			if strings.Contains(tc.path, "favcat=2") && got.Query().Get("favcat") != "2" {
				t.Errorf("category favorites upstream URL=%s, want favcat=2", got)
			}
			if strings.Contains(tc.path, "/opds/v1.2/") {
				var atom feed.AtomFeed
				if err := xml.Unmarshal(rec.Body.Bytes(), &atom); err != nil {
					t.Fatal(err)
				}
				for _, link := range atom.Links {
					parsed, err := url.Parse(link.Href)
					if err != nil {
						t.Fatal(err)
					}
					switch link.Rel {
					case "self", "next":
						if parsed.Query().Get("favcat") != "all" || parsed.Query().Get("f_search") != tc.want {
							t.Errorf("v1.2 %s link lost favorite scope: %s", link.Rel, link.Href)
						}
					default:
						if strings.Contains(link.Rel, "facet") && parsed.Query().Get("f_search") != tc.want {
							t.Errorf("v1.2 favorite facet lost search term: %s", link.Href)
						}
					}
				}
			}
		}
	}

	categoryFeed := httptest.NewRecorder()
	s.gallery20(categoryFeed, httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery?query=favorites&favcat=2&f_search=blue%20period", nil))
	var doc map[string]any
	if err := json.Unmarshal(categoryFeed.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	self := findLinkByRel(t, doc, "self")
	if !strings.Contains(self, "favcat=2") || !strings.Contains(self, "f_search=blue%20period") {
		t.Errorf("favorites self link lost scope: %s", self)
	}
	next := findLinkByRel(t, doc, "next")
	if !strings.Contains(next, "favcat=2") || !strings.Contains(next, "f_search=blue%20period") {
		t.Errorf("favorites next link lost scope: %s", next)
	}
	facets, ok := doc["facets"].([]any)
	if !ok || len(facets) == 0 {
		t.Fatalf("favorites facets missing: %#v", doc["facets"])
	}
	facetLinks := facets[0].(map[string]any)["links"].([]any)
	for _, raw := range facetLinks {
		href := raw.(map[string]any)["href"].(string)
		parsed, err := url.Parse(href)
		if err != nil || parsed.Query().Get("f_search") != "blue period" {
			t.Errorf("favorite category facet lost search term: %s", href)
			break
		}
	}
}

func TestScopedFeedsForwardFiltersAndPreserveNavigation(t *testing.T) {
	s, transport := newScopedSearchTestServer(t)
	filterValues := map[string]string{
		"f_cats":    "1021",
		"advsearch": "1",
		"f_sr":      "on",
		"f_srdd":    "4",
		"f_sh":      "on",
		"f_sfl":     "on",
	}
	filters := "&f_cats=1021&advsearch=1&f_sr=on&f_srdd=4&f_sh=on&f_sfl=on"
	cases := []struct {
		path, term, upstreamPath string
		v12                      bool
	}{
		{"/opds/v1.2/gallery?query=watched&f_search=watch-term" + filters, "watch-term", "/watched", true},
		{"/opds/v1.2/gallery?query=favorites&favcat=all&f_search=favorite-term" + filters, "favorite-term", "/favorites.php", true},
		{"/opds/v2.0/gallery?query=watched&f_search=watch-term-2" + filters, "watch-term-2", "/watched", false},
		{"/opds/v2.0/gallery?query=favorites&favcat=3&f_search=favorite-term-2" + filters, "favorite-term-2", "/favorites.php", false},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		if tc.v12 {
			s.gallery12(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		} else {
			s.gallery20(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("feed %q status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		requests := transport.requestsWithSearch(tc.term)
		if len(requests) == 0 {
			t.Fatalf("no upstream request carried search term %q", tc.term)
		}
		upstream := requests[len(requests)-1]
		if upstream.Path != tc.upstreamPath {
			t.Errorf("feed %q used upstream path %q", tc.path, upstream.Path)
		}
		if strings.Contains(tc.path, "favorites") && upstream.Query().Get("favcat") == "" {
			t.Errorf("favorites feed %q lost favcat: %s", tc.path, upstream)
		}
		for key, want := range filterValues {
			if got := upstream.Query().Get(key); got != want {
				t.Errorf("upstream %s=%q for %q, want %q", key, got, tc.path, want)
			}
		}

		checkURL := func(label, href string) {
			t.Helper()
			parsed, err := url.Parse(href)
			if err != nil {
				t.Errorf("invalid %s URL %q: %v", label, href, err)
				return
			}
			for key, want := range filterValues {
				if got := parsed.Query().Get(key); got != want {
					t.Errorf("%s lost %s=%s: %s", label, key, want, href)
				}
			}
			if label == "next" && parsed.Query().Get("next") != "123-456" {
				t.Errorf("next link lost upstream cursor: %s", href)
			}
		}
		nextHref := ""
		if tc.v12 {
			var atom feed.AtomFeed
			if err := xml.Unmarshal(rec.Body.Bytes(), &atom); err != nil {
				t.Fatal(err)
			}
			foundNext := false
			for _, link := range atom.Links {
				if link.Rel == "self" || link.Rel == "next" {
					if link.Rel == "next" {
						foundNext = true
						nextHref = link.Href
					}
					checkURL(link.Rel, link.Href)
				} else if strings.Contains(link.Rel, "facet") {
					checkURL("favorite facet", link.Href)
				}
			}
			if !foundNext {
				t.Errorf("v1.2 feed %q missing next link", tc.path)
			}
		} else {
			var doc map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			checkURL("self", findLinkByRel(t, doc, "self"))
			nextHref = findLinkByRel(t, doc, "next")
			checkURL("next", nextHref)
			if strings.Contains(tc.path, "favorites") {
				groups := doc["facets"].([]any)
				links := groups[0].(map[string]any)["links"].([]any)
				checkURL("favorite facet", links[0].(map[string]any)["href"].(string))
			}
		}
		pageURL, err := url.Parse(nextHref)
		if err != nil {
			t.Fatalf("invalid next link %q: %v", nextHref, err)
		}
		page := httptest.NewRecorder()
		pageRequest := httptest.NewRequest(http.MethodGet, pageURL.RequestURI(), nil)
		if tc.v12 {
			s.gallery12(page, pageRequest)
		} else {
			s.gallery20(page, pageRequest)
		}
		if page.Code != http.StatusOK {
			t.Fatalf("following next link %q status=%d body=%s", nextHref, page.Code, page.Body.String())
		}
		requests = transport.requestsWithSearch(tc.term)
		upstream = requests[len(requests)-1]
		if upstream.Query().Get("next") != "123-456" {
			t.Errorf("following next link did not reach next upstream page: %s", upstream)
		}
		for key, want := range filterValues {
			if got := upstream.Query().Get(key); got != want {
				t.Errorf("next-page upstream %s=%q, want %q", key, got, want)
			}
		}
	}
}

func TestWatchedCustomFacetKeepsScopeSeparateFromSearchTerms(t *testing.T) {
	s, transport := newScopedSearchTestServer(t)
	s.Settings.Facets = []config.Facet{{Name: "Manga", Mask: 1021}}
	items := []home.NavigationItem{
		{Title: "Chinese", Query: "l:chinese", Expunged: true},
		{Title: "Japanese manga", Query: "l:japanese", Categories: []string{"Manga"}},
	}
	request := httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery?query=watched", nil)
	links := s.customFacetLinks(items, "search", "watched", request.URL)
	if len(links) != 2 {
		t.Fatalf("custom facet links=%d, want 2", len(links))
	}
	facetURL, err := url.Parse(links[0]["href"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if got := facetURL.Query().Get("query"); got != "watched" {
		t.Fatalf("facet query=%q, want watched scope", got)
	}
	if got := facetURL.Query().Get("f_search"); got != "l:chinese" {
		t.Fatalf("facet f_search=%q, want only facet terms", got)
	}
	if facetURL.Query().Get("f_sh") != "on" || facetURL.Query().Get("advsearch") != "1" {
		t.Fatalf("expunged facet parameters missing: %s", facetURL)
	}

	response := httptest.NewRecorder()
	s.gallery20(response, httptest.NewRequest(http.MethodGet, facetURL.RequestURI(), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("facet request status=%d body=%s", response.Code, response.Body.String())
	}
	upstream := transport.requestsWithSearch("l:chinese")
	if len(upstream) == 0 || upstream[len(upstream)-1].Path != "/watched" {
		t.Fatalf("custom facet did not search Watched: requests=%v", upstream)
	}

	searchedRequest := httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery?query=watched&f_search=artist%3Aalice+l%3Achinese&advsearch=1&f_sh=on", nil)
	links = s.customFacetLinks(items, "search", "watched", searchedRequest.URL)
	if links[0]["rel"] != "self" {
		t.Fatalf("selected custom facet has no active state: %#v", links[0])
	}
	facetURL, err = url.Parse(links[0]["href"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if got := facetURL.Query().Get("f_search"); got != "artist:alice l:chinese" {
		t.Fatalf("selected facet f_search=%q, want base search plus one facet", got)
	}
	if got := facetURL.Query().Get("f_cats"); got != "" {
		t.Fatalf("Chinese facet unexpectedly has category mask %q", got)
	}
	otherFacetURL, err := url.Parse(links[1]["href"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if got := otherFacetURL.Query().Get("f_search"); got != "artist:alice l:japanese" {
		t.Fatalf("switching facet accumulated terms: f_search=%q", got)
	}
	if got := otherFacetURL.Query().Get("f_cats"); got != "1021" {
		t.Fatalf("category facet mask=%q, want 1021", got)
	}
	if got := otherFacetURL.Query().Get("f_sh"); got != "" {
		t.Fatalf("switching facet retained old expunged filter: f_sh=%q", got)
	}

	selectedRequest := httptest.NewRequest(http.MethodGet, otherFacetURL.RequestURI(), nil)
	switchedLinks := s.customFacetLinks(items, "search", "watched", selectedRequest.URL)
	if switchedLinks[1]["rel"] != "self" {
		t.Fatalf("switched facet has no selected state: %#v", switchedLinks[1])
	}
	selectedAgain, err := url.Parse(switchedLinks[0]["href"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if got := selectedAgain.Query().Get("f_search"); got != "artist:alice l:chinese" {
		t.Fatalf("switching back accumulated terms: f_search=%q", got)
	}
}

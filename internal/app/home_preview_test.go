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
	server.Settings.Facets = []config.Facet{{Name: "Doujinshi", Mask: 1021}, {Name: "Manga", Mask: 1019}}
	rr := httptest.NewRecorder()
	server.rootV20(rr, httptest.NewRequest(http.MethodGet, "/opds/v2.0", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"title": "Category"`) || !strings.Contains(rr.Body.String(), `category=Doujinshi`) || !strings.Contains(rr.Body.String(), `query=language%3Achinese`) {
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

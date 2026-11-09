package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

func TestCommentHTMLRewritesGalleryAndAllowedInlineImages(t *testing.T) {
	s := &Server{Settings: config.Settings{PublicBaseURL: "https://reader.example", ImageProxyHosts: []string{"ehgt.org", "s.exhentai.org"}}}
	got := s.rewriteCommentHTML(`<a href="https://e-hentai.org/g/123/abcdef/">gallery</a><img src="https://ehgt.org/w/cover.webp"><a href="https://ehgt.org/x.jpg">direct</a><img src="https://evil.example/x.jpg">`)
	for _, want := range []string{
		`href="https://reader.example/opds/v2.0/gallery/123/abcdef"`,
		`src="https://reader.example/image/fetch?url=https%3A%2F%2Fehgt.org%2Fw%2Fcover.webp"`,
		`href="https://ehgt.org/x.jpg"`,
		`src="https://evil.example/x.jpg"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rewritten comment missing %q: %s", want, got)
		}
	}
}

func TestNewerVersionsNoteChoosesNewestDatedAndEscapesContent(t *testing.T) {
	s := &Server{Settings: config.Settings{PublicBaseURL: "https://reader.example"}}
	versions := []eh.GalleryVersion{
		{GID: 10, Token: "aaaa", Title: "Older", AddedAt: "2024-01-02T12:00:00Z"},
		{GID: 20, Token: "bbbb", Title: `<New & Best>`, AddedAt: "2025-02-03T09:30:00Z"},
		{GID: 30, Token: "cccc", Title: "Undated final"},
	}
	want := `There are newer versions of this gallery available:<br><a href="https://reader.example/opds/v2.0/gallery/20/bbbb">&lt;New &amp; Best&gt;</a>`
	if got := newerVersionsNote(versions, s.builder().Href); got != want {
		t.Fatalf("newest-version note=%q, want %q", got, want)
	}
	if got := newerVersionsNote([]eh.GalleryVersion{versions[2], {GID: 40, Token: "dddd", Title: "Last undated"}}, s.builder().Href); !strings.Contains(got, "/gallery/40/dddd") || !strings.Contains(got, "Last undated") {
		t.Fatalf("undated fallback did not select last item: %q", got)
	}
}

type newerVersionFixtureTransport struct{}

func (newerVersionFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := `<html>ok</html>`
	if r.URL.Host == "e-hentai.org" && r.URL.Path == "/g/123/abc/" {
		body = `<!doctype html><html><body><div id="gn">Original</div>
		<div id="gdt"><div class="gdtm"><a href="/s/img/123-1"><div style="background:url(https://ehgt.org/t/thumb.jpg);width:100px;height:100px"></div></a></div></div>
		<div id="gnd"><a href="/g/10/aaaa/">Older</a> Added on 2024-01-02 12:00
		<a href="/g/20/bbbb/">New &amp; Best</a> Added on 2025-02-03 09:30</div>
		</body></html>`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func TestDetailPublicationEmitsLocalNewerVersionNote(t *testing.T) {
	temp := t.TempDir()
	settings := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, PageURLTTLSeconds: 60, CacheDir: filepath.Join(temp, "cache"), PublicBaseURL: "https://reader.example"}
	service := eh.NewService(settings, newerVersionFixtureTransport{})
	defer service.Close()
	store, err := archive.NewStore(filepath.Join(temp, "archives"))
	if err != nil {
		t.Fatal(err)
	}
	service.AttachArchive(store)
	s := &Server{Settings: settings, Service: service, Store: store}
	pub, err := s.detailPublication(httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery/123/abc/publication", nil), 123, "abc")
	if err != nil {
		t.Fatal(err)
	}
	metadata := pub["metadata"].(map[string]any)
	want := `There are newer versions of this gallery available:<br><a href="https://reader.example/opds/v2.0/gallery/20/bbbb">New &amp; Best</a>`
	if got := metadata["x:note"]; got != want {
		t.Fatalf("publication x:note=%q, want %q", got, want)
	}
}

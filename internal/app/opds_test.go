package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/eh"
	"pandaopds/internal/favorites"
)

type favoriteListFixtureTransport string

func (f favoriteListFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(string(f))), Request: r}, nil
}

func TestGalleryFeedOmitsStaleFavoriteSnapshot(t *testing.T) {
	temp := t.TempDir()
	settings := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, PageURLTTLSeconds: 60, ListCacheTTLSeconds: 60, CacheDir: filepath.Join(temp, "cache")}
	service := eh.NewService(settings, favoriteListFixtureTransport(`<html><body><div class="gl1e"><a href="/g/123/abc/"><div class="glink">Fixture Gallery</div></a><div class="cn">Doujinshi</div><img src="/cover.jpg">12 pages</div></body></html>`))
	defer service.Close()
	favoriteState := favorites.NewState(filepath.Join(temp, "favorites.json"))
	if err := favoriteState.Update(nil, nil, nil, map[string]int{"123:abc": 2}, map[int]string{2: "Stale Folder"}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	s := &Server{Settings: settings, Service: service, Favorites: &favorites.Syncer{State: favoriteState}}
	response := httptest.NewRecorder()
	s.gallery20(response, httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery?query=fixture", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("gallery status=%d body=%s", response.Code, response.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	publications := doc["publications"].([]any)
	metadata := publications[0].(map[string]any)["metadata"].(map[string]any)
	assertNoFavoriteCollection(t, metadata)
}

func assertNoFavoriteCollection(t *testing.T, metadata map[string]any) {
	t.Helper()
	if _, ok := metadata["belongsTo"]; ok {
		t.Fatal("favorite collection must not be emitted outside the Divina manifest")
	}
}

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
		<div id="gdf">CurrentFolder<div style="background:url(https://ehgt.org/fav.png);background-position:0px -59px"></div></div>
		<p class="gpc">Showing 1 - 1 of 1 images</p>
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
	favoriteState := favorites.NewState(filepath.Join(temp, "favorites.json"))
	if err := favoriteState.Update(nil, nil, nil, map[string]int{"123:abc": 2}, map[int]string{2: "Doujinshi"}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	s := &Server{Settings: settings, Service: service, Store: store, Favorites: &favorites.Syncer{State: favoriteState}}
	pub, detail, err := s.detailPublication(httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery/123/abc/publication", nil), 123, "abc")
	if err != nil {
		t.Fatal(err)
	}
	metadata := pub["metadata"].(map[string]any)
	want := `There are newer versions of this gallery available:<br><a href="https://reader.example/opds/v2.0/gallery/20/bbbb">New &amp; Best</a>`
	if got := metadata["x:note"]; got != want {
		t.Fatalf("publication x:note=%q, want %q", got, want)
	}
	assertNoFavoriteCollection(t, metadata)
	if detail.Favcat == nil || *detail.Favcat != 3 || detail.FavcatName != "CurrentFolder" {
		t.Fatalf("detail favorite category=%v/%q, want 3/CurrentFolder", detail.Favcat, detail.FavcatName)
	}

	requestWithPath := func(path string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.SetPathValue("gid", "123")
		r.SetPathValue("token", "abc")
		return r
	}
	acquisitionResponse := httptest.NewRecorder()
	s.detail20(acquisitionResponse, requestWithPath("/opds/v2.0/gallery/123/abc"))
	var acquisition map[string]any
	if err := json.Unmarshal(acquisitionResponse.Body.Bytes(), &acquisition); err != nil {
		t.Fatal(err)
	}
	acquisitionMetadata := acquisition["publications"].([]any)[0].(map[string]any)["metadata"].(map[string]any)
	assertNoFavoriteCollection(t, acquisitionMetadata)

	publicationResponse := httptest.NewRecorder()
	s.publication20(publicationResponse, requestWithPath("/opds/v2.0/gallery/123/abc/publication"))
	var publication map[string]any
	if err := json.Unmarshal(publicationResponse.Body.Bytes(), &publication); err != nil {
		t.Fatal(err)
	}
	assertNoFavoriteCollection(t, publication["metadata"].(map[string]any))

	manifestResponse := httptest.NewRecorder()
	s.manifest20(manifestResponse, requestWithPath("/opds/v2.0/gallery/123/abc/manifest"))
	var manifest map[string]any
	if err := json.Unmarshal(manifestResponse.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	manifestMetadata := manifest["metadata"].(map[string]any)
	collection := manifestMetadata["belongsTo"].(map[string]any)["collection"].(map[string]any)
	if got := collection["name"]; got != "CurrentFolder" {
		t.Fatalf("manifest collection name=%v, want CurrentFolder", got)
	}
	links := collection["links"].([]any)
	link := links[0].(map[string]any)
	if got, want := link["href"], "https://reader.example/opds/v2.0/gallery?favcat=3&query=favorites"; got != want {
		t.Fatalf("manifest collection href=%v, want %q", got, want)
	}
	if got, want := link["type"], "application/opds+json"; got != want {
		t.Fatalf("manifest collection link type=%v, want %q", got, want)
	}
	if _, ok := collection["identifier"]; ok {
		t.Fatal("manifest collection unexpectedly has a synthetic identifier")
	}

	s.Settings.PublicBaseURL = ""
	relativeResponse := httptest.NewRecorder()
	s.manifest20(relativeResponse, requestWithPath("/opds/v2.0/gallery/123/abc/manifest"))
	var relativeManifest map[string]any
	if err := json.Unmarshal(relativeResponse.Body.Bytes(), &relativeManifest); err != nil {
		t.Fatal(err)
	}
	relativeMetadata := relativeManifest["metadata"].(map[string]any)
	relativeCollection := relativeMetadata["belongsTo"].(map[string]any)["collection"].(map[string]any)
	relativeLink := relativeCollection["links"].([]any)[0].(map[string]any)
	if got, want := relativeLink["href"], "/opds/v2.0/gallery?favcat=3&query=favorites"; got != want {
		t.Fatalf("relative manifest collection href=%v, want %q", got, want)
	}
}

func TestUnarchivedAndFailedEntriesUseOPDSOnlinePath(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	settings.PublicBaseURL = "https://reader.example"
	settings.PSEPageBase = 1
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	imageBytes := []byte("\xff\xd8\xff\xe0test-jpeg")
	server.Service.Client = eh.NewClient(settings, staticRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "e-hentai.org" && r.URL.Path == "/g/8001/aa8001/":
			body := `<!doctype html><html><body><div id="gn">Online Gallery</div><p class="gpc">Showing 1 - 1 of 1 images</p><div id="gdt"><div class="gdtm"><a href="/s/p1/8001-1"><div style="background:url(https://ehgt.org/t/thumb.jpg);width:100px;height:100px"></div></a></div></div></body></html>`
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		case r.URL.Host == "e-hentai.org" && r.URL.Path == "/g/8002/bb8002/":
			body := `<!doctype html><html><body><div id="gn">Failed Archive Gallery</div><p class="gpc">Showing 1 - 1 of 1 images</p><div id="gdt"><div class="gdtm"><a href="/s/p2/8002-1"><div style="background:url(https://ehgt.org/t/thumb.jpg);width:100px;height:100px"></div></a></div></div></body></html>`
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		case r.URL.Host == "e-hentai.org" && (r.URL.Path == "/s/p1/8001-1" || r.URL.Path == "/s/p2/8002-1"):
			body := `<!doctype html><html><body><img id="img" src="https://img.example/page.jpg"/></body></html>`
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		case r.URL.Host == "img.example":
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader(string(imageBytes))), Request: r}, nil
		default:
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<html>ok</html>")), Request: r}, nil
		}
	}), server.Service.Throttle)

	if _, err := server.Store.Catalog(8001, "aa8001", map[string]any{"title": "Online Gallery"}, map[string]any{"title": "Online Gallery", "filecount": 1}); err != nil {
		t.Fatal(err)
	}
	if err := server.Store.WriteCover(8001, "aa8001", []byte("RIFF\x00\x00\x00\x00WEBPlocal")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Store.Upsert(8002, "bb8002", map[string]any{"status": "failed", "title": "Failed Archive Gallery"}); err != nil {
		t.Fatal(err)
	}

	// Offline OPDS archives shelf must stay empty.
	archFeed := httptest.NewRecorder()
	server.Handler().ServeHTTP(archFeed, httptest.NewRequest(http.MethodGet, "/opds/v2.0/archives", nil))
	if archFeed.Code != 200 || strings.Contains(archFeed.Body.String(), "8001") || strings.Contains(archFeed.Body.String(), "8002") {
		t.Fatalf("archives feed leaked non-ready entries: status=%d body=%s", archFeed.Code, archFeed.Body.String())
	}

	// Local cover from /config/archive is served via /image/{gid}/{token}/thumb with detected MIME.
	thumbRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(thumbRec, httptest.NewRequest(http.MethodGet, "/image/8001/aa8001/thumb", nil))
	if thumbRec.Code != 200 || thumbRec.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("thumb status=%d content-type=%q", thumbRec.Code, thumbRec.Header().Get("Content-Type"))
	}

	// Unarchived and failed galleries both resolve OPDS detail and stream pages online.
	for _, tc := range []struct {
		gid   string
		token string
	}{{"8001", "aa8001"}, {"8002", "bb8002"}} {
		detailRec := httptest.NewRecorder()
		server.Handler().ServeHTTP(detailRec, httptest.NewRequest(http.MethodGet, "/opds/v2.0/gallery/"+tc.gid+"/"+tc.token, nil))
		if detailRec.Code != 200 || !strings.Contains(detailRec.Body.String(), "/stream/"+tc.gid+"/"+tc.token+"/page/1") {
			t.Fatalf("detail for %s status=%d body=%s", tc.gid, detailRec.Code, detailRec.Body.String())
		}
		streamRec := httptest.NewRecorder()
		server.Handler().ServeHTTP(streamRec, httptest.NewRequest(http.MethodGet, "/stream/"+tc.gid+"/"+tc.token+"/page/1", nil))
		if streamRec.Code != 200 || string(streamRec.Body.Bytes()) != string(imageBytes) {
			t.Fatalf("stream for %s status=%d body=%q", tc.gid, streamRec.Code, streamRec.Body.String())
		}
	}
}

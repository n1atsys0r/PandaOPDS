package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

type quotaFixtureTransport struct{}

func (quotaFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := "<html>ok</html>"
	status := http.StatusOK
	switch {
	case r.URL.Host == "e-hentai.org" && r.URL.Path == "/g/1/abc/":
		body = `<div id="gdt"><div class="gdtl"><a href="/s/abc/1-1"><div style="background:url(https://ehgt.org/t/thumb.jpg);width:100px;height:100px"></div></a></div></div>`
	case r.URL.Host == "e-hentai.org" && strings.HasPrefix(r.URL.Path, "/s/"):
		body = `<div id="img" src="https://ehgt.org/g/509.gif"></div>`
	case r.URL.Host == "ehgt.org":
		body = "You have exceeded your image limit"
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func TestQuotaHandlersUseEndpointSpecificBodyAndHeaders(t *testing.T) {
	settings := config.Settings{
		EHSite: "e-hentai", TimeoutSeconds: 2, Retries: 0, HTMLIntervalSeconds: 0,
		ImageSourceRetries: 0, PageURLTTLSeconds: 60, PSEPageBase: 1,
		ExceedCooldownSeconds: 47, CacheDir: filepath.Join(t.TempDir(), "cache"),
		ImageCacheEnabled: false, ImageProxyHosts: []string{"ehgt.org"},
	}
	for _, tc := range []struct {
		name, path string
		register   func(*http.ServeMux, *Server)
		noStore    bool
		detail     string
	}{
		{name: "stream", path: "/stream/1/abc/page/1", detail: "image limit exceeded (509 placeholder)", register: func(m *http.ServeMux, s *Server) { m.HandleFunc("GET /stream/{gid}/{token}/page/{page}", s.streamPage) }},
		{name: "page thumbnail", path: "/image/1/abc/page/1/thumb", detail: "image limit exceeded", register: func(m *http.ServeMux, s *Server) {
			m.HandleFunc("GET /image/{gid}/{token}/page/{page}/thumb", s.pageThumb)
		}},
		{name: "cover fetch", path: "/image/fetch?url=" + url.QueryEscape("https://ehgt.org/w/cover.jpg"), detail: "image limit exceeded", register: func(m *http.ServeMux, s *Server) { m.HandleFunc("GET /image/fetch", s.imageFetch) }, noStore: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := eh.NewService(settings, quotaFixtureTransport{})
			defer service.Close()
			s := &Server{Settings: settings, Service: service}
			mux := http.NewServeMux()
			tc.register(mux, s)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != http.StatusTooManyRequests {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["detail"] != tc.detail || len(body) != 1 {
				t.Fatalf("quota body=%v, want only detail", body)
			}
			if tc.noStore {
				if got := response.Header().Get("Cache-Control"); got != "no-store" {
					t.Fatalf("cover proxy cache-control=%q", got)
				}
				if got := response.Header().Get("Retry-After"); got != "" {
					t.Fatalf("cover proxy unexpectedly has Retry-After=%q", got)
				}
			} else if got := response.Header().Get("Retry-After"); got != "47" {
				t.Fatalf("Retry-After=%q, want configured 47", got)
			}
		})
	}
}

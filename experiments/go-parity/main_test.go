package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLatestPublicationAndOpaqueCursor(t *testing.T) {
	b, e := parseListing(`<table><tr><td class="gl1e"><a href="/g/12345/abcdef12/"></a></td><td><div class="gl3e"><div>Doujinshi</div><div>12 pages</div></div><div class="glink"><a>[Artist] Sample</a></div></td></tr></table>`)
	if e != nil {
		t.Fatal(e)
	}
	if b.Self != "/opds/v2.0/gallery/12345/abcdef12/publication" || b.Pages != 12 {
		t.Fatalf("unexpected parsed publication: %+v", b)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestStreamParsesImageHTMLAndMaps509To429(t *testing.T) {
	for _, quota := range []bool{false, true} {
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var b []byte
			mime := "image/jpeg"
			if strings.Contains(r.URL.Path, "/s/") {
				mime = "text/html"
				path := "fixtures/image_page.html"
				if quota {
					path = "fixtures/image_509.html"
				}
				b, _ = os.ReadFile(path)
			} else {
				b, _ = os.ReadFile("fixtures/image.jpg")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{mime}}, Body: io.NopCloser(bytes.NewReader(b)), Request: r}, nil
		})}
		s := &Server{detail: readFixture("detail.html"), client: client}
		r := httptest.NewRequest("GET", "/stream/12345/abcdef12/page/1", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		want := 200
		if quota {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("status %d want %d", w.Code, want)
		}
		if quota && w.Header().Get("Retry-After") != "300" {
			t.Fatal("509 retry-after missing")
		}
		if !quota && (w.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(w.Body.Bytes(), mustReadFixture("image.jpg"))) {
			t.Fatal("fixture JPEG not served")
		}
	}
}
func readFixture(n string) string     { b, _ := os.ReadFile("fixtures/" + n); return string(b) }
func mustReadFixture(n string) []byte { b, _ := os.ReadFile("fixtures/" + n); return b }

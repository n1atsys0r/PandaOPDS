package app

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestToplistNextLinksUseVersionedRoutes(t *testing.T) {
	const body = `<!doctype html><html><body>
	<table class="ptt"><tbody><tr>
		<td><a href="/toplist.php?tl=15&amp;p=0">1</a></td>
		<td><a href="/toplist.php?tl=15&amp;p=1">2</a></td>
		<td><a href="/toplist.php?tl=15&amp;p=2">Next &rsaquo;</a></td>
	</tr></tbody></table>
	</body></html>`
	var upstreamRequests atomic.Int32
	server := newHomePreviewServer(t, ``, homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "e-hentai.org" {
			return nil, http.ErrServerClosed
		}
		if r.URL.Path == "/" {
			return homePreviewResponse(r, `<html>session</html>`), nil
		}
		if r.URL.Path != "/toplist.php" || r.URL.Query().Get("p") != "1" {
			return nil, http.ErrServerClosed
		}
		upstreamRequests.Add(1)
		return homePreviewResponse(r, body), nil
	}))
	server.mux = http.NewServeMux()
	server.register()
	handler := server.Handler()

	atomResponse := httptest.NewRecorder()
	handler.ServeHTTP(atomResponse, httptest.NewRequest(http.MethodGet, "/opds/v1.2/toplist?period=yesterday&page=2", nil))
	if atomResponse.Code != http.StatusOK {
		t.Fatalf("v1.2 toplist status=%d body=%s", atomResponse.Code, atomResponse.Body.String())
	}
	var atom struct {
		Links []toplistAtomLink `xml:"link"`
	}
	if err := xml.Unmarshal(atomResponse.Body.Bytes(), &atom); err != nil {
		t.Fatalf("decode v1.2 toplist: %v", err)
	}
	if got := nextAtomHref(t, atom.Links); got != "https://reader.example/opds/v1.2/toplist?period=yesterday&page=3" {
		t.Fatalf("v1.2 next href=%q, want versioned page 3 route", got)
	}

	v2Response := httptest.NewRecorder()
	handler.ServeHTTP(v2Response, httptest.NewRequest(http.MethodGet, "/opds/v2.0/toplist?period=yesterday&page=2", nil))
	if v2Response.Code != http.StatusOK {
		t.Fatalf("v2.0 toplist status=%d body=%s", v2Response.Code, v2Response.Body.String())
	}
	var v2 struct {
		Links []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"links"`
	}
	if err := json.Unmarshal(v2Response.Body.Bytes(), &v2); err != nil {
		t.Fatalf("decode v2.0 toplist: %v", err)
	}
	var v2Next string
	for _, link := range v2.Links {
		if link.Rel == "next" {
			v2Next = link.Href
			break
		}
	}
	if v2Next != "https://reader.example/opds/v2.0/toplist?period=yesterday&page=3" {
		t.Fatalf("v2.0 next href=%q, want versioned page 3 route", v2Next)
	}
	if got := upstreamRequests.Load(); got != 1 {
		t.Fatalf("toplist upstream requests=%d, want one cached response reused across versions", got)
	}
}

type toplistAtomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

func nextAtomHref(t *testing.T, links []toplistAtomLink) string {
	t.Helper()
	for _, link := range links {
		if link.Rel == "next" {
			return link.Href
		}
	}
	t.Fatal("Atom feed has no rel=next link")
	return ""
}

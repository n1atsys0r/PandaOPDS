// Package main is an isolated, fixture-driven migration spike. It is not production code.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"golang.org/x/net/html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Gallery struct {
	GID      int                 `json:"gid"`
	Token    string              `json:"token"`
	Title    string              `json:"title"`
	Category string              `json:"category"`
	Pages    int                 `json:"numberOfPages"`
	Subject  []map[string]string `json:"subject"`
	Self     string              `json:"self"`
}
type Server struct {
	listing, detail, imagePage string
	client                     *http.Client
}

func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}
func attr(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}
func hasClass(n *html.Node, c string) bool {
	for _, v := range strings.Fields(attr(n, "class")) {
		if v == c {
			return true
		}
	}
	return false
}
func find(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if x := find(c, pred); x != nil {
			return x
		}
	}
	return nil
}
func parseListing(src string) (Gallery, error) {
	d, e := html.Parse(strings.NewReader(src))
	if e != nil {
		return Gallery{}, e
	}
	row := find(d, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "gl1e") })
	if row == nil {
		return Gallery{}, fmt.Errorf("unsupported fixture: extended layout .gl1e missing")
	}
	a := find(row, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
	parts := strings.Split(strings.Trim(attr(a, "href"), "/"), "/")
	if len(parts) < 3 {
		return Gallery{}, fmt.Errorf("invalid gallery href")
	}
	gid, e := strconv.Atoi(parts[len(parts)-2])
	if e != nil {
		return Gallery{}, e
	}
	tok := parts[len(parts)-1]
	titleNode := find(d, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "glink") })
	if titleNode == nil {
		return Gallery{}, fmt.Errorf("fixture title missing")
	}
	catNode := find(d, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "gl3e") })
	cat := ""
	pages := 0
	if catNode != nil {
		for c := catNode.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "div" {
				if cat == "" {
					cat = text(c)
				}
				if strings.HasSuffix(text(c), "pages") {
					fmt.Sscanf(text(c), "%d pages", &pages)
				}
			}
		}
	}
	if pages == 0 {
		return Gallery{}, fmt.Errorf("fixture page count missing")
	}
	return Gallery{gid, tok, text(titleNode), cat, pages, []map[string]string{{"name": "language:english"}}, fmt.Sprintf("/opds/v2.0/gallery/%d/%s/publication", gid, tok)}, nil
}

var showingRE = regexp.MustCompile(`Showing\s+\d+\s*-\s*\d+\s+of\s+(\d+)\s+images`)

func parseDetail(src string) (string, int, string, error) {
	d, e := html.Parse(strings.NewReader(src))
	if e != nil {
		return "", 0, "", e
	}
	title := find(d, func(n *html.Node) bool { return n.Type == html.ElementNode && attr(n, "id") == "gn" })
	gpc := find(d, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "gpc") })
	gdt := find(d, func(n *html.Node) bool { return n.Type == html.ElementNode && attr(n, "id") == "gdt" })
	if title == nil || gpc == nil || gdt == nil {
		return "", 0, "", fmt.Errorf("unsupported detail fixture")
	}
	m := showingRE.FindStringSubmatch(text(gpc))
	a := find(gdt, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
	if len(m) < 2 || a == nil {
		return "", 0, "", fmt.Errorf("detail count or first /s/ link missing")
	}
	count, _ := strconv.Atoi(m[1])
	return text(title), count, attr(a, "href"), nil
}
func parseImage(src string) (string, bool, error) {
	d, e := html.Parse(strings.NewReader(src))
	if e != nil {
		return "", false, e
	}
	img := find(d, func(n *html.Node) bool { return n.Type == html.ElementNode && attr(n, "id") == "img" })
	if img == nil {
		return "", false, fmt.Errorf("unsupported image fixture")
	}
	u := attr(img, "src")
	return u, strings.HasSuffix(u, "/509.gif"), nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/opds/v2.0/gallery":
		g, e := parseListing(s.listing)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		d, _ := html.Parse(strings.NewReader(s.listing))
		cur := ""
		if n := find(d, func(n *html.Node) bool { return n.Type == html.ElementNode && attr(n, "id") == "unext" }); n != nil {
			for _, q := range strings.Split(strings.TrimPrefix(attr(n, "href"), "/?"), "&") {
				if strings.HasPrefix(q, "next=") {
					cur = strings.TrimPrefix(q, "next=")
				}
			}
		}
		if cur == "" {
			http.Error(w, "fixture next cursor missing", 500)
			return
		}
		publication := map[string]any{"metadata": map[string]any{"title": g.Title, "identifier": fmt.Sprintf("urn:ehentai:gallery:%d:%s", g.GID, g.Token), "numberOfPages": g.Pages}, "category": g.Category, "subject": g.Subject, "links": []map[string]string{{"rel": "self", "href": g.Self}}}
		doc := map[string]any{"metadata": map[string]string{"title": "E-Hentai: Latest", "identifier": "urn:ehentai:gallery-list:latest"}, "publications": []any{publication}, "links": []map[string]string{{"rel": "next", "href": "/opds/v2.0/gallery?next=" + cur}}}
		w.Header().Set("Content-Type", "application/opds+json;profile=acquisition")
		_ = json.NewEncoder(w).Encode(doc)
	case strings.HasSuffix(r.URL.Path, "/publication"):
		g, e := parseListing(s.listing)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		title, count, _, e := parseDetail(s.detail)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		doc := map[string]any{"metadata": map[string]any{"identifier": fmt.Sprintf("urn:ehentai:gallery:%d:%s", g.GID, g.Token), "title": title, "numberOfPages": count}, "links": []map[string]string{{"rel": "self", "href": r.URL.Path}}, "readingOrder": []map[string]string{{"href": fmt.Sprintf("/stream/%d/%s/page/1", g.GID, g.Token), "type": "image/jpeg"}}}
		w.Header().Set("Content-Type", "application/opds+json")
		_ = json.NewEncoder(w).Encode(doc)
	case strings.HasPrefix(r.URL.Path, "/stream/"):
		_, _, firstURL, e := parseDetail(s.detail)
		if e != nil {
			http.Error(w, e.Error(), 502)
			return
		}
		parsedFirst, _ := url.Parse(firstURL)
		pageResp, e := s.client.Get("http://fixture" + parsedFirst.Path)
		if e != nil {
			http.Error(w, e.Error(), 502)
			return
		}
		pageBytes, _ := io.ReadAll(pageResp.Body)
		pageResp.Body.Close()
		imgURL, is509, e := parseImage(string(pageBytes))
		if e != nil {
			http.Error(w, e.Error(), 502)
			return
		}
		if is509 {
			w.Header().Set("Retry-After", "300")
			http.Error(w, "image quota exceeded", 429)
			return
		}
		resp, e := s.client.Get(imgURL)
		if e != nil {
			http.Error(w, e.Error(), 502)
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 429 {
			retry := resp.Header.Get("Retry-After")
			if retry == "" {
				retry = "900"
			}
			w.Header().Set("Retry-After", retry)
			http.Error(w, "image quota exceeded", 429)
			return
		}
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("Cache-Control", "public, max-age=604800")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(b)
	default:
		http.NotFound(w, r)
	}
}

func readFixtureBytes(n string) []byte { b, _ := os.ReadFile("fixtures/" + n); return b }

type fixedTransport struct{}

func (fixedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "fixture" {
		return nil, fmt.Errorf("unexpected fixture host %q", r.URL.Host)
	}
	var body []byte
	mime := ""
	switch r.URL.Path {
	case "/s/firsthash/12345-1":
		file := "fixtures/image_page.html"
		if os.Getenv("FIXTURE_509") == "1" {
			file = "fixtures/image_509.html"
		}
		body, _ = os.ReadFile(file)
		mime = "text/html"
	case "/image.jpg":
		body, _ = os.ReadFile("fixtures/image.jpg")
		mime = "image/jpeg"
	default:
		return nil, fmt.Errorf("unexpected fixture destination %s%s", r.URL.Host, r.URL.Path)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{mime}}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

func main() {
	l, _ := os.ReadFile("fixtures/latest.html")
	d, _ := os.ReadFile("fixtures/detail.html")
	s := &Server{listing: string(l), detail: string(d), client: &http.Client{Transport: fixedTransport{}}}
	if len(os.Args) > 1 && os.Args[1] == "replay" {
		capture := func(path string) (int, http.Header, []byte) {
			r := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			return w.Code, w.Header(), w.Body.Bytes()
		}
		_, _, lb := capture("/opds/v2.0/gallery")
		listing := map[string]any{}
		_ = json.Unmarshal(lb, &listing)
		if listing["publications"] == nil {
			fmt.Fprintln(os.Stderr, string(lb))
			return
		}
		pubs := listing["publications"].([]any)
		pub := pubs[0].(map[string]any)
		_, detailHeaders, db := capture(pub["links"].([]any)[0].(map[string]any)["href"].(string))
		detail := map[string]any{}
		_ = json.Unmarshal(db, &detail)
		ro := detail["readingOrder"].([]any)[0].(map[string]any)
		is, ih, ib := capture(ro["href"].(string))
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"listing": listing, "publication": pub, "detail": detail, "imageStatus": is, "imageType": ih.Get("Content-Type"), "image": string(ib), "imageBase64": base64.StdEncoding.EncodeToString(ib), "imageRetryAfter": ih.Get("Retry-After"), "imageHeaders": ih, "quotaStatus": is, "publicationContentType": detailHeaders.Get("Content-Type")})
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:8088")
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen on 127.0.0.1:8088: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Go parity spike listening at http://127.0.0.1:8088")
	if err := http.Serve(listener, s); err != nil {
		fmt.Fprintf(os.Stderr, "serve Go parity spike: %v\n", err)
		os.Exit(1)
	}
}

package eh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
	"pandaopds/internal/config"
)

type UpstreamError struct {
	Status     int
	Detail     string
	RetryAfter string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream HTTP %d: %s", e.Status, e.Detail)
}

type Client struct {
	cfg          config.Settings
	HTTP         *http.Client
	Throttle     *Throttle
	sessionReady bool
	sessionMu    *contextMutex
}

func NewClient(cfg config.Settings, transport http.RoundTripper, throttle *Throttle) *Client {
	jar, _ := cookiejar.New(nil)
	for _, origin := range []string{"https://e-hentai.org", "https://exhentai.org", "https://api.e-hentai.org", "https://ehgt.org", "https://s.exhentai.org"} {
		u, _ := url.Parse(origin)
		cookies := []*http.Cookie{{Name: "nw", Value: cfg.NW}, {Name: "datatags", Value: cfg.DataTags}}
		if cfg.IPBMemberID != "" {
			cookies = append(cookies, &http.Cookie{Name: "ipb_member_id", Value: cfg.IPBMemberID})
		}
		if cfg.IPBPassHash != "" {
			cookies = append(cookies, &http.Cookie{Name: "ipb_pass_hash", Value: cfg.IPBPassHash})
		}
		if cfg.Igneous != "" {
			cookies = append(cookies, &http.Cookie{Name: "igneous", Value: cfg.Igneous})
		}
		jar.SetCookies(u, cookies)
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Client{cfg: cfg, HTTP: &http.Client{Transport: transport, Timeout: time.Duration(cfg.TimeoutSeconds * float64(time.Second)), Jar: jar}, Throttle: throttle, sessionMu: newContextMutex()}
}
func (c *Client) BaseURL() string { return "https://" + c.cfg.SiteHost() }
func (c *Client) EstablishSession(ctx context.Context) error {
	if err := c.sessionMu.Lock(ctx); err != nil {
		return err
	}
	defer c.sessionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.sessionReady {
		return nil
	}
	if c.cfg.EHSite == "exhentai" {
		if _, _, err := c.request(ctx, http.MethodGet, "https://e-hentai.org/", nil, KindHTML, nil, false); err != nil {
			return err
		}
	}
	if _, _, err := c.request(ctx, http.MethodGet, c.BaseURL()+"/", nil, KindHTML, nil, false); err != nil {
		return err
	}
	if c.cfg.EHProfile != "" {
		if err := c.ensureProfile(ctx); err != nil {
			return err
		}
	}
	c.sessionReady = true
	return nil
}

func (c *Client) ensureProfile(ctx context.Context) error {
	u := c.BaseURL() + "/uconfig.php"
	body, _, err := c.request(ctx, http.MethodGet, u, nil, KindHTML, nil, false)
	if err != nil {
		return nil
	}
	doc, _ := parseDoc(string(body))
	profileForm := findID(doc, "profile_form")
	optionRoot := doc
	if profileForm != nil {
		optionRoot = profileForm
	}
	postHeaders := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Referer": u}
	for _, opt := range all(optionRoot, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && n.Data == "option"
	}) {
		if optionText, ok := profileOptionText(opt); !ok || optionText != c.cfg.EHProfile {
			continue
		}
		id := attr(opt, "value")
		if !decimalString(id) {
			continue
		}
		_, _, _ = c.request(ctx, http.MethodPost, u, []byte(url.Values{"profile_set": {id}}.Encode()), KindHTML, postHeaders, false)
		return nil
	}
	form := url.Values{"profile_action": {"create"}, "profile_name": {c.cfg.EHProfile}, "profile_set": {"616"}}
	_, _, _ = c.request(ctx, http.MethodPost, u, []byte(form.Encode()), KindHTML, postHeaders, false)
	return nil
}

func profileOptionText(option *xhtml.Node) (string, bool) {
	var text strings.Builder
	for child := option.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode {
			return "", false
		}
		if child.Type == xhtml.TextNode {
			text.WriteString(child.Data)
		}
	}
	return strings.TrimSpace(text.String()), true
}

func decimalString(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func (c *Client) request(ctx context.Context, method, target string, body []byte, kind RequestKind, headers map[string]string, establish bool) ([]byte, http.Header, error) {
	return c.requestRetries(ctx, method, target, body, kind, headers, establish, c.cfg.Retries)
}

func (c *Client) requestRetries(ctx context.Context, method, target string, body []byte, kind RequestKind, headers map[string]string, establish bool, retries int) ([]byte, http.Header, error) {
	if establish && kind != KindThumb {
		if err := c.EstablishSession(ctx); err != nil {
			return nil, nil, err
		}
	}
	if err := c.Throttle.Acquire(ctx, kind); err != nil {
		return nil, nil, err
	}
	defer c.Throttle.Release(kind)
	var last error
	for attempt := 0; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			last = err
			if attempt < retries {
				if err := sleepContext(ctx, time.Duration(100*(attempt+1))*time.Millisecond); err != nil {
					return nil, nil, err
				}
				continue
			}
			break
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			last = readErr
			if attempt < retries {
				if err := ctx.Err(); err != nil {
					return nil, nil, err
				}
				continue
			}
			break
		}
		text := strings.TrimSpace(string(data))
		c.Throttle.TripFromBody(text)
		if strings.HasPrefix(text, "Your IP address") || strings.HasPrefix(text, "This IP address") {
			return data, resp.Header, &UpstreamError{Status: 503, Detail: "E-Hentai IP address is banned"}
		}
		if strings.HasPrefix(text, "You have exceeded your image") {
			return data, resp.Header, &UpstreamError{Status: 429, Detail: "image limit exceeded"}
		}
		if strings.Contains(text, "Page load has been aborted due to a fatal error") && attempt >= retries {
			return data, resp.Header, &UpstreamError{Status: 502, Detail: "E-Hentai fatal server error"}
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusServiceUnavailable {
			if attempt < retries {
				if err := sleepContext(ctx, time.Duration(200*(attempt+1))*time.Millisecond); err != nil {
					return nil, nil, err
				}
				continue
			}
		}
		if strings.Contains(text, "Page load has been aborted due to a fatal error") && attempt < retries {
			if err := sleepContext(ctx, time.Duration(300*(attempt+1))*time.Millisecond); err != nil {
				return nil, nil, err
			}
			continue
		}
		if resp.StatusCode >= 400 {
			status := resp.StatusCode
			if status >= 500 {
				status = 502
			}
			return data, resp.Header, &UpstreamError{Status: status, Detail: brief(text), RetryAfter: resp.Header.Get("Retry-After")}
		}
		if len(data) == 0 {
			return data, resp.Header, &UpstreamError{Status: 503, Detail: "empty upstream response (session may be expired)"}
		}
		return data, resp.Header, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return nil, nil, last
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return ctx.Err()
	}
}
func brief(s string) string {
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
func (c *Client) Get(ctx context.Context, target string, kind RequestKind) ([]byte, http.Header, error) {
	return c.request(ctx, http.MethodGet, target, nil, kind, nil, true)
}
func (c *Client) PostForm(ctx context.Context, target string, form url.Values, kind RequestKind) ([]byte, http.Header, error) {
	return c.request(ctx, http.MethodPost, target, []byte(form.Encode()), kind, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, true)
}

// PostFormOnce is for irreversible upstream actions such as buying an archive.
// A transport error can happen after the server accepted the POST, so retrying
// could spend GP twice. The caller must surface an uncertain outcome instead.
func (c *Client) PostFormOnce(ctx context.Context, target string, form url.Values, kind RequestKind) ([]byte, http.Header, error) {
	return c.requestRetries(ctx, http.MethodPost, target, []byte(form.Encode()), kind, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, true, 0)
}
func (c *Client) PostJSON(ctx context.Context, target string, data any, kind RequestKind) ([]byte, http.Header, error) {
	body, e := json.Marshal(data)
	if e != nil {
		return nil, nil, e
	}
	return c.request(ctx, http.MethodPost, target, body, kind, map[string]string{"Content-Type": "application/json"}, true)
}
func (c *Client) Download(ctx context.Context, target, path string, offset int64, progress func(int64)) (int64, error) {
	if err := c.Throttle.Circuit.Check(); err != nil {
		return 0, err
	}
	if err := c.EstablishSession(ctx); err != nil {
		return 0, err
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if e != nil {
		return 0, e
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	client := &http.Client{Transport: c.HTTP.Transport, Jar: c.HTTP.Jar, Timeout: 600 * time.Second}
	resp, e := client.Do(req)
	if e != nil {
		return 0, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, &UpstreamError{Status: resp.StatusCode, Detail: "archive download failed"}
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 && resp.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		offset = 0
	}
	f, e := os.OpenFile(path, flags, 0o600)
	if e != nil {
		return 0, e
	}
	defer f.Close()
	written, e := io.Copy(f, &progressReader{r: resp.Body, base: offset, cb: progress})
	return offset + written, e
}

type progressReader struct {
	r    io.Reader
	base int64
	cb   func(int64)
	n    int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, e := p.r.Read(b)
	p.n += int64(n)
	if p.cb != nil {
		p.cb(p.base + p.n)
	}
	return n, e
}
func (c *Client) GData(ctx context.Context, gid int, token string) (GalleryMetadata, error) {
	body, _, e := c.PostJSON(ctx, "https://api.e-hentai.org/api.php", map[string]any{"method": "gdata", "gidlist": [][]any{{gid, token}}, "namespace": 1}, KindAPI)
	if e != nil {
		return GalleryMetadata{}, e
	}
	m, e := ParseGData(body)
	if e != nil {
		return GalleryMetadata{}, e
	}
	if len(m) == 0 {
		return GalleryMetadata{}, errors.New("gdata returned no metadata")
	}
	return m[0], nil
}
func (c *Client) APIURL() string {
	if c.cfg.EHSite == "exhentai" {
		return "https://exhentai.org/api.php"
	}
	return "https://api.e-hentai.org/api.php"
}
func (c *Client) GalleryURL(gid int, token string) string {
	return c.BaseURL() + fmt.Sprintf("/g/%d/%s/", gid, token)
}
func (c *Client) ToplistURL(period string, page int) string {
	tl := map[string]string{"yesterday": "15", "month": "13", "year": "12", "alltime": "11"}[period]
	if tl == "" {
		tl = "15"
	}
	if page < 1 {
		page = 1
	}
	u, _ := url.Parse("https://e-hentai.org/toplist.php")
	q := u.Query()
	q.Set("tl", tl)
	q.Set("p", fmt.Sprint(page-1))
	q.Set("inline_set", "dm_e")
	u.RawQuery = q.Encode()
	return u.String()
}
func (c *Client) ListURL(query, next string, extra url.Values) string {
	host := c.BaseURL()
	var p string
	switch query {
	case "popular":
		p = "/popular"
	case "watched":
		p = "/watched"
	case "favorites":
		p = "/favorites.php"
	default:
		p = "/"
	}
	u, _ := url.Parse(host + p)
	q := u.Query()
	if query != "popular" && query != "watched" && query != "favorites" && query != "" {
		q.Set("f_search", query)
	}
	if next != "" {
		q.Set("next", next)
	}
	for k, vs := range extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("inline_set", "dm_e")
	u.RawQuery = q.Encode()
	return u.String()
}
func (c *Client) Close() error { return nil }

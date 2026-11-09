package eh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/config"
)

type replayRoundTripper func(*http.Request) (*http.Response, error)

func (f replayRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func replayResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestCompoundFavoritesCursorIsForwardedOpaque(t *testing.T) {
	const cursor = "2753175-1786365950"
	var got string
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, ListCacheTTLSeconds: 30, PageURLTTLSeconds: 60, CacheDir: t.TempDir()}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		got = r.URL.Query().Get("next")
		return replayResponse(r, 200, `<html><body></body></html>`), nil
	}))
	service.Client.sessionReady = true
	defer service.Close()
	_, err := service.GetList(context.Background(), "favorites", cursor, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != cursor {
		t.Fatalf("cursor forwarded as %q, want %q", got, cursor)
	}
}

func Test509ImagePlaceholderTripsCircuitBeforeCDNFetch(t *testing.T) {
	var detailRequests, sRequests, cdnRequests int
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, ImageSourceRetries: 0, PageURLTTLSeconds: 60, CacheDir: t.TempDir(), ImageCacheEnabled: false, ExceedCooldownSeconds: 60, PSEPageBase: 1}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasPrefix(r.URL.Host, "e-hentai.org") && strings.HasPrefix(r.URL.Path, "/g/"):
			detailRequests++
			return replayResponse(r, 200, `<div id="gdt"><div class="gdtm"><a href="/s/abc/1-1"><div style="background:url(https://ehgt.org/t/thumb.jpg);width:100px;height:100px"></div></a></div></div>`), nil
		case strings.HasPrefix(r.URL.Host, "e-hentai.org") && strings.HasPrefix(r.URL.Path, "/s/"):
			sRequests++
			return replayResponse(r, 200, `<div id="img" src="https://ehgt.org/g/509.gif" style="width:100px;height:100px"></div>`), nil
		case r.URL.Host == "ehgt.org":
			cdnRequests++
			return replayResponse(r, 200, "unexpected CDN request"), nil
		default:
			return replayResponse(r, 404, "unexpected URL"), nil
		}
	}))
	service.Client.sessionReady = true
	defer service.Close()
	_, _, err := service.GetImage(context.Background(), 1, "abc", 1)
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 429 {
		t.Fatalf("image error=%v, want quota HTTP 429", err)
	}
	if detailRequests != 1 || sRequests != 1 || cdnRequests != 0 {
		t.Fatalf("requests detail=%d s=%d CDN=%d", detailRequests, sRequests, cdnRequests)
	}
	if snap := service.Throttle.Circuit.Snapshot(); snap.State != "open" || snap.Reason != "exceed" {
		t.Fatalf("509 did not open quota circuit: %#v", snap)
	}
}

func TestRangeResumeAppendsPartialContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.part")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, Retries: 0}
	client := NewClient(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Range"); got != "bytes=3-" {
			return nil, fmt.Errorf("Range header=%q", got)
		}
		return replayResponse(r, 206, "def"), nil
	}), NewThrottle(cfg))
	client.sessionReady = true
	if n, err := client.Download(context.Background(), "https://node.hath.network/archive/start", path, 3, nil); err != nil || n != 6 {
		t.Fatalf("download offset=%d err=%v", n, err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "abcdef" {
		t.Fatalf("resumed archive=%q err=%v", body, err)
	}
}

func TestRatingRefreshesStaleAPIKeyOnce(t *testing.T) {
	detailCount := 0
	keys := []string{}
	cfg := config.Settings{EHSite: "e-hentai", IPBMemberID: "123", IPBPassHash: "hash", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/g/4/token/" {
			detailCount++
			return replayResponse(r, 200, fmt.Sprintf(`<html>var apikey = "key-%d";</html>`, detailCount)), nil
		}
		if r.URL.Path == "/api.php" {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			keys = append(keys, payload["apikey"].(string))
			if len(keys) == 1 {
				return replayResponse(r, 200, `{"error":"invalid apikey"}`), nil
			}
			return replayResponse(r, 200, `{"rating_usr":4.5,"rating_avg":4.2,"rating_cnt":10}`), nil
		}
		return replayResponse(r, 404, "unexpected request"), nil
	}))
	service.Client.sessionReady = true
	defer service.Close()
	result, err := service.RateGallery(context.Background(), 4, "token", 4.5)
	if err != nil {
		t.Fatal(err)
	}
	if result["rating_cnt"] != 10 || detailCount != 2 || len(keys) != 2 || keys[0] != "key-1" || keys[1] != "key-2" {
		t.Fatalf("stale-key replay result=%v details=%d keys=%v", result, detailCount, keys)
	}
}

func TestMyTagsLazyRefreshPersistsCompatibleSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mytags.json")
	cfg := config.Settings{EHSite: "e-hentai", IPBMemberID: "1", IPBPassHash: "hash", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, MyTagsTTLSeconds: 3600, MyTagsState: path, CacheDir: t.TempDir()}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/mytags" {
			return replayResponse(r, 404, "unexpected"), nil
		}
		return replayResponse(r, 200, `<div id="tagpreview_1" title="m:favorite" style="color:#f00;border-color:#00f"></div>`), nil
	}))
	service.Client.sessionReady = true
	styles := service.GetMyTags(context.Background())
	if styles["male:favorite"]["color"] != "#f00" || styles["male:favorite"]["borderColor"] != "#00f" {
		t.Fatalf("refreshed styles=%#v", styles)
	}
	_ = service.Close()
	loaded := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		return replayResponse(r, 500, "offline"), nil
	}))
	defer loaded.Close()
	if got := loaded.CachedMyTags()["male:favorite"]["color"]; got != "#f00" {
		t.Fatalf("persisted style missing: %q", got)
	}
}

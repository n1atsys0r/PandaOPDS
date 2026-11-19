package eh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pandaopds/internal/config"
)

func TestToplistPopulatesCoverCacheAndThumbHitsCDN(t *testing.T) {
	cover := losslessWebPSprite(t)
	toplistHTML := `<!doctype html><html><body>
	<table class="itg glte">
		<tr>
			<td class="gl1e"><div><a href="https://e-hentai.org/g/101/aaa101/"><img data-src="https://ehgt.org/t/c101.webp" /></a></div></td>
			<td class="gl2e"><div><div class="gl4e glname"><div class="glink">Gallery 101</div></div></div></td>
		</tr>
		<tr>
			<td class="gl1e"><div><a href="https://e-hentai.org/g/102/bbb102/"><img data-src="https://ehgt.org/t/c102.webp" /></a></div></td>
			<td class="gl2e"><div><div class="gl4e glname"><div class="glink">Gallery 102</div></div></div></td>
		</tr>
	</table>
	</body></html>`

	var toplistRequests, detailRequests, cdnRequests atomic.Int32
	cfg := config.Settings{
		EHSite:              "e-hentai",
		TimeoutSeconds:      2,
		HTMLIntervalSeconds: 0,
		Retries:             0,
		ListCacheTTLSeconds: 60,
		PageURLTTLSeconds:   60,
		CacheDir:            t.TempDir(),
		ImageCacheEnabled:   false,
	}

	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/toplist.php":
			toplistRequests.Add(1)
			return replayResponse(r, http.StatusOK, toplistHTML), nil
		case "/g/101/aaa101/":
			detailRequests.Add(1)
			return replayResponse(r, http.StatusOK, "<html>detail</html>"), nil
		case "/t/c101.webp":
			cdnRequests.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"image/webp"}},
				Body:       io.NopCloser(bytes.NewReader(cover)),
				Request:    r,
			}, nil
		default:
			return replayResponse(r, http.StatusNotFound, "unexpected request: "+r.URL.Path), nil
		}
	}))
	service.Client.sessionReady = true
	defer service.Close()

	info, err := service.Toplist(context.Background(), "yesterday", 1)
	if err != nil {
		t.Fatalf("Toplist: %v", err)
	}
	if len(info.Galleries) != 2 {
		t.Fatalf("got %d galleries, want 2", len(info.Galleries))
	}

	// Requesting thumbnail for gallery 101 MUST hit the CDN directly and NOT call getDetailPage!
	got, mime, err := service.GetThumb(context.Background(), 101, "aaa101")
	if err != nil {
		t.Fatalf("GetThumb: %v", err)
	}
	if mime != "image/webp" || !bytes.Equal(got, cover) {
		t.Fatalf("unexpected thumb response: mime=%q", mime)
	}

	if toplistRequests.Load() != 1 {
		t.Fatalf("toplist requests=%d, want 1", toplistRequests.Load())
	}
	if detailRequests.Load() != 0 {
		t.Fatalf("detail requests=%d, want 0 (cover must hit CDN without detail page scrape!)", detailRequests.Load())
	}
	if cdnRequests.Load() != 1 {
		t.Fatalf("cdn requests=%d, want 1", cdnRequests.Load())
	}
}

func TestToplistConcurrentDeduplication(t *testing.T) {
	toplistHTML := `<!doctype html><html><body><div class="itg gltc"></div></body></html>`
	var toplistRequests atomic.Int32
	cfg := config.Settings{
		EHSite:              "e-hentai",
		TimeoutSeconds:      2,
		HTMLIntervalSeconds: 0,
		Retries:             0,
		ListCacheTTLSeconds: 60,
		CacheDir:            t.TempDir(),
	}

	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/toplist.php" {
			toplistRequests.Add(1)
			time.Sleep(20 * time.Millisecond) // simulate upstream latency
			return replayResponse(r, http.StatusOK, toplistHTML), nil
		}
		return replayResponse(r, http.StatusNotFound, "unexpected: "+r.URL.Path), nil
	}))
	service.Client.sessionReady = true
	defer service.Close()

	var wg sync.WaitGroup
	const callers = 5
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = service.Toplist(context.Background(), "yesterday", 1)
		}()
	}
	close(start)
	wg.Wait()

	if toplistRequests.Load() != 1 {
		t.Fatalf("concurrent toplist requests=%d, want 1 (deduplicated by acquireList)", toplistRequests.Load())
	}
}

func TestGetThumbSingleflightDeduplication(t *testing.T) {
	cover := losslessWebPSprite(t)
	var cdnRequests atomic.Int32
	cfg := config.Settings{
		EHSite:            "e-hentai",
		TimeoutSeconds:    2,
		CacheDir:          t.TempDir(),
		ImageCacheEnabled: false,
	}

	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/t/shared.webp" {
			cdnRequests.Add(1)
			time.Sleep(25 * time.Millisecond)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"image/webp"}},
				Body:       io.NopCloser(bytes.NewReader(cover)),
				Request:    r,
			}, nil
		}
		return replayResponse(r, http.StatusNotFound, "unexpected: "+r.URL.Path), nil
	}))
	service.Client.sessionReady = true
	defer service.Close()

	service.RememberCover(201, "tok201", "https://ehgt.org/t/shared.webp")

	var wg sync.WaitGroup
	const callers = 6
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, _, err := service.GetThumb(context.Background(), 201, "tok201")
			if err != nil {
				t.Errorf("GetThumb: %v", err)
			}
			if !bytes.Equal(got, cover) {
				t.Errorf("got invalid cover bytes")
			}
		}()
	}
	close(start)
	wg.Wait()

	if cdnRequests.Load() != 1 {
		t.Fatalf("concurrent GetThumb requests=%d, want 1 (singleflight)", cdnRequests.Load())
	}
}

func TestGetThumbColdUsesGDataFallback(t *testing.T) {
	cover := losslessWebPSprite(t)
	var gdataRequests, detailRequests, cdnRequests atomic.Int32

	cfg := config.Settings{
		EHSite:              "e-hentai",
		TimeoutSeconds:      2,
		HTMLIntervalSeconds: 0,
		Retries:             0,
		PageURLTTLSeconds:   60,
		CacheDir:            t.TempDir(),
		ImageCacheEnabled:   false,
	}

	gdataResponse := struct {
		GMetadata []map[string]any `json:"gmetadata"`
	}{
		GMetadata: []map[string]any{
			{
				"gid":   301,
				"token": "tok301",
				"thumb": "https://ehgt.org/t/gdata_cover.webp",
			},
		},
	}
	gdataJSON, _ := json.Marshal(gdataResponse)

	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api.php":
			gdataRequests.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(gdataJSON)),
				Request:    r,
			}, nil
		case "/g/301/tok301/":
			detailRequests.Add(1)
			return replayResponse(r, http.StatusOK, "<html>detail</html>"), nil
		case "/t/gdata_cover.webp":
			cdnRequests.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"image/webp"}},
				Body:       io.NopCloser(bytes.NewReader(cover)),
				Request:    r,
			}, nil
		default:
			return replayResponse(r, http.StatusNotFound, "unexpected: "+r.URL.Path), nil
		}
	}))
	service.Client.sessionReady = true
	defer service.Close()

	// 301 is cold (neither in s.cover nor on disk). It should use GData API to get thumb URL!
	got, mime, err := service.GetThumb(context.Background(), 301, "tok301")
	if err != nil {
		t.Fatalf("GetThumb: %v", err)
	}
	if mime != "image/webp" || !bytes.Equal(got, cover) {
		t.Fatalf("got unexpected bytes, mime=%q", mime)
	}

	if gdataRequests.Load() != 1 {
		t.Fatalf("gdataRequests=%d, want 1", gdataRequests.Load())
	}
	if detailRequests.Load() != 0 {
		t.Fatalf("detailRequests=%d, want 0 (GData should prevent HTML detail scrape)", detailRequests.Load())
	}
	if cdnRequests.Load() != 1 {
		t.Fatalf("cdnRequests=%d, want 1", cdnRequests.Load())
	}

	// Verify s.cover was populated so subsequent requests don't even call GData
	cachedURL, found := service.cover[fmt.Sprintf("%d:%s", 301, "tok301")]
	if !found || cachedURL.value != "https://ehgt.org/t/gdata_cover.webp" {
		t.Fatalf("s.cover not populated: found=%v, value=%q", found, cachedURL.value)
	}
}

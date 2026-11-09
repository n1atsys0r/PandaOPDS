package eh

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pandaopds/internal/config"
)

const spritePreviewURL = "https://ehgt.org/sprite.webp"

func TestConcurrentPageThumbnailsShareDetailAndSpriteFetch(t *testing.T) {
	sprite := losslessWebPSprite(t)
	detail := webPSpriteDetail(`background: url(https://ehgt.org/sprite.webp) -8px 0px no-repeat; width: 8px; height: 8px`)
	releaseDetail := make(chan struct{})
	releaseSprite := make(chan struct{})
	detailStarted := make(chan struct{})
	spriteStarted := make(chan struct{})
	var detailOnce, spriteOnce, releaseDetailOnce, releaseSpriteOnce sync.Once
	defer releaseDetailOnce.Do(func() { close(releaseDetail) })
	defer releaseSpriteOnce.Do(func() { close(releaseSprite) })
	var detailRequests, spriteRequests atomic.Int32
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, PSEPageBase: 1, CacheDir: t.TempDir(), ImageCacheEnabled: false}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/g/77/token/":
			detailRequests.Add(1)
			detailOnce.Do(func() { close(detailStarted) })
			if err := waitForRelease(r, releaseDetail); err != nil {
				return nil, err
			}
			return replayResponse(r, http.StatusOK, detail), nil
		case "/sprite.webp":
			spriteRequests.Add(1)
			spriteOnce.Do(func() { close(spriteStarted) })
			if err := waitForRelease(r, releaseSprite); err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/webp"}}, Body: io.NopCloser(bytes.NewReader(sprite)), Request: r}, nil
		default:
			return replayResponse(r, http.StatusNotFound, "unexpected request"), nil
		}
	}))
	service.Client.sessionReady = true
	t.Cleanup(func() { _ = service.Close() })

	type result struct {
		page int
		data []byte
		err  error
	}
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, page := range []int{1, 2} {
		go func(page int) {
			ready <- struct{}{}
			<-start
			data, _, err := service.GetPageThumbnail(context.Background(), 77, "token", page)
			results <- result{page: page, data: data, err: err}
		}(page)
	}
	<-ready
	<-ready
	close(start)
	waitForSignal(t, detailStarted, "detail request")
	time.Sleep(30 * time.Millisecond)
	releaseDetailOnce.Do(func() { close(releaseDetail) })
	waitForSignal(t, spriteStarted, "sprite request")
	time.Sleep(30 * time.Millisecond)
	releaseSpriteOnce.Do(func() { close(releaseSprite) })

	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatalf("page %d thumbnail: %v", got.page, got.err)
		}
		assertPreviewColor(t, got.page, got.data)
	}
	if got := detailRequests.Load(); got != 1 {
		t.Errorf("detail requests=%d, want 1", got)
	}
	if got := spriteRequests.Load(); got != 1 {
		t.Errorf("sprite requests=%d, want 1", got)
	}
}

func TestCanceledThumbnailWaiterLeavesSharedFetchRunning(t *testing.T) {
	sprite := losslessWebPSprite(t)
	detail := webPSpriteDetail(`background: url(https://ehgt.org/sprite.webp) -8px 0px no-repeat; width: 8px; height: 8px`)
	releaseSprite := make(chan struct{})
	spriteStarted := make(chan struct{})
	var spriteOnce, releaseSpriteOnce sync.Once
	defer releaseSpriteOnce.Do(func() { close(releaseSprite) })
	var spriteRequests atomic.Int32
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, PSEPageBase: 1, CacheDir: t.TempDir(), ImageCacheEnabled: false}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/g/77/token/":
			return replayResponse(r, http.StatusOK, detail), nil
		case "/sprite.webp":
			spriteRequests.Add(1)
			spriteOnce.Do(func() { close(spriteStarted) })
			if err := waitForRelease(r, releaseSprite); err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/webp"}}, Body: io.NopCloser(bytes.NewReader(sprite)), Request: r}, nil
		default:
			return replayResponse(r, http.StatusNotFound, "unexpected request"), nil
		}
	}))
	service.Client.sessionReady = true
	t.Cleanup(func() { _ = service.Close() })

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		_, _, err := service.GetPageThumbnail(firstCtx, 77, "token", 2)
		firstResult <- err
	}()
	waitForSignal(t, spriteStarted, "sprite request")
	type previewResult struct {
		data []byte
		err  error
	}
	secondResult := make(chan previewResult, 1)
	go func() {
		preview, _, err := service.GetPageThumbnail(context.Background(), 77, "token", 1)
		secondResult <- previewResult{data: preview, err: err}
	}()
	time.Sleep(30 * time.Millisecond)
	cancelFirst()
	select {
	case err := <-firstResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first waiter error=%v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter did not leave the shared fetch")
	}
	time.Sleep(30 * time.Millisecond)
	releaseSpriteOnce.Do(func() { close(releaseSprite) })
	select {
	case got := <-secondResult:
		if got.err != nil {
			t.Fatalf("remaining waiter: %v", got.err)
		}
		assertPreviewColor(t, 1, got.data)
	case <-time.After(2 * time.Second):
		t.Fatal("remaining waiter did not receive shared sprite")
	}
	if got := spriteRequests.Load(); got != 1 {
		t.Fatalf("sprite requests=%d, want 1", got)
	}
}

func TestPageThumbnailReadsPythonCompatibleSpriteDiskKey(t *testing.T) {
	sprite := losslessWebPSprite(t)
	detail := webPSpriteDetail(`background: url(https://ehgt.org/sprite.webp) -8px 0px no-repeat; width: 8px; height: 8px`)
	cacheDir := t.TempDir()
	key := "thumb-url:" + spritePreviewURL
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	cachePath := filepath.Join(cacheDir, hash[:2], hash[2:])
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, sprite, 0o600); err != nil {
		t.Fatal(err)
	}
	var spriteRequests atomic.Int32
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, PSEPageBase: 1, CacheDir: cacheDir, ImageCacheEnabled: true}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/g/77/token/" {
			return replayResponse(r, http.StatusOK, detail), nil
		}
		if r.URL.Path == "/sprite.webp" {
			spriteRequests.Add(1)
			return replayResponse(r, http.StatusBadGateway, "disk hit expected"), nil
		}
		return replayResponse(r, http.StatusNotFound, "unexpected request"), nil
	}))
	service.Client.sessionReady = true
	defer service.Close()
	preview, _, err := service.GetPageThumbnail(context.Background(), 77, "token", 2)
	if err != nil {
		t.Fatalf("GetPageThumbnail: %v", err)
	}
	assertPreviewColor(t, 2, preview)
	if got := spriteRequests.Load(); got != 0 {
		t.Fatalf("sprite network requests=%d, want cache hit", got)
	}
}

func TestFailedSpriteDecodeIsNotCachedAndRetriesOnNextRequest(t *testing.T) {
	sprite := losslessWebPSprite(t)
	detail := webPSpriteDetail(`background: url(https://ehgt.org/sprite.webp) -8px 0px no-repeat; width: 8px; height: 8px`)
	var spriteRequests atomic.Int32
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, PSEPageBase: 1, CacheDir: t.TempDir(), ImageCacheEnabled: true}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/g/77/token/":
			return replayResponse(r, http.StatusOK, detail), nil
		case "/sprite.webp":
			if spriteRequests.Add(1) == 1 {
				return replayResponse(r, http.StatusOK, "not an image"), nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/webp"}}, Body: io.NopCloser(bytes.NewReader(sprite)), Request: r}, nil
		default:
			return replayResponse(r, http.StatusNotFound, "unexpected request"), nil
		}
	}))
	service.Client.sessionReady = true
	defer service.Close()
	if _, _, err := service.GetPageThumbnail(context.Background(), 77, "token", 2); err == nil {
		t.Fatal("first thumbnail should fail to decode")
	}
	preview, _, err := service.GetPageThumbnail(context.Background(), 77, "token", 2)
	if err != nil {
		t.Fatalf("retry GetPageThumbnail: %v", err)
	}
	assertPreviewColor(t, 2, preview)
	if got := spriteRequests.Load(); got != 2 {
		t.Fatalf("sprite requests=%d, want retry after decode failure", got)
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitForRelease(r *http.Request, release <-chan struct{}) error {
	select {
	case <-release:
		return nil
	case <-r.Context().Done():
		return r.Context().Err()
	}
}

func assertPreviewColor(t *testing.T, page int, data []byte) {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode page %d preview: %v", page, err)
	}
	if format != "jpeg" || img.Bounds().Dx() != 8 || img.Bounds().Dy() != 8 {
		t.Fatalf("page %d preview format=%q bounds=%v, want jpeg 8x8", page, format, img.Bounds())
	}
	r, g, b, _ := img.At(4, 4).RGBA()
	if page == 1 && (r < 0x8000 || r < g*2 || r < b*2) {
		t.Fatalf("page 1 crop is not red: center RGB=(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
	if page == 2 && (g < 0x8000 || g < r*2 || g < b*2) {
		t.Fatalf("page 2 crop is not green: center RGB=(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

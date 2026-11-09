package eh

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"pandaopds/internal/config"
)

func TestGetThumbColdUsesDetailCoverBeforeFirstPageSprite(t *testing.T) {
	cover := losslessWebPSprite(t)
	detail := `<!doctype html><html><body>
	<div id="gd1"><div style="background:transparent url(https://ehgt.org/cover.webp) no-repeat;width:100px;height:100px"></div></div>
	<div id="gdt"><div class="gdtm"><a href="/s/abc/77-1"><div style="background:transparent url(https://ehgt.org/sprite.webp) 0px 0px no-repeat;width:8px;height:8px"></div></a></div></div>
	</body></html>`
	var detailRequests, coverRequests, spriteRequests atomic.Int32
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, CacheDir: t.TempDir(), ImageCacheEnabled: false}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/g/77/token/":
			detailRequests.Add(1)
			return replayResponse(r, http.StatusOK, detail), nil
		case "/cover.webp":
			coverRequests.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/webp"}}, Body: io.NopCloser(bytes.NewReader(cover)), Request: r}, nil
		case "/sprite.webp":
			spriteRequests.Add(1)
			return replayResponse(r, http.StatusOK, "wrong source"), nil
		default:
			return replayResponse(r, http.StatusNotFound, "unexpected request"), nil
		}
	}))
	service.Client.sessionReady = true
	defer service.Close()

	got, mime, err := service.GetThumb(context.Background(), 77, "token")
	if err != nil {
		t.Fatalf("GetThumb: %v", err)
	}
	if mime != "image/webp" || !bytes.Equal(got, cover) {
		t.Fatalf("cold cover result mime=%q bytes match=%v", mime, bytes.Equal(got, cover))
	}
	if detailRequests.Load() != 1 || coverRequests.Load() != 1 || spriteRequests.Load() != 0 {
		t.Fatalf("requests detail=%d cover=%d first-page sprite=%d, want 1/1/0", detailRequests.Load(), coverRequests.Load(), spriteRequests.Load())
	}
}

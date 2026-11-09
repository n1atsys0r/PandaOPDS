package eh

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"io"
	"net/http"
	"strings"
	"testing"

	"pandaopds/internal/config"
)

const losslessWebPSpriteBase64 = "UklGRigAAABXRUJQVlA4TBwAAAAvD8ABAA/wjNz3cF8hY/7DA2LBZP7SnSlE9D90"

func losslessWebPSprite(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(losslessWebPSpriteBase64)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newWebPSpriteService(t *testing.T, detail string, sprite []byte) *Service {
	t.Helper()
	cfg := config.Settings{EHSite: "e-hentai", TimeoutSeconds: 2, HTMLIntervalSeconds: 0, Retries: 0, PSEPageBase: 1, CacheDir: t.TempDir(), ImageCacheEnabled: false}
	service := NewService(cfg, replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/":
			return replayResponse(r, http.StatusOK, `<html>session</html>`), nil
		case "/g/77/token/":
			return replayResponse(r, http.StatusOK, detail), nil
		case "/sprite.webp":
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/webp"}}, Body: io.NopCloser(bytes.NewReader(sprite)), Request: r}, nil
		default:
			return replayResponse(r, http.StatusNotFound, "unexpected request"), nil
		}
	}))
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func webPSpriteDetail(secondStyle string) string {
	return `<div id="gdt">
		<div class="gdtm"><a href="/s/aaa/77-1"><div style="background: url(https://ehgt.org/sprite.webp) 0px 0px no-repeat; width: 8px; height: 8px"></div></a></div>
		<div class="gdtm"><a href="/s/bbb/77-2"><div style="` + secondStyle + `"></div></a></div>
	</div>`
}

func TestGetPageThumbnailRendersWebPSpriteRegion(t *testing.T) {
	sprite := losslessWebPSprite(t)
	detail := webPSpriteDetail(`background: url(https://ehgt.org/sprite.webp) -8px 0px no-repeat; width: 8px; height: 8px`)
	service := newWebPSpriteService(t, detail, sprite)
	preview, mime, err := service.GetPageThumbnail(context.Background(), 77, "token", 2)
	if err != nil {
		t.Fatalf("GetPageThumbnail: %v", err)
	}
	if mime != "image/jpeg" {
		t.Fatalf("thumbnail MIME=%q, want image/jpeg", mime)
	}
	img, format, err := image.Decode(bytes.NewReader(preview))
	if err != nil {
		t.Fatalf("decode returned preview: %v", err)
	}
	if format != "jpeg" || img.Bounds().Dx() != 8 || img.Bounds().Dy() != 8 {
		t.Fatalf("preview format=%q bounds=%v, want jpeg 8x8", format, img.Bounds())
	}
	r, g, b, _ := img.At(4, 4).RGBA()
	if g < 0x8000 || g < r*2 || g < b*2 {
		t.Fatalf("page 2 crop is not green: center RGB=(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestGetPageThumbnailWebPSpriteStillFailsClosed(t *testing.T) {
	sprite := losslessWebPSprite(t)
	tests := []struct {
		name        string
		secondStyle string
		wantError   string
	}{
		{name: "out of bounds", secondStyle: `background: url(https://ehgt.org/sprite.webp) -16px 0px no-repeat; width: 8px; height: 8px`, wantError: "sprite crop is outside source image bounds"},
		{name: "ambiguous layout", secondStyle: `background: url(https://ehgt.org/sprite.webp); width: 8px; height: 8px`, wantError: "thumbnail layout is ambiguous; preview omitted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail := webPSpriteDetail(test.secondStyle)
			service := newWebPSpriteService(t, detail, sprite)
			_, _, err := service.GetPageThumbnail(context.Background(), 77, "token", 2)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("GetPageThumbnail error=%v, want %q", err, test.wantError)
			}
		})
	}
}

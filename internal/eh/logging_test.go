package eh

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"pandaopds/internal/config"
)

func TestClientLogsRetriesAndErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	attempts := 0
	rt := replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts <= 2 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     http.Header{},
				Body:       http.NoBody,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("<html>ok</html>")),
		}, nil
	})

	cfg := config.Settings{Retries: 2}
	client := NewClient(cfg, rt, NewThrottle(cfg))
	client.Logger = logger

	_, _, err := client.Get(context.Background(), "https://e-hentai.org/test", KindHTML)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "upstream HTTP 503:") || !strings.Contains(logOutput, "(retry 1/2)") {
		t.Fatalf("expected retry log, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "(retry 2/2)") {
		t.Fatalf("expected retry 2 log, got: %s", logOutput)
	}
}

package app

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandaopds/internal/eh"
)

func TestLoggingMiddlewareRecordsAccessLogWithXForwardedFor(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	settings := loadTestSettings(t.TempDir())
	settings.PublicBaseURL = "http://reader.test"
	server, err := New(settings, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.195, 10.0.0.1")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, `203.0.113.195 - "GET /health HTTP/1.1" 200`) {
		t.Fatalf("expected access log with forwarded IP and status, got: %s", logOutput)
	}
}

func TestLoggingMiddlewareRecordsBasicAuthRejection(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	settings := loadTestSettings(t.TempDir())
	settings.AuthUsername = "admin"
	settings.AuthPassword = "secret"
	settings.PublicBaseURL = "http://reader.test"

	server, err := New(settings, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/opds/v1.2", nil)
	req.Header.Set("X-Real-IP", "198.51.100.42")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "basic auth rejected: GET /opds/v1.2 from 198.51.100.42") {
		t.Fatalf("expected basic auth rejection log, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, `198.51.100.42 - "GET /opds/v1.2 HTTP/1.1" 401`) {
		t.Fatalf("expected access log line with 401, got: %s", logOutput)
	}
}

func TestRecoverMiddlewareLogsPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	settings := loadTestSettings(t.TempDir())
	server := &Server{
		Settings: settings,
		Logger:   logger,
		mux:      http.NewServeMux(),
	}
	server.mux.HandleFunc("GET /crash", func(w http.ResponseWriter, r *http.Request) {
		panic("simulated server crash")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/crash", nil)
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "HTTP panic GET /crash: simulated server crash") {
		t.Fatalf("expected panic log, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"GET /crash HTTP/1.1" 500`) {
		t.Fatalf("expected 500 access log, got: %s", logOutput)
	}
}

func TestWriteErrorLogsUpstreamErrorDetails(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	settings := loadTestSettings(t.TempDir())
	server := &Server{
		Settings: settings,
		Logger:   logger,
		mux:      http.NewServeMux(),
	}
	server.mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) {
		server.writeError(w, r, &eh.UpstreamError{Status: 503, Detail: "E-Hentai IP address is banned"})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "upstream error GET /fail: status=503 E-Hentai IP address is banned") {
		t.Fatalf("expected upstream error log, got: %s", logOutput)
	}
}

func TestImageFetchLogsRejectionForUntrustedHost(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	settings := loadTestSettings(t.TempDir())
	settings.ImageProxyHosts = []string{"ehgt.org"}
	server := &Server{
		Settings: settings,
		Logger:   logger,
		mux:      http.NewServeMux(),
	}
	server.mux.HandleFunc("GET /image/fetch", server.imageFetch)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/image/fetch?url=https://malicious.example/image.jpg", nil)
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "cover proxy rejected /image/fetch?url=https://malicious.example/image.jpg: invalid or unsupported url") {
		t.Fatalf("expected cover proxy rejection log, got: %s", logOutput)
	}
}

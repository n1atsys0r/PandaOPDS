package app

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const redirectTestHome = `[[section]]
kind = "navigation"
title = "Latest"
type = "preset"
query = "latest"
`

func TestOPDSEntryAliasesRedirectExactlyAndKeepRawQuery(t *testing.T) {
	var upstreamRequests atomic.Int32
	server := newHomePreviewServer(t, redirectTestHome, homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		upstreamRequests.Add(1)
		return homePreviewResponse(r, `<html>unexpected upstream request</html>`), nil
	}))
	server.Settings.PublicBaseURL = "https://reader.example/proxy"
	server.mux = http.NewServeMux()
	server.register()
	handler := server.Handler()

	tests := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/opds/v1.2/?q=a%20b&raw=1+2", "https://reader.example/proxy/opds/v1.2?q=a%20b&raw=1+2"},
		{http.MethodGet, "/opds/v2.0/?q=%2f", "https://reader.example/proxy/opds/v2.0?q=%2f"},
		{http.MethodGet, "/opds/v2?", "https://reader.example/proxy/opds/v2.0?"},
		{http.MethodHead, "/opds/v2/?n=%2B", "https://reader.example/proxy/opds/v2.0?n=%2B"},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != http.StatusTemporaryRedirect {
				t.Fatalf("status=%d, want 307: body=%q", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Location"); got != tc.want {
				t.Fatalf("Location=%q, want %q", got, tc.want)
			}
			if response.Body.Len() != 0 {
				t.Fatalf("redirect body=%q, want empty", response.Body.String())
			}
		})
	}

	for _, path := range []string{"/opds", "/opds/v2/child", "/opds/v2.0/child", "/opds/v1.2/child"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("unknown path %q status=%d, want 404", path, response.Code)
		}
	}

	for _, path := range []string{"/opds/v1.2", "/opds/v2.0"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("canonical path %q status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	if got := upstreamRequests.Load(); got != 0 {
		t.Fatalf("redirect/canonical routes made %d upstream calls, want zero", got)
	}
}

func TestOPDSEntryAliasUsesNormalBasicAuthGate(t *testing.T) {
	server := newHomePreviewServer(t, redirectTestHome, homePreviewTransport(func(r *http.Request) (*http.Response, error) {
		return homePreviewResponse(r, `<html>unexpected upstream request</html>`), nil
	}))
	server.Settings.PublicBaseURL = ""
	server.Settings.AuthUsername = "reader"
	server.Settings.AuthPassword = "secret"
	server.mux = http.NewServeMux()
	server.register()
	handler := server.Handler()

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/opds/v2", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated alias status=%d, want 401", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/opds/v2", nil)
	request.SetBasicAuth("reader", "secret")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusTemporaryRedirect {
		t.Fatalf("authenticated alias status=%d body=%q, want 307", authorized.Code, authorized.Body.String())
	}
	if got := authorized.Header().Get("Location"); got != "/opds/v2.0" {
		t.Fatalf("authenticated alias Location=%q", got)
	}
}

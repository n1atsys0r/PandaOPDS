package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestProgressionProtocolAndBasicAuthenticationChallenge(t *testing.T) {
	base := t.TempDir()
	cfg := loadTestSettings(base)
	cfg.ArchiveStateDir = filepath.Join(base, "state")
	s, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	valid := `{"modified":"2026-01-01T00:00:00Z","device":{"id":"urn:uuid:test","name":"Reader"},"progression":0.8,"references":["page8"]}`
	path := "/opds/progression/81/DEADBEEF"
	if got := call(http.MethodPut, path, valid); got.Code != 201 || got.Header().Get("Content-Type") != "application/opds-progression+json" {
		t.Fatalf("create response=%d %s %s", got.Code, got.Header().Get("Content-Type"), got.Body.String())
	}
	if got := call(http.MethodPut, path, valid); got.Code != 200 {
		t.Fatalf("retry response=%d %s", got.Code, got.Body.String())
	}
	changed := strings.Replace(valid, "0.8", "0.3", 1)
	if got := call(http.MethodPut, path, changed); got.Code != 409 || !strings.Contains(got.Header().Get("Content-Type"), "problem+json") {
		t.Fatalf("conflict response=%d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodPut, path, strings.Replace(valid, "0.8", "1.1", 1)); got.Code != 400 {
		t.Fatalf("invalid progression response=%d", got.Code)
	}
	withUnknown := strings.TrimSuffix(valid, "}") + `,"client_extension":true}`
	if got := call(http.MethodPut, "/opds/progression/82/abcdef", withUnknown); got.Code != 201 {
		t.Fatalf("unknown extension response=%d %s", got.Code, got.Body.String())
	}
	if err := s.Progression.Observe(82, "abcdef", 83, "fedcba"); err != nil {
		t.Fatal(err)
	}
	retry := call(http.MethodPut, "/opds/progression/83/fedcba", withUnknown)
	if retry.Code != 200 || strings.Contains(retry.Body.String(), `"references":["page8"]`) {
		t.Fatalf("cross-version retry response=%d %s", retry.Code, retry.Body.String())
	}
	cfg.AuthUsername = "reader"
	cfg.AuthPassword = "secret"
	s.Settings = cfg
	unauthorized := call(http.MethodGet, path, "")
	if unauthorized.Code != http.StatusUnauthorized || unauthorized.Header().Get("WWW-Authenticate") == "" || !strings.Contains(unauthorized.Header().Get("Content-Type"), "application/opds-authentication+json") {
		t.Fatalf("unauthorized response=%d headers=%v body=%s", unauthorized.Code, unauthorized.Header(), unauthorized.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(unauthorized.Body.Bytes(), &doc); err != nil || doc["id"] == nil || doc["title"] == nil {
		t.Fatalf("authentication document=%s err=%v", unauthorized.Body.String(), err)
	}
}

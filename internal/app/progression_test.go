package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/home"
	"pandaopds/internal/progression"
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

func TestHistoryEndpoints(t *testing.T) {
	base := t.TempDir()
	cfg := loadTestSettings(base)
	cfg.ArchiveStateDir = filepath.Join(base, "state")
	s, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	call := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}

	putProg := func(gid int, token, modified string) {
		body := `{"modified":"` + modified + `","device":{"id":"urn:uuid:test","name":"Reader"},"progression":0.5}`
		r := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/opds/progression/%d/%s", gid, token), strings.NewReader(body))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 && w.Code != 201 {
			t.Fatalf("put progression %d/%s code=%d", gid, token, w.Code)
		}
	}

	putProg(101, "deadbeef01", "2026-01-01T10:00:00Z")
	_ = s.Progression.SetSnapshot(101, "deadbeef01", progression.Snapshot{
		Title:    "Gallery 101",
		Category: "Manga",
	})

	putProg(202, "deadbeef02", "2026-01-01T12:00:00Z")
	_ = s.Progression.SetSnapshot(202, "deadbeef02", progression.Snapshot{
		Title:    "Gallery 202",
		Category: "Doujinshi",
	})

	// OPDS 1.2 history
	res12 := call(http.MethodGet, "/opds/v1.2/history")
	if res12.Code != 200 {
		t.Fatalf("history12 code=%d body=%s", res12.Code, res12.Body.String())
	}
	body12 := res12.Body.String()
	if !strings.Contains(body12, "E-Hentai: Recently Read") || !strings.Contains(body12, "Gallery 202") || !strings.Contains(body12, "Gallery 101") {
		t.Fatalf("history12 missing expected content: %s", body12)
	}
	idx202 := strings.Index(body12, "Gallery 202")
	idx101 := strings.Index(body12, "Gallery 101")
	if idx202 > idx101 {
		t.Fatalf("expected Gallery 202 before Gallery 101 in reverse chronological history")
	}

	// OPDS 2.0 history
	res20 := call(http.MethodGet, "/opds/v2.0/history")
	if res20.Code != 200 {
		t.Fatalf("history20 code=%d body=%s", res20.Code, res20.Body.String())
	}
	var doc20 map[string]any
	if err := json.Unmarshal(res20.Body.Bytes(), &doc20); err != nil {
		t.Fatal(err)
	}
	pubs, _ := doc20["publications"].([]any)
	if len(pubs) != 2 {
		t.Fatalf("expected 2 publications in history20, got %d", len(pubs))
	}
	p0 := pubs[0].(map[string]any)["metadata"].(map[string]any)["title"].(string)
	p1 := pubs[1].(map[string]any)["metadata"].(map[string]any)["title"].(string)
	if p0 != "Gallery 202" || p1 != "Gallery 101" {
		t.Fatalf("expected [Gallery 202, Gallery 101], got [%s, %s]", p0, p1)
	}

	// OPDS 2.0 category filtering
	resFiltered := call(http.MethodGet, "/opds/v2.0/history?category=Manga")
	if resFiltered.Code != 200 {
		t.Fatalf("history20 filtered code=%d", resFiltered.Code)
	}
	var docFiltered map[string]any
	_ = json.Unmarshal(resFiltered.Body.Bytes(), &docFiltered)
	pubsFiltered, _ := docFiltered["publications"].([]any)
	if len(pubsFiltered) != 1 {
		t.Fatalf("expected 1 publication for Manga filter, got %d", len(pubsFiltered))
	}

	// Home fetch section preset
	homeSec, err := s.fetchHomeSection(context.Background(), home.Section{
		Kind:  "publication",
		Group: true,
		Title: "Recently Read",
		Type:  "preset",
		Query: "history",
	})
	if err != nil {
		t.Fatalf("fetchHomeSection error: %v", err)
	}
	if len(homeSec.Galleries) != 2 {
		t.Fatalf("expected 2 galleries in homeSec, got %d", len(homeSec.Galleries))
	}
	if homeSec.Galleries[0].GID != 202 || homeSec.Galleries[1].GID != 101 {
		t.Fatalf("expected [202, 101], got [%d, %d]", homeSec.Galleries[0].GID, homeSec.Galleries[1].GID)
	}
}

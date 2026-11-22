package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/config"
)

func TestConfigEditorRequiresETagAndKeepsActiveSnapshot(t *testing.T) {
	t.Setenv("EH_SITE", "")
	_ = os.Unsetenv("EH_SITE")
	t.Setenv("OPDS_ACQ_MODE", "")
	_ = os.Unsetenv("OPDS_ACQ_MODE")
	p := filepath.Join(t.TempDir(), "settings.toml")
	s := &Server{Settings: config.Settings{EHSite: "e-hentai", PSEPageBase: 1, CacheMaxGB: 4}, settingsPath: p, editKey: []byte("test-only-etag-key")}
	get := httptest.NewRecorder()
	s.configEdit(get, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
	if get.Code != 200 || get.Header().Get("ETag") == "" {
		t.Fatalf("GET status=%d etag=%q", get.Code, get.Header().Get("ETag"))
	}
	if strings.Contains(get.Body.String(), "member-id-secret") {
		t.Fatal("response contains credentials")
	}
	missing := httptest.NewRequest(http.MethodPatch, "/api/config/edit", strings.NewReader(`{"eh_site":"exhentai"}`))
	missing.Header.Set("Content-Type", "application/json")
	bad := httptest.NewRecorder()
	s.configEdit(bad, missing)
	if bad.Code != 428 {
		t.Fatalf("missing If-Match status=%d", bad.Code)
	}
	patch := httptest.NewRequest(http.MethodPatch, "/api/config/edit", strings.NewReader(`{"eh_site":"exhentai"}`))
	patch.Header.Set("Content-Type", "application/json")
	patch.Header.Set("If-Match", get.Header().Get("ETag"))
	ok := httptest.NewRecorder()
	s.configEdit(ok, patch)
	if ok.Code != 200 {
		t.Fatalf("PATCH status=%d body=%s", ok.Code, ok.Body.String())
	}
	if s.Settings.EHSite != "e-hentai" {
		t.Fatalf("active settings changed before restart: %s", s.Settings.EHSite)
	}
	var saved map[string]any
	if err := json.Unmarshal(ok.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	field := saved["fields"].(map[string]any)["eh_site"].(map[string]any)
	if field["active"] != "e-hentai" || field["saved"] != "exhentai" || field["pending"] != true {
		t.Fatalf("active/saved restart state mismatch: %#v", field)
	}
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("settings file mode: %v %v", st, err)
	}
	currentTag := ok.Header().Get("ETag")
	if err := os.WriteFile(p, append(mustRead(t, p), []byte("\n# external update\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	stale := httptest.NewRequest(http.MethodPatch, "/api/config/edit", strings.NewReader(`{"public_base_url":"https://reader.example/proxy"}`))
	stale.Header.Set("Content-Type", "application/json")
	stale.Header.Set("If-Match", currentTag)
	conflict := httptest.NewRecorder()
	s.configEdit(conflict, stale)
	if conflict.Code != 412 {
		t.Fatalf("stale write status=%d", conflict.Code)
	}
}

func TestArchiveEditorPersistsFileOnlySettingsForRestart(t *testing.T) {
	t.Setenv("ARCHIVE_UPDATE_INTERVAL_DAYS", "2")
	t.Setenv("ARCHIVE_AUTO_UPDATE", "true")
	t.Setenv("ARCHIVE_UPDATE_INTERVAL_SECONDS", "0")
	t.Setenv("FAVORITES_SYNC_ARCHIVE", "")
	_ = os.Unsetenv("FAVORITES_SYNC_ARCHIVE")
	p := filepath.Join(t.TempDir(), "settings.toml")
	active := config.Settings{ArchiveUpdateIntervalSeconds: 7 * 86400}
	s := &Server{Settings: active, settingsPath: p, editKey: []byte("test-only-etag-key")}
	get := httptest.NewRecorder()
	s.configEdit(get, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
	var shape map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &shape); err != nil {
		t.Fatal(err)
	}
	fields := shape["fields"].(map[string]any)
	for _, key := range []string{"archive_update_interval_days", "archive_auto_update"} {
		field := fields[key].(map[string]any)
		if field["source"] != "default" || field["locked"] != false {
			t.Fatalf("%s incorrectly env-locked: %#v", key, field)
		}
	}
	patch := httptest.NewRequest(http.MethodPatch, "/api/config/edit", strings.NewReader(`{"archive_update_interval_days":0,"archive_auto_update":true}`))
	patch.Header.Set("Content-Type", "application/json")
	patch.Header.Set("If-Match", get.Header().Get("ETag"))
	ok := httptest.NewRecorder()
	s.configEdit(ok, patch)
	if ok.Code != 200 {
		t.Fatalf("PATCH status=%d body=%s", ok.Code, ok.Body.String())
	}
	restarted := config.LoadWithConfigPath(p)
	if restarted.ArchiveUpdateIntervalSeconds != 0 || !restarted.ArchiveAutoUpdate {
		t.Fatalf("restarted interval=%d auto=%v", restarted.ArchiveUpdateIntervalSeconds, restarted.ArchiveAutoUpdate)
	}
	if strings.Contains(string(mustRead(t, p)), "ARCHIVE_UPDATE_INTERVAL_SECONDS") {
		t.Fatal("legacy seconds key persisted")
	}
	bad := httptest.NewRequest(http.MethodPatch, "/api/config/edit", strings.NewReader(`{"archive_update_interval_days":1.25}`))
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("If-Match", ok.Header().Get("ETag"))
	invalid := httptest.NewRecorder()
	s.configEdit(invalid, bad)
	if invalid.Code != 400 {
		t.Fatalf("fractional interval status=%d", invalid.Code)
	}
}

func TestConfigEditorOmitsFavoritesSyncSettings(t *testing.T) {
	s := &Server{Settings: config.Settings{}, settingsPath: filepath.Join(t.TempDir(), "settings.toml"), editKey: []byte("test-only-etag-key")}
	get := httptest.NewRecorder()
	s.configEdit(get, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
	var shape map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &shape); err != nil {
		t.Fatal(err)
	}
	fields := shape["fields"].(map[string]any)
	for _, key := range []string{"favorites_sync_interval_seconds", "favorites_sync_archive", "favorites_sync_categories"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("retired setting %q is still exposed", key)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHomeEditorGETPreservesOmittedAndZeroCount(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.toml")
	content := `[[section]]
kind="publication"
type="preset"
query="latest"
title="Omitted"

[[section]]
kind="publication"
type="preset"
query="popular"
title="Zero"
count=0
`
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{Settings: config.Settings{HomeConfigPath: p}, editKey: []byte("test-only-etag-key")}
	rr := httptest.NewRecorder()
	s.homeEdit(rr, httptest.NewRequest(http.MethodGet, "/api/home/edit", nil))
	if rr.Code != 200 {
		t.Fatalf("GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	sections := body["sections"].([]any)
	if sections[0].(map[string]any)["count"] != nil {
		t.Fatalf("omitted count was not null: %#v", sections[0])
	}
	if sections[1].(map[string]any)["count"] != float64(0) {
		t.Fatalf("explicit zero was lost: %#v", sections[1])
	}
	firstTag := rr.Header().Get("ETag")
	if err := os.WriteFile(p, append([]byte(content), []byte("\n# external comment\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	next := httptest.NewRecorder()
	s.homeEdit(next, httptest.NewRequest(http.MethodGet, "/api/home/edit", nil))
	if next.Header().Get("ETag") == firstTag {
		t.Fatal("comment-only external edit did not change ETag")
	}
	stale := httptest.NewRequest(http.MethodPut, "/api/home/edit", strings.NewReader(`{"sections":[]}`))
	stale.Header.Set("Content-Type", "application/json")
	stale.Header.Set("If-Match", firstTag)
	conflict := httptest.NewRecorder()
	s.homeEdit(conflict, stale)
	if conflict.Code != 412 {
		t.Fatalf("stale home write status=%d", conflict.Code)
	}
}

func TestConfigEditorETagTracksCredentialRotationWithoutExposure(t *testing.T) {
	t.Setenv("IPB_MEMBER_ID", "")
	_ = os.Unsetenv("IPB_MEMBER_ID")
	t.Setenv("IPB_PASS_HASH", "")
	_ = os.Unsetenv("IPB_PASS_HASH")
	p := filepath.Join(t.TempDir(), "settings.toml")
	first := []byte("IPB_MEMBER_ID=\"member-secret-one\"\nIPB_PASS_HASH=\"hash-secret-one\"\n")
	if err := os.WriteFile(p, first, 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{Settings: config.Settings{}, settingsPath: p, editKey: []byte("test-only-etag-key")}
	a := httptest.NewRecorder()
	s.configEdit(a, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
	if strings.Contains(a.Body.String(), "member-secret-one") || strings.Contains(a.Body.String(), "hash-secret-one") {
		t.Fatal("credential value exposed")
	}
	second := []byte("IPB_MEMBER_ID=\"member-secret-two\"\nIPB_PASS_HASH=\"hash-secret-two\"\n")
	if err := os.WriteFile(p, second, 0600); err != nil {
		t.Fatal(err)
	}
	b := httptest.NewRecorder()
	s.configEdit(b, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
	if b.Header().Get("ETag") == a.Header().Get("ETag") {
		t.Fatal("credential rotation did not change ETag")
	}
	if strings.Contains(b.Body.String(), "member-secret-two") || strings.Contains(b.Body.String(), "hash-secret-two") {
		t.Fatal("rotated credential value exposed")
	}
}

func TestAdminEditAuthIgnoresExemptions(t *testing.T) {
	paths := []struct {
		path, method string
	}{
		{"/api/config/edit", http.MethodGet},
		{"/api/config/edit", http.MethodPatch},
		{"/api/home/edit", http.MethodGet},
		{"/api/home/edit", http.MethodPut},
	}
	for _, route := range paths {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			t.Run("authentication disabled permits anonymous access", func(t *testing.T) {
				s := &Server{Settings: config.Settings{AuthExemptPaths: []string{route.path}, AuthExemptPrefixes: []string{"/api/"}}}
				h := s.basicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, httptest.NewRequest(route.method, route.path, nil))
				if rr.Code != 204 {
					t.Fatalf("anonymous status=%d want 204", rr.Code)
				}
			})
			t.Run("authentication enabled requires correct credentials despite exemptions", func(t *testing.T) {
				s := &Server{Settings: config.Settings{AuthUsername: "u", AuthPassword: "p", AuthExemptPaths: []string{route.path}, AuthExemptPrefixes: []string{"/api/"}}}
				h := s.basicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
				for _, creds := range []struct {
					name, username, password string
					want                     int
				}{{"missing", "", "", 401}, {"wrong", "u", "bad", 401}, {"valid", "u", "p", 204}} {
					t.Run(creds.name, func(t *testing.T) {
						req := httptest.NewRequest(route.method, route.path, nil)
						if creds.username != "" {
							req.SetBasicAuth(creds.username, creds.password)
						}
						rr := httptest.NewRecorder()
						h.ServeHTTP(rr, req)
						if rr.Code != creds.want {
							t.Fatalf("status=%d want %d", rr.Code, creds.want)
						}
					})
				}
			})
		})
	}
}

func TestEditorWritesRejectCrossOriginRequests(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler func(*Server, http.ResponseWriter, *http.Request)
		method  string
		body    string
	}{
		{"config", (*Server).configEdit, http.MethodPatch, `{"eh_site":"exhentai"}`},
		{"home", (*Server).homeEdit, http.MethodPut, `{"sections":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EH_SITE", "")
			_ = os.Unsetenv("EH_SITE")
			t.Setenv("OPDS_ACQ_MODE", "")
			_ = os.Unsetenv("OPDS_ACQ_MODE")
			dir := t.TempDir()
			s := &Server{Settings: config.Settings{HomeConfigPath: filepath.Join(dir, "home.toml")}, settingsPath: filepath.Join(dir, "settings.toml"), editKey: []byte("test-only-etag-key")}
			h := s.basicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { tc.handler(s, w, r) }))
			get := httptest.NewRecorder()
			getReq := httptest.NewRequest(http.MethodGet, "/api/"+tc.name+"/edit", nil)
			h.ServeHTTP(get, getReq)
			if get.Code != http.StatusOK {
				t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
			}
			save := httptest.NewRequest(tc.method, "/api/"+tc.name+"/edit", strings.NewReader(tc.body))
			save.Host = "reader.example"
			save.Header.Set("Origin", "http://reader.example")
			save.Header.Set("Content-Type", "application/json")
			save.Header.Set("If-Match", get.Header().Get("ETag"))
			saved := httptest.NewRecorder()
			h.ServeHTTP(saved, save)
			if saved.Code != http.StatusOK {
				t.Fatalf("anonymous same-origin save status=%d body=%s", saved.Code, saved.Body.String())
			}
			if _, err := os.Stat(filepath.Join(dir, map[string]string{"config": "settings.toml", "home": "home.toml"}[tc.name])); err != nil {
				t.Fatalf("anonymous save did not persist file: %v", err)
			}

			get = httptest.NewRecorder()
			getReq = httptest.NewRequest(http.MethodGet, "/api/"+tc.name+"/edit", nil)
			h.ServeHTTP(get, getReq)
			req := httptest.NewRequest(tc.method, "/api/"+tc.name+"/edit", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", get.Header().Get("ETag"))
			req.Header.Set("Origin", "https://attacker.example")
			req.Host = "reader.example"
			resp := httptest.NewRecorder()
			h.ServeHTTP(resp, req)
			if resp.Code != http.StatusForbidden {
				t.Fatalf("cross-origin write status=%d body=%s", resp.Code, resp.Body.String())
			}
		})
	}
}

func TestConfigEditorAcceptsWeakETagFromReverseProxy(t *testing.T) {
	t.Setenv("EH_SITE", "")
	_ = os.Unsetenv("EH_SITE")
	p := filepath.Join(t.TempDir(), "settings.toml")
	s := &Server{Settings: config.Settings{EHSite: "e-hentai"}, settingsPath: p, editKey: []byte("test-only-etag-key")}

	get := httptest.NewRecorder()
	s.configEdit(get, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
	if get.Code != 200 {
		t.Fatalf("GET status=%d", get.Code)
	}
	if get.Header().Get("Cache-Control") != "no-cache, no-store" {
		t.Fatalf("Cache-Control header missing or unexpected: %q", get.Header().Get("Cache-Control"))
	}
	tag := get.Header().Get("ETag")
	if tag == "" {
		t.Fatal("missing ETag")
	}

	// Reverse proxies like Caddy/Nginx/Cloudflare convert strong ETags to weak ETags (e.g. W/"...").
	weakTag := "W/" + tag
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/config/edit", strings.NewReader(`{"eh_site":"exhentai"}`))
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.Header.Set("If-Match", weakTag)
	patchRec := httptest.NewRecorder()
	s.configEdit(patchRec, patchReq)
	if patchRec.Code != 200 {
		t.Fatalf("PATCH with weak ETag status=%d body=%s", patchRec.Code, patchRec.Body.String())
	}
	if patchRec.Header().Get("Cache-Control") != "no-cache, no-store" {
		t.Fatalf("PATCH Cache-Control header missing: %q", patchRec.Header().Get("Cache-Control"))
	}

	// Comma-separated If-Match with whitespace and weak prefix should also match.
	newTag := patchRec.Header().Get("ETag")
	multiReq := httptest.NewRequest(http.MethodPatch, "/api/config/edit", strings.NewReader(`{"public_base_url":"https://example.com"}`))
	multiReq.Header.Set("Content-Type", "application/json")
	multiReq.Header.Set("If-Match", `"stale-tag", W/`+newTag)
	multiRec := httptest.NewRecorder()
	s.configEdit(multiRec, multiReq)
	if multiRec.Code != 200 {
		t.Fatalf("PATCH with multi/weak If-Match status=%d body=%s", multiRec.Code, multiRec.Body.String())
	}

	// Stale weak tag after external modification should still return 412.
	if err := os.WriteFile(p, []byte("EH_SITE=\"exhentai\"\n# external modify\n"), 0600); err != nil {
		t.Fatal(err)
	}
	staleReq := httptest.NewRequest(http.MethodPatch, "/api/config/edit", strings.NewReader(`{"cache_max_gb":10}`))
	staleReq.Header.Set("Content-Type", "application/json")
	staleReq.Header.Set("If-Match", "W/"+newTag)
	staleRec := httptest.NewRecorder()
	s.configEdit(staleRec, staleReq)
	if staleRec.Code != 412 {
		t.Fatalf("stale weak ETag status=%d want 412 body=%s", staleRec.Code, staleRec.Body.String())
	}
}

func TestHomeEditorAcceptsWeakETagFromReverseProxy(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.toml")
	s := &Server{Settings: config.Settings{HomeConfigPath: p}, editKey: []byte("test-only-etag-key")}

	get := httptest.NewRecorder()
	s.homeEdit(get, httptest.NewRequest(http.MethodGet, "/api/home/edit", nil))
	if get.Code != 200 {
		t.Fatalf("GET home status=%d", get.Code)
	}
	if get.Header().Get("Cache-Control") != "no-cache, no-store" {
		t.Fatalf("Cache-Control header missing: %q", get.Header().Get("Cache-Control"))
	}
	tag := get.Header().Get("ETag")

	putReq := httptest.NewRequest(http.MethodPut, "/api/home/edit", strings.NewReader(`{"sections":[]}`))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("If-Match", "W/"+tag)
	putRec := httptest.NewRecorder()
	s.homeEdit(putRec, putReq)
	if putRec.Code != 200 {
		t.Fatalf("PUT with weak ETag status=%d body=%s", putRec.Code, putRec.Body.String())
	}
	if putRec.Header().Get("Cache-Control") != "no-cache, no-store" {
		t.Fatalf("PUT Cache-Control header missing: %q", putRec.Header().Get("Cache-Control"))
	}
}

func TestHomeEditorCanRepairDecodableInvalidMixedLayout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.toml")
	old := []byte(`[[section]]
kind="publication"
group=true
type="preset"
query="popular"
title="Grouped"

[[section]]
kind="publication"
type="preset"
query="latest"
title="Root"
`)
	if err := os.WriteFile(p, old, 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{Settings: config.Settings{HomeConfigPath: p}, editKey: []byte("test-only-etag-key")}
	get := httptest.NewRecorder()
	s.homeEdit(get, httptest.NewRequest(http.MethodGet, "/api/home/edit", nil))
	if get.Code != 200 || !strings.Contains(get.Body.String(), "top-level publications and groups cannot coexist") || !strings.Contains(get.Body.String(), `"sections"`) {
		t.Fatalf("invalid layout not shown for repair: %d %s", get.Code, get.Body.String())
	}
	tag := get.Header().Get("ETag")
	req := httptest.NewRequest(http.MethodPut, "/api/home/edit", strings.NewReader(`{"sections":[{"kind":"publication","group":false,"title":"Root","type":"preset","query":"latest"},{"kind":"navigation","group":false,"type":"preset","query":"popular"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", tag)
	rr := httptest.NewRecorder()
	s.homeEdit(rr, req)
	if rr.Code != 200 {
		t.Fatalf("repair PUT status=%d body=%s", rr.Code, rr.Body.String())
	}
	backup, err := os.ReadFile(p + ".bak")
	if err != nil || string(backup) != string(old) {
		t.Fatalf("repair did not preserve original backup: %v", err)
	}
}

func TestHomeEditorCanOverwriteCorruptFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.toml")
	corrupt := []byte("this is completely invalid toml content [[[ {{{")
	if err := os.WriteFile(p, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{Settings: config.Settings{HomeConfigPath: p}, editKey: []byte("test-only-etag-key")}
	get := httptest.NewRecorder()
	s.homeEdit(get, httptest.NewRequest(http.MethodGet, "/api/home/edit", nil))
	if get.Code != 200 {
		t.Fatalf("expected 200 on corrupt file GET, got %d", get.Code)
	}
	tag := get.Header().Get("ETag")
	req := httptest.NewRequest(http.MethodPut, "/api/home/edit", strings.NewReader(`{"sections":[{"kind":"publication","group":false,"title":"Root","type":"preset","query":"latest"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", tag)
	rr := httptest.NewRecorder()
	s.homeEdit(rr, req)
	if rr.Code != 200 {
		t.Fatalf("expected overwrite PUT to succeed on corrupt file, got %d body=%s", rr.Code, rr.Body.String())
	}
	backup, err := os.ReadFile(p + ".bak")
	if err != nil || string(backup) != string(corrupt) {
		t.Fatalf("corrupt file backup missing or corrupt: %v", err)
	}
}

func TestHomeEditorNavigationGroupWithItems(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.toml")
	s := &Server{Settings: config.Settings{HomeConfigPath: p}, editKey: []byte("test-only-etag-key")}

	get := httptest.NewRecorder()
	s.homeEdit(get, httptest.NewRequest(http.MethodGet, "/api/home/edit", nil))
	tag := get.Header().Get("ETag")

	payload := `{"sections":[{"kind":"navigation","group":true,"title":"Rankings","items":[{"title":"Month","type":"preset","query":"toplist:month"},{"title":"Year","type":"preset","query":"toplist:year"}]}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/home/edit", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", tag)
	rr := httptest.NewRecorder()
	s.homeEdit(rr, req)
	if rr.Code != 200 {
		t.Fatalf("PUT failed: %d %s", rr.Code, rr.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	sections, ok := res["sections"].([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("unexpected sections: %#v", res["sections"])
	}
	sec := sections[0].(map[string]any)
	if sec["title"] != "Rankings" {
		t.Fatalf("expected title Rankings, got %v", sec["title"])
	}
	items, ok := sec["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 items, got %#v", sec["items"])
	}
	it0 := items[0].(map[string]any)
	if it0["title"] != "Month" || it0["query"] != "toplist:month" {
		t.Fatalf("item 0 mismatch: %#v", it0)
	}
}

func TestMultiPageEditorAndOPDSPageEndpoint(t *testing.T) {
	p := filepath.Join(t.TempDir(), "home.toml")
	s := &Server{Settings: config.Settings{HomeConfigPath: p}, editKey: []byte("test-only-etag-key")}

	// 1. Initial GET /api/home/edit has pages and sections
	get := httptest.NewRecorder()
	s.homeEdit(get, httptest.NewRequest(http.MethodGet, "/api/home/edit", nil))
	tag := get.Header().Get("ETag")
	var getRes map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &getRes); err != nil {
		t.Fatal(err)
	}
	pages, ok := getRes["pages"].([]any)
	if !ok || len(pages) == 0 {
		t.Fatalf("expected pages array, got %#v", getRes["pages"])
	}

	// 2. PUT /api/home/edit with multiple pages
	multiPayload := `{
		"pages": [
			{
				"id": "home",
				"title": "首页",
				"sections": [
					{"kind":"navigation","group":false,"title":"同人专区","type":"page","query":"doujin"}
				]
			},
			{
				"id": "doujin",
				"title": "同人专区",
				"sections": [
					{"kind":"navigation","group":false,"title":"返回首页","type":"page","query":"home"}
				]
			}
		]
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/home/edit", strings.NewReader(multiPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", tag)
	rr := httptest.NewRecorder()
	s.homeEdit(rr, req)
	if rr.Code != 200 {
		t.Fatalf("PUT multi-page failed: %d %s", rr.Code, rr.Body.String())
	}

	// 3. GET /api/home returns pages
	apiHomeRec := httptest.NewRecorder()
	s.apiHome(apiHomeRec, httptest.NewRequest(http.MethodGet, "/api/home", nil))
	if apiHomeRec.Code != 200 {
		t.Fatalf("GET /api/home failed: %d", apiHomeRec.Code)
	}
	var apiHomeData map[string]any
	if err := json.Unmarshal(apiHomeRec.Body.Bytes(), &apiHomeData); err != nil {
		t.Fatal(err)
	}
	homePages := apiHomeData["pages"].([]any)
	if len(homePages) != 2 {
		t.Fatalf("expected 2 pages in /api/home, got %d", len(homePages))
	}

	// 4. GET /opds/v2.0/page/doujin
	pageReq := httptest.NewRequest(http.MethodGet, "/opds/v2.0/page/doujin", nil)
	pageReq.SetPathValue("id", "doujin")
	pageRec := httptest.NewRecorder()
	s.pageV20(pageRec, pageReq)
	if pageRec.Code != 200 {
		t.Fatalf("GET /opds/v2.0/page/doujin status=%d %s", pageRec.Code, pageRec.Body.String())
	}
	var pageDoc map[string]any
	if err := json.Unmarshal(pageRec.Body.Bytes(), &pageDoc); err != nil {
		t.Fatal(err)
	}
	meta := pageDoc["metadata"].(map[string]any)
	if meta["title"] != "同人专区" {
		t.Fatalf("expected title 同人专区, got %v", meta["title"])
	}

	// 5. GET /opds/v2.0/page/nonexistent -> 404
	badReq := httptest.NewRequest(http.MethodGet, "/opds/v2.0/page/nonexistent", nil)
	badReq.SetPathValue("id", "nonexistent")
	badRec := httptest.NewRecorder()
	s.pageV20(badRec, badReq)
	if badRec.Code != 404 {
		t.Fatalf("expected 404 for nonexistent page, got %d", badRec.Code)
	}
}

func TestHomeFileExportAndImport(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "home.toml")
	s := &Server{
		Settings: config.Settings{HomeConfigPath: p},
		editKey:  []byte("test-only-etag-key"),
	}

	// 1. Initial GET /api/home/file when file does not exist -> returns default TOML
	getRec := httptest.NewRecorder()
	s.homeFile(getRec, httptest.NewRequest(http.MethodGet, "/api/home/file", nil))
	if getRec.Code != 200 {
		t.Fatalf("GET /api/home/file status=%d %s", getRec.Code, getRec.Body.String())
	}
	if ct := getRec.Header().Get("Content-Type"); !strings.Contains(ct, "application/toml") {
		t.Fatalf("unexpected Content-Type: %q", ct)
	}
	if cd := getRec.Header().Get("Content-Disposition"); !strings.Contains(cd, "filename=\"home.toml\"") {
		t.Fatalf("unexpected Content-Disposition: %q", cd)
	}
	if !strings.Contains(getRec.Body.String(), "id = \"home\"") {
		t.Fatalf("expected default toml to contain home page: %s", getRec.Body.String())
	}

	// 2. PUT /api/home/file with invalid TOML -> 400
	badTomlReq := httptest.NewRequest(http.MethodPut, "/api/home/file", strings.NewReader("broken toml = ["))
	badTomlRec := httptest.NewRecorder()
	s.homeFile(badTomlRec, badTomlReq)
	if badTomlRec.Code != 400 {
		t.Fatalf("expected 400 for bad toml, got %d", badTomlRec.Code)
	}

	// 3. PUT /api/home/file with invalid layout (no home page) -> 400
	noHomeReq := httptest.NewRequest(http.MethodPut, "/api/home/file", strings.NewReader("[[page]]\nid = \"other\"\ntitle = \"Other\"\n"))
	noHomeRec := httptest.NewRecorder()
	s.homeFile(noHomeRec, noHomeReq)
	if noHomeRec.Code != 400 {
		t.Fatalf("expected 400 for layout without home, got %d", noHomeRec.Code)
	}

	// 4. PUT /api/home/file cross-origin -> 403
	validToml := `
[[page]]
id = "home"
title = "Home"
mode = "groups"

[[page.section]]
group = true
kind = "publication"
title = "Popular"
type = "preset"
query = "popular"
count = 15
`
	corsReq := httptest.NewRequest(http.MethodPut, "/api/home/file", strings.NewReader(validToml))
	corsReq.Header.Set("Origin", "http://evil.com")
	corsRec := httptest.NewRecorder()
	s.homeFile(corsRec, corsReq)
	if corsRec.Code != 403 {
		t.Fatalf("expected 403 for cross-origin, got %d", corsRec.Code)
	}

	// 5. PUT /api/home/file with valid TOML -> 200
	putReq := httptest.NewRequest(http.MethodPut, "/api/home/file", strings.NewReader(validToml))
	putRec := httptest.NewRecorder()
	s.homeFile(putRec, putReq)
	if putRec.Code != 200 {
		t.Fatalf("PUT /api/home/file status=%d %s", putRec.Code, putRec.Body.String())
	}

	// 6. GET /api/home/file now returns the updated content
	getRec2 := httptest.NewRecorder()
	s.homeFile(getRec2, httptest.NewRequest(http.MethodGet, "/api/home/file", nil))
	if getRec2.Code != 200 {
		t.Fatalf("GET /api/home/file after put status=%d", getRec2.Code)
	}
	if !strings.Contains(getRec2.Body.String(), "count = 15") {
		t.Fatalf("expected updated content with count = 15, got %s", getRec2.Body.String())
	}

	// 7. Verify .bak file was created on subsequent write
	secondToml := strings.Replace(validToml, "count = 15", "count = 25", 1)
	putReq2 := httptest.NewRequest(http.MethodPut, "/api/home/file", strings.NewReader(secondToml))
	putRec2 := httptest.NewRecorder()
	s.homeFile(putRec2, putReq2)
	if putRec2.Code != 200 {
		t.Fatalf("second PUT status=%d", putRec2.Code)
	}
	bakContent, err := os.ReadFile(p + ".bak")
	if err != nil {
		t.Fatalf("expected .bak file to exist: %v", err)
	}
	if !strings.Contains(string(bakContent), "count = 15") {
		t.Fatalf("expected .bak to contain count = 15, got %s", string(bakContent))
	}
}

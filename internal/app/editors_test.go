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

func TestFavoritesAutoArchiveEditorSavesSettingAndRespectsEnvironmentLock(t *testing.T) {
	t.Setenv("FAVORITES_SYNC_ARCHIVE", "")
	_ = os.Unsetenv("FAVORITES_SYNC_ARCHIVE")
	t.Setenv("FAVORITES_SYNC_CATEGORIES", "")
	_ = os.Unsetenv("FAVORITES_SYNC_CATEGORIES")
	p := filepath.Join(t.TempDir(), "settings.toml")
	s := &Server{Settings: config.Settings{FavoritesSyncArchive: false, FavoritesSyncCategories: []int{1}, FavoritesSyncExcludes: []int{0}}, settingsPath: p, editKey: []byte("test-only-etag-key")}
	get := httptest.NewRecorder()
	s.configEdit(get, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
	var shape map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &shape); err != nil {
		t.Fatal(err)
	}
	field := shape["fields"].(map[string]any)["favorites_sync_archive"].(map[string]any)
	if field["active"] != false || field["locked"] != false {
		t.Fatalf("unexpected initial setting field: %#v", field)
	}
	scopeField := shape["fields"].(map[string]any)["favorites_sync_categories"].(map[string]any)
	if scopeField["active"] != "1,-0" || scopeField["locked"] != false {
		t.Fatalf("unexpected initial scope field: %#v", scopeField)
	}
	patch := httptest.NewRequest(http.MethodPatch, "/api/config/edit", strings.NewReader(`{"favorites_sync_archive":true,"favorites_sync_categories":"2,-5"}`))
	patch.Header.Set("Content-Type", "application/json")
	patch.Header.Set("If-Match", get.Header().Get("ETag"))
	ok := httptest.NewRecorder()
	s.configEdit(ok, patch)
	if ok.Code != 200 {
		t.Fatalf("PATCH status=%d body=%s", ok.Code, ok.Body.String())
	}
	restarted := config.LoadWithConfigPath(p)
	if !restarted.FavoritesSyncArchive || len(restarted.FavoritesSyncCategories) != 1 || restarted.FavoritesSyncCategories[0] != 2 || len(restarted.FavoritesSyncExcludes) != 1 || restarted.FavoritesSyncExcludes[0] != 5 {
		t.Fatalf("saved favorites settings were not loaded after restart: %+v", restarted)
	}

	t.Setenv("FAVORITES_SYNC_ARCHIVE", "true")
	s.Settings.FavoritesSyncArchive = true
	locked := httptest.NewRecorder()
	s.configEdit(locked, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
	if err := json.Unmarshal(locked.Body.Bytes(), &shape); err != nil {
		t.Fatal(err)
	}
	field = shape["fields"].(map[string]any)["favorites_sync_archive"].(map[string]any)
	if field["locked"] != true || field["source"] != "env" {
		t.Fatalf("environment override did not lock setting: %#v", field)
	}
	t.Setenv("FAVORITES_SYNC_CATEGORIES", "3")
	locked = httptest.NewRecorder()
	s.configEdit(locked, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
	if err := json.Unmarshal(locked.Body.Bytes(), &shape); err != nil {
		t.Fatal(err)
	}
	scopeField = shape["fields"].(map[string]any)["favorites_sync_categories"].(map[string]any)
	if scopeField["locked"] != true || scopeField["source"] != "env" {
		t.Fatalf("environment override did not lock favorites scope: %#v", scopeField)
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
	stale := httptest.NewRequest(http.MethodPut, "/api/home/edit", strings.NewReader(`{"groups":[],"sections":[]}`))
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
	for _, tc := range []struct {
		name string
		cfg  config.Settings
		want int
	}{{"disabled", config.Settings{AuthExemptPaths: []string{"/api/config/edit"}}, 403}, {"enabled and exempt", config.Settings{AuthUsername: "u", AuthPassword: "p", AuthExemptPaths: []string{"/api/config/edit"}}, 401}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Settings: tc.cfg}
			h := s.basicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/config/edit", nil))
			if rr.Code != tc.want {
				t.Fatalf("status=%d want %d", rr.Code, tc.want)
			}
		})
	}
}

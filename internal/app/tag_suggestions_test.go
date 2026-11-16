package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

func newTagSuggestionTestServer(t *testing.T, enabled bool) *Server {
	t.Helper()
	settings := config.Settings{TagTranslationEnabled: enabled, TagTranslationState: filepath.Join(t.TempDir(), "translation.json")}
	state, err := json.Marshal(map[string]any{
		"namespaces": map[string]string{"female": "女性"},
		"abbrs":      map[string]string{"f": "female"},
		"tags":       map[string]string{"female:catgirl": "猫娘"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.TagTranslationState, state, 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{Settings: settings, Service: eh.NewService(settings, nil), mux: http.NewServeMux()}
	server.register()
	t.Cleanup(func() { _ = server.Service.Close() })
	return server
}

func TestTagSuggestionsEndpointReturnsTranslatedAndNegativeMatches(t *testing.T) {
	server := newTagSuggestionTestServer(t, true)
	for _, query := range []string{"female:cat", "-女性:猫"} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/tags/suggestions?q="+url.QueryEscape(query), nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status for %q=%d body=%s", query, recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("cache-control=%q, want no-store", recorder.Header().Get("Cache-Control"))
		}
		var payload struct {
			Suggestions []eh.TagSuggestion `json:"suggestions"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		want := "female:catgirl"
		if strings.HasPrefix(query, "-") {
			want = "-" + want
		}
		if len(payload.Suggestions) != 1 || payload.Suggestions[0].Query != want || payload.Suggestions[0].Display != "女性:猫娘 (female:catgirl)" {
			t.Fatalf("suggestions for %q = %+v", query, payload.Suggestions)
		}
	}
}

func TestTagSuggestionsEndpointDisabledAndEmptyQueries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		query   string
	}{
		{name: "disabled", enabled: false, query: "cat"},
		{name: "empty", enabled: true, query: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newTagSuggestionTestServer(t, tc.enabled)
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/tags/suggestions?q="+url.QueryEscape(tc.query), nil))
			var payload struct {
				Suggestions []eh.TagSuggestion `json:"suggestions"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusOK || payload.Suggestions == nil || len(payload.Suggestions) != 0 {
				t.Fatalf("status=%d suggestions=%+v body=%s", recorder.Code, payload.Suggestions, recorder.Body.String())
			}
		})
	}
}

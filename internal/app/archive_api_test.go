package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestArchiveListCoverUsesDecimalGalleryID(t *testing.T) {
	base := t.TempDir()
	settings := loadTestSettings(base)
	const gid = 1234567
	const token = "abcdef1234"
	if err := os.WriteFile(settings.FavoritesSyncState, []byte(`{"known":["1234567:abcdef1234"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := server.Store.Upsert(gid, token, map[string]any{"status": "ready"}); err != nil {
		t.Fatal(err)
	}
	coverBytes := []byte("local cover bytes")
	if err := server.Store.WriteCover(gid, token, coverBytes); err != nil {
		t.Fatal(err)
	}

	listResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/archive", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("archive list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var payload struct {
		Entries []struct {
			CoverURL    string `json:"cover_url"`
			IsFavorited bool   `json:"is_favorited"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Entries) != 1 {
		t.Fatalf("archive list returned %d entries, want 1", len(payload.Entries))
	}
	entry := payload.Entries[0]
	coverResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(coverResponse, httptest.NewRequest(http.MethodGet, entry.CoverURL, nil))
	if coverResponse.Code != http.StatusOK {
		t.Fatalf("cover response status=%d for emitted URL %q body=%q", coverResponse.Code, entry.CoverURL, coverResponse.Body.String())
	}
	if entry.CoverURL != "/api/archive/1234567/abcdef1234/cover" {
		t.Fatalf("cover URL=%q, want decimal gallery ID", entry.CoverURL)
	}
	if !strings.Contains(coverResponse.Header().Get("Content-Type"), "image/jpeg") || string(coverResponse.Body.Bytes()) != string(coverBytes) {
		t.Fatalf("cover response status=%d content-type=%q body=%q", coverResponse.Code, coverResponse.Header().Get("Content-Type"), coverResponse.Body.String())
	}
	if !entry.IsFavorited {
		t.Fatal("archive entry was not matched to its favorites key")
	}
}

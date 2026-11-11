package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pandaopds/internal/eh"
)

type archiveVersionTransport func(*http.Request) (*http.Response, error)

func (f archiveVersionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestArchiveVersionCheckMarksCandidateWithoutBuying(t *testing.T) {
	settings := loadTestSettings(t.TempDir())
	settings.IPBMemberID, settings.IPBPassHash = "1", "test"
	s, err := New(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Store.Upsert(100, "aaaa", map[string]any{"status": "ready"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.WriteMetadata(100, "aaaa", map[string]any{"gid": 100, "token": "aaaa", "title": "Local snapshot"}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	s.Service.Client.HTTP.Transport = archiveVersionTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/":
			body = "<html></html>"
		case "/api.php":
			requests++
			var payload struct {
				Method  string              `json:"method"`
				GIDList [][]json.RawMessage `json:"gidlist"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Method != "gdata" || len(payload.GIDList) != 1 || len(payload.GIDList[0]) != 2 || string(payload.GIDList[0][0]) != "100" || string(payload.GIDList[0][1]) != `"aaaa"` {
				t.Fatalf("unexpected gdata request: %+v", payload)
			}
			body = `{"gmetadata":[{"gid":100,"token":"aaaa","title":"Upstream snapshot","current_gid":"200","current_key":"bbbb"}]}`
		default:
			t.Fatalf("unexpected upstream request: %s %s", req.Method, req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if err := s.checkArchiveVersions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("gdata requests = %d, want 1", requests)
	}
	snapshot, err := s.Store.ReadMetadata(100, "aaaa")
	if err != nil || snapshot["title"] != "Local snapshot" {
		t.Fatalf("scan replaced the archive snapshot: %v, %v", snapshot, err)
	}
	list := httptest.NewRecorder()
	s.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/archive", nil))
	var response struct {
		Entries []struct {
			UpdateAvailable eh.GalleryRef `json:"update_available"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil || list.Code != http.StatusOK || len(response.Entries) != 1 ||
		response.Entries[0].UpdateAvailable != (eh.GalleryRef{GID: 200, Token: "bbbb"}) {
		t.Fatalf("archive list status=%d body=%s", list.Code, list.Body)
	}
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"gid":200,"token":"bbbb"}`, http.StatusUnprocessableEntity},
		{`{"gid":300,"token":"cccc","max_gp":0}`, http.StatusConflict},
	} {
		result := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/archive/100/aaaa/update", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		s.Handler().ServeHTTP(result, req)
		if result.Code != tc.want {
			t.Fatalf("update with %s: status=%d, want %d, body=%s", tc.body, result.Code, tc.want, result.Body)
		}
	}
}

func TestArchiveVersionCheckHonorsRetryAfter(t *testing.T) {
	if got := archiveVersionCheckDelay(time.Second, &eh.UpstreamError{Status: 429, RetryAfter: "30"}); got != 30*time.Second {
		t.Fatalf("retry delay = %s, want 30s", got)
	}
}

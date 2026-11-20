package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
	"pandaopds/internal/storage"
	"pandaopds/internal/subscription"
)

func TestManualDownloadBatchValidatesWholeBatchBeforeQueueing(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w := subscription.NewWorker(db.DB, nil, nil, nil, config.Settings{ArchiveQuality: "original"})
	mux := http.NewServeMux()
	RegisterSubscriptionRoutes(mux, w)
	bad := httptest.NewRequest(http.MethodPost, "/api/download-jobs/batch", strings.NewReader(`{"acknowledged":true,"items":[{"gid":10,"token":"abcdef12"},{"gid":11,"token":"not-hex"}]}`))
	badRes := httptest.NewRecorder()
	mux.ServeHTTP(badRes, bad)
	if badRes.Code != 400 {
		t.Fatalf("invalid batch status=%d body=%s", badRes.Code, badRes.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_jobs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid batch queued %d jobs err=%v", count, err)
	}
	good := httptest.NewRequest(http.MethodPost, "/api/download-jobs/batch", strings.NewReader(`{"acknowledged":true,"items":[{"gid":10,"token":"abcdef12","quality":"res"},{"gid":11,"token":"fedcba12","quality":"original","force":true}]}`))
	goodRes := httptest.NewRecorder()
	mux.ServeHTTP(goodRes, good)
	if goodRes.Code != http.StatusAccepted {
		t.Fatalf("batch status=%d body=%s", goodRes.Code, goodRes.Body.String())
	}
	var response struct {
		Items []struct {
			Status string `json:"status"`
			JobID  string `json:"job_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(goodRes.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 2 || response.Items[0].Status != "queued" || response.Items[0].JobID == "" || response.Items[1].Status != "queued" || response.Items[1].JobID == "" {
		t.Fatalf("batch response=%s", goodRes.Body.String())
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_job_sources WHERE source='manual'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("manual sources=%d err=%v", count, err)
	}
}

func TestSubscriptionCleanupAPIRequiresAcknowledgmentAndRetainsMember(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "cleanup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := subscription.New(db.DB)
	c, err := store.Create(context.Background(), subscription.Config{Name: "cleanup", Source: "search", Query: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES('cleanup-run',?,'2026-01-01T00:00:00Z','running',1)`, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AddPage(context.Background(), c.ID, "cleanup-run", 1, 1, "", false, false, []eh.GalleryListItem{{GID: 55, Token: "abcdef12", Title: "read"}}); err != nil {
		t.Fatal(err)
	}
	inspected := true
	if err = store.MarkItems(context.Background(), []string{c.ID}, 55, "abcdef12", &inspected, nil); err != nil {
		t.Fatal(err)
	}
	w := subscription.NewWorker(db.DB, nil, nil, nil, config.Settings{})
	mux := http.NewServeMux()
	RegisterSubscriptionRoutes(mux, w)
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/subscriptions/"+c.ID+"/cleanup", strings.NewReader(body))
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, r)
		return out
	}
	if got := request(`{"mode":"inspected"}`); got.Code != 400 {
		t.Fatalf("missing acknowledgment status=%d", got.Code)
	}
	result := request(`{"mode":"inspected","acknowledged":true}`)
	if result.Code != 200 || !strings.Contains(result.Body.String(), `"removed":1`) {
		t.Fatalf("cleanup status=%d body=%s", result.Code, result.Body.String())
	}
	var purged int
	if err = db.QueryRow(`SELECT purged FROM subscription_items WHERE subscription_id=?`, c.ID).Scan(&purged); err != nil || purged != 1 {
		t.Fatalf("dedup tombstone=%d err=%v", purged, err)
	}
}

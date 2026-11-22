package app

import (
	"context"
	"encoding/json"
	"io"
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

type subscriptionCoverTransport func(*http.Request) (*http.Response, error)

func (f subscriptionCoverTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSubscriptionCoverEndpointFetchesAndCachesUpstreamThumb(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "cover.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,title,cover_url,created_at,updated_at) VALUES('eh',99,'abcdef12','{"title":"Cover test"}','Cover test','https://ehgt.org/t/99.jpg','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,title,created_at,updated_at) VALUES('eh',100,'fedcba12','{"title":"Metadata cover test","cover_url":"https://ehgt.org/t/100.jpg"}','Metadata cover test','now','now')`); err != nil {
		t.Fatal(err)
	}
	requests := 0
	service := eh.NewService(config.Settings{ImageProxyHosts: []string{"ehgt.org"}}, subscriptionCoverTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("\xff\xd8\xffcover")), Request: r}, nil
	}))
	w := subscription.NewWorker(db.DB, service, nil, nil, config.Settings{ImageProxyHosts: []string{"ehgt.org"}})
	mux := http.NewServeMux()
	RegisterSubscriptionRoutes(mux, w)
	for _, path := range []string{"/api/publications/eh/99/abcdef12/cover", "/api/publications/eh/100/fedcba12/cover"} {
		for i := 0; i < 2; i++ {
			out := httptest.NewRecorder()
			mux.ServeHTTP(out, httptest.NewRequest(http.MethodGet, path, nil))
			if out.Code != 200 || out.Body.String() != "\xff\xd8\xffcover" {
				t.Fatalf("cover request %s attempt %d status=%d body=%q", path, i+1, out.Code, out.Body.String())
			}
		}
	}
	if requests != 2 {
		t.Fatalf("upstream cover fetches=%d, want one fetch per publication followed by SQLite cache hits", requests)
	}
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM covers WHERE publication_id=(SELECT id FROM publications WHERE gid=99)`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("cached cover rows=%d err=%v", n, err)
	}
}

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

func TestNormalizeSubscriptionUsesSinglePageForSearchAndFavoritePresets(t *testing.T) {
	for _, source := range []string{"search", "preset"} {
		c := subscription.Config{Source: source, Query: map[string]any{"max_pages": float64(10)}, DownloadRules: map[string]any{}}
		if err := normalizeSubscription(&c, config.Settings{}); err != nil {
			t.Fatalf("normalize %s: %v", source, err)
		}
		if got := c.Query["max_pages"]; got != 1 {
			t.Errorf("%s max_pages=%v, want 1", source, got)
		}
	}
	fav := subscription.Config{Source: "preset", Query: map[string]any{"preset": "favorites", "max_pages": float64(10)}, DownloadRules: map[string]any{}}
	if err := normalizeSubscription(&fav, config.Settings{}); err != nil {
		t.Fatal(err)
	}
	if got := fav.Query["max_pages"]; got != 1 {
		t.Fatalf("favorites preset max_pages=%v, want 1", got)
	}
}

func TestNormalizeSubscriptionSupportsExpandedPresets(t *testing.T) {
	validPresets := []string{
		"latest",
		"popular",
		"watched",
		"favorites",
		"favorites:0",
		"favorites:5",
		"favorites:9",
		"toplist:yesterday",
		"toplist:month",
		"toplist:year",
		"toplist:alltime",
	}
	for _, p := range validPresets {
		c := subscription.Config{Source: "preset", Query: map[string]any{"preset": p}}
		if err := normalizeSubscription(&c, config.Settings{}); err != nil {
			t.Errorf("expected preset %q to be valid, got: %v", p, err)
		}
	}
	invalidPresets := []string{"invalid", "toplist:unknown", "favorites:10", "page:home"}
	for _, p := range invalidPresets {
		c := subscription.Config{Source: "preset", Query: map[string]any{"preset": p}}
		if err := normalizeSubscription(&c, config.Settings{}); err == nil {
			t.Errorf("expected preset %q to be rejected, but it succeeded", p)
		}
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

func TestBatchInspectAPIAndReadingViews(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "batch_inspect.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := subscription.New(db.DB)
	c, err := store.Create(context.Background(), subscription.Config{Name: "sub", Source: "preset", Query: map[string]any{"preset": "latest"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES('run-1',?,'2026-01-01T00:00:00Z','running',1)`, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AddPage(context.Background(), c.ID, "run-1", 1, 1, "", false, false, []eh.GalleryListItem{
		{GID: 201, Token: "tok201", Title: "Gallery 201"},
		{GID: 202, Token: "tok202", Title: "Gallery 202"},
	}); err != nil {
		t.Fatal(err)
	}

	// Insert progression for 202 (in progress, reading)
	_, err = db.Exec(`INSERT INTO progressions(target_type,target_id,gid,token,modified,device_json,progression,written) VALUES('eh','202/tok202',202,'tok202','2026-01-01T00:00:00Z','{}',0.5,1000)`)
	if err != nil {
		t.Fatal(err)
	}

	w := subscription.NewWorker(db.DB, nil, nil, nil, config.Settings{})
	mux := http.NewServeMux()
	RegisterSubscriptionRoutes(mux, w)

	// Test GET items?view=new (both uninspected)
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/"+c.ID+"/items?view=new", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("view=new code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resNew struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resNew); err != nil {
		t.Fatal(err)
	}
	if resNew.Total != 2 {
		t.Fatalf("expected 2 items in view=new, got %d", resNew.Total)
	}

	// Test GET items?view=reading (only 202 with 0 < progression < 0.95)
	req = httptest.NewRequest(http.MethodGet, "/api/subscriptions/"+c.ID+"/items?view=reading", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("view=reading code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resReading struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resReading); err != nil {
		t.Fatal(err)
	}
	if resReading.Total != 1 || int(resReading.Items[0]["gid"].(float64)) != 202 {
		t.Fatalf("expected 1 item with gid 202 in view=reading, got %+v", resReading.Items)
	}
	if prog, ok := resReading.Items[0]["progression"].(float64); !ok || prog != 0.5 {
		t.Fatalf("expected progression 0.5, got %v", resReading.Items[0]["progression"])
	}

	// Call POST /api/subscription-items/batch-inspect for 201
	batchBody := `{"items":[{"gid":201,"token":"tok201","subscription_ids":["` + c.ID + `"]}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/subscription-items/batch-inspect", strings.NewReader(batchBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("batch-inspect code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resBatch struct {
		OK    bool  `json:"ok"`
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resBatch); err != nil {
		t.Fatal(err)
	}
	if !resBatch.OK || resBatch.Count != 1 {
		t.Fatalf("expected count 1, got %+v", resBatch)
	}

	// Check GET items?view=new again (now only 202)
	req = httptest.NewRequest(http.MethodGet, "/api/subscriptions/"+c.ID+"/items?view=new", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("view=new code=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resNew); err != nil {
		t.Fatal(err)
	}
	if resNew.Total != 1 || int(resNew.Items[0]["gid"].(float64)) != 202 {
		t.Fatalf("expected 1 item with gid 202 in view=new after inspect, got %+v", resNew.Items)
	}
}

package subscription

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/document"
	"pandaopds/internal/eh"
	"pandaopds/internal/storage"
)

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline test transport")
}

func workerTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedQueuedJob(t *testing.T, db *storage.DB, id, subscriptionID, sourceKey string, revision, favcat int, source string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO subscriptions(id,name,source,query_json,interval_seconds,enabled,auto_download,download_rules_json,revision,baseline_complete,created_at,updated_at) VALUES(?,?,?,'{}',0,1,1,'{}',?,1,?,?)`, subscriptionID, subscriptionID, source, revision, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO publications(id,provider,gid,token,metadata_json,created_at,updated_at) VALUES(1,'eh',123,'abcdef','{}',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	var fav any
	if favcat >= 0 {
		fav = favcat
	}
	_, err = db.Exec(`INSERT INTO subscription_items(subscription_id,publication_id,first_seen_at,last_seen_at,first_seen_revision,baseline_item,pending_auto,favcat) VALUES(?,1,?,?,?,0,1,?)`, subscriptionID, now, now, revision, fav)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,automatic,created_at,updated_at) VALUES(?,'eh',123,'abcdef','original','queued','queued',1,?,?)`, id, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO download_job_sources(job_id,subscription_id,source,source_key) VALUES(?,?,?,?)`, id, subscriptionID, "subscription", sourceKey)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStartMarksInterruptedPaidJobsNeedsReview(t *testing.T) {
	db := workerTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,created_at,updated_at) VALUES('paid','eh',1,'token','original','running','downloading',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{})
	w.Start()
	var status string
	if err = db.QueryRow(`SELECT status FROM download_jobs WHERE id='paid'`).Scan(&status); err != nil || status != "needs_review" {
		t.Fatalf("restart status = %q, err=%v; want needs_review", status, err)
	}
	w.Stop()
}

func TestRunPersistsReservationBeforeReturning(t *testing.T) {
	db := workerTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO subscriptions(id,name,source,query_json,interval_seconds,enabled,auto_download,download_rules_json,revision,baseline_complete,created_at,updated_at) VALUES('sub','s','search','{}',0,1,0,'{}',1,0,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Settings{TimeoutSeconds: 1, Retries: 0}
	service := eh.NewService(cfg, offlineTransport{})
	t.Cleanup(func() { _ = service.Close() })
	w := NewWorker(db.DB, service, nil, nil, cfg)
	w.started = true
	if err = w.Run("sub"); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = db.QueryRow(`SELECT status FROM subscription_runs WHERE subscription_id='sub'`).Scan(&state); err != nil || state != "running" {
		t.Fatalf("run reservation = %q, err=%v; want running before return", state, err)
	}
	w.Stop()
}

func TestQueuedSourceAuthorizationUsesRevisionGalleryCategoryAndManualRetention(t *testing.T) {
	db := workerTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO subscriptions(id,name,source,query_json,interval_seconds,enabled,auto_download,download_rules_json,revision,baseline_complete,created_at,updated_at) VALUES('fav','favorites','preset','{"preset":"favorites","categories":[1],"category_mask":1019}',0,1,1,'{"quality":"original"}',3,1,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO publications(id,provider,gid,token,metadata_json,category,created_at,updated_at) VALUES(1,'eh',123,'abcdef','{"category":"Manga"}','Manga',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO subscription_items(subscription_id,publication_id,first_seen_at,last_seen_at,first_seen_revision,baseline_item,favcat) VALUES('fav',1,?,?,3,0,2)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,created_at,updated_at) VALUES('job','eh',123,'abcdef','original','queued','queued',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO download_job_sources(job_id,subscription_id,source,source_key) VALUES('job','fav','subscription','subscription:fav:r3')`)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{Facets: []config.Facet{{Name: "Manga", Mask: 1019}, {Name: "Doujinshi", Mask: 1021}}})
	validate := func() (bool, bool, string, error) {
		tx, err := db.Begin()
		if err != nil {
			return false, false, "", err
		}
		defer tx.Rollback()
		return w.validateQueuedSources(context.Background(), tx, downloadJob{id: "job", gid: 123, token: "abcdef", quality: "original"})
	}
	manual, valid, selected, err := validate()
	if err != nil || manual || !valid || selected != "original" {
		t.Fatalf("matching gallery category authorization: manual=%v valid=%v quality=%v err=%v", manual, valid, selected, err)
	}
	if _, err = db.Exec(`UPDATE publications SET category='Doujinshi' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	manual, valid, _, err = validate()
	if err != nil || manual || valid {
		t.Fatalf("excluded gallery category authorization: manual=%v valid=%v err=%v; want revoked", manual, valid, err)
	}
	if _, err = db.Exec(`INSERT INTO download_job_sources(job_id,subscription_id,source,source_key) VALUES('job',NULL,'manual','manual')`); err != nil {
		t.Fatal(err)
	}
	manual, valid, _, err = validate()
	if err != nil || !manual || valid {
		t.Fatalf("manual source retention: manual=%v valid=%v err=%v", manual, valid, err)
	}
}

func TestQualityUpgradeDirection(t *testing.T) {
	if !qualitySatisfies("res", "original") || qualitySatisfies("original", "res") || qualitySatisfies("unknown", "original") || qualityHigher("res", "original") || !qualityHigher("original", "res") {
		t.Fatal("quality comparisons do not preserve original > resampled semantics")
	}
}

func TestCancelSubscriptionKeepsSharedManualSource(t *testing.T) {
	db := workerTestDB(t)
	seedQueuedJob(t, db, "shared", "sub-cancel", "subscription:sub-cancel:r1", 1, 2, "preset")
	if _, err := db.Exec(`INSERT INTO download_job_sources(job_id,subscription_id,source,source_key,requested_quality) VALUES('shared',NULL,'manual','manual','res')`); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{})
	if err := w.CancelSubscription(context.Background(), "sub-cancel"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_job_sources WHERE job_id='shared'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("sources after cancel = %d, err=%v; want retained manual source", count, err)
	}
	var status, quality string
	if err := db.QueryRow(`SELECT status,quality FROM download_jobs WHERE id='shared'`).Scan(&status, &quality); err != nil || status != "queued" || quality != "res" {
		t.Fatalf("shared job status/quality = %q/%q, err=%v; want queued at remaining source quality res", status, quality, err)
	}
}

func TestConcurrentDispatchClaimsQueuedJobOnceAndHonorsSuppression(t *testing.T) {
	db := workerTestDB(t)
	seedQueuedJob(t, db, "revoked", "sub-revoked", "subscription:sub-revoked:r1", 1, 2, "preset")
	if _, err := db.Exec(`INSERT INTO download_suppressions(provider,gid,token,created_at) VALUES('eh',123,'abcdef',?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{ArchiveDownloadConcurrency: 1})
	w.started, w.dispatchEnabled = true, true
	var calls sync.WaitGroup
	calls.Add(2)
	go func() { defer calls.Done(); w.DispatchQueued() }()
	go func() { defer calls.Done(); w.DispatchQueued() }()
	calls.Wait()
	var status string
	if err := db.QueryRow(`SELECT status FROM download_jobs WHERE id='revoked'`).Scan(&status); err != nil || status != "canceled" {
		t.Fatalf("suppressed queue status = %q, err=%v; want canceled", status, err)
	}
	w.Stop()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func responseHTML(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: nil}
}
func listFixture(gid int, token, next string) string {
	link := ""
	if next != "" {
		link = `<a id="unext" href="/?next=` + next + `">Next</a>`
	}
	return `<html><body><div class="gl1e"><a href="https://e-hentai.org/g/` + strconv.Itoa(gid) + `/` + token + `/"><div class="glink">Fixture</div></a></div>` + link + `</body></html>`
}
func testSubscriptionConfig(t *testing.T, db *storage.DB, id string) Config {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO subscriptions(id,name,source,query_json,interval_seconds,enabled,auto_download,download_rules_json,revision,baseline_complete,created_at,updated_at) VALUES(?,?, 'search','{"keywords":"fixture","max_pages":2}',0,1,0,'{}',1,0,?,?)`, id, id, now, now)
	if err != nil {
		t.Fatal(err)
	}
	return Config{ID: id, Name: id, Source: "search", Query: map[string]any{"keywords": "fixture", "max_pages": 2}, Revision: 1, Enabled: true}
}
func newFixtureService(t *testing.T, transport http.RoundTripper) *eh.Service {
	t.Helper()
	cfg := config.Settings{CacheDir: t.TempDir(), TimeoutSeconds: 3, Retries: 0, ListCacheTTLSeconds: 1, IPBMemberID: "test", IPBPassHash: "test"}
	s := eh.NewService(cfg, transport)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRunOneCompletesRealBaselineOnlyAfterEndOfResults(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "baseline")
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("f_search") == "" {
			return responseHTML(`<html><body>home</body></html>`), nil
		}
		return responseHTML(listFixture(501, "abc501", "")), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{})
	runID := "baseline-run"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,? ,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.runOne(context.Background(), c.ID, runID, c); err != nil {
		t.Fatal(err)
	}
	var complete, baseline, itemBaseline int
	if err := db.QueryRow(`SELECT complete FROM subscription_runs WHERE id=?`, runID).Scan(&complete); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT baseline_complete FROM subscriptions WHERE id=?`, c.ID).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT baseline_item FROM subscription_items WHERE subscription_id=?`, c.ID).Scan(&itemBaseline); err != nil {
		t.Fatal(err)
	}
	if complete != 1 || baseline != 1 || itemBaseline != 1 {
		t.Fatalf("baseline completion=%d subscription=%d item=%d", complete, baseline, itemBaseline)
	}
}

func TestRunOneSinglePageWithMoreResultsCompletesWindowBaseline(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "partial")
	listRequests := 0
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("f_search") == "" {
			return responseHTML(`<html><body>home</body></html>`), nil
		}
		listRequests++
		return responseHTML(listFixture(502, "abc502", "502-123")), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{})
	runID, now := "partial-run", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.runOne(context.Background(), c.ID, runID, c); err != nil {
		t.Fatal(err)
	}
	var complete, baseline, pages int
	var status, coverage string
	if err := db.QueryRow(`SELECT complete,status,pages,coverage FROM subscription_runs WHERE id=?`, runID).Scan(&complete, &status, &pages, &coverage); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT baseline_complete FROM subscriptions WHERE id=?`, c.ID).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if complete != 1 || baseline != 1 || pages != 1 || status != "complete" || coverage != "window" || listRequests != 1 {
		t.Fatalf("single page complete=%d baseline=%d pages=%d status=%s coverage=%s listRequests=%d", complete, baseline, pages, status, coverage, listRequests)
	}
}

func TestRunOneRevisionChangeRejectsInFlightSearchPage(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "revision")
	firstPage := make(chan struct{})
	release := make(chan struct{})
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("f_search") == "" {
			return responseHTML(`<html><body>home</body></html>`), nil
		}
		close(firstPage)
		<-release
		return responseHTML(listFixture(503, "abc503", "503-123")), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{})
	runID, now := "revision-run", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.runOne(context.Background(), c.ID, runID, c) }()
	select {
	case <-firstPage:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream page did not start")
	}
	if _, err := db.Exec(`UPDATE subscriptions SET revision=2 WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("stale revision page should fail")
	}
	var complete, baseline, items int
	var status string
	if err := db.QueryRow(`SELECT complete,status FROM subscription_runs WHERE id=?`, runID).Scan(&complete, &status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT baseline_complete FROM subscriptions WHERE id=?`, c.ID).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_items WHERE subscription_id=?`, c.ID).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if complete != 0 || baseline != 0 || items != 0 || status != "partial" {
		t.Fatalf("revision run complete=%d baseline=%d items=%d status=%s", complete, baseline, items, status)
	}
}

func TestRunOneCancellationNeverCompletesBaseline(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "cancel")
	entered := make(chan struct{})
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("f_search") == "" {
			return responseHTML(`<html><body>home</body></html>`), nil
		}
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{})
	runID, now := "cancel-run", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.runOne(ctx, c.ID, runID, c) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request did not start")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("expected canceled run error")
	}
	var complete, baseline int
	var status string
	if err := db.QueryRow(`SELECT complete,status FROM subscription_runs WHERE id=?`, runID).Scan(&complete, &status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT baseline_complete FROM subscriptions WHERE id=?`, c.ID).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if complete != 0 || baseline != 0 || status != "interrupted" {
		t.Fatalf("canceled run complete=%d baseline=%d status=%s", complete, baseline, status)
	}
}

func TestTerminalJobDrainsQueuedManualJobsAtConcurrencyLimit(t *testing.T) {
	db := workerTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("manual-%d", i)
		if _, err := db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,created_at,updated_at) VALUES(?,'eh',?,?, 'original','queued','queued',?,?)`, id, 700+i, fmt.Sprintf("abc%d", i), now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO download_job_sources(job_id,subscription_id,source,source_key,requested_quality) VALUES(?,NULL,'manual','manual','original')`, id); err != nil {
			t.Fatal(err)
		}
	}
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{ArchiveDownloadConcurrency: 1})
	w.started, w.dispatchEnabled = true, true
	w.DispatchQueued()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var terminal int
		if err := db.QueryRow(`SELECT COUNT(*) FROM download_jobs WHERE status='failed'`).Scan(&terminal); err != nil {
			t.Fatal(err)
		}
		if terminal == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var queued, failed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_jobs WHERE status='queued'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_jobs WHERE status='failed'`).Scan(&failed); err != nil {
		t.Fatal(err)
	}
	w.Stop()
	if queued != 0 || failed != 3 {
		t.Fatalf("terminal drain failed=%d queued=%d; want 3 terminal and no queued jobs", failed, queued)
	}
}

func TestQueuedClaimDoesNotUseHigherTierThanCurrentSourceAuthorization(t *testing.T) {
	db := workerTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO subscriptions(id,name,source,query_json,interval_seconds,enabled,auto_download,download_rules_json,revision,baseline_complete,created_at,updated_at) VALUES('sub-tier','tier','preset','{"preset":"favorites"}',0,1,1,'{"quality":"res"}',1,1,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO publications(id,provider,gid,token,metadata_json,created_at,updated_at) VALUES(1,'eh',801,'abc801','{}',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO subscription_items(subscription_id,publication_id,first_seen_at,last_seen_at,first_seen_revision,baseline_item,favcat) VALUES('sub-tier',1,?,?,1,0,0)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,created_at,updated_at) VALUES('tier-job','eh',801,'abc801','original','queued','queued',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO download_job_sources(job_id,subscription_id,source,source_key,requested_quality) VALUES('tier-job','sub-tier','subscription','subscription:sub-tier:r1','original')`)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{})
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	manual, valid, quality, err := w.validateQueuedSources(context.Background(), tx, downloadJob{id: "tier-job", gid: 801, token: "abc801", quality: "original"})
	_ = tx.Rollback()
	if err != nil || manual || valid || quality != "" {
		t.Fatalf("stale original authorization manual=%v valid=%v quality=%q err=%v", manual, valid, quality, err)
	}
}

func TestConcurrentDispatchDoesNotClaimSamePaidJobTwice(t *testing.T) {
	db := workerTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,created_at,updated_at) VALUES('once','eh',900,'abc900','original','queued','queued',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO download_job_sources(job_id,subscription_id,source,source_key,requested_quality) VALUES('once',NULL,'manual','manual','original')`); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{ArchiveDownloadConcurrency: 1})
	w.started, w.dispatchEnabled = true, true
	var callers sync.WaitGroup
	callers.Add(2)
	go func() { defer callers.Done(); w.DispatchQueued() }()
	go func() { defer callers.Done(); w.DispatchQueued() }()
	callers.Wait()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var status string
		if err := db.QueryRow(`SELECT status FROM download_jobs WHERE id='once'`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "failed" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM download_jobs WHERE id='once'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	w.Stop()
	if status != "failed" {
		t.Fatalf("job state %q; expected one claim and one terminal archive-unavailable outcome", status)
	}
}

func TestReadyArchiveRegistersOnlyVerifiedZipAndExactQuality(t *testing.T) {
	db := workerTestDB(t)
	root := t.TempDir()
	state := t.TempDir()
	archives, err := archive.NewStoreWithSQLite(root, state, db.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer archives.Close()
	if _, err = archives.Upsert(909, "abc909", map[string]any{"status": "ready", "or": "org", "quality": "Original"}); err != nil {
		t.Fatal(err)
	}
	path, _, err := archives.ArchivePublishPath(909, "abc909")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	entry, err := zw.Create("0001.png")
	if err != nil {
		t.Fatal(err)
	}
	onePixel := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 8, 215, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}
	if _, err = entry.Write(onePixel); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	occupiedPath := filepath.Join(root, "occupied.zip")
	occupiedFile, err := os.Create(occupiedPath)
	if err != nil {
		t.Fatal(err)
	}
	occupiedZip := zip.NewWriter(occupiedFile)
	occupiedEntry, err := occupiedZip.Create("0001.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = occupiedEntry.Write(onePixel); err != nil {
		t.Fatal(err)
	}
	if err = occupiedZip.Close(); err != nil {
		t.Fatal(err)
	}
	if err = occupiedFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = archives.Upsert(909, "abc909", map[string]any{"status": "ready", "quality": "original", "or": "original", "page_count": 1}); err != nil {
		t.Fatal(err)
	}
	docs, err := document.New(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docs.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _, err := docs.List(context.Background(), 1, 10, "available", "")
	if err != nil || len(items) != 2 {
		t.Fatalf("fixture docs=%d err=%v", len(items), err)
	}
	var occupiedID string
	for _, item := range items {
		if item.Path == "occupied.zip" {
			occupiedID = item.ID
		}
	}
	if occupiedID == "" {
		t.Fatal("occupied document missing")
	}
	if err = docs.Associate(context.Background(), occupiedID, 909, "abc909"); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, &archive.Manager{Store: archives}, docs, config.Settings{ArchiveDir: root})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,created_at,updated_at) VALUES('reconcile','eh',909,'abc909','original','needs_review','restart_review',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if err = w.ReviewJob(context.Background(), "reconcile", "reconcile", false); err != nil {
		t.Fatal(err)
	}
	var id, status string
	if err = db.QueryRow(`SELECT document_id,status FROM download_jobs WHERE id='reconcile'`).Scan(&id, &status); err != nil {
		t.Fatal(err)
	}
	if id == "" || status != "succeeded" {
		t.Fatalf("reconciled job document=%q status=%q", id, status)
	}
	registered, err := docs.Get(context.Background(), id)
	if err != nil || registered.Status != "available" {
		t.Fatalf("registered file not readable: %+v err=%v", registered, err)
	}
	var other int
	if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE id<>? AND status<>'missing'`, id).Scan(&other); err != nil || other != 1 {
		t.Fatalf("occupied document changed: count=%d err=%v", other, err)
	}
	newDoc, err := docs.Get(context.Background(), id)
	if err != nil || newDoc.GID != nil {
		t.Fatalf("download must succeed as unlinked new document: %+v err=%v", newDoc, err)
	}
	oldDoc, err := docs.Get(context.Background(), occupiedID)
	if err != nil || oldDoc.GID == nil || *oldDoc.GID != 909 {
		t.Fatalf("existing gallery association changed: %+v err=%v", oldDoc, err)
	}
}

type favoriteFixture struct {
	gid      int
	token    string
	favcat   int
	category string
}

func favoritePageFixture(items []favoriteFixture, next string) string {
	var b strings.Builder
	b.WriteString(`<html><body><div class="nosel"><div class="fp" onclick="popUp('/gallerypopups.php?favcat=2')"><div>2</div><div>Favorite</div><div>Scoped</div></div><div class="fp" onclick="popUp('/gallerypopups.php?favcat=1')"><div>1</div><div>Favorite</div><div>Other</div></div></div>`)
	for _, it := range items {
		name := "Other"
		if it.favcat == 2 {
			name = "Scoped"
		}
		fmt.Fprintf(&b, `<div class="gl1e"><a href="/g/%d/%s/"><div class="glink">Gallery %d</div></a><div class="cn">%s</div><div id="posted_%d" title="%s">2026-11-20</div></div>`, it.gid, it.token, it.gid, it.category, it.gid, name)
	}
	if next != "" {
		fmt.Fprintf(&b, `<a id="unext" href="/favorites.php?next=%s">Next</a>`, next)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}
func TestFavoritesPresetFetchesOnePageAndFiltersCategories(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "favorites-preset")
	if _, err := db.Exec(`UPDATE subscriptions SET source='preset',query_json=? WHERE id=?`, `{"preset":"favorites:2","categories":[1],"category_mask":1019,"max_pages":25}`, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Source = "preset"
	c.Query = map[string]any{"preset": "favorites:2", "categories": []any{float64(1)}, "category_mask": 1019, "max_pages": 25}
	requests := 0
	page := favoritePageFixture([]favoriteFixture{
		{gid: 12, token: "abc012", favcat: 2, category: "Manga"},
		{gid: 13, token: "abc013", favcat: 2, category: "Doujinshi"},
	}, "12-1786365950")
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" {
			return responseHTML(`<html>session</html>`), nil
		}
		if r.URL.Path != "/favorites.php" {
			return nil, fmt.Errorf("unexpected path %s", r.URL.Path)
		}
		requests++
		return responseHTML(page), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{Facets: []config.Facet{{Name: "Doujinshi", Mask: 1021}, {Name: "Manga", Mask: 1019}}})
	runID, now := "favorites-preset-run", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.runOne(context.Background(), c.ID, runID, c); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("favorites pages fetched=%d, want one page per run", requests)
	}
	var kept, filtered, pages int
	var status, coverage string
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND p.gid=12`, c.ID).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND p.gid=13`, c.ID).Scan(&filtered); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status,coverage,pages FROM subscription_runs WHERE id=?`, runID).Scan(&status, &coverage, &pages); err != nil {
		t.Fatal(err)
	}
	if kept != 1 || filtered != 0 || status != "complete" || coverage != "window" || pages != 1 {
		t.Fatalf("favorites filter kept=%d filtered=%d status=%s coverage=%s pages=%d", kept, filtered, status, coverage, pages)
	}
}

func TestNeedsReviewRetryRequiresAcknowledgmentAndNeverQueuesAutomatically(t *testing.T) {
	db := workerTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,created_at,updated_at) VALUES('review-retry','eh',920,'abc920','original','needs_review','restart_review',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{})
	if err := w.ReviewJob(context.Background(), "review-retry", "allow_manual_retry", false); err == nil {
		t.Fatal("manual retry was allowed without acknowledgment")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM download_jobs WHERE id='review-retry'`).Scan(&status); err != nil || status != "needs_review" {
		t.Fatalf("unacknowledged review state=%q err=%v", status, err)
	}
	if err := w.ReviewJob(context.Background(), "review-retry", "allow_manual_retry", true); err != nil {
		t.Fatal(err)
	}
	var stage string
	if err := db.QueryRow(`SELECT status,stage FROM download_jobs WHERE id='review-retry'`).Scan(&status, &stage); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || stage != "user_reviewed" {
		t.Fatalf("reviewed job status/stage=%s/%s", status, stage)
	}
	var queued int
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_jobs WHERE status='queued'`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("manual review unexpectedly queued %d jobs err=%v", queued, err)
	}
}

func TestNeedsReviewReconcileWithoutReadyFileKeepsReviewState(t *testing.T) {
	db := workerTestDB(t)
	root := t.TempDir()
	archives, err := archive.NewStoreWithSQLite(root, t.TempDir(), db.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer archives.Close()
	if _, err = archives.Upsert(921, "abc921", map[string]any{"status": "ready", "or": "org", "quality": "Original"}); err != nil {
		t.Fatal(err)
	}
	docs, err := document.New(db, root)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, &archive.Manager{Store: archives}, docs, config.Settings{ArchiveDir: root})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = db.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,created_at,updated_at) VALUES('no-file','eh',921,'abc921','original','needs_review','restart_review',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if err = w.ReviewJob(context.Background(), "no-file", "reconcile", false); err == nil {
		t.Fatal("missing published ZIP unexpectedly reconciled")
	}
	var status string
	if err = db.QueryRow(`SELECT status FROM download_jobs WHERE id='no-file'`).Scan(&status); err != nil || status != "needs_review" {
		t.Fatalf("missing file review state=%q err=%v", status, err)
	}
}

func TestVersionUpdateIntentIsDurableBeforeManagerSubmission(t *testing.T) {
	db := workerTestDB(t)
	root := t.TempDir()
	archives, err := archive.NewStoreWithSQLite(root, t.TempDir(), db.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer archives.Close()
	if _, err = archives.Upsert(930, "abc930", map[string]any{"status": "ready", "or": "org", "quality": "Original"}); err != nil {
		t.Fatal(err)
	}
	manager := archive.NewManager(config.Settings{}, nil, nil, archives)
	defer manager.Close()
	w := NewWorker(db.DB, nil, manager, nil, config.Settings{})
	w.started = true
	maxGP := 37
	jobID, _, err := w.SubmitManualVersionUpdate(context.Background(), 930, "abc930", 931, "abc931", &maxGP)
	if err == nil {
		t.Fatal("missing IPB should fail before any upstream submission")
	}
	if jobID == "" {
		t.Fatal("durable version update intent was not assigned an id")
	}
	var status, stage, quality, intent string
	if err = db.QueryRow(`SELECT status,stage,quality,intent_json FROM download_jobs WHERE id=?`, jobID).Scan(&status, &stage, &quality, &intent); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || stage != "version_update_start" || quality != "org" || !strings.Contains(intent, `"root_gid":930`) || !strings.Contains(intent, `"target_gid":931`) || !strings.Contains(intent, `"max_gp":37`) {
		t.Fatalf("version intent status=%s stage=%s quality=%s json=%s", status, stage, quality, intent)
	}
	var sources int
	if err = db.QueryRow(`SELECT COUNT(*) FROM download_job_sources WHERE job_id=? AND source='manual' AND requested_quality='org'`, jobID).Scan(&sources); err != nil || sources != 1 {
		t.Fatalf("manual version source count=%d err=%v", sources, err)
	}
}

func TestSearchMaxPagesCompletesAsWindowNotFullRemoteCoverage(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "search-window")
	c.Query["keywords"] = `l:chinese expunged -o:"rough translation$" -o:"extraneous ads$"`
	c.Query["max_pages"] = float64(10) // pre-existing configuration from before the one-page policy
	listRequests := 0
	wantSearch := `l:chinese -o:"rough translation$" -o:"extraneous ads$"`
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("f_search") == "" {
			return responseHTML(`<html>session</html>`), nil
		}
		listRequests++
		searchValues := r.URL.Query()["f_search"]
		if len(searchValues) != 1 || searchValues[0] != wantSearch {
			t.Fatalf("upstream f_search values=%q, want exactly one value %q; full URL=%s", searchValues, wantSearch, r.URL)
		}
		if r.URL.Query().Get("f_sh") != "on" || r.URL.Query().Get("advsearch") != "1" {
			t.Fatalf("advanced query params=%v, want expunged/advsearch filters", r.URL.Query())
		}
		return responseHTML(listFixture(991, "abc991", "991-1786365950")), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{})
	runID, now := "search-window-run", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.runOne(context.Background(), c.ID, runID, c); err != nil {
		t.Fatal(err)
	}
	var status, coverage string
	var complete int
	if err := db.QueryRow(`SELECT status,coverage,complete FROM subscription_runs WHERE id=?`, runID).Scan(&status, &coverage, &complete); err != nil {
		t.Fatal(err)
	}
	if status != "complete" || coverage != "window" || complete != 1 {
		t.Fatalf("search capped coverage status=%s coverage=%s complete=%d", status, coverage, complete)
	}
	if listRequests != 1 {
		t.Fatalf("search upstream list requests=%d, want exactly one despite legacy max_pages=10 and next page", listRequests)
	}
	var pages int
	if err := db.QueryRow(`SELECT pages FROM subscription_runs WHERE id=?`, runID).Scan(&pages); err != nil || pages != 1 {
		t.Fatalf("stored pages=%d err=%v, want 1", pages, err)
	}
}

func TestFavoritesRunWithoutCredentialsMakesNoUpstreamRequest(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "no-fav-auth")
	if _, err := db.Exec(`UPDATE subscriptions SET source='preset',query_json='{"preset":"favorites"}' WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	requests := 0
	service := eh.NewService(config.Settings{CacheDir: t.TempDir(), TimeoutSeconds: 1, Retries: 0}, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected upstream request")
	}))
	defer service.Close()
	w := NewWorker(db.DB, service, nil, nil, config.Settings{})
	w.started = true
	if err := w.Run(c.ID); !errors.Is(err, ErrFavoritesAuthUnavailable) {
		t.Fatalf("Run error=%v, want auth unavailable", err)
	}
	var runs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_runs WHERE subscription_id=?`, c.ID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if requests != 0 || runs != 0 {
		t.Fatalf("without credentials requests=%d runs=%d", requests, runs)
	}
}

func TestFetchSubscriptionPageExpandedPresets(t *testing.T) {
	db := workerTestDB(t)
	var requestedPath, requestedQuery string
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" && r.URL.RawQuery == "" {
			return responseHTML(`<html>session</html>`), nil
		}
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		return responseHTML(listFixture(100, "token100", "")), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{IPBMemberID: "1", IPBPassHash: "a"})

	// 1. popular
	cPop := Config{Source: "preset", Query: map[string]any{"preset": "popular"}}
	if _, err := w.runSourcePage(context.Background(), cPop, ""); err != nil {
		t.Fatalf("popular: %v", err)
	}
	if requestedPath != "/popular" {
		t.Errorf("popular requested path=%q, want /popular", requestedPath)
	}

	// 2. watched
	cWatched := Config{Source: "preset", Query: map[string]any{"preset": "watched"}}
	if _, err := w.runSourcePage(context.Background(), cWatched, ""); err != nil {
		t.Fatalf("watched: %v", err)
	}
	if requestedPath != "/watched" {
		t.Errorf("watched requested path=%q, want /watched", requestedPath)
	}

	// 3. favorites:2
	cFav := Config{Source: "preset", Query: map[string]any{"preset": "favorites:2"}}
	if _, err := w.runSourcePage(context.Background(), cFav, ""); err != nil {
		t.Fatalf("favorites:2: %v", err)
	}
	if requestedPath != "/favorites.php" || !strings.Contains(requestedQuery, "favcat=2") {
		t.Errorf("favorites:2 path=%q query=%q, want /favorites.php with favcat=2", requestedPath, requestedQuery)
	}

	// 4. toplist:month
	cTop := Config{Source: "preset", Query: map[string]any{"preset": "toplist:month"}}
	if _, err := w.runSourcePage(context.Background(), cTop, ""); err != nil {
		t.Fatalf("toplist:month: %v", err)
	}
	if requestedPath != "/toplist.php" || !strings.Contains(requestedQuery, "tl=13") {
		t.Errorf("toplist:month path=%q query=%q, want /toplist.php with tl=13", requestedPath, requestedQuery)
	}
}

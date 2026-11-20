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

func TestQueuedSourceAuthorizationUsesRevisionFavcatAndManualRetention(t *testing.T) {
	db := workerTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO subscriptions(id,name,source,query_json,interval_seconds,enabled,auto_download,download_rules_json,revision,baseline_complete,created_at,updated_at) VALUES('fav','favorites','favorites','{}',0,1,1,'{"categories":[2]}',3,1,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO publications(id,provider,gid,token,metadata_json,created_at,updated_at) VALUES(1,'eh',123,'abcdef','{"category":"Manga"}',?,?)`, now, now)
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
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{})
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	manual, valid, selected, err := w.validateQueuedSources(context.Background(), tx, downloadJob{id: "job", gid: 123, token: "abcdef", quality: "original"})
	_ = tx.Rollback()
	if err != nil || manual || !valid || selected != "original" {
		t.Fatalf("matching favcat authorization: manual=%v valid=%v quality=%v err=%v", manual, valid, selected, err)
	}
	if _, err = db.Exec(`UPDATE subscription_items SET favcat=1 WHERE subscription_id='fav'`); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	manual, valid, selected, err = w.validateQueuedSources(context.Background(), tx, downloadJob{id: "job", gid: 123, token: "abcdef", quality: "original"})
	_ = tx.Rollback()
	if err != nil || manual || valid {
		t.Fatalf("excluded favcat authorization: manual=%v valid=%v err=%v; want revoked", manual, valid, err)
	}
	if _, err = db.Exec(`INSERT INTO download_job_sources(job_id,subscription_id,source,source_key) VALUES('job',NULL,'manual','manual')`); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	manual, valid, selected, err = w.validateQueuedSources(context.Background(), tx, downloadJob{id: "job", gid: 123, token: "abcdef", quality: "original"})
	_ = tx.Rollback()
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
	seedQueuedJob(t, db, "shared", "sub-cancel", "subscription:sub-cancel:r1", 1, 2, "favorites")
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
	seedQueuedJob(t, db, "revoked", "sub-revoked", "subscription:sub-revoked:r1", 1, 2, "favorites")
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
	_, err := db.Exec(`INSERT INTO subscriptions(id,name,source,query_json,interval_seconds,enabled,auto_download,download_rules_json,revision,baseline_complete,created_at,updated_at) VALUES('sub-tier','tier','favorites','{}',0,1,1,'{"quality":"res"}',1,1,?,?)`, now, now)
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
	docs, err := document.New(db, root)
	if err != nil {
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
	if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE id<>? AND status<>'missing'`, id).Scan(&other); err != nil || other != 0 {
		t.Fatalf("single-file registration affected %d unrelated docs err=%v", other, err)
	}
}

func TestCanceledFavoriteSourceIsNotDispatchedFromQueuedIntent(t *testing.T) {
	db := workerTestDB(t)
	seedQueuedJob(t, db, "removed-favorite", "favorite-sub", "subscription:favorite-sub:r1", 1, 2, "favorites")
	if _, err := db.Exec(`UPDATE subscription_items SET remote_member=0 WHERE subscription_id='favorite-sub' AND publication_id=1`); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db.DB, nil, nil, nil, config.Settings{ArchiveDownloadConcurrency: 1})
	w.started, w.dispatchEnabled = true, true
	w.DispatchQueued()
	var status string
	if err := db.QueryRow(`SELECT status FROM download_jobs WHERE id='removed-favorite'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	w.Stop()
	if status != "canceled" {
		t.Fatalf("removed favorite job status=%q; want canceled before any archive request", status)
	}
}

type favoriteFixture struct {
	gid      int
	token    string
	category int
}

func favoritePageFixture(items []favoriteFixture, next string) string {
	var b strings.Builder
	b.WriteString(`<html><body><div class="nosel"><div class="fp" onclick="popUp('/gallerypopups.php?favcat=2')"><div>2</div><div>Favorite</div><div>Scoped</div></div><div class="fp" onclick="popUp('/gallerypopups.php?favcat=1')"><div>1</div><div>Favorite</div><div>Other</div></div></div>`)
	for _, it := range items {
		name := "Other"
		if it.category == 2 {
			name = "Scoped"
		}
		fmt.Fprintf(&b, `<div class="gl1e"><a href="/g/%d/%s/"><div class="glink">Gallery %d</div></a><div id="posted_%d" title="%s">2026-11-20</div></div>`, it.gid, it.token, it.gid, it.gid, name)
	}
	if next != "" {
		fmt.Fprintf(&b, `<a id="unext" href="/favorites.php?next=%s">Next</a>`, next)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}
func seedFavoriteMembership(t *testing.T, db *storage.DB, sub string, gid int, token string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,created_at,updated_at) VALUES('eh',?,?,'{}',?,?)`, gid, token, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_items(subscription_id,publication_id,first_seen_at,last_seen_at,first_seen_revision,baseline_item,favcat) SELECT ?,id,?,?,1,1,2 FROM publications WHERE provider='eh' AND gid=? AND token=?`, sub, now, now, gid, token); err != nil {
		t.Fatal(err)
	}
}
func TestFavoritesKnownThresholdStopsBeforeDeepHistoryAndHonorsScope(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "fav-window")
	if _, err := db.Exec(`UPDATE subscriptions SET source='favorites',query_json='{"favorite_categories":[2]}',auto_download=1,baseline_complete=1 WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Source = "favorites"
	c.Query = map[string]any{"favorite_categories": []any{float64(2)}}
	c.AutoDownload = true
	c.BaselineComplete = true
	seedFavoriteMembership(t, db, c.ID, 10, "aaa010")
	seedFavoriteMembership(t, db, c.ID, 11, "aaa011")
	first := favoritePageFixture([]favoriteFixture{{12, "abc012", 2}, {99, "abc099", 1}}, "12-1786365950")
	second := favoritePageFixture([]favoriteFixture{{10, "aaa010", 2}, {11, "aaa011", 2}, {20, "abc020", 2}}, "20-1786365951")
	requests := 0
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" {
			return responseHTML(`<html>session</html>`), nil
		}
		if r.URL.Path != "/favorites.php" {
			return nil, fmt.Errorf("unexpected path %s", r.URL.Path)
		}
		requests++
		if requests == 1 {
			return responseHTML(first), nil
		}
		if requests == 2 {
			return responseHTML(second), nil
		}
		return nil, errors.New("threshold should stop before deep page")
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{IPBMemberID: "test", IPBPassHash: "test", FavoritesSyncMaxPages: 10, FavoritesSyncMatchThreshold: 2})
	runID, now := "fav-window-run", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.runOne(context.Background(), c.ID, runID, c); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("favorites pages fetched=%d; known threshold should stop at two", requests)
	}
	var status, coverage string
	var complete, pages int
	if err := db.QueryRow(`SELECT status,coverage,complete,pages FROM subscription_runs WHERE id=?`, runID).Scan(&status, &coverage, &complete, &pages); err != nil {
		t.Fatal(err)
	}
	if status != "complete" || coverage != "window" || complete != 1 || pages != 2 {
		t.Fatalf("incremental run status=%s coverage=%s complete=%d pages=%d", status, coverage, complete, pages)
	}
	var scoped, outsider, deep int
	_ = db.QueryRow(`SELECT COUNT(*) FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND p.gid=12`, c.ID).Scan(&scoped)
	_ = db.QueryRow(`SELECT COUNT(*) FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND p.gid=99`, c.ID).Scan(&outsider)
	_ = db.QueryRow(`SELECT COUNT(*) FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND p.gid=20`, c.ID).Scan(&deep)
	if scoped != 1 || outsider != 0 || deep != 0 {
		t.Fatalf("scope/window membership scoped=%d outside=%d deep=%d", scoped, outsider, deep)
	}
}

func TestPartialFavoriteBaselineResumesCursorWithoutAutoDownloadingBaseline(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "fav-baseline")
	if _, err := db.Exec(`UPDATE subscriptions SET source='favorites',query_json='{}',auto_download=1,baseline_complete=0 WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Source = "favorites"
	c.Query = map[string]any{}
	c.AutoDownload = true
	c.BaselineComplete = false
	cursors := []string{}
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" {
			return responseHTML(`<html>session</html>`), nil
		}
		if r.URL.Path != "/favorites.php" {
			return nil, fmt.Errorf("unexpected path %s", r.URL.Path)
		}
		cursor := r.URL.Query().Get("next")
		cursors = append(cursors, cursor)
		if cursor == "" {
			return responseHTML(favoritePageFixture([]favoriteFixture{{1, "abc001", 2}}, "1-1786365950")), nil
		}
		return responseHTML(favoritePageFixture([]favoriteFixture{{2, "abc002", 2}}, "")), nil
	}))
	cfg := config.Settings{IPBMemberID: "test", IPBPassHash: "test", FavoritesSyncMaxPages: 1, FavoritesSyncMatchThreshold: 2}
	w := NewWorker(db.DB, service, nil, nil, cfg)
	w.started = true
	firstID, now := "baseline-first", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, firstID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.runOne(context.Background(), c.ID, firstID, c); err != nil {
		t.Fatal(err)
	}
	var firstStatus, firstCoverage, firstCursor string
	var firstComplete int
	if err := db.QueryRow(`SELECT status,coverage,cursor,complete FROM subscription_runs WHERE id=?`, firstID).Scan(&firstStatus, &firstCoverage, &firstCursor, &firstComplete); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "partial" || firstCoverage != "partial" || firstCursor != "1-1786365950" || firstComplete != 0 {
		t.Fatalf("first segment status=%s coverage=%s cursor=%s complete=%d", firstStatus, firstCoverage, firstCursor, firstComplete)
	}
	var baseline int
	if err := db.QueryRow(`SELECT baseline_complete FROM subscriptions WHERE id=?`, c.ID).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if baseline != 0 {
		t.Fatal("partial first baseline was marked complete")
	}
	if err := w.Run(c.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		_ = db.QueryRow(`SELECT status FROM subscription_runs WHERE subscription_id=? ORDER BY started_at DESC LIMIT 1`, c.ID).Scan(&status)
		if status == "complete" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	w.Stop()
	if len(cursors) != 2 || cursors[0] != "" || cursors[1] != "1-1786365950" {
		t.Fatalf("upstream cursors=%v; expected durable continuation", cursors)
	}
	if err := db.QueryRow(`SELECT baseline_complete FROM subscriptions WHERE id=?`, c.ID).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	var pending, itemBaseline int
	if err := db.QueryRow(`SELECT pending_auto,baseline_item FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND p.gid=2`, c.ID).Scan(&pending, &itemBaseline); err != nil {
		t.Fatal(err)
	}
	var coverage string
	var complete int
	if err := db.QueryRow(`SELECT coverage,complete FROM subscription_runs WHERE subscription_id=? ORDER BY started_at DESC LIMIT 1`, c.ID).Scan(&coverage, &complete); err != nil {
		t.Fatal(err)
	}
	if baseline != 1 || pending != 0 || itemBaseline != 1 || coverage != "window" || complete != 1 {
		t.Fatalf("resumed baseline baseline=%d pending=%d itemBaseline=%d coverage=%s complete=%d", baseline, pending, itemBaseline, coverage, complete)
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

func TestFailedBaselineCursorRestartsAtHeadOnNextAttempt(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "bad-cursor")
	if _, err := db.Exec(`UPDATE subscriptions SET source='favorites',query_json='{}',auto_download=1,baseline_complete=0 WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Source = "favorites"
	c.Query = map[string]any{}
	c.AutoDownload = true
	c.BaselineComplete = false
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,finished_at,status,revision,cursor,pages,complete,coverage) VALUES('bad-cursor-run',?,?,?,'partial',1,'bad-cursor',1,0,'partial')`, c.ID, now, now); err != nil {
		t.Fatal(err)
	}
	cursors := []string{}
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" {
			return responseHTML(`<html>session</html>`), nil
		}
		if r.URL.Path != "/favorites.php" {
			return nil, fmt.Errorf("unexpected path %s", r.URL.Path)
		}
		cursor := r.URL.Query().Get("next")
		cursors = append(cursors, cursor)
		if cursor == "bad-cursor" {
			return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("expired cursor"))}, nil
		}
		return responseHTML(favoritePageFixture([]favoriteFixture{{3, "abc003", 2}}, "")), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{IPBMemberID: "test", IPBPassHash: "test", FavoritesSyncMaxPages: 5, FavoritesSyncMatchThreshold: 2})
	w.started = true
	runUntil := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			var state string
			_ = db.QueryRow(`SELECT status FROM subscription_runs WHERE subscription_id=? ORDER BY started_at DESC LIMIT 1`, c.ID).Scan(&state)
			if state == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("latest run did not reach %s", want)
	}
	if err := w.Run(c.ID); err != nil {
		t.Fatal(err)
	}
	runUntil("failed")
	var pages int
	if err := db.QueryRow(`SELECT pages FROM subscription_runs WHERE subscription_id=? ORDER BY started_at DESC LIMIT 1`, c.ID).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if pages != 0 {
		t.Fatalf("bad cursor unexpectedly fetched %d pages", pages)
	}
	if err := w.Run(c.ID); err != nil {
		t.Fatal(err)
	}
	runUntil("complete")
	w.Stop()
	if len(cursors) != 2 || cursors[0] != "bad-cursor" || cursors[1] != "" {
		t.Fatalf("cursor retry sequence=%v", cursors)
	}
	var pending, baselineItem int
	if err := db.QueryRow(`SELECT pending_auto,baseline_item FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND p.gid=3`, c.ID).Scan(&pending, &baselineItem); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || baselineItem != 1 {
		t.Fatalf("head restart item auto state pending=%d baseline=%d", pending, baselineItem)
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
	if _, err := db.Exec(`UPDATE subscriptions SET source='favorites',query_json='{}' WHERE id=?`, c.ID); err != nil {
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

func TestExpiredFavoriteSessionHTMLCannotReconcileMembership(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "expired-fav-auth")
	if _, err := db.Exec(`UPDATE subscriptions SET source='favorites',query_json='{}',auto_download=1,baseline_complete=1 WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Source = "favorites"
	c.Query = map[string]any{}
	c.AutoDownload = true
	c.BaselineComplete = true
	seedFavoriteMembership(t, db, c.ID, 10, "aaa010")
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" {
			return responseHTML(`<html>session</html>`), nil
		}
		return responseHTML(`<html><body><form id="login_form" action="/login.php"><input type="password" name="pass"></form></body></html>`), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{IPBMemberID: "test", IPBPassHash: "test", FavoritesSyncMaxPages: 5})
	runID, now := "expired-login-run", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.runOne(context.Background(), c.ID, runID, c); !errors.Is(err, eh.ErrAuthenticationRequired) {
		t.Fatalf("run error=%v, want authentication error", err)
	}
	var baseline, remote, complete int
	var status string
	if err := db.QueryRow(`SELECT baseline_complete FROM subscriptions WHERE id=?`, c.ID).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT remote_member FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND p.gid=10`, c.ID).Scan(&remote); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status,complete FROM subscription_runs WHERE id=?`, runID).Scan(&status, &complete); err != nil {
		t.Fatal(err)
	}
	if baseline != 1 || remote != 1 || status != "failed" || complete != 0 {
		t.Fatalf("expired auth baseline=%d remote=%d status=%s complete=%d", baseline, remote, status, complete)
	}
}

func TestUnknownEmptyFavoritePageDoesNotCompleteBaseline(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "unknown-empty-favs")
	if _, err := db.Exec(`UPDATE subscriptions SET source='favorites',query_json='{}',baseline_complete=0 WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Source = "favorites"
	c.Query = map[string]any{}
	c.BaselineComplete = false
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" {
			return responseHTML(`<html>session</html>`), nil
		}
		return responseHTML(`<html><body>not a favorites page</body></html>`), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{IPBMemberID: "test", IPBPassHash: "test", FavoritesSyncMaxPages: 5})
	runID, now := "unknown-empty-run", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.runOne(context.Background(), c.ID, runID, c); !errors.Is(err, eh.ErrUnverifiedFavoritesPage) {
		t.Fatalf("run error=%v, want unverified favorites page", err)
	}
	var baseline, complete int
	var status string
	if err := db.QueryRow(`SELECT baseline_complete FROM subscriptions WHERE id=?`, c.ID).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status,complete FROM subscription_runs WHERE id=?`, runID).Scan(&status, &complete); err != nil {
		t.Fatal(err)
	}
	if baseline != 0 || status != "failed" || complete != 0 {
		t.Fatalf("unverified empty baseline=%d status=%s complete=%d", baseline, status, complete)
	}
}

func TestVerifiedEmptyFavoritesPageCanReconcileMembership(t *testing.T) {
	db := workerTestDB(t)
	c := testSubscriptionConfig(t, db, "verified-empty")
	if _, err := db.Exec(`UPDATE subscriptions SET source='favorites',query_json='{}',auto_download=1,baseline_complete=1 WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Source = "favorites"
	c.Query = map[string]any{}
	c.AutoDownload = true
	c.BaselineComplete = true
	seedFavoriteMembership(t, db, c.ID, 10, "aaa010")
	emptyPage := `<html><body><div class="nosel"><div class="fp" onclick="popUp('/gallerypopups.php?favcat=0')"><div>0</div><div>Favorite</div><div>Common</div></div></div><table class="itg glte"></table></body></html>`
	service := newFixtureService(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/" {
			return responseHTML(`<html>session</html>`), nil
		}
		return responseHTML(emptyPage), nil
	}))
	w := NewWorker(db.DB, service, nil, nil, config.Settings{IPBMemberID: "test", IPBPassHash: "test", FavoritesSyncMaxPages: 5})
	runID, now := "verified-empty-run", time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision) VALUES(?,?,?,'running',1)`, runID, c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.runOne(context.Background(), c.ID, runID, c); err != nil {
		t.Fatal(err)
	}
	var remote, complete int
	var coverage string
	if err := db.QueryRow(`SELECT remote_member FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND p.gid=10`, c.ID).Scan(&remote); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT complete,coverage FROM subscription_runs WHERE id=?`, runID).Scan(&complete, &coverage); err != nil {
		t.Fatal(err)
	}
	if remote != 0 || complete != 1 || coverage != "full" {
		t.Fatalf("verified empty page remote=%d complete=%d coverage=%s", remote, complete, coverage)
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

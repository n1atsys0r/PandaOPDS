package subscription

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/document"
	"pandaopds/internal/eh"
)

var ErrAlreadyRunning = errors.New("subscription is already running")
var ErrFavoritesAuthUnavailable = errors.New("favorites subscription requires IPB cookies")

type Worker struct {
	Store                  *Store
	Service                *eh.Service
	Archives               *archive.Manager
	Documents              *document.Store
	Settings               config.Settings
	db                     *sql.DB
	mu                     sync.Mutex
	active                 map[string]context.CancelFunc
	ctx                    context.Context
	cancel                 context.CancelFunc
	wg                     sync.WaitGroup
	dispatchEnabled        bool
	started                bool
	stopping               bool
	maxConcurrent          int
	subscriptionFetchQueue chan struct{}
}
type downloadJob struct {
	id             string
	gid            int
	token, quality string
	force          bool
	automatic      bool
}

func NewWorker(db *sql.DB, service *eh.Service, archives *archive.Manager, docs *document.Store, cfg config.Settings) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	limit := cfg.ArchiveDownloadConcurrency
	if limit < 1 {
		limit = 1
	}
	return &Worker{Store: New(db), Service: service, Archives: archives, Documents: docs, Settings: cfg, db: db, active: map[string]context.CancelFunc{}, ctx: ctx, cancel: cancel, maxConcurrent: limit, subscriptionFetchQueue: make(chan struct{}, 1)}
}
func (w *Worker) Start() error {
	w.mu.Lock()
	if w.stopping || w.ctx.Err() != nil || w.started {
		w.mu.Unlock()
		return errors.New("subscription worker cannot start")
	}
	// Mark both interrupted paid work and interrupted read-only runs before
	// enabling any scheduler. A later run starts from the beginning and relies
	// on member deduplication, so a partial baseline cannot become new content.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := w.db.Begin()
	if err != nil {
		w.mu.Unlock()
		return fmt.Errorf("begin subscription worker recovery: %w", err)
	}
	_, e1 := tx.Exec(`UPDATE download_jobs SET status='needs_review',stage='restart_review',error='server restarted during archive task; inspect upstream result before retrying',updated_at=? WHERE status='running'`, now)
	_, e2 := tx.Exec(`UPDATE subscription_runs SET status='interrupted',finished_at=?,error='server restarted during subscription run',complete=0,coverage=CASE WHEN pages>0 THEN 'partial' ELSE 'unknown' END WHERE status='running'`, now)
	if e1 != nil || e2 != nil {
		_ = tx.Rollback()
		w.mu.Unlock()
		return fmt.Errorf("recover interrupted subscription work: %w", errors.Join(e1, e2))
	}
	if err = tx.Commit(); err != nil {
		w.mu.Unlock()
		return fmt.Errorf("commit subscription worker recovery: %w", err)
	}
	w.started = true
	w.wg.Add(1)
	w.mu.Unlock()
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		w.scheduleDue()
		for {
			select {
			case <-w.ctx.Done():
				return
			case <-ticker.C:
				w.resumePending()
				w.scheduleDue()
			}
		}
	}()
	return nil
}

func (w *Worker) EnableDispatch() {
	w.mu.Lock()
	if !w.stopping && w.started && w.ctx.Err() == nil {
		w.dispatchEnabled = true
	}
	w.mu.Unlock()
	w.resumePending()
	w.DispatchQueued()
}
func (w *Worker) resumePending() {
	if w.ctx.Err() != nil {
		return
	}
	rows, err := w.db.QueryContext(w.ctx, `SELECT DISTINCT subscription_id FROM subscription_items WHERE pending_auto=1`)
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			_ = rows.Close()
			return
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		_ = rows.Close()
		return
	}
	_ = rows.Close()
	for _, id := range ids {
		if w.ctx.Err() != nil {
			return
		}
		w.DispatchPending(id)
	}
}
func (w *Worker) Stop() {
	w.mu.Lock()
	if w.stopping {
		w.mu.Unlock()
		w.wg.Wait()
		return
	}
	w.stopping = true
	w.dispatchEnabled = false
	w.cancel()
	for _, c := range w.active {
		c()
	}
	w.mu.Unlock()
	w.wg.Wait()
}
func (w *Worker) Run(id string) error {
	cfg, err := w.Store.Get(context.Background(), id)
	if err != nil {
		return err
	}
	if IsFavoritesSubscription(cfg) && !w.Settings.HasIPB() {
		return ErrFavoritesAuthUnavailable
	}
	runID, err := randomID()
	if err != nil {
		return err
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started || w.stopping || w.ctx.Err() != nil {
		return errors.New("subscription worker is not accepting runs")
	}
	if _, ok := w.active[id]; ok {
		return ErrAlreadyRunning
	}
	tx, err := w.db.Begin()
	if err != nil {
		return err
	}
	var currentRevision int
	if err = tx.QueryRow(`SELECT revision FROM subscriptions WHERE id=?`, id).Scan(&currentRevision); err != nil {
		_ = tx.Rollback()
		return err
	}
	if currentRevision != cfg.Revision {
		_ = tx.Rollback()
		return errors.New("subscription changed; retry with current settings")
	}
	var running int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM subscription_runs WHERE subscription_id=? AND status='running'`, id).Scan(&running); err != nil {
		_ = tx.Rollback()
		return err
	}
	if running > 0 {
		_ = tx.Rollback()
		return ErrAlreadyRunning
	}
	resumeCursor := ""
	if _, err = tx.Exec(`INSERT INTO subscription_runs(id,subscription_id,started_at,status,revision,cursor) VALUES(?,?,?,'running',?,?)`, runID, id, started, cfg.Revision, resumeCursor); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(w.ctx)
	w.active[id] = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer func() { w.mu.Lock(); delete(w.active, id); w.mu.Unlock(); cancel() }()
		_ = w.runOne(ctx, id, runID, cfg)
	}()
	return nil
}
func (w *Worker) scheduleDue() {
	subs, e := w.Store.List(w.ctx)
	if e != nil {
		return
	}
	now := time.Now().UTC()
	for _, s := range subs {
		if IsFavoritesSubscription(s) && !w.Settings.HasIPB() {
			continue
		}
		if !s.Enabled || s.IntervalSeconds <= 0 {
			continue
		}
		if s.LastRunAt != nil && now.Sub(*s.LastRunAt) < time.Duration(s.IntervalSeconds)*time.Second {
			continue
		}
		_ = w.Run(s.ID)
	}
}
func (w *Worker) runOne(ctx context.Context, id, runID string, c Config) error {
	var e error
	var pages, count int
	complete, windowEnded := false, false
	baseComplete := c.BaselineComplete
	var cursor string
	if err := w.db.QueryRowContext(ctx, `SELECT cursor FROM subscription_runs WHERE id=?`, runID).Scan(&cursor); err != nil {
		return err
	}
	maxPages := 1
	for e == nil && pages < maxPages {
		if err := ctx.Err(); err != nil {
			e = err
			break
		}
		var page eh.GalleryPageInfo
		page, e = w.fetchSubscriptionPageQueued(ctx, c, cursor, pages+1)
		if e != nil {
			break
		}
		pages++
		cursor = page.NextGID
		var items []eh.GalleryListItem
		items, e = filterSubscriptionCategories(page.Galleries, c.Query, w.Settings)
		if e != nil {
			break
		}
		_, e = w.Store.AddPage(ctx, id, runID, c.Revision, pages, cursor, baseComplete, c.AutoDownload, items)
		if e != nil {
			break
		}
		count += len(items)
		if windowEnded {
			complete = true
			break
		}
		if cursor == "" || len(page.Galleries) == 0 {
			complete = true
			break
		}
		if pages >= maxPages {
			complete = true
			windowEnded = cursor != ""
			break
		}
	}
	if e == nil && pages >= maxPages {
		complete = true
		windowEnded = cursor != ""
	}
	if ctx.Err() != nil {
		e = ctx.Err()
		complete = false
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	status := "partial"
	errText := any(nil)
	if e != nil {
		status = "failed"
		if errors.Is(e, context.Canceled) {
			status = "interrupted"
		}
		errText = e.Error()
	} else if complete {
		status = "complete"
	}
	coverage := "unknown"
	if pages > 0 {
		coverage = "partial"
	}
	if complete && !windowEnded {
		coverage = "full"
	}
	if windowEnded {
		coverage = "window"
	}
	finalCtx, finalCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finalCancel()
	w.mu.Lock()
	if ctx.Err() != nil {
		e = ctx.Err()
		complete = false
		status = "interrupted"
		errText = e.Error()
		if pages > 0 {
			coverage = "partial"
		} else {
			coverage = "unknown"
		}
	}
	tx, txErr := w.db.BeginTx(finalCtx, nil)
	if txErr == nil {
		var revision int
		if txErr = tx.QueryRowContext(finalCtx, `SELECT revision FROM subscriptions WHERE id=?`, id).Scan(&revision); txErr == sql.ErrNoRows {
			status, complete = "interrupted", false
			coverage = "partial"
			errText = "subscription was removed during the run"
			txErr = nil
		} else if txErr == nil && revision != c.Revision {
			status, complete = "partial", false
			coverage = "partial"
			errText = "subscription changed during the run; refresh again for the new revision"
		}
		if txErr == nil {
			_, txErr = tx.ExecContext(finalCtx, `UPDATE subscription_runs SET finished_at=?,status=?,pages=?,results=?,error=?,complete=?,coverage=? WHERE id=?`, finished, status, pages, count, errText, boolInt(complete), coverage, runID)
		}
		if txErr == nil && complete {
			_, txErr = tx.ExecContext(finalCtx, `UPDATE subscriptions SET baseline_complete=1,updated_at=? WHERE id=? AND revision=?`, finished, id, c.Revision)
		}
		if txErr == nil {
			txErr = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	}
	w.mu.Unlock()
	if txErr != nil {
		return txErr
	}
	if complete && e == nil {
		w.DispatchPending(id)
	}
	return e
}

func subscriptionCategoryMask(query map[string]any) (int, error) {
	raw, exists := query["categories"]
	if !exists || raw == nil {
		return 0, nil
	}
	ids, ok := intSlice(raw)
	if !ok {
		return 0, errors.New("categories must be numeric category IDs")
	}
	if len(ids) == 0 {
		return 0, nil
	}
	mask := intNumber(query["category_mask"])
	if mask == 0 {
		return 0, errors.New("category filter mask is unavailable")
	}
	return mask, nil
}

func filterSubscriptionCategories(items []eh.GalleryListItem, query map[string]any, settings config.Settings) ([]eh.GalleryListItem, error) {
	mask, err := subscriptionCategoryMask(query)
	if err != nil {
		return nil, err
	}
	if mask == 0 {
		return items, nil
	}
	out := make([]eh.GalleryListItem, 0, len(items))
	for _, item := range items {
		if galleryCategoryMatches(item.Category, mask, settings) {
			out = append(out, item)
		}
	}
	return out, nil
}

func galleryCategoryMatches(category string, mask int, settings config.Settings) bool {
	if mask == 0 {
		return true
	}
	actualMask := eh.CategoryMask([]string{strings.TrimSpace(category)}, settings)
	return actualMask != 0 && actualMask&mask == mask
}

func (w *Worker) ListItems(ctx context.Context, q ItemQuery) ([]map[string]any, int, error) {
	q.CategoryNamesBySubscription = make(map[string][]string)
	for _, id := range q.SubscriptionIDs {
		c, err := w.Store.Get(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		mask, err := subscriptionCategoryMask(c.Query)
		if err != nil {
			return nil, 0, err
		}
		if mask == 0 {
			continue
		}
		rows, err := w.db.QueryContext(ctx, `SELECT DISTINCT p.category FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=?`, id)
		if err != nil {
			return nil, 0, err
		}
		names := []string{}
		for rows.Next() {
			var category string
			if err = rows.Scan(&category); err != nil {
				_ = rows.Close()
				return nil, 0, err
			}
			if galleryCategoryMatches(category, mask, w.Settings) {
				names = append(names, strings.ToLower(category))
			}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		_ = rows.Close()
		q.CategoryNamesBySubscription[id] = names
	}
	return w.Store.ListItems(ctx, q)
}

func intNumber(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}
func (w *Worker) DispatchPending(subscriptionID string) {
	ctx := w.ctx
	c, err := w.Store.Get(ctx, subscriptionID)
	if err != nil || !c.Enabled || !c.AutoDownload || !c.BaselineComplete || ctx.Err() != nil {
		return
	}
	if IsFavoritesSubscription(c) && !w.Settings.HasIPB() {
		return
	}
	rows, err := w.db.QueryContext(ctx, `SELECT p.gid,p.token,si.publication_id FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND si.pending_auto=1 AND si.baseline_item=0 AND si.first_seen_revision=?`, c.ID, c.Revision)
	if err != nil {
		return
	}
	type candidate struct {
		gid   int
		token string
		pub   int64
	}
	items := []candidate{}
	for rows.Next() {
		var x candidate
		if err = rows.Scan(&x.gid, &x.token, &x.pub); err != nil {
			_ = rows.Close()
			return
		}
		items = append(items, x)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return
	}
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		allowed, quality, force, e := w.autoCandidate(ctx, c, it.gid, it.token, it.pub)
		if e != nil {
			continue
		} // preserve the durable intent and fail closed
		if !allowed {
			_, _ = w.db.ExecContext(ctx, `UPDATE subscription_items SET pending_auto=0 WHERE subscription_id=? AND publication_id=? AND first_seen_revision=?`, c.ID, it.pub, c.Revision)
			continue
		}
		jobID, e := randomID()
		if e != nil {
			return
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		tx, e := w.db.BeginTx(ctx, nil)
		if e != nil {
			return
		}
		var revision int
		e = tx.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=? AND enabled=1 AND auto_download=1 AND baseline_complete=1`, c.ID).Scan(&revision)
		if e == nil && revision != c.Revision {
			e = errors.New("subscription revision changed")
		}
		if e == nil {
			_, e = tx.ExecContext(ctx, `INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,automatic,force,created_at,updated_at) VALUES(?,'eh',?,?,?,'queued','queued',1,?,?,?) ON CONFLICT DO NOTHING`, jobID, it.gid, strings.ToLower(it.token), quality, boolInt(force), now, now)
		}
		var realID, activeQuality, activeStatus string
		if e == nil {
			e = tx.QueryRowContext(ctx, `SELECT id,quality,status FROM download_jobs WHERE provider='eh' AND gid=? AND token=? AND status IN ('queued','running','needs_review')`, it.gid, strings.ToLower(it.token)).Scan(&realID, &activeQuality, &activeStatus)
		}
		if e == nil && activeStatus == "needs_review" {
			_ = tx.Rollback()
			continue
		}
		if e == nil && activeStatus == "queued" && qualityHigher(quality, activeQuality) {
			_, e = tx.ExecContext(ctx, `UPDATE download_jobs SET quality=?,force=1,updated_at=? WHERE id=? AND status='queued'`, quality, now, realID)
		}
		if e == nil {
			_, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO download_job_sources(job_id,subscription_id,source,source_key,requested_quality) VALUES(?,?,?,?,?)`, realID, c.ID, "subscription", fmt.Sprintf("subscription:%s:r%d", c.ID, c.Revision), quality)
		}
		// Keep a higher-quality request pending while a lower-quality shared
		// job runs. Its successful completion will trigger a fresh quality check.
		if e == nil && !(activeStatus == "running" && qualityHigher(quality, activeQuality)) {
			_, e = tx.ExecContext(ctx, `UPDATE subscription_items SET pending_auto=0 WHERE subscription_id=? AND publication_id=? AND first_seen_revision=?`, c.ID, it.pub, c.Revision)
		}
		if e == nil {
			e = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	}
	w.DispatchQueued()
}

func (w *Worker) autoCandidate(ctx context.Context, c Config, gid int, token string, pubID int64) (bool, string, bool, error) {
	quality := stringVal(c.DownloadRules["quality"])
	if quality == "" {
		quality = w.Settings.ArchiveQuality
	}
	if quality == "" {
		quality = "original"
	}
	if qualityRank(quality) == 0 {
		return false, "", false, errors.New("unsupported archive quality")
	}
	var suppressed int
	if e := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_suppressions WHERE provider='eh' AND gid=? AND token=?`, gid, strings.ToLower(token)).Scan(&suppressed); e != nil {
		return false, "", false, e
	}
	if suppressed > 0 {
		return false, quality, false, nil
	}
	var priorFailure int
	if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_jobs WHERE provider='eh' AND gid=? AND lower(token)=lower(?) AND status IN ('failed','needs_review')`, gid, token).Scan(&priorFailure); err != nil {
		return false, "", false, err
	}
	if priorFailure > 0 {
		return false, quality, false, nil
	}
	var galleryCategory string
	var revision, baseline int
	e := w.db.QueryRowContext(ctx, `SELECT si.first_seen_revision,si.baseline_item,p.category FROM subscription_items si JOIN subscriptions s ON s.id=si.subscription_id JOIN publications p ON p.id=si.publication_id WHERE si.subscription_id=? AND si.publication_id=? AND s.enabled=1 AND s.auto_download=1 AND s.baseline_complete=1`, c.ID, pubID).Scan(&revision, &baseline, &galleryCategory)
	if e != nil {
		return false, "", false, e
	}
	if revision != c.Revision || baseline != 0 {
		return false, quality, false, nil
	}
	categoryMask, err := subscriptionCategoryMask(c.Query)
	if err != nil {
		return false, "", false, err
	}
	if categoryMask != 0 && !galleryCategoryMatches(galleryCategory, categoryMask, w.Settings) {
		return false, quality, false, nil
	}
	if hasAny(c.DownloadRules["categories"]) || hasAny(c.DownloadRules["excluded_categories"]) {
		return false, quality, false, nil
	}
	rows, e := w.db.QueryContext(ctx, `SELECT id FROM documents WHERE publication_id=? AND status='available'`, pubID)
	if e != nil {
		return false, "", false, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			_ = rows.Close()
			return false, "", false, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	_ = rows.Close()
	if e != nil {
		return false, "", false, e
	}
	for _, id := range ids {
		if w.Documents == nil {
			return false, "", false, errors.New("document store unavailable")
		}
		d, x := w.Documents.Get(ctx, id)
		if x == nil && d.Status == "available" {
			var existing string
			if x = w.db.QueryRowContext(ctx, `SELECT av.quality FROM documents d JOIN archive_versions av ON av.publication_id=d.publication_id AND av.archive_path=d.archive_path WHERE d.id=?`, id).Scan(&existing); x != nil {
				return false, "", false, fmt.Errorf("document archive quality cannot be verified: %w", x)
			}
			if qualityRank(existing) == 0 {
				return false, "", false, errors.New("existing archive quality is unknown")
			}
			if !qualityHigher(quality, existing) {
				return false, quality, false, nil
			}
			return true, quality, true, nil
		}
		if x != nil && !errors.Is(x, document.ErrNotFound) {
			return false, "", false, x
		}
	}
	return true, quality, false, nil
}
func intSlice(v any) ([]int, bool) {
	switch xs := v.(type) {
	case []any:
		out := make([]int, 0, len(xs))
		for _, x := range xs {
			n, ok := asInt(x)
			if !ok {
				return nil, false
			}
			out = append(out, n)
		}
		return out, true
	case []int:
		return xs, true
	default:
		return nil, false
	}
}
func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if x == float64(int(x)) {
			return int(x), true
		}
	case string:
		n, e := strconv.Atoi(x)
		return n, e == nil
	}
	return 0, false
}
func containsInt(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}
func qualityRank(q string) int {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "org", "original", "original source":
		return 2
	case "res", "resampled", "resample":
		return 1
	default:
		return 0
	}
}
func qualityHigher(requested, existing string) bool {
	return qualityRank(requested) > qualityRank(existing)
}
func qualitySatisfies(requested, actual string) bool {
	r := qualityRank(requested)
	return r > 0 && qualityRank(actual) >= r
}

func hasAny(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case []any:
		return len(x) > 0
	case []int:
		return len(x) > 0
	default:
		return true
	}
}
func (w *Worker) DispatchQueued() {
	w.mu.Lock()
	if !w.dispatchEnabled || !w.started || w.stopping || w.ctx.Err() != nil {
		w.mu.Unlock()
		return
	}
	tx, e := w.db.BeginTx(w.ctx, nil)
	if e != nil {
		w.mu.Unlock()
		return
	}
	var running int
	if e = tx.QueryRowContext(w.ctx, `SELECT COUNT(*) FROM download_jobs WHERE status='running'`).Scan(&running); e != nil {
		_ = tx.Rollback()
		w.mu.Unlock()
		return
	}
	remaining := w.maxConcurrent - running
	if remaining <= 0 {
		_ = tx.Rollback()
		w.mu.Unlock()
		return
	}
	rows, e := tx.QueryContext(w.ctx, `SELECT id,gid,token,quality,force,automatic FROM download_jobs WHERE status='queued' ORDER BY created_at,id LIMIT ?`, remaining*2)
	if e != nil {
		_ = tx.Rollback()
		w.mu.Unlock()
		return
	}
	jobs := []downloadJob{}
	for rows.Next() {
		var j downloadJob
		var force, automatic int
		if e = rows.Scan(&j.id, &j.gid, &j.token, &j.quality, &force, &automatic); e != nil {
			break
		}
		j.force = force != 0
		j.automatic = automatic != 0
		jobs = append(jobs, j)
	}
	if e == nil {
		e = rows.Err()
	}
	_ = rows.Close()
	if e != nil {
		_ = tx.Rollback()
		w.mu.Unlock()
		return
	}
	claimed := []downloadJob{}
	pruned := false
	for _, j := range jobs {
		if len(claimed) >= remaining {
			break
		}
		manual, valid, requestedQuality, verr := w.validateQueuedSources(w.ctx, tx, j)
		if verr != nil {
			e = verr
			break
		}
		if !manual && !valid {
			_, e = tx.ExecContext(w.ctx, `UPDATE download_jobs SET status='canceled',stage='authorization_revoked',error='no active authorized source remains',updated_at=? WHERE id=? AND status='queued'`, time.Now().UTC().Format(time.RFC3339Nano), j.id)
			if e != nil {
				break
			}
			pruned = true
			continue
		}
		// Recheck deletion suppression immediately before claiming a paid task.
		var suppressed int
		if e = tx.QueryRowContext(w.ctx, `SELECT COUNT(*) FROM download_suppressions WHERE provider='eh' AND gid=? AND token=?`, j.gid, strings.ToLower(j.token)).Scan(&suppressed); e != nil {
			break
		}
		if suppressed > 0 && !manual {
			_, e = tx.ExecContext(w.ctx, `UPDATE download_jobs SET status='canceled',stage='suppressed',error='automatic download suppressed after local deletion',updated_at=? WHERE id=? AND status='queued'`, time.Now().UTC().Format(time.RFC3339Nano), j.id)
			if e != nil {
				break
			}
			pruned = true
			continue
		}
		if requestedQuality == "" {
			e = errors.New("authorized source has no valid archive quality")
			break
		}
		var currentQuality string
		e = tx.QueryRowContext(w.ctx, `SELECT COALESCE(av.quality,'') FROM publications p LEFT JOIN archive_versions av ON av.publication_id=p.id WHERE p.provider='eh' AND p.gid=? AND lower(p.token)=lower(?)`, j.gid, j.token).Scan(&currentQuality)
		if e == sql.ErrNoRows {
			e = nil
		} else if e != nil {
			break
		}
		if qualityRank(currentQuality) == 0 {
			currentQuality = ""
		}
		force := (manual && j.force) || (currentQuality != "" && qualityHigher(requestedQuality, currentQuality))
		j.quality, j.force = requestedQuality, force
		res, x := tx.ExecContext(w.ctx, `UPDATE download_jobs SET quality=?,force=?,status='running',stage='submitting',updated_at=? WHERE id=? AND status='queued'`, j.quality, boolInt(j.force), time.Now().UTC().Format(time.RFC3339Nano), j.id)
		if x != nil {
			e = x
			break
		}
		n, x := res.RowsAffected()
		if x != nil {
			e = x
			break
		}
		if n == 1 {
			claimed = append(claimed, j)
		}
	}
	if e == nil {
		e = tx.Commit()
	} else {
		_ = tx.Rollback()
	}
	if e != nil {
		w.mu.Unlock()
		return
	}
	// Add under the same lock Stop uses before Wait, so Add can never race with
	// Wait and no goroutine can begin after shutdown starts.
	for range claimed {
		w.wg.Add(1)
	}
	w.mu.Unlock()
	if len(claimed) == 0 && pruned {
		go w.DispatchQueued()
		return
	}
	for _, j := range claimed {
		go func(j downloadJob) {
			defer func() { w.DispatchQueued(); w.wg.Done() }()
			w.runJob(j)
		}(j)
	}
}

func (w *Worker) validateQueuedSources(ctx context.Context, tx *sql.Tx, j downloadJob) (manual, valid bool, selectedQuality string, err error) {
	rows, err := tx.QueryContext(ctx, `SELECT d.source,d.source_key,d.subscription_id,d.requested_quality,s.enabled,s.auto_download,s.revision,si.first_seen_revision,si.baseline_item,s.download_rules_json,s.query_json,p.category FROM download_job_sources d LEFT JOIN subscriptions s ON s.id=d.subscription_id LEFT JOIN publications p ON p.provider='eh' AND p.gid=? AND lower(p.token)=lower(?) LEFT JOIN subscription_items si ON si.subscription_id=d.subscription_id AND si.publication_id=p.id WHERE d.job_id=?`, j.gid, strings.ToLower(j.token), j.id)
	if err != nil {
		return false, false, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var source, sourceKey string
		var requested sql.NullString
		var subscriptionID sql.NullString
		var enabled, auto, subRev, firstRev, baseline sql.NullInt64
		var rawRules sql.NullString
		var rawQuery, galleryCategory sql.NullString
		if err = rows.Scan(&source, &sourceKey, &subscriptionID, &requested, &enabled, &auto, &subRev, &firstRev, &baseline, &rawRules, &rawQuery, &galleryCategory); err != nil {
			return manual, valid, selectedQuality, err
		}
		if source == "manual" && sourceKey == "manual" && !subscriptionID.Valid {
			q := requested.String
			if q == "" {
				q = j.quality
			}
			if qualityRank(q) == 0 {
				return manual, valid, selectedQuality, errors.New("manual source quality is invalid")
			}
			manual = true
			selectedQuality = higherQuality(selectedQuality, q)
			continue
		}
		if source != "subscription" || !subscriptionID.Valid || !enabled.Valid || enabled.Int64 == 0 || !auto.Valid || auto.Int64 == 0 || !firstRev.Valid || !baseline.Valid || baseline.Int64 != 0 || !subRev.Valid || firstRev.Int64 != subRev.Int64 {
			continue
		}
		if sourceKey != fmt.Sprintf("subscription:%s:r%d", subscriptionID.String, subRev.Int64) && sourceKey != subscriptionID.String {
			continue
		}
		query := map[string]any{}
		if rawQuery.Valid && json.Unmarshal([]byte(rawQuery.String), &query) != nil {
			return manual, valid, selectedQuality, errors.New("invalid subscription query")
		}
		categoryMask, err := subscriptionCategoryMask(query)
		if err != nil {
			return manual, valid, selectedQuality, err
		}
		if categoryMask != 0 && (!galleryCategory.Valid || !galleryCategoryMatches(galleryCategory.String, categoryMask, w.Settings)) {
			continue
		}
		rules := map[string]any{}
		if rawRules.Valid && json.Unmarshal([]byte(rawRules.String), &rules) != nil {
			return manual, valid, selectedQuality, errors.New("invalid subscription download rules")
		}
		if hasAny(rules["categories"]) || hasAny(rules["excluded_categories"]) {
			continue
		}
		q := stringVal(rules["quality"])
		if q == "" {
			q = w.Settings.ArchiveQuality
		}
		if q == "" {
			q = "original"
		}
		if qualityRank(q) == 0 {
			return manual, valid, selectedQuality, errors.New("subscription archive quality is invalid")
		}
		if requested.Valid && requested.String != "" && qualityRank(requested.String) != qualityRank(q) {
			continue
		}
		valid = true
		selectedQuality = higherQuality(selectedQuality, q)
	}
	if err = rows.Err(); err != nil {
		return manual, valid, selectedQuality, err
	}
	return manual, valid, selectedQuality, nil
}

func higherQuality(a, b string) string {
	if qualityRank(b) > qualityRank(a) {
		return b
	}
	return a
}

func (w *Worker) runJob(j downloadJob) {
	if w.Archives == nil {
		w.finishJob(j.id, "failed", "complete", "archive manager unavailable", "")
		return
	}
	ctx := w.ctx
	if ctx.Err() != nil {
		w.finishJob(j.id, "needs_review", "shutdown", "worker stopped while archive task was claimed; inspect upstream before retrying", "")
		return
	}
	if j.automatic {
		var prior int
		if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_jobs WHERE provider='eh' AND gid=? AND lower(token)=lower(?) AND id<>? AND status IN ('failed','needs_review')`, j.gid, j.token, j.id).Scan(&prior); err != nil || prior > 0 {
			message := "prior archive outcome requires manual review"
			if err != nil {
				message = "cannot verify prior archive outcome: " + err.Error()
			}
			w.finishJob(j.id, "canceled", "prior_review", message, "")
			return
		}
	}
	if id, q, err := w.findVerifiedDocument(ctx, j.gid, j.token); err == nil {
		if qualitySatisfies(j.quality, q) {
			w.finishJob(j.id, "succeeded", "complete", "", id)
			return
		}
		if qualityRank(q) == 0 {
			w.finishJob(j.id, "needs_review", "quality_unknown", "existing archive quality cannot be verified; inspect before retrying", "")
			return
		}
		// Manager.Start(force=false) treats any ready ZIP as a cache hit. An
		// explicit quality upgrade must bypass that cache while preserving the
		// existing published archive until the replacement has been validated.
		j.force = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		w.finishJob(j.id, "needs_review", "document_verify", "existing archive registration could not be verified: "+err.Error(), "")
		return
	}
	state, err := w.Archives.Start(ctx, j.gid, j.token, j.quality, j.force)
	if err != nil {
		var uncertain *archive.SubmissionUncertainError
		status, stage := "failed", "complete"
		if errors.As(err, &uncertain) {
			status, stage = "needs_review", "archive_start"
		}
		w.finishJob(j.id, status, stage, err.Error(), "")
		return
	}
	// A cache hit is not proof that this job's requested tier exists. It is
	// safe to upgrade only by starting a forced archive job.
	if state != nil && state["skipped"] == true {
		if id, q, verifyErr := w.findVerifiedDocument(ctx, j.gid, j.token); verifyErr == nil && qualitySatisfies(j.quality, q) {
			w.finishJob(j.id, "succeeded", "complete", "", id)
			return
		}
		if !j.force {
			state, err = w.Archives.Start(ctx, j.gid, j.token, j.quality, true)
			if err != nil {
				var uncertain *archive.SubmissionUncertainError
				status, stage := "failed", "archive_upgrade"
				if errors.As(err, &uncertain) {
					status = "needs_review"
				}
				w.finishJob(j.id, status, stage, err.Error(), "")
				return
			}
		}
	}
	_, _ = w.db.ExecContext(ctx, `UPDATE download_jobs SET stage='downloading',updated_at=? WHERE id=? AND status='running'`, time.Now().UTC().Format(time.RFC3339Nano), j.id)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.finishJob(j.id, "needs_review", "shutdown", "worker stopped while archive may be in progress; inspect upstream before retrying", "")
			return
		case <-ticker.C:
			state := w.Archives.Status(j.gid, j.token)
			status := fmt.Sprint(state["status"])
			if status == "ready" {
				id, actual, verifyErr := w.registerReadyDocument(ctx, j.gid, j.token)
				if verifyErr != nil {
					w.finishJob(j.id, "needs_review", "document_registration", verifyErr.Error(), "")
					return
				}
				if !qualitySatisfies(j.quality, actual) {
					w.finishJob(j.id, "needs_review", "quality_mismatch", fmt.Sprintf("archive is ready at quality %q; requested %q", actual, j.quality), "")
					return
				}
				w.finishJob(j.id, "succeeded", "complete", "", id)
				w.dispatchWaitingSources(j.id)
				return
			}
			if status == "failed" || status == "canceled" {
				w.finishJob(j.id, "needs_review", "archive_result", fmt.Sprintf("archive ended %s; inspect upstream charge/result before retrying: %v", status, state["error"]), "")
				return
			}
		}
	}
}

func (w *Worker) finishJob(id, status, stage, message, documentID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = w.db.ExecContext(ctx, `UPDATE download_jobs SET status=?,stage=?,error=NULLIF(?,''),document_id=NULLIF(?,''),updated_at=? WHERE id=? AND status='running'`, status, stage, message, documentID, time.Now().UTC().Format(time.RFC3339Nano), id)
}

// findVerifiedDocument accepts only a readable registered document associated
// with this exact EH identity and returns its actual database archive quality.
func (w *Worker) findVerifiedDocument(ctx context.Context, gid int, token string) (string, string, error) {
	if w.Documents == nil {
		return "", "", errors.New("document store unavailable")
	}
	var id, quality string
	err := w.db.QueryRowContext(ctx, `SELECT d.id,COALESCE(av.quality,'') FROM documents d JOIN publications p ON p.id=d.publication_id JOIN archive_versions av ON av.publication_id=p.id AND av.archive_path=d.archive_path AND av.status='ready' WHERE p.provider='eh' AND p.gid=? AND lower(p.token)=lower(?) AND d.status='available' ORDER BY d.updated_at DESC LIMIT 1`, gid, token).Scan(&id, &quality)
	if err != nil {
		return "", "", err
	}
	d, err := w.Documents.Get(ctx, id)
	if err != nil || d.Status != "available" {
		if err == nil {
			err = errors.New("registered document is not available")
		}
		return "", "", err
	}
	return id, quality, nil
}

func (w *Worker) registerReadyDocument(ctx context.Context, gid int, token string) (string, string, error) {
	if w.Archives == nil || w.Archives.Store == nil {
		return "", "", errors.New("archive store unavailable")
	}
	if w.Documents == nil {
		return "", "", errors.New("document store unavailable")
	}
	path, _, err := w.Archives.Store.ArchivePublishPath(gid, token)
	if err != nil {
		return "", "", err
	}
	root, err := filepath.Abs(w.Settings.ArchiveDir)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("published archive path is outside configured archive directory")
	}
	doc, err := w.Documents.RegisterPublished(ctx, rel, gid, token, "download_job")
	if err != nil {
		return "", "", err
	}
	verified, err := w.Documents.Get(ctx, doc.ID)
	if err != nil || verified.Status != "available" {
		if err == nil {
			err = errors.New("registered archive document is not available")
		}
		return "", "", err
	}
	var quality string
	archiveState := w.Archives.Status(gid, token)
	quality, _ = archiveState["or"].(string)
	if quality == "" {
		quality, _ = archiveState["quality"].(string)
	}
	if quality == "" {
		if err = w.db.QueryRowContext(ctx, `SELECT COALESCE(av.quality,'') FROM publications p JOIN archive_versions av ON av.publication_id=p.id WHERE p.provider='eh' AND p.gid=? AND lower(p.token)=lower(?) AND av.archive_path=? AND av.status='ready'`, gid, token, path).Scan(&quality); err != nil {
			return "", "", err
		}
	}
	return doc.ID, quality, nil
}

func (w *Worker) dispatchWaitingSources(jobID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := w.db.QueryContext(ctx, `SELECT DISTINCT subscription_id FROM download_job_sources WHERE job_id=? AND source='subscription' AND subscription_id IS NOT NULL`, jobID)
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			_ = rows.Close()
			return
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		_ = rows.Close()
		return
	}
	_ = rows.Close()
	for _, id := range ids {
		w.DispatchPending(id)
	}
}

func (w *Worker) ListJobs(ctx context.Context, status string) ([]map[string]any, error) {
	query := `SELECT j.id,j.gid,j.token,j.quality,j.status,j.stage,j.error,j.created_at,j.updated_at,j.document_id,COALESCE(json_extract(p.metadata_json,'$.title'),''),j.intent_json FROM download_jobs j LEFT JOIN publications p ON p.provider=j.provider AND p.gid=j.gid AND p.token=j.token`
	args := []any{}
	if status != "" {
		query += ` WHERE j.status=?`
		args = append(args, status)
	}
	query += ` ORDER BY j.created_at DESC`
	rows, e := w.db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, token, quality, state, stage, created, updated string
		var gid int
		var errText, doc, intentJSON sql.NullString
		if e = rows.Scan(&id, &gid, &token, &quality, &state, &stage, &errText, &created, &updated, &doc, &title, &intentJSON); e != nil {
			return nil, e
		}
		item := map[string]any{"id": id, "gid": gid, "token": token, "quality": quality, "status": state, "stage": stage, "created_at": created, "updated_at": updated, "title": title}
		if errText.Valid {
			item["error"] = errText.String
		}
		if doc.Valid {
			item["document_id"] = doc.String
		}
		if intentJSON.Valid {
			var intent versionUpdateIntent
			if json.Unmarshal([]byte(intentJSON.String), &intent) == nil && intent.Kind == "version_update" {
				item["operation"] = "version_update"
				item["root_gid"] = intent.RootGID
				item["root_token"] = intent.RootToken
				item["target_gid"] = intent.TargetGID
				item["target_token"] = intent.TargetToken
				if intent.MaxGP != nil {
					item["max_gp"] = *intent.MaxGP
				}
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type versionUpdateIntent struct {
	Kind        string `json:"kind"`
	RootGID     int    `json:"root_gid"`
	RootToken   string `json:"root_token"`
	TargetGID   int    `json:"target_gid"`
	TargetToken string `json:"target_token"`
	MaxGP       *int   `json:"max_gp"`
}

// SubmitManualVersionUpdate records an explicit target-version request before
// invoking the manager's one-shot paid submit path, then tracks that exact target.
func (w *Worker) SubmitManualVersionUpdate(ctx context.Context, rootGID int, rootToken string, targetGID int, targetToken string, maxGP *int) (string, map[string]any, error) {
	if w.Archives == nil || w.Archives.Store == nil {
		return "", nil, errors.New("archive manager unavailable")
	}
	if rootGID <= 0 || rootToken == "" || targetGID <= 0 || targetToken == "" {
		return "", nil, errors.New("root and target EH identity are required")
	}
	activeGID, activeToken := w.Archives.Store.ResolveActive(rootGID, rootToken)
	active := w.Archives.Store.Get(activeGID, activeToken)
	if active == nil || fmt.Sprint(active["status"]) != "ready" {
		return "", nil, errors.New("active archive is not ready")
	}
	quality := fmt.Sprint(active["or"])
	if qualityRank(quality) == 0 {
		quality = fmt.Sprint(active["quality"])
	}
	if qualityRank(quality) == 0 {
		return "", nil, errors.New("active archive quality is unknown")
	}
	jobID, err := randomID()
	if err != nil {
		return "", nil, err
	}
	intent := versionUpdateIntent{Kind: "version_update", RootGID: rootGID, RootToken: strings.ToLower(rootToken), TargetGID: targetGID, TargetToken: strings.ToLower(targetToken)}
	if maxGP != nil {
		v := *maxGP
		intent.MaxGP = &v
	}
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	w.mu.Lock()
	if !w.started || w.stopping || w.ctx.Err() != nil {
		w.mu.Unlock()
		return "", nil, errors.New("subscription worker is not accepting downloads")
	}
	var existing string
	err = w.db.QueryRowContext(ctx, `SELECT id FROM download_jobs WHERE provider='eh' AND gid=? AND lower(token)=lower(?) AND status IN ('queued','running','needs_review')`, targetGID, targetToken).Scan(&existing)
	if err == nil {
		w.mu.Unlock()
		return "", nil, fmt.Errorf("target already has active download job %s", existing)
	}
	if err != sql.ErrNoRows {
		w.mu.Unlock()
		return "", nil, err
	}
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		w.mu.Unlock()
		return "", nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,automatic,force,intent_json,created_at,updated_at) VALUES(?,'eh',?,?,?,'running','submitting_version_update',0,0,?,?,?)`, jobID, targetGID, strings.ToLower(targetToken), quality, string(intentJSON), now, now); err != nil {
		_ = tx.Rollback()
		w.mu.Unlock()
		return "", nil, err
	}
	sourceKey := fmt.Sprintf("version-update:%d:%s", rootGID, strings.ToLower(rootToken))
	if _, err = tx.ExecContext(ctx, `INSERT INTO download_job_sources(job_id,subscription_id,source,source_key,requested_quality) VALUES(?,NULL,'manual',?,?)`, jobID, sourceKey, quality); err != nil {
		_ = tx.Rollback()
		w.mu.Unlock()
		return "", nil, err
	}
	if err = tx.Commit(); err != nil {
		w.mu.Unlock()
		return "", nil, err
	}
	w.wg.Add(1)
	monitorCtx, monitorCancel := context.WithCancel(w.ctx)
	w.mu.Unlock()
	result, submitErr := w.Archives.StartVersionUpdate(monitorCtx, rootGID, strings.ToLower(rootToken), targetGID, strings.ToLower(targetToken), maxGP)
	if submitErr != nil {
		var uncertain *archive.SubmissionUncertainError
		state, stage := "failed", "version_update_start"
		if errors.As(submitErr, &uncertain) {
			state, stage = "needs_review", "version_update_uncertain"
		}
		w.finishJob(jobID, state, stage, submitErr.Error(), "")
		monitorCancel()
		w.wg.Done()
		return jobID, nil, submitErr
	}
	_, _ = w.db.ExecContext(context.Background(), `UPDATE download_jobs SET stage='version_update_downloading',updated_at=? WHERE id=? AND status='running'`, time.Now().UTC().Format(time.RFC3339Nano), jobID)
	job := downloadJob{id: jobID, gid: targetGID, token: strings.ToLower(targetToken), quality: quality}
	go func() { defer w.wg.Done(); defer monitorCancel(); w.trackVersionUpdate(monitorCtx, job, intent) }()
	return jobID, result, nil
}

func (w *Worker) trackVersionUpdate(ctx context.Context, j downloadJob, intent versionUpdateIntent) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			w.finishJob(j.id, "needs_review", "shutdown", "worker stopped while version update may be in progress; inspect upstream before retrying", "")
			return
		}
		state := w.Archives.Status(j.gid, j.token)
		status := fmt.Sprint(state["status"])
		if status == "ready" {
			docID, actual, err := w.registerReadyDocument(ctx, j.gid, j.token)
			if err != nil {
				w.finishJob(j.id, "needs_review", "version_document_registration", err.Error(), "")
				return
			}
			if !qualitySatisfies(j.quality, actual) {
				w.finishJob(j.id, "needs_review", "version_quality_mismatch", fmt.Sprintf("version archive quality %q does not satisfy %q", actual, j.quality), "")
				return
			}
			w.finishJob(j.id, "succeeded", "complete", "", docID)
			return
		}
		if status == "failed" || status == "canceled" {
			w.finishJob(j.id, "needs_review", "version_update_result", fmt.Sprintf("version update ended %s; inspect upstream result before retrying: %v", status, state["error"]), "")
			return
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

func (w *Worker) ReviewJob(ctx context.Context, id, resolution string, acknowledged bool) error {
	var gid int
	var token, quality, status string
	if err := w.db.QueryRowContext(ctx, `SELECT gid,token,quality,status FROM download_jobs WHERE id=?`, id).Scan(&gid, &token, &quality, &status); err != nil {
		return err
	}
	if status != "needs_review" {
		return errors.New("download job is not awaiting review")
	}
	switch resolution {
	case "reconcile":
		documentID, actual, err := w.registerReadyDocument(ctx, gid, token)
		if err != nil {
			return fmt.Errorf("cannot reconcile archive result: %w", err)
		}
		if !qualitySatisfies(quality, actual) {
			return fmt.Errorf("existing archive quality %q does not satisfy requested %q", actual, quality)
		}
		res, err := w.db.ExecContext(ctx, `UPDATE download_jobs SET status='succeeded',stage='complete',error=NULL,document_id=?,updated_at=? WHERE id=? AND status='needs_review'`, documentID, time.Now().UTC().Format(time.RFC3339Nano), id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("download job review state changed")
		}
		return nil
	case "allow_manual_retry":
		if !acknowledged {
			return errors.New("explicit acknowledgment is required before allowing a manual retry")
		}
		res, err := w.db.ExecContext(ctx, `UPDATE download_jobs SET status='failed',stage='user_reviewed',error='operator acknowledged prior outcome; any retry must be an explicit manual request',updated_at=? WHERE id=? AND status='needs_review'`, time.Now().UTC().Format(time.RFC3339Nano), id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("download job review state changed")
		}
		return nil
	default:
		return errors.New("resolution must be reconcile or allow_manual_retry")
	}
}

func (w *Worker) CancelJob(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, e := w.db.ExecContext(ctx, `UPDATE download_jobs SET status='canceled',stage='canceled',updated_at=? WHERE id=? AND status='queued'`, now, id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("job is already running or was not found")
	}
	return nil
}

// CancelSubscription removes only queued automatic source claims. A shared job
// remains queued when another subscription or a manual request still needs it.
// Running jobs are left to finish because the upstream may already have charged.
func (w *Worker) CancelSubscription(ctx context.Context, id string) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT s.job_id FROM download_job_sources s JOIN download_jobs j ON j.id=s.job_id WHERE s.subscription_id=? AND s.source='subscription' AND j.status='queued'`, id)
	if err != nil {
		return err
	}
	jobs := []string{}
	for rows.Next() {
		var job string
		if err = rows.Scan(&job); err != nil {
			_ = rows.Close()
			return err
		}
		jobs = append(jobs, job)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM download_job_sources WHERE subscription_id=? AND source='subscription' AND job_id IN (SELECT id FROM download_jobs WHERE status='queued')`, id); err != nil {
		return err
	}
	for _, job := range jobs {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_job_sources WHERE job_id=?`, job).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE download_jobs SET status='canceled',stage='source_removed',error='subscription source was removed before dispatch',updated_at=? WHERE id=? AND status='queued'`, time.Now().UTC().Format(time.RFC3339Nano), job); err != nil {
				return err
			}
			continue
		}
		qualityRows, qerr := tx.QueryContext(ctx, `SELECT requested_quality FROM download_job_sources WHERE job_id=?`, job)
		if qerr != nil {
			return qerr
		}
		quality := ""
		for qualityRows.Next() {
			var q string
			if qerr = qualityRows.Scan(&q); qerr != nil {
				_ = qualityRows.Close()
				return qerr
			}
			quality = higherQuality(quality, q)
		}
		qerr = qualityRows.Err()
		_ = qualityRows.Close()
		if qerr != nil {
			return qerr
		}
		if quality == "" {
			if err = tx.QueryRowContext(ctx, `SELECT quality FROM download_jobs WHERE id=? AND status='queued'`, job).Scan(&quality); err != nil {
				return err
			}
		}
		if qualityRank(quality) == 0 {
			return errors.New("remaining download source has no valid requested quality")
		}
		if _, err = tx.ExecContext(ctx, `UPDATE download_jobs SET quality=?,force=0,updated_at=? WHERE id=? AND status='queued'`, quality, time.Now().UTC().Format(time.RFC3339Nano), job); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (w *Worker) QueueManual(ctx context.Context, gid int, token, quality string) (string, error) {
	return w.QueueManualForce(ctx, gid, token, quality, false)
}

func (w *Worker) QueueManualForce(ctx context.Context, gid int, token, quality string, force bool) (string, error) {
	if gid < 1 || token == "" {
		return "", errors.New("gallery identity required")
	}
	if quality == "" {
		quality = w.Settings.ArchiveQuality
	}
	if quality == "" {
		quality = "original"
	}
	if qualityRank(quality) == 0 {
		return "", errors.New("unsupported archive quality")
	}
	token = strings.ToLower(token)
	id, e := randomID()
	if e != nil {
		return "", e
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, e := w.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var activeID, activeQuality, activeStatus string
	e = tx.QueryRowContext(ctx, `SELECT id,quality,status FROM download_jobs WHERE provider='eh' AND gid=? AND token=? AND status IN ('queued','running','needs_review')`, gid, token).Scan(&activeID, &activeQuality, &activeStatus)
	if e != nil && e != sql.ErrNoRows {
		return "", e
	}
	if e == nil {
		if activeStatus == "needs_review" {
			return "", errors.New("archive submission outcome needs review before another paid request")
		}
		if activeStatus == "running" && qualityHigher(quality, activeQuality) {
			return "", errors.New("current archive task must finish before requesting a higher quality")
		}
		if activeStatus == "queued" && qualityHigher(quality, activeQuality) {
			if _, e = tx.ExecContext(ctx, `UPDATE download_jobs SET quality=?,force=1,updated_at=? WHERE id=? AND status='queued'`, quality, now, activeID); e != nil {
				return "", e
			}
		} else if activeStatus == "queued" && force {
			if _, e = tx.ExecContext(ctx, `UPDATE download_jobs SET force=1,updated_at=? WHERE id=? AND status='queued'`, now, activeID); e != nil {
				return "", e
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO download_job_sources(job_id,subscription_id,source,source_key,requested_quality) VALUES(?,NULL,'manual','manual',?)`, activeID, quality); e != nil {
			return "", e
		}
		var manualQuality string
		if e = tx.QueryRowContext(ctx, `SELECT requested_quality FROM download_job_sources WHERE job_id=? AND source='manual' AND source_key='manual'`, activeID).Scan(&manualQuality); e != nil {
			return "", e
		}
		if qualityHigher(quality, manualQuality) {
			if _, e = tx.ExecContext(ctx, `UPDATE download_job_sources SET requested_quality=? WHERE job_id=? AND source='manual' AND source_key='manual'`, quality, activeID); e != nil {
				return "", e
			}
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM download_suppressions WHERE provider='eh' AND gid=? AND token=?`, gid, token); e != nil {
			return "", e
		}
		if e = tx.Commit(); e != nil {
			return "", e
		}
		w.DispatchQueued()
		return activeID, nil
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM download_suppressions WHERE provider='eh' AND gid=? AND token=?`, gid, token); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,automatic,force,created_at,updated_at) VALUES(?,'eh',?,?,?,'queued','queued',0,?,?,?)`, id, gid, token, quality, boolInt(force), now, now); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO download_job_sources(job_id,subscription_id,source,source_key,requested_quality) VALUES(?,NULL,'manual','manual',?)`, id, quality); e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	w.DispatchQueued()
	return id, nil
}

func (w *Worker) fetchSubscriptionPage(ctx context.Context, c Config, cursor string, pageNumber int) (eh.GalleryPageInfo, error) {
	extra := url.Values{}
	if q := QueryFromConfig(c); q != nil {
		extra = q
	}
	if c.Source == "preset" {
		preset := stringVal(c.Query["preset"])
		if preset == "" {
			preset = "latest"
		}
		switch {
		case preset == "latest":
			return w.Service.Search(ctx, "", cursor, extra)
		case preset == "popular":
			return w.Service.Popular(ctx, cursor)
		case preset == "watched":
			return w.Service.Watched(ctx, cursor, extra)
		case preset == "favorites":
			return w.Service.Favorites(ctx, cursor, "")
		case strings.HasPrefix(preset, "favorites:"):
			favcat := strings.TrimPrefix(preset, "favorites:")
			return w.Service.Favorites(ctx, cursor, favcat)
		case strings.HasPrefix(preset, "toplist:"):
			period := strings.TrimPrefix(preset, "toplist:")
			return w.Service.Toplist(ctx, period, pageNumber)
		default:
			return eh.GalleryPageInfo{}, fmt.Errorf("unsupported preset %q", preset)
		}
	}
	return w.Service.Search(ctx, stringVal(c.Query["keywords"]), cursor, extra)
}

func (w *Worker) fetchSubscriptionPageQueued(ctx context.Context, c Config, cursor string, pageNumber int) (eh.GalleryPageInfo, error) {
	select {
	case w.subscriptionFetchQueue <- struct{}{}:
	case <-ctx.Done():
		return eh.GalleryPageInfo{}, ctx.Err()
	}
	defer func() { <-w.subscriptionFetchQueue }()
	if err := ctx.Err(); err != nil {
		return eh.GalleryPageInfo{}, err
	}
	return w.fetchSubscriptionPage(ctx, c, cursor, pageNumber)
}

func (w *Worker) runSourcePage(ctx context.Context, c Config, cursor string) (eh.GalleryPageInfo, error) {
	return w.fetchSubscriptionPage(ctx, c, cursor, 1)
}

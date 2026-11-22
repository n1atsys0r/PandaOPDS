package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
	"pandaopds/internal/subscription"
)

func RegisterSubscriptionRoutes(m *http.ServeMux, w *subscription.Worker) {
	if m == nil || w == nil {
		return
	}
	m.HandleFunc("GET /api/publications/eh/{gid}/{token}/cover", func(rw http.ResponseWriter, r *http.Request) {
		gid, e := strconv.Atoi(r.PathValue("gid"))
		if e != nil || gid < 1 {
			http.NotFound(rw, r)
			return
		}
		token := strings.ToLower(r.PathValue("token"))
		var mime string
		var data []byte
		e = w.Store.DB().QueryRowContext(r.Context(), `SELECT c.mime,c.data FROM covers c JOIN publications p ON p.id=c.publication_id WHERE p.provider='eh' AND p.gid=? AND p.token=?`, gid, token).Scan(&mime, &data)
		if e == sql.ErrNoRows {
			var raw string
			if err := w.Store.DB().QueryRowContext(r.Context(), `SELECT COALESCE(NULLIF(cover_url,''),NULLIF(json_extract(metadata_json,'$.cover_url'),''),json_extract(metadata_json,'$.thumb'),'') FROM publications WHERE provider='eh' AND gid=? AND token=?`, gid, token).Scan(&raw); err != nil || raw == "" || !hasAllowedCoverThumb(map[string]any{"thumb": raw}, w.Settings.ImageProxyHosts) {
				http.NotFound(rw, r)
				return
			}
			data, mime, e = w.Service.FetchCoverBytes(r.Context(), raw)
			if e != nil {
				http.NotFound(rw, r)
				return
			}
			sum := sha256.Sum256(data)
			if _, e = w.Store.DB().ExecContext(r.Context(), `INSERT INTO covers(publication_id,mime,data,sha256,source,updated_at) SELECT id,?,?,?,?,? FROM publications WHERE provider='eh' AND gid=? AND token=? ON CONFLICT(publication_id)DO UPDATE SET mime=excluded.mime,data=excluded.data,sha256=excluded.sha256,source=excluded.source,updated_at=excluded.updated_at`, mime, data, hex.EncodeToString(sum[:]), "upstream", time.Now().UTC().Format(time.RFC3339Nano), gid, token); e != nil {
				http.Error(rw, "could not cache cover", http.StatusInternalServerError)
				return
			}
		} else if e != nil {
			http.Error(rw, "could not read cover", http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", mime)
		rw.Header().Set("Cache-Control", "public, max-age=86400")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write(data)
	})
	m.HandleFunc("GET /api/subscriptions", func(rw http.ResponseWriter, r *http.Request) {
		items, e := w.Store.List(r.Context())
		if e != nil {
			writeJSON(rw, 500, map[string]any{"detail": "could not list subscriptions"})
			return
		}
		writeJSON(rw, 200, map[string]any{"items": items})
	})
	m.HandleFunc("POST /api/subscriptions", func(rw http.ResponseWriter, r *http.Request) {
		var c subscription.Config
		if e := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<20)).Decode(&c); e != nil {
			writeJSON(rw, 400, map[string]any{"detail": "invalid subscription"})
			return
		}
		if subscription.IsFavoritesSubscription(c) && !w.Settings.HasIPB() {
			writeJSON(rw, 403, map[string]any{"detail": "favorites require IPB cookies"})
			return
		}
		if e := normalizeSubscription(&c, w.Settings); e != nil {
			writeJSON(rw, 400, map[string]any{"detail": e.Error()})
			return
		}
		out, e := w.Store.Create(r.Context(), c)
		if e != nil {
			writeJSON(rw, 400, map[string]any{"detail": e.Error()})
			return
		}
		writeJSON(rw, 201, out)
	})
	m.HandleFunc("GET /api/subscriptions/{id}", func(rw http.ResponseWriter, r *http.Request) {
		c, e := w.Store.Get(r.Context(), r.PathValue("id"))
		if errors.Is(e, sql.ErrNoRows) {
			writeJSON(rw, 404, map[string]any{"detail": "subscription not found"})
			return
		}
		if e != nil {
			writeJSON(rw, 500, map[string]any{"detail": "could not read subscription"})
			return
		}
		writeJSON(rw, 200, c)
	})
	m.HandleFunc("PATCH /api/subscriptions/{id}", func(rw http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		old, e := w.Store.Get(r.Context(), id)
		if e != nil {
			writeJSON(rw, 404, map[string]any{"detail": "subscription not found"})
			return
		}
		var patch map[string]json.RawMessage
		if e = json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<20)).Decode(&patch); e != nil {
			writeJSON(rw, 400, map[string]any{"detail": "invalid subscription update"})
			return
		}
		raw, _ := json.Marshal(old)
		var merged map[string]json.RawMessage
		_ = json.Unmarshal(raw, &merged)
		for k, v := range patch {
			merged[k] = v
		}
		var next subscription.Config
		if e = json.Unmarshal(mustMarshalRaw(merged), &next); e != nil {
			writeJSON(rw, 400, map[string]any{"detail": "invalid subscription update"})
			return
		}
		if subscription.IsFavoritesSubscription(next) && !w.Settings.HasIPB() {
			writeJSON(rw, 403, map[string]any{"detail": "favorites require IPB cookies"})
			return
		}
		if e := normalizeSubscription(&next, w.Settings); e != nil {
			writeJSON(rw, 400, map[string]any{"detail": e.Error()})
			return
		}
		revoke := (old.Enabled && !next.Enabled) || (old.AutoDownload && !next.AutoDownload) || old.Revision != next.Revision
		if revoke {
			if e := w.CancelSubscription(r.Context(), id); e != nil {
				writeJSON(rw, 500, map[string]any{"detail": "could not revoke queued automatic downloads"})
				return
			}
		}
		out, e := w.Store.Update(r.Context(), id, next)
		if e != nil {
			writeJSON(rw, 400, map[string]any{"detail": e.Error()})
			return
		}
		writeJSON(rw, 200, out)
	})
	m.HandleFunc("DELETE /api/subscriptions/{id}", func(rw http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, e := w.Store.Get(r.Context(), id); e != nil {
			writeJSON(rw, 404, map[string]any{"detail": "subscription not found"})
			return
		}
		if e := w.CancelSubscription(r.Context(), id); e != nil {
			writeJSON(rw, 500, map[string]any{"detail": "could not cancel queued subscription downloads"})
			return
		}
		if e := w.Store.Delete(r.Context(), id); e != nil {
			writeJSON(rw, 404, map[string]any{"detail": "subscription not found"})
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	})
	m.HandleFunc("POST /api/subscriptions/{id}/cleanup", func(rw http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, err := w.Store.Get(r.Context(), id); err != nil {
			writeJSON(rw, 404, map[string]any{"detail": "subscription not found"})
			return
		}
		var body struct {
			Mode         string `json:"mode"`
			Acknowledged bool   `json:"acknowledged"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<16)).Decode(&body); err != nil || !body.Acknowledged {
			writeJSON(rw, 400, map[string]any{"detail": "cleanup requires a valid mode and acknowledged=true"})
			return
		}
		removed, err := w.Store.CleanupItems(r.Context(), id, body.Mode)
		if err != nil {
			writeJSON(rw, 400, map[string]any{"detail": err.Error()})
			return
		}
		writeJSON(rw, 200, map[string]any{"removed": removed})
	})
	m.HandleFunc("POST /api/subscriptions/{id}/run", func(rw http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, e := w.Store.Get(r.Context(), id); e != nil {
			writeJSON(rw, 404, map[string]any{"detail": "subscription not found"})
			return
		}
		if e := w.Run(id); e != nil {
			if errors.Is(e, subscription.ErrAlreadyRunning) {
				writeJSON(rw, http.StatusConflict, map[string]any{"status": "running"})
			} else if errors.Is(e, subscription.ErrFavoritesAuthUnavailable) {
				writeJSON(rw, http.StatusForbidden, map[string]any{"detail": "favorites require IPB cookies"})
			} else {
				writeJSON(rw, http.StatusInternalServerError, map[string]any{"detail": "could not start subscription run"})
			}
			return
		}
		writeJSON(rw, http.StatusAccepted, map[string]any{"status": "running"})
	})
	m.HandleFunc("GET /api/subscriptions/{id}/items", func(rw http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, e := w.Store.Get(r.Context(), id); e != nil {
			writeJSON(rw, 404, map[string]any{"detail": "subscription not found"})
			return
		}
		q := itemQuery(r, []string{id})
		items, total, e := w.ListItems(r.Context(), q)
		if e != nil {
			writeJSON(rw, 500, map[string]any{"detail": "could not list subscription items"})
			return
		}
		writeJSON(rw, 200, map[string]any{"items": items, "total": total, "page": q.Page, "per_page": q.PerPage, "run": latestRun(r, w, id)})
	})
	m.HandleFunc("GET /api/subscription-items", func(rw http.ResponseWriter, r *http.Request) {
		ids := splitIDs(r.URL.Query().Get("subscription_ids"))
		q := itemQuery(r, ids)
		items, total, e := w.ListItems(r.Context(), q)
		if e != nil {
			writeJSON(rw, 500, map[string]any{"detail": "could not list subscription items"})
			return
		}
		writeJSON(rw, 200, map[string]any{"items": items, "total": total, "page": q.Page, "per_page": q.PerPage})
	})
	m.HandleFunc("PATCH /api/subscriptions/{id}/items/{provider}/{gid}/{token}", func(rw http.ResponseWriter, r *http.Request) { patchItem(rw, r, w, []string{r.PathValue("id")}) })
	m.HandleFunc("PATCH /api/subscription-items/{provider}/{gid}/{token}", func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			SubscriptionIDs []string `json:"subscription_ids"`
			Inspected       *bool    `json:"inspected"`
			Hidden          *bool    `json:"hidden"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<16)).Decode(&body); e != nil {
			writeJSON(rw, 400, map[string]any{"detail": "invalid member update"})
			return
		}
		patchItemValues(rw, r, w, body.SubscriptionIDs, body.Inspected, body.Hidden)
	})
	m.HandleFunc("POST /api/subscription-items/batch-inspect", func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			Items []subscription.ItemRef `json:"items"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<20)).Decode(&body); e != nil {
			writeJSON(rw, 400, map[string]any{"detail": "invalid batch inspect request"})
			return
		}
		if len(body.Items) == 0 {
			writeJSON(rw, 200, map[string]any{"ok": true, "count": 0})
			return
		}
		if len(body.Items) > 200 {
			writeJSON(rw, 400, map[string]any{"detail": "batch items must contain at most 200 entries"})
			return
		}
		count, e := w.Store.MarkBatchInspected(r.Context(), body.Items)
		if e != nil {
			writeJSON(rw, 500, map[string]any{"detail": "could not mark batch inspected"})
			return
		}
		writeJSON(rw, 200, map[string]any{"ok": true, "count": count})
	})
	m.HandleFunc("GET /api/download-jobs", func(rw http.ResponseWriter, r *http.Request) {
		items, e := w.ListJobs(r.Context(), r.URL.Query().Get("status"))
		if e != nil {
			writeJSON(rw, 500, map[string]any{"detail": "could not list download jobs"})
			return
		}
		writeJSON(rw, 200, map[string]any{"items": items})
	})
	m.HandleFunc("POST /api/download-jobs/batch", func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			Items []struct {
				GID     int    `json:"gid"`
				Token   string `json:"token"`
				Quality string `json:"quality"`
				Force   bool   `json:"force"`
			} `json:"items"`
			Acknowledged bool `json:"acknowledged"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(rw, 400, map[string]any{"detail": "invalid batch request"})
			return
		}
		if !body.Acknowledged {
			writeJSON(rw, 400, map[string]any{"detail": "batch download requires acknowledged=true"})
			return
		}
		if len(body.Items) < 1 || len(body.Items) > 200 {
			writeJSON(rw, 400, map[string]any{"detail": "batch items must contain 1 to 200 entries"})
			return
		}
		// Validate the entire explicit batch before writing any paid task intent.
		for i, item := range body.Items {
			if item.GID < 1 || !archiveVersionToken.MatchString(item.Token) {
				writeJSON(rw, 400, map[string]any{"detail": fmt.Sprintf("items[%d] requires a valid gid and EH token", i)})
				return
			}
			if item.Quality != "" && item.Quality != "original" && item.Quality != "org" && item.Quality != "res" {
				writeJSON(rw, 400, map[string]any{"detail": fmt.Sprintf("items[%d] has unsupported quality", i)})
				return
			}
		}
		results := make([]map[string]any, 0, len(body.Items))
		for _, item := range body.Items {
			var activeID string
			activeErr := w.Store.DB().QueryRowContext(r.Context(), `SELECT id FROM download_jobs WHERE provider='eh' AND gid=? AND lower(token)=lower(?) AND status IN ('queued','running','needs_review')`, item.GID, item.Token).Scan(&activeID)
			if activeErr != nil && activeErr != sql.ErrNoRows {
				results = append(results, map[string]any{"gid": item.GID, "token": item.Token, "status": "error", "error": "could not inspect existing download job"})
				continue
			}
			jobID, err := w.QueueManualForce(r.Context(), item.GID, item.Token, item.Quality, item.Force)
			if err != nil {
				results = append(results, map[string]any{"gid": item.GID, "token": item.Token, "status": "error", "error": err.Error()})
				continue
			}
			state := "queued"
			if activeErr == nil && activeID == jobID {
				state = "already_active"
			}
			results = append(results, map[string]any{"gid": item.GID, "token": item.Token, "status": state, "job_id": jobID})
		}
		writeJSON(rw, http.StatusAccepted, map[string]any{"items": results})
	})
	m.HandleFunc("POST /api/download-jobs/{id}/cancel", func(rw http.ResponseWriter, r *http.Request) {
		if e := w.CancelJob(r.Context(), r.PathValue("id")); e != nil {
			writeJSON(rw, 409, map[string]any{"detail": e.Error()})
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	})
	m.HandleFunc("POST /api/download-jobs/{id}/review", func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			Resolution   string `json:"resolution"`
			Acknowledged bool   `json:"acknowledged"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<16)).Decode(&body); e != nil {
			writeJSON(rw, 400, map[string]any{"detail": "invalid download job review"})
			return
		}
		if e := w.ReviewJob(r.Context(), r.PathValue("id"), body.Resolution, body.Acknowledged); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				writeJSON(rw, 404, map[string]any{"detail": "download job not found"})
				return
			}
			writeJSON(rw, 409, map[string]any{"detail": e.Error()})
			return
		}
		writeJSON(rw, 200, map[string]any{"status": "reviewed"})
	})
}

func isKnownSubscriptionPreset(p string) bool {
	switch p {
	case "latest", "popular", "watched", "favorites",
		"toplist:yesterday", "toplist:month", "toplist:year", "toplist:alltime":
		return true
	}
	if strings.HasPrefix(p, "favorites:") {
		cat := strings.TrimPrefix(p, "favorites:")
		if len(cat) == 1 && cat[0] >= '0' && cat[0] <= '9' {
			return true
		}
	}
	return false
}

func normalizeSubscription(c *subscription.Config, settings config.Settings) error {
	if c.Query == nil {
		c.Query = map[string]any{}
	}
	if c.DownloadRules == nil {
		c.DownloadRules = map[string]any{}
	}
	if c.Source == "preset" {
		if c.Query["preset"] == nil {
			c.Query["preset"] = "latest"
		}
		preset := subscriptionString(c.Query["preset"])
		if !isKnownSubscriptionPreset(preset) {
			return fmt.Errorf("unsupported preset %q", preset)
		}
	}
	if c.Source != "search" && c.Source != "preset" {
		return errors.New("source must be search or preset")
	}
	if err := normalizeGalleryCategoryQuery(c, settings); err != nil {
		return err
	}
	if _, ok := c.Query["favorite_categories"]; ok {
		return errors.New("favorite_categories is no longer supported; choose a favorites preset")
	}
	sort := subscriptionString(c.Query["sort"])
	if sort != "" && sort != "newest" {
		return errors.New("supported sort is newest")
	}
	for _, key := range []string{"categories", "excluded_categories"} {
		if vals, ok := c.DownloadRules[key]; ok {
			if ids, err := categoryIDs(vals); err != nil || len(ids) > 0 {
				return errors.New("download category rules are not supported")
			}
		}
	}
	// All subscription sources use the same one-page rolling snapshot model.
	c.Query["max_pages"] = 1
	return nil
}

func normalizeGalleryCategoryQuery(c *subscription.Config, settings config.Settings) error {
	ids, err := categoryIDs(c.Query["categories"])
	if err != nil {
		return fmt.Errorf("categories: %w", err)
	}
	if _, exists := c.Query["categories"]; exists {
		c.Query["categories"] = ids
	}
	if len(ids) == 0 {
		delete(c.Query, "category_mask")
		return nil
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if id >= len(settings.Facets) {
			return fmt.Errorf("category id %d is unavailable", id)
		}
		names = append(names, settings.Facets[id].Name)
	}
	mask := eh.CategoryMask(names, settings)
	if mask == 0 {
		return errors.New("invalid gallery categories")
	}
	c.Query["category_mask"] = mask
	return nil
}
func categoryIDs(v any) ([]int, error) {
	if v == nil {
		return nil, nil
	}
	values, ok := v.([]any)
	if !ok {
		if ints, ok := v.([]int); ok {
			return ints, nil
		}
		return nil, errors.New("categories must be an array of integer IDs")
	}
	out := make([]int, 0, len(values))
	seen := map[int]bool{}
	for _, v := range values {
		n := number(v)
		if fv, ok := v.(float64); ok && fv != float64(n) {
			return nil, errors.New("category IDs must be integers from 0 to 9")
		}
		if n < 0 || n > 9 {
			return nil, errors.New("category IDs must be integers from 0 to 9")
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}
func number(v any) int {
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
func subscriptionString(v any) string { s, _ := v.(string); return s }
func itemQuery(r *http.Request, ids []string) subscription.ItemQuery {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "all"
	}
	return subscription.ItemQuery{SubscriptionIDs: ids, View: view, Q: r.URL.Query().Get("q"), Category: r.URL.Query().Get("category"), Page: page, PerPage: per}
}
func splitIDs(v string) []string {
	out := []string{}
	for _, id := range strings.Split(v, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}
func patchItem(wr http.ResponseWriter, r *http.Request, w *subscription.Worker, ids []string) {
	var body struct {
		Inspected *bool `json:"inspected"`
		Hidden    *bool `json:"hidden"`
	}
	if e := json.NewDecoder(http.MaxBytesReader(wr, r.Body, 1<<16)).Decode(&body); e != nil {
		writeJSON(wr, 400, map[string]any{"detail": "invalid member update"})
		return
	}
	patchItemValues(wr, r, w, ids, body.Inspected, body.Hidden)
}
func patchItemValues(wr http.ResponseWriter, r *http.Request, w *subscription.Worker, ids []string, inspected, hidden *bool) {
	if len(ids) == 0 {
		writeJSON(wr, 400, map[string]any{"detail": "subscription_ids required"})
		return
	}
	if r.PathValue("provider") != "eh" {
		writeJSON(wr, 404, map[string]any{"detail": "publication not found"})
		return
	}
	gid, e := strconv.Atoi(r.PathValue("gid"))
	token := r.PathValue("token")
	if e != nil || gid < 1 || token == "" {
		writeJSON(wr, 400, map[string]any{"detail": "invalid publication identity"})
		return
	}
	if e = w.Store.MarkItems(r.Context(), ids, gid, token, inspected, hidden); e != nil {
		writeJSON(wr, 404, map[string]any{"detail": "subscription item not found"})
		return
	}
	rw := map[string]any{"ok": true}
	writeJSON(wr, 200, rw)
}
func latestRun(r *http.Request, w *subscription.Worker, id string) any {
	return w.Store.LatestRun(r.Context(), id)
}
func mustMarshalRaw(m map[string]json.RawMessage) []byte { b, _ := json.Marshal(m); return b }

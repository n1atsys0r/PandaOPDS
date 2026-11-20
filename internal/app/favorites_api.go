package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"pandaopds/internal/eh"
	"pandaopds/internal/favorites"
	"pandaopds/internal/subscription"
)

type favoriteItem struct {
	gid   int
	token string
}

func (s *Server) favoriteAction(w http.ResponseWriter, r *http.Request) {
	if !s.Settings.HasIPB() {
		writeJSON(w, 403, map[string]any{"detail": "favorites require IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"})
		return
	}
	payload := map[string]any{}
	if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
		writeJSON(w, 422, map[string]any{"detail": "request body must be a JSON object"})
		return
	}
	action, _ := payload["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "add" && action != "move" && action != "remove" {
		writeJSON(w, 400, map[string]any{"detail": "'action' must be one of (add, move, remove)"})
		return
	}
	itemsRaw, provided := payload["items"]
	if !provided {
		gid, ok1 := payload["gid"]
		tok, ok2 := payload["token"].(string)
		if !ok1 || !ok2 || tok == "" {
			writeJSON(w, 400, map[string]any{"detail": "provide either 'items' [{gid, token}, ...] or a single 'gid' + 'token'"})
			return
		}
		itemsRaw = []any{map[string]any{"gid": gid, "token": tok}}
	}
	rawList, ok := itemsRaw.([]any)
	if !ok || len(rawList) == 0 {
		writeJSON(w, 400, map[string]any{"detail": "'items' must be a non-empty list"})
		return
	}
	if len(rawList) > 200 {
		writeJSON(w, 400, map[string]any{"detail": fmt.Sprintf("batch too large (%d > 200); split into chunks", len(rawList))})
		return
	}
	items := []eh.FavoriteItem{}
	for _, entry := range rawList {
		m, ok := entry.(map[string]any)
		if !ok {
			writeJSON(w, 400, map[string]any{"detail": "each item must be an object"})
			return
		}
		id, ok1 := numberToInt(m["gid"])
		token, ok2 := m["token"].(string)
		if !ok1 || !ok2 || token == "" {
			writeJSON(w, 400, map[string]any{"detail": "each item needs gid + token"})
			return
		}
		items = append(items, eh.FavoriteItem{GID: id, Token: token})
	}
	var favcat *int
	if action != "remove" {
		n, ok := numberToInt(payload["favcat"])
		if !ok {
			writeJSON(w, 400, map[string]any{"detail": "'favcat' is required for add/move"})
			return
		}
		favcat = &n
	}
	note := ""
	if value, ok := payload["note"]; ok && value != nil {
		note = fmt.Sprint(value)
	}
	results := s.Service.FavoriteAction(r.Context(), action, items, favcat, note)
	keys := map[string]bool{}
	for _, result := range results {
		if result["ok"] == true {
			keys[fmt.Sprintf("%v:%v", result["gid"], result["token"])] = true
		}
	}
	if len(keys) > 0 {
		localWarnings := []string{}
		for key := range keys {
			gidForMembership, tokenForMembership, validIdentity := favorites.ParseKey(key)
			if validIdentity && s.Subscriptions != nil {
				if e := s.Subscriptions.Store.RecordFavoriteAction(r.Context(), s.FavoriteSubscriptionID, gidForMembership, tokenForMembership, favcat, action == "remove"); e != nil {
					localWarnings = append(localWarnings, fmt.Sprintf("%s:%d: local cache update failed: %v", tokenForMembership, gidForMembership, e))
				}
			}
		}
		if s.Subscriptions != nil && s.FavoriteSubscriptionID != "" {
			if e := s.Subscriptions.Run(s.FavoriteSubscriptionID); e != nil && e != subscription.ErrAlreadyRunning {
				localWarnings = append(localWarnings, "favorites refresh could not be scheduled: "+e.Error())
			}
		}
		if len(localWarnings) > 0 {
			w.Header().Set("X-PandaOPDS-Local-Warning", "true")
		}
		// Include warnings in the response below without turning an already
		// successful upstream favorites action into an apparent failure.
		payload["_local_warnings"] = localWarnings
	}
	okCount := 0
	for _, result := range results {
		if result["ok"] == true {
			okCount++
		}
	}
	warnings, _ := payload["_local_warnings"].([]string)
	writeJSON(w, 200, map[string]any{"action": action, "ok": okCount == len(results), "ok_count": okCount, "items": results, "local_warnings": warnings})
}
func numberToInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), x == float64(int(x))
	case int:
		return x, true
	case int64:
		return int(x), int64(int(x)) == x
	case json.Number:
		n, e := x.Int64()
		return int(n), e == nil
	case string:
		n, e := strconv.Atoi(x)
		return n, e == nil
	}
	return 0, false
}
func (s *Server) favoriteCategories(w http.ResponseWriter, r *http.Request) {
	if !s.Settings.HasIPB() {
		writeJSON(w, 403, map[string]any{"detail": "favorites require IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"})
		return
	}
	cats, e := s.Service.FavoriteCategories(r.Context())
	if e != nil || len(cats) == 0 {
		if saved := s.Favorites.State.FavcatMap(); len(saved) > 0 {
			cats = saved
			e = nil
		}
	}
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	ids := make([]int, 0, len(cats))
	for id := range cats {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := []map[string]any{}
	for _, id := range ids {
		out = append(out, map[string]any{"id": id, "name": cats[id]})
	}
	writeJSON(w, 200, map[string]any{"categories": out})
}
func (s *Server) favoriteFavcats(w http.ResponseWriter, r *http.Request) {
	if !s.Settings.HasIPB() {
		writeJSON(w, 403, map[string]any{"detail": "favorites require IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"})
		return
	}
	cats, e := s.Service.FavoriteCategories(r.Context())
	if e != nil || len(cats) == 0 {
		if saved := s.Favorites.State.FavcatMap(); len(saved) > 0 {
			cats = saved
			e = nil
		}
	}
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	out := map[string]string{}
	for id, name := range cats {
		out[strconv.Itoa(id)] = name
	}
	writeJSON(w, 200, out)
}
func (s *Server) favoriteSyncStatus(w http.ResponseWriter, r *http.Request) {
	if s.Subscriptions == nil || s.FavoriteSubscriptionID == "" {
		writeJSON(w, 200, legacyFavoriteStatus(nil, true))
		return
	}
	c, err := s.Subscriptions.Store.Get(r.Context(), s.FavoriteSubscriptionID)
	if err != nil {
		writeJSON(w, 200, legacyFavoriteStatus(nil, true))
		return
	}
	run := s.Subscriptions.Store.LatestRun(r.Context(), c.ID)
	status := legacyFavoriteStatus(&c, false)
	status["scanned_pages"] = run["pages"]
	status["subscription_id"] = c.ID
	status["run"] = run
	writeJSON(w, 200, status)
}

func legacyFavoriteStatus(c *subscription.Config, disabled bool) map[string]any {
	status := map[string]any{"running": false, "disabled": disabled, "enabled": false, "interval_seconds": 0, "auto_archive": false, "categories": []int{}, "excluded_categories": []int{}, "baseline": false, "baseline_complete": false, "last_run": nil, "last_run_at": nil, "last_ok": nil, "last_success_at": nil, "last_error": nil, "scanned_pages": 0}
	if c == nil {
		return status
	}
	status["running"] = c.Running
	status["disabled"] = false
	status["enabled"] = c.Enabled
	status["interval_seconds"] = c.IntervalSeconds
	status["auto_archive"] = c.AutoDownload
	if c.DownloadRules["categories"] != nil {
		status["categories"] = c.DownloadRules["categories"]
	}
	if c.DownloadRules["excluded_categories"] != nil {
		status["excluded_categories"] = c.DownloadRules["excluded_categories"]
	}
	status["baseline"] = c.BaselineComplete
	status["baseline_complete"] = c.BaselineComplete
	status["last_run"] = c.LastRunAt
	status["last_run_at"] = c.LastRunAt
	status["last_ok"] = c.LastSuccessAt
	status["last_success_at"] = c.LastSuccessAt
	status["last_error"] = c.LastError
	return status
}
func (s *Server) favoriteSyncRun(w http.ResponseWriter, r *http.Request) {
	if !s.Settings.HasIPB() {
		writeJSON(w, 403, map[string]any{"detail": "favorites require IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"})
		return
	}
	if s.Subscriptions == nil || s.FavoriteSubscriptionID == "" {
		writeJSON(w, 409, map[string]any{"detail": "favorites subscription is unavailable"})
		return
	}
	if e := s.Subscriptions.Run(s.FavoriteSubscriptionID); e != nil && !errors.Is(e, subscription.ErrAlreadyRunning) {
		s.writeError(w, r, e)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "running", "subscription_id": s.FavoriteSubscriptionID})
}

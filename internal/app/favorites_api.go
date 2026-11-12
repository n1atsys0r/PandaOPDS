package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"pandaopds/internal/eh"
	"pandaopds/internal/favorites"
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
		known := s.Favorites.State.Known()
		archived := s.Favorites.State.Archived()
		errs := s.Favorites.State.Errors()
		favcats := s.Favorites.State.Favcats()
		for key := range keys {
			if action == "remove" {
				delete(known, key)
				delete(archived, key)
				delete(errs, key)
				delete(favcats, key)
				if gid, token, ok := favorites.ParseKey(key); ok && s.Store != nil && s.Archives != nil {
					if meta := s.Store.Get(gid, token); meta != nil && fmt.Sprint(meta["status"]) == "unarchived" {
						s.Archives.Remove(gid, token)
					}
				}
			} else {
				known[key] = true
				if favcat != nil {
					favcats[key] = *favcat
				}
			}
		}
		baseline := s.Favorites.State.Baseline()
		okRun := true
		_ = s.Favorites.State.Update(known, archived, errs, favcats, nil, &baseline, &okRun, nil)
		s.Favorites.RequestRun()
	}
	okCount := 0
	for _, result := range results {
		if result["ok"] == true {
			okCount++
		}
	}
	writeJSON(w, 200, map[string]any{"action": action, "ok": okCount == len(results), "ok_count": okCount, "items": results})
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
		writeError(w, e)
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
		writeError(w, e)
		return
	}
	out := map[string]string{}
	for id, name := range cats {
		out[strconv.Itoa(id)] = name
	}
	writeJSON(w, 200, out)
}
func (s *Server) favoriteSyncStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Favorites.Status())
}
func (s *Server) favoriteSyncRun(w http.ResponseWriter, r *http.Request) {
	if !s.Settings.HasIPB() {
		writeJSON(w, 403, map[string]any{"detail": "favorites require IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"})
		return
	}
	full := r.URL.Query().Get("full") == "1" || strings.EqualFold(r.URL.Query().Get("full"), "true")
	if !full && r.Body != nil {
		var body struct {
			Full bool `json:"full"`
		}
		if json.NewDecoder(r.Body).Decode(&body) == nil && body.Full {
			full = true
		}
	}
	v, e := s.Favorites.RunWithOptions(r.Context(), favorites.RunOptions{FullCatalog: full})
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}

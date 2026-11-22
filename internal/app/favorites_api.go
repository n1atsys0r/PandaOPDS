package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"pandaopds/internal/eh"
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

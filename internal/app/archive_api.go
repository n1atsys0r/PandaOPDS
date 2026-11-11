package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"pandaopds/internal/archive"
)

func (s *Server) archiveList(w http.ResponseWriter, r *http.Request) {
	entries := s.Archives.ListEntries()
	known := s.Favorites.State.Known()
	favcats := s.Favorites.State.Favcats()
	names := map[string]string{}
	for id, name := range s.Favorites.State.FavcatMap() {
		names[fmt.Sprint(id)] = name
	}
	enriched := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		gid := gidInt(entry["gid"])
		gidText := fmt.Sprint(gid)
		token := fmt.Sprint(entry["token"])
		key := gidText + ":" + token
		activeGID := gidInt(entry["active_gid"])
		activeToken, _ := entry["active_token"].(string)
		if activeGID == 0 || activeToken == "" {
			activeGID, activeToken = gid, token
		}
		snapshot, _ := s.Store.ReadMetadata(activeGID, activeToken)
		var meta any
		if snapshot != nil {
			meta = map[string]any{"title": snapshot["title"], "title_jpn": snapshot["title_jpn"], "category": snapshot["category"], "rating": snapshot["rating"], "filecount": snapshot["filecount"], "filesize": snapshot["filesize"], "posted": snapshot["posted"], "uploader": snapshot["uploader"], "expunged": snapshot["expunged"], "torrentcount": snapshot["torrentcount"], "tags": snapshot["tags"], "thumb": snapshot["thumb"]}
		}
		var cover any
		if _, ok := s.Store.Cover(activeGID, activeToken); ok {
			cover = fmt.Sprintf("/api/archive/%d/%s/cover", activeGID, activeToken)
		}
		var favcat any
		var favcatName any
		if n, ok := favcats[key]; ok {
			favcat = n
			favcatName = names[fmt.Sprint(n)]
		}
		card := map[string]any{}
		for k, v := range entry {
			card[k] = v
		}
		card["metadata"] = meta
		card["cover_url"] = cover
		card["is_favorited"] = known[key]
		card["favcat"] = favcat
		card["favcat_name"] = favcatName
		s.addArchiveUpdate(card, gid, token)
		enriched = append(enriched, card)
	}
	writeJSON(w, 200, map[string]any{"entries": enriched, "stats": s.Archives.Stats()})
}

func (s *Server) addArchiveUpdate(entry map[string]any, gid int, token string) {
	if target, ok := s.archiveUpdateTarget(gid, token); ok {
		entry["update_available"] = map[string]any{"gid": target.GID, "token": target.Token}
	}
}
func gidInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		var n int
		_, _ = fmt.Sscan(x, &n)
		return n
	}
	return 0
}
func (s *Server) archiveQuote(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	v, e := s.Archives.Quote(r.Context(), gid, token)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) archiveStart(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	var p struct {
		Quality string `json:"quality"`
		Force   bool   `json:"force"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&p)
	}
	v, e := s.Archives.Start(r.Context(), gid, token, p.Quality, p.Force)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) archiveStatus(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	v := s.Archives.Status(gid, token)
	if v["status"] == "absent" {
		writeJSON(w, 404, map[string]any{"detail": "no archive entry for this gallery"})
		return
	}
	s.addArchiveUpdate(v, gid, token)
	writeJSON(w, 200, v)
}
func (s *Server) archiveMetadata(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	v, e := s.Archives.Metadata(gid, token)
	if e != nil {
		writeJSON(w, 404, map[string]any{"detail": "no metadata snapshot for this gallery"})
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) archiveMetadataRefresh(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	v, e := s.Archives.Snapshot(r.Context(), gid, token, true)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) archiveRefresh(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	v, e := s.Archives.Refresh(r.Context(), gid, token)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) archiveUpdate(w http.ResponseWriter, r *http.Request) {
	gid, token, err := pathID(r)
	if err != nil {
		writeJSON(w, 422, map[string]any{"detail": err.Error()})
		return
	}
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(w, 415, map[string]any{"detail": "archive updates require application/json"})
		return
	}
	var confirmed struct {
		GID   int    `json:"gid"`
		Token string `json:"token"`
		MaxGP *int   `json:"max_gp"`
	}
	if r.Body == nil || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&confirmed) != nil || confirmed.GID <= 0 || !archiveVersionToken.MatchString(confirmed.Token) || confirmed.MaxGP == nil || *confirmed.MaxGP < 0 {
		writeJSON(w, 422, map[string]any{"detail": "a confirmed update gid, token, and nonnegative max_gp are required"})
		return
	}
	if s.Store.Get(gid, token) == nil {
		writeJSON(w, 404, map[string]any{"detail": "no archive entry for this gallery"})
		return
	}
	target, ok := s.archiveUpdateTarget(gid, token)
	if !ok || confirmed.GID != target.GID || confirmed.Token != target.Token {
		writeJSON(w, 409, map[string]any{"detail": "the confirmed update is no longer available"})
		return
	}
	v, err := s.Archives.StartVersionUpdate(r.Context(), gid, token, target.GID, target.Token, confirmed.MaxGP)
	if err != nil {
		var conflict *archive.VersionUpdateConflictError
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"detail": conflict.Detail})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) archiveDelete(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	if !s.Archives.Remove(gid, token) {
		writeJSON(w, 404, map[string]any{"detail": "no archive entry for this gallery"})
		return
	}
	writeJSON(w, 200, map[string]any{"removed": true})
}
func (s *Server) archiveCover(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	data, ok := s.Archives.Cover(gid, token)
	if !ok {
		writeJSON(w, 404, map[string]any{"detail": "no cover for this gallery"})
		return
	}
	mime := "image/jpeg"
	if snap, e := s.Archives.Metadata(gid, token); e == nil {
		if value, ok := snap["cover_mime"].(string); ok && strings.HasPrefix(value, "image/") {
			mime = value
		}
	}
	writeBytes(w, 200, mime, map[string]string{"Cache-Control": "public, max-age=86400"}, data)
}

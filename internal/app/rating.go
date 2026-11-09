package app

import (
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var galleryURLPattern = regexp.MustCompile(`^/g/(\d+)/([0-9a-fA-F]+)/?$`)

func (s *Server) rateGallery(w http.ResponseWriter, r *http.Request) {
	if !s.Settings.HasIPB() {
		writeJSON(w, 403, map[string]any{"detail": "rating requires IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"})
		return
	}
	var p map[string]any
	if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
		writeJSON(w, 422, map[string]any{"detail": "request body must be a JSON object"})
		return
	}
	rawURL, ok := p["url"].(string)
	if !ok {
		writeJSON(w, 400, map[string]any{"detail": "'url' must be a gallery URL"})
		return
	}
	u, e := url.Parse(rawURL)
	if e != nil || strings.ToLower(u.Scheme) != "https" || strings.ToLower(u.Host) != s.Settings.SiteHost() || u.User != nil || u.Port() != "" || u.Fragment != "" {
		writeJSON(w, 400, map[string]any{"detail": "'url' must be an official gallery URL for the configured site"})
		return
	}
	m := galleryURLPattern.FindStringSubmatch(u.Path)
	if len(m) < 3 {
		writeJSON(w, 400, map[string]any{"detail": "'url' must be an official gallery URL for the configured site"})
		return
	}
	gid, _ := strconv.Atoi(m[1])
	token := m[2]
	value, ok := p["rating"].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0.1 || value > 5 {
		writeJSON(w, 400, map[string]any{"detail": "'rating' must be a finite number from 0.1 to 5.0"})
		return
	}
	score := math.Floor(value*2+0.5) / 2
	if score < 0.5 {
		score = 0.5
	}
	if score > 5 {
		score = 5
	}
	result, e := s.Service.RateGallery(r.Context(), gid, token, score)
	if e != nil {
		writeError(w, e)
		return
	}
	out := map[string]any{"gid": gid, "token": token, "rating": score}
	for k, v := range result {
		out[k] = v
	}
	writeJSON(w, 200, out)
}

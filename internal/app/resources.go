package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"pandaopds/internal/eh"
)

const imageCache = "public, max-age=604800"

func (s *Server) streamPage(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	page, e := strconv.Atoi(r.PathValue("page"))
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": "page must be an integer"})
		return
	}
	if page < s.Settings.PSEPageBase {
		writeJSON(w, 400, map[string]any{"detail": fmt.Sprintf("page must be >= %d (PSE page base)", s.Settings.PSEPageBase)})
		return
	}
	if s.Documents != nil {
		if d, lookup := s.Documents.AssociatedDocument(r.Context(), gid, token); lookup == nil {
			data, typ, e := s.Documents.OpenPage(r.Context(), d.ID, page-s.Settings.PSEPageBase)
			if e == nil {
				writeBytes(w, 200, typ, map[string]string{"Cache-Control": "no-cache"}, data)
				return
			}
		}
	}
	data, mime, e := s.Service.GetImage(r.Context(), gid, token, page)
	if e != nil {
		if s.writeImageQuotaError(w, r, e, s.Settings.ExceedCooldownSeconds, false) {
			return
		}
		s.writeError(w, r, e)
		return
	}
	writeBytes(w, 200, mime, map[string]string{"Cache-Control": imageCache}, data)
}
func (s *Server) thumb(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	if s.Documents != nil {
		if d, lookup := s.Documents.AssociatedDocument(r.Context(), gid, token); lookup == nil {
			data, typ, e := s.Documents.OpenPage(r.Context(), d.ID, 0)
			if e == nil {
				writeBytes(w, 200, typ, map[string]string{"Cache-Control": "no-cache"}, data)
				return
			}
		}
	}
	data, mime, e := s.Service.GetThumb(r.Context(), gid, token)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	writeBytes(w, 200, mime, map[string]string{"Cache-Control": imageCache}, data)
}
func (s *Server) pageThumb(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	page, e := strconv.Atoi(r.PathValue("page"))
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": "page must be an integer"})
		return
	}
	if page < s.Settings.PSEPageBase {
		writeJSON(w, 400, map[string]any{"detail": fmt.Sprintf("page must be >= %d (PSE page base)", s.Settings.PSEPageBase)})
		return
	}
	data, _, e := s.Service.GetPageThumbnail(r.Context(), gid, token, page)
	if e != nil {
		if s.writeImageQuotaError(w, r, e, s.Settings.ExceedCooldownSeconds, false) {
			return
		}
		s.writeError(w, r, e)
		return
	}
	writeBytes(w, 200, "image/jpeg", map[string]string{"Cache-Control": imageCache}, data)
}
func (s *Server) imageFetch(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Port() != "" || !contains(s.Settings.ImageProxyHosts, strings.ToLower(u.Host)) {
		if s.Logger != nil {
			s.Logger.Printf("cover proxy rejected %s: invalid or unsupported url %q", r.URL.RequestURI(), raw)
		}
		writeJSON(w, 400, map[string]any{"detail": "unsupported image url"})
		return
	}
	data, mime, e := s.Service.FetchCoverBytes(r.Context(), raw)
	if e != nil {
		if s.writeImageQuotaError(w, r, e, 0, true) {
			return
		}
		s.writeError(w, r, e)
		return
	}
	writeBytes(w, 200, mime, map[string]string{"Cache-Control": imageCache}, data)
}

func (s *Server) writeImageQuotaError(w http.ResponseWriter, r *http.Request, err error, cooldown float64, noStore bool) bool {
	var upstream *eh.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusTooManyRequests {
		return false
	}
	headers := map[string]string{}
	if noStore {
		headers["Cache-Control"] = "no-store"
	} else {
		headers["Retry-After"] = fmt.Sprint(int(cooldown))
	}
	if s.Logger != nil {
		s.Logger.Printf("image quota exceeded %s %s: status=429 %s", r.Method, r.URL.RequestURI(), upstream.Detail)
	}
	writeJSONHeaders(w, http.StatusTooManyRequests, map[string]any{"detail": upstream.Detail}, headers)
	return true
}

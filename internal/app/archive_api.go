package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pandaopds/internal/archive"
	"pandaopds/internal/document"
)

func (s *Server) archiveList(w http.ResponseWriter, r *http.Request) {
	if s.Documents == nil {
		writeJSON(w, 200, map[string]any{"entries": []any{}, "stats": map[string]any{}})
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per < 1 {
		per = 200
	}
	if per > 200 {
		per = 200
	}
	docs, total, err := s.Documents.List(r.Context(), page, per, "all", "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	known := map[string]bool{}
	favcats := map[string]int{}
	if s.Subscriptions != nil && s.FavoriteSubscriptionID != "" {
		if members, e := s.Subscriptions.Store.FavoriteMemberships(r.Context(), s.FavoriteSubscriptionID); e == nil {
			for key, cat := range members {
				known[key] = true
				if cat >= 0 {
					favcats[key] = cat
				}
			}
		}
	}
	names := map[string]string{}
	for id, name := range s.Favorites.State.FavcatMap() {
		names[fmt.Sprint(id)] = name
	}
	enriched := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		archiveStatus := "unavailable"
		if d.Status == "available" {
			archiveStatus = "ready"
		}
		entry := map[string]any{"id": d.ID, "document_id": d.ID, "title": d.Title, "path": d.Path, "format": d.Format, "status": archiveStatus, "archive_status": archiveStatus, "document_status": d.Status, "size": d.Size, "page_count": d.PageCount, "association_source": d.AssociationSource, "manifest_url": "/opds/v2.0/documents/" + url.PathEscape(d.ID) + "/manifest", "cover_url": "/image/documents/" + url.PathEscape(d.ID) + "/thumb", "progression_url": documentProgressionURL(d)}
		gid := 0
		token := ""
		if d.GID != nil {
			gid = *d.GID
			token = d.Token
			entry["gid"] = gid
			entry["token"] = token
			entry["archive_status"] = map[bool]string{true: "ready", false: "unavailable"}[d.Status == "available"]
			var cached int
			if e := s.Database.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM covers c JOIN publications p ON p.id=c.publication_id WHERE p.provider='eh' AND p.gid=? AND p.token=?)`, gid, strings.ToLower(token)).Scan(&cached); e == nil && cached != 0 {
				entry["cover_url"] = fmt.Sprintf("/api/publications/eh/%d/%s/cover", gid, url.PathEscape(token))
			}
		}
		gidText := fmt.Sprint(gid)
		key := gidText + ":" + strings.ToLower(token)
		var favcat any
		var favcatName any
		if n, ok := favcats[key]; ok {
			favcat = n
			favcatName = names[fmt.Sprint(n)]
		}
		entry["is_favorited"] = gid > 0 && known[key]
		entry["favcat"] = favcat
		entry["favcat_name"] = favcatName
		if gid > 0 {
			s.addArchiveUpdate(entry, gid, token)
		}
		enriched = append(enriched, entry)
	}
	_, readyTotal, _ := s.Documents.List(r.Context(), 1, 1, "available", "")
	writeJSON(w, 200, map[string]any{"entries": enriched, "items": enriched, "total": total, "page": page, "per_page": per, "stats": map[string]any{"ready": readyTotal}})
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
		s.writeError(w, r, e)
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
	if s.Subscriptions == nil {
		writeJSON(w, 503, map[string]any{"detail": "download job service unavailable"})
		return
	}
	id, e := s.Subscriptions.QueueManualForce(r.Context(), gid, token, p.Quality, p.Force)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "job_id": id, "gid": gid, "token": token})
}
func (s *Server) archiveStatus(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	v := s.Archives.Status(gid, token)
	if s.Documents != nil {
		d, err := s.Documents.AssociatedDocument(r.Context(), gid, token)
		if err == nil {
			status := "unavailable"
			if d.Status == "available" {
				status = "ready"
			}
			v["status"] = status
			v["archive_status"] = status
			v["document_status"] = d.Status
			v["document_id"] = d.ID
			v["association_source"] = d.AssociationSource
			v["title"] = d.Title
			v["page_count"] = d.PageCount
			v["path"] = d.Path
		} else if !errors.Is(err, document.ErrNotFound) {
			s.writeError(w, r, err)
			return
		} else if !s.managedArchiveAllowed(r.Context(), gid, token) {
			v["status"] = "unarchived"
			v["archive_status"] = "unarchived"
		}
	}
	if v["status"] == "absent" || v["status"] == nil {
		writeJSON(w, 404, map[string]any{"detail": "no archive entry for this gallery"})
		return
	}
	s.addArchiveUpdate(v, gid, token)
	writeJSON(w, 200, v)
}

func (s *Server) managedArchiveAllowed(ctx context.Context, gid int, token string) bool {
	if s.Documents == nil || s.Store == nil {
		return true
	}
	path, _, e := s.Store.ArchivePublishPath(gid, token)
	if e != nil {
		return true
	}
	root, e := filepath.Abs(s.Settings.ArchiveDir)
	if e != nil {
		return false
	}
	rel, e := filepath.Rel(root, path)
	if e != nil {
		return false
	}
	return s.Documents.ManagedArchiveAllowed(ctx, filepath.ToSlash(rel), gid, token)
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
		s.writeError(w, r, e)
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
		s.writeError(w, r, e)
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
	if s.Subscriptions == nil {
		writeJSON(w, 503, map[string]any{"detail": "durable download worker is unavailable"})
		return
	}
	jobID, v, err := s.Subscriptions.SubmitManualVersionUpdate(r.Context(), gid, token, target.GID, target.Token, confirmed.MaxGP)
	if err != nil {
		var conflict *archive.VersionUpdateConflictError
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"detail": conflict.Detail})
			return
		}
		s.writeError(w, r, err)
		return
	}
	if v == nil {
		v = map[string]any{}
	}
	v["job_id"] = jobID
	writeJSON(w, 200, v)
}
func (s *Server) archiveDelete(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	if s.Documents == nil {
		writeJSON(w, 404, map[string]any{"detail": "no local document for this gallery"})
		return
	}
	d, e := s.Documents.AssociatedDocument(r.Context(), gid, token)
	if e != nil {
		writeJSON(w, 404, map[string]any{"detail": "no local document for this gallery"})
		return
	}
	if err := s.Documents.Delete(r.Context(), d.ID); err != nil {
		s.logArchiveDeleteFailure(gid, "document_delete", "delete local ZIP", err)
		writeJSON(w, 409, map[string]any{"detail": "could not remove local document"})
		return
	}
	writeJSON(w, 200, map[string]any{"removed": true, "documents_removed": 1, "retained_catalog": true})
}

func (s *Server) archiveUnlink(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	if s.Documents == nil {
		writeJSON(w, 404, map[string]any{"detail": "no associated document"})
		return
	}
	id, e := s.Documents.UnlinkGallery(r.Context(), gid, token)
	if e != nil {
		writeJSON(w, 404, map[string]any{"detail": "no associated document"})
		return
	}
	writeJSON(w, 200, map[string]any{"unlinked": true, "document_id": id, "file_preserved": true})
}

func (s *Server) logArchiveDeleteFailure(gid int, branch, stage string, err error) {
	if s.Logger != nil {
		s.Logger.Printf("archive delete failed: gid=%d branch=%s stage=%s: %v", gid, branch, stage, err)
	}
}

func hasAllowedCoverThumb(snapshot map[string]any, allowedHosts []string) bool {
	if snapshot == nil {
		return false
	}
	raw, _ := snapshot["thumb"].(string)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && u.Port() == "" && contains(allowedHosts, strings.ToLower(u.Host))
}

func (s *Server) archiveCover(w http.ResponseWriter, r *http.Request) {
	gid, token, e := pathID(r)
	if e != nil {
		writeJSON(w, 422, map[string]any{"detail": e.Error()})
		return
	}
	data, ok := s.Archives.Cover(gid, token)
	var fetchedMime string
	if !ok {
		snap, metaErr := s.Archives.Metadata(gid, token)
		if metaErr != nil || !hasAllowedCoverThumb(snap, s.Settings.ImageProxyHosts) {
			writeJSON(w, 404, map[string]any{"detail": "no cover for this gallery"})
			return
		}
		thumb, _ := snap["thumb"].(string)
		activeGID, activeToken := s.Store.ResolveActive(gid, token)
		type coverResult struct {
			data []byte
			mime string
		}
		val, fetchErr, _ := s.coverFlight.Do(fmt.Sprintf("%d:%s", activeGID, activeToken), func() (any, error) {
			if cached, found := s.Store.Cover(activeGID, activeToken); found {
				return coverResult{data: cached}, nil
			}
			fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
			defer cancel()
			bytes, mimeType, err := s.Service.FetchCoverBytes(fetchCtx, thumb)
			if err != nil {
				return nil, err
			}
			if len(bytes) == 0 {
				return nil, errors.New("empty cover response")
			}
			_ = s.Store.WriteCover(activeGID, activeToken, bytes)
			return coverResult{data: bytes, mime: mimeType}, nil
		})
		if fetchErr != nil {
			s.writeError(w, r, fetchErr)
			return
		}
		res := val.(coverResult)
		data, fetchedMime = res.data, res.mime
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		if strings.HasPrefix(fetchedMime, "image/") {
			mimeType = fetchedMime
		} else {
			mimeType = "image/jpeg"
		}
	}
	if snap, e := s.Archives.Metadata(gid, token); e == nil {
		if value, ok := snap["cover_mime"].(string); ok && strings.HasPrefix(value, "image/") {
			mimeType = value
		}
	}
	writeBytes(w, 200, mimeType, map[string]string{"Cache-Control": "public, max-age=86400"}, data)
}

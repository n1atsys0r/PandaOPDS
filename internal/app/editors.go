package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"

	"pandaopds/internal/config"
	"pandaopds/internal/home"
)

var configWireToEnv = map[string]string{"eh_site": "EH_SITE", "ipb_member_id": "IPB_MEMBER_ID", "ipb_pass_hash": "IPB_PASS_HASH", "igneous": "IGNEOUS", "public_base_url": "PUBLIC_BASE_URL", "pse_page_base": "PSE_PAGE_BASE", "tag_status_filter": "TAG_STATUS_FILTER", "tag_translation_enabled": "TAG_TRANSLATION_ENABLED", "comments_enabled": "COMMENTS_ENABLED", "image_cache_enabled": "IMAGE_CACHE_ENABLED", "cache_max_gb": "CACHE_MAX_GB", "favorites_sync_interval_seconds": "FAVORITES_SYNC_INTERVAL_SECONDS", "favorites_sync_archive": "FAVORITES_SYNC_ARCHIVE", "favorites_sync_categories": "FAVORITES_SYNC_CATEGORIES", "archive_update_interval_days": "ARCHIVE_UPDATE_INTERVAL_DAYS", "archive_auto_update": "ARCHIVE_AUTO_UPDATE"}

func readSnapshot(path string) (bool, []byte, error) {
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return false, nil, nil
	}
	if e != nil {
		return false, nil, e
	}
	return true, b, nil
}
func (s *Server) fileETag(kind string, exists bool, b []byte) string {
	key := s.editKey
	if len(key) == 0 {
		panic("editor ETag key not initialized")
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(kind))
	if exists {
		m.Write([]byte{1})
	} else {
		m.Write([]byte{0})
	}
	m.Write(b)
	return `"` + hex.EncodeToString(m.Sum(nil)) + `"`
}
func matchETag(header, current string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	target := strings.TrimPrefix(strings.TrimSpace(current), "W/")
	for _, part := range strings.Split(header, ",") {
		candidate := strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if candidate == target {
			return true
		}
	}
	return false
}
func sameSnapshot(path string, exists bool, b []byte) bool {
	nowExists, now, err := readSnapshot(path)
	return err == nil && exists == nowExists && string(now) == string(b)
}
func settingEnvLocked(k string) bool {
	if k == "ARCHIVE_UPDATE_INTERVAL_DAYS" || k == "ARCHIVE_AUTO_UPDATE" {
		return false
	}
	if k == "IPB_MEMBER_ID" || k == "IPB_PASS_HASH" {
		_, a := os.LookupEnv("IPB_MEMBER_ID")
		_, b := os.LookupEnv("IPB_PASS_HASH")
		return a || b
	}
	_, ok := os.LookupEnv(k)
	return ok
}
func configWireName(env string) string {
	for wire, key := range configWireToEnv {
		if key == env {
			return wire
		}
	}
	return env
}
func (s *Server) configEdit(w http.ResponseWriter, r *http.Request) {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	exists, snapshot, err := readSnapshot(s.settingsPath)
	if err != nil {
		writeJSON(w, 500, map[string]any{"detail": "could not read settings file"})
		return
	}
	if r.Method == http.MethodPatch {
		if !sameOriginJSON(w, r) {
			return
		}
		tag := s.fileETag("settings", exists, snapshot)
		if r.Header.Get("If-Match") == "" {
			writeJSON(w, 428, map[string]any{"detail": "If-Match required"})
			return
		}
		if !matchETag(r.Header.Get("If-Match"), tag) {
			writeJSON(w, 412, map[string]any{"detail": "settings changed; reload before editing"})
			return
		}
		var patch map[string]json.RawMessage
		if err := decodeJSON(w, r, &patch); err != nil || patch == nil {
			writeJSON(w, 400, map[string]any{"detail": "request must be a JSON object"})
			return
		}
		values := map[string]any{}
		if exists {
			values, err = config.DecodeEditableFile(snapshot)
		}
		if err != nil {
			writeJSON(w, 400, map[string]any{"detail": "settings file is invalid"})
			return
		}
		if _, a := patch["ipb_member_id"]; a {
			if _, b := patch["ipb_pass_hash"]; !b {
				writeJSON(w, 400, map[string]any{"detail": "IPB_MEMBER_ID and IPB_PASS_HASH must be changed together"})
				return
			}
		} else if _, b := patch["ipb_pass_hash"]; b {
			writeJSON(w, 400, map[string]any{"detail": "IPB_MEMBER_ID and IPB_PASS_HASH must be changed together"})
			return
		}
		if _, a := patch["ipb_member_id"]; a {
			memberRaw, hashRaw := patch["ipb_member_id"], patch["ipb_pass_hash"]
			if (string(memberRaw) == "null") != (string(hashRaw) == "null") {
				writeJSON(w, 400, map[string]any{"detail": "IPB credential pair must be two strings or two nulls"})
				return
			}
			if string(memberRaw) != "null" {
				var member, hash string
				if json.Unmarshal(memberRaw, &member) != nil || json.Unmarshal(hashRaw, &hash) != nil || strings.TrimSpace(member) == "" || strings.TrimSpace(hash) == "" {
					writeJSON(w, 400, map[string]any{"detail": "IPB credential pair must be two nonempty strings or two nulls"})
					return
				}
			}
		}
		for wireKey, raw := range patch {
			k, known := configWireToEnv[wireKey]
			if !known || !config.IsEditableKey(k) {
				writeJSON(w, 400, map[string]any{"detail": "unknown setting: " + wireKey})
				return
			}
			if settingEnvLocked(k) {
				writeJSON(w, 400, map[string]any{"detail": "setting is locked by environment: " + wireKey})
				return
			}
			if string(raw) == "null" {
				delete(values, k)
				continue
			}
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				writeJSON(w, 400, map[string]any{"detail": "invalid value for " + wireKey})
				return
			}
			if err := config.ValidateEditableValue(k, v); err != nil {
				writeJSON(w, 400, map[string]any{"detail": err.Error()})
				return
			}
			values[k] = v
		}
		if !sameSnapshot(s.settingsPath, exists, snapshot) {
			writeJSON(w, 412, map[string]any{"detail": "settings changed during save; reload before editing"})
			return
		}
		if err := config.WriteEditableFile(s.settingsPath, values); err != nil {
			writeJSON(w, 500, map[string]any{"detail": "could not save settings"})
			return
		}
	}
	if r.Method == http.MethodPatch {
		exists, snapshot, err = readSnapshot(s.settingsPath)
		if err != nil {
			writeJSON(w, 500, map[string]any{"detail": "could not read saved settings"})
			return
		}
	}
	shape := s.configEditShapeFromSnapshot(exists, snapshot)
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("ETag", s.fileETag("settings", exists, snapshot))
	writeJSON(w, 200, shape)
}

func (s *Server) configEditShapeFromSnapshot(exists bool, snapshot []byte) map[string]any {
	active := s.Settings
	raw := map[string]any{}
	var err error
	if exists {
		raw, err = config.DecodeEditableFile(snapshot)
	}
	savedSettings := config.LoadWithEditableValues(raw)
	fields := map[string]any{}
	for _, k := range config.EditableKeys() {
		v := config.EditableValue(active, k)
		saved := config.EditableValue(savedSettings, k)
		source := "default"
		if _, ok := raw[k]; ok {
			source = "file"
		}
		locked := settingEnvLocked(k)
		if locked {
			source = "env"
		}
		f := map[string]any{"active": v, "saved": saved, "source": source, "locked": locked, "pending": !reflect.DeepEqual(v, saved)}
		if k == "IPB_MEMBER_ID" || k == "IPB_PASS_HASH" || k == "IGNEOUS" {
			f["active"] = nil
			f["saved"] = nil
			f["set"] = v != ""
			f["saved_set"] = fmt.Sprint(saved) != ""
		}
		fields[configWireName(k)] = f
	}
	out := map[string]any{"fields": fields, "pending": false}
	for _, f := range fields {
		if f.(map[string]any)["pending"] == true {
			out["pending"] = true
		}
	}
	if err != nil {
		out["error"] = "settings file is invalid"
	}
	return out
}

func sameOriginJSON(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaErr != nil || !strings.EqualFold(mediaType, "application/json") {
		writeJSON(w, 400, map[string]any{"detail": "Content-Type must be application/json"})
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host || !strings.EqualFold(u.Scheme, requestScheme(r)) {
			writeJSON(w, 403, map[string]any{"detail": "cross-origin edit rejected"})
			return false
		}
	}
	return true
}
func requestScheme(r *http.Request) string {
	if v := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); v != "" {
		return strings.ToLower(v)
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return fmt.Errorf("request must contain one JSON value")
	}
	return nil
}

func (s *Server) homeEdit(w http.ResponseWriter, r *http.Request) {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	exists, snapshot, err := readSnapshot(s.Settings.HomeConfigPath)
	if err != nil {
		writeJSON(w, 500, map[string]any{"detail": "could not read home layout"})
		return
	}
	if r.Method == http.MethodPut {
		if !sameOriginJSON(w, r) {
			return
		}
		tag := s.fileETag("home", exists, snapshot)
		if r.Header.Get("If-Match") == "" {
			writeJSON(w, 428, map[string]any{"detail": "If-Match required"})
			return
		}
		if !matchETag(r.Header.Get("If-Match"), tag) {
			writeJSON(w, 412, map[string]any{"detail": "home layout changed; reload before editing"})
			return
		}
		_, err := home.DecodeEditor(snapshot)
		if err != nil {
			writeJSON(w, 400, map[string]any{"detail": "home configuration is invalid"})
			return
		}
		// A decodable legacy layout may violate current output-shape rules. Permit
		// replacing it with a valid layout; the original is backed up on save.
		var c home.Config
		if e := decodeJSON(w, r, &c); e != nil || c.Sections == nil {
			writeJSON(w, 400, map[string]any{"detail": "sections must be a JSON array"})
			return
		}
		if e := home.Validate(c); e != nil {
			writeJSON(w, 400, map[string]any{"detail": e.Error()})
			return
		}
		if !sameSnapshot(s.Settings.HomeConfigPath, exists, snapshot) {
			writeJSON(w, 412, map[string]any{"detail": "home layout changed during save; reload before editing"})
			return
		}
		if e := home.SaveSnapshot(s.Settings.HomeConfigPath, c, exists, snapshot); e != nil {
			writeJSON(w, 500, map[string]any{"detail": "could not save home layout"})
			return
		}
	}
	if r.Method == http.MethodPut {
		exists, snapshot, err = readSnapshot(s.Settings.HomeConfigPath)
		if err != nil {
			writeJSON(w, 500, map[string]any{"detail": "could not read saved home layout"})
			return
		}
	}
	shape := s.homeEditShapeFromSnapshot(exists, snapshot)
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("ETag", s.fileETag("home", exists, snapshot))
	writeJSON(w, 200, shape)
}
func (s *Server) homeEditShapeFromSnapshot(exists bool, snapshot []byte) map[string]any {
	c := home.Default()
	var parseErr, validationErr error
	if exists {
		c, parseErr = home.DecodeEditor(snapshot)
		if parseErr == nil {
			validationErr = home.Validate(c)
		}
	}
	sections := []map[string]any{}
	for _, v := range c.Sections {
		var count any
		if v.Count != nil {
			count = *v.Count
		}
		sections = append(sections, map[string]any{"group": v.Group, "kind": v.Kind, "title": v.Title, "type": v.Type, "query": v.Query, "count": count})
	}
	out := map[string]any{"path": s.Settings.HomeConfigPath, "using_file": exists}
	if parseErr == nil { out["sections"] = sections }
	if parseErr != nil {
		out["error"] = parseErr.Error()
	} else if validationErr != nil {
		out["validation_error"] = validationErr.Error()
	}
	return out
}

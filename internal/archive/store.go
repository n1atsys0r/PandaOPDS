package archive

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pandaopds/internal/eh"
)

const (
	metaName     = "meta.json"
	zipName      = "archive.zip"
	partName     = "archive.part"
	metadataName = "metadata.json"
	coverName    = "cover.jpg"
	versionsName = "versions"
)

type rootRecord struct {
	key    string
	dir    string
	legacy bool
	meta   map[string]any
}

// Store keeps one shelf record per original gallery. A flat shelf uses
// ARCHIVE_DIR/{original gid}/meta.json and ARCHIVE_DIR/{original gid}/{actual gid}.zip.
// Old {gid}/{token}/ shelves remain in place and continue using archive.zip.
type Store struct {
	Root         string
	mu           sync.RWMutex
	roots        map[string]*rootRecord
	identityRoot map[string]string
	conflicts    map[string]bool
}

func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	s := &Store{Root: root, roots: map[string]*rootRecord{}, identityRoot: map[string]string{}, conflicts: map[string]bool{}}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != metaName {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return nil
		}
		meta := map[string]any{}
		if json.Unmarshal(b, &meta) != nil {
			return nil
		}
		gid, token := toInt(meta["gid"]), toString(meta["token"])
		if gid <= 0 || token == "" {
			return nil
		}
		flat := filepath.Dir(path) == filepath.Join(root, strconv.Itoa(gid)) && meta["layout"] == "flat"
		rec := &rootRecord{key: key(gid, token), dir: filepath.Dir(path), legacy: !flat, meta: meta}
		if toInt(meta["active_gid"]) <= 0 || toString(meta["active_token"]) == "" {
			meta["active_gid"], meta["active_token"] = gid, token
		}
		if _, exists := s.roots[rec.key]; exists {
			s.conflicts[rec.key] = true
			return nil
		}
		s.roots[rec.key] = rec
		s.registerIdentityLocked(rec, gid, token)
		for _, version := range versionRecords(meta) {
			vgid, vtoken := toInt(version["gid"]), toString(version["token"])
			if vgid > 0 && vtoken != "" {
				s.registerIdentityLocked(rec, vgid, vtoken)
			}
		}
		return nil
	})
	// Finish or roll back a partially published active snapshot before exposing
	// the store to readers.
	for _, rec := range s.roots {
		_ = s.recoverActivationLocked(rec)
	}
	return s, nil
}

func key(gid int, token string) string { return strconv.Itoa(gid) + ":" + token }
func parseKey(k string) (int, string, bool) {
	parts := strings.SplitN(k, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, "", false
	}
	gid, err := strconv.Atoi(parts[0])
	return gid, parts[1], err == nil && gid > 0
}

func versionRecords(meta map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	versions, _ := meta[versionsName].(map[string]any)
	for k, raw := range versions {
		if m, ok := raw.(map[string]any); ok {
			out[k] = m
		}
	}
	return out
}

func (s *Store) registerIdentityLocked(rec *rootRecord, gid int, token string) {
	id := key(gid, token)
	if prior, exists := s.identityRoot[id]; exists && prior != rec.key {
		delete(s.identityRoot, id)
		s.conflicts[id] = true
		return
	}
	if !s.conflicts[id] {
		s.identityRoot[id] = rec.key
	}
}

func (s *Store) rootLocked(gid int, token string) *rootRecord {
	if s.conflicts[key(gid, token)] {
		return nil
	}
	rec := s.roots[s.identityRoot[key(gid, token)]]
	if rec != nil && !s.flatOwnedLocked(rec) {
		return nil
	}
	return rec
}

func (s *Store) flatOwnedLocked(rec *rootRecord) bool {
	if rec == nil {
		return false
	}
	metaPath := filepath.Join(rec.dir, metaName)
	b, err := os.ReadFile(metaPath)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		return false
	}
	var disk map[string]any
	if json.Unmarshal(b, &disk) != nil {
		return false
	}
	if toInt(disk["gid"]) != toInt(rec.meta["gid"]) || toString(disk["token"]) != toString(rec.meta["token"]) {
		return false
	}
	if rec.legacy {
		return disk["layout"] != "flat"
	}
	return disk["layout"] == "flat"
}

func (s *Store) rootIdentity(rec *rootRecord) (int, string) {
	return toInt(rec.meta["gid"]), toString(rec.meta["token"])
}

func (s *Store) RootIdentity(gid int, token string) (int, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	if rec == nil {
		return 0, "", false
	}
	rootGID, rootToken := s.rootIdentity(rec)
	return rootGID, rootToken, true
}

func (s *Store) Identities(rootGID int, rootToken string) [][2]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(rootGID, rootToken)
	if rec == nil || rec.key != key(rootGID, rootToken) {
		return nil
	}
	out := [][2]string{{strconv.Itoa(rootGID), rootToken}}
	for id := range versionRecords(rec.meta) {
		gid, token, ok := parseKey(id)
		if ok && (gid != rootGID || token != rootToken) {
			out = append(out, [2]string{strconv.Itoa(gid), token})
		}
	}
	return out
}

func (s *Store) activeIdentity(rec *rootRecord) (int, string) {
	gid, token := toInt(rec.meta["active_gid"]), toString(rec.meta["active_token"])
	if gid <= 0 || token == "" {
		return s.rootIdentity(rec)
	}
	return gid, token
}

// dir is retained for manager tests and migration code. Flat shelves resolve
// to the stable root directory; legacy shelves resolve to their original path.
func (s *Store) dir(gid int, token string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if rec := s.rootLocked(gid, token); rec != nil {
		return rec.dir
	}
	return filepath.Join(s.Root, strconv.Itoa(gid), token)
}

func (s *Store) archivePathLocked(rec *rootRecord, gid int) string {
	if rec.legacy && gid == toInt(rec.meta["gid"]) {
		return filepath.Join(rec.dir, zipName)
	}
	return filepath.Join(rec.dir, strconv.Itoa(gid)+".zip")
}

func (s *Store) archivePath(gid int, token string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	if rec == nil {
		return ""
	}
	return s.archivePathLocked(rec, gid)
}

func (s *Store) ArchivePublishPath(gid int, token string) (string, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	if rec == nil || s.versionStateLocked(rec, gid, token) == nil || !s.flatOwnedLocked(rec) {
		return "", "", errors.New("archive identity or ownership conflict")
	}
	target := s.archivePathLocked(rec, gid)
	return target, target + ".publish.tmp", nil
}

func (s *Store) PublishArchive(gid int, token, publishPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.rootLocked(gid, token)
	if rec == nil || s.versionStateLocked(rec, gid, token) == nil || !s.flatOwnedLocked(rec) {
		return errors.New("archive identity or ownership conflict")
	}
	target := s.archivePathLocked(rec, gid)
	if filepath.Clean(publishPath) != filepath.Clean(target+".publish.tmp") {
		return errors.New("archive publish path does not belong to this identity")
	}
	return os.Rename(publishPath, target)
}

func (s *Store) metadataPathLocked(rec *rootRecord, gid int, token string) (string, bool) {
	if rec == nil {
		return "", false
	}
	if pending, _ := rec.meta["activation_pending"].(map[string]any); pending != nil {
		return "", false
	}
	activeGID, activeToken := s.activeIdentity(rec)
	if gid == activeGID && token == activeToken {
		if !s.flatOwnedLocked(rec) {
			return "", false
		}
		return filepath.Join(rec.dir, metadataName), true
	}
	return "", false
}

func (s *Store) persistLocked(rec *rootRecord) error {
	if rec == nil {
		return errors.New("archive root not found")
	}
	if !s.flatOwnedLocked(rec) {
		return errors.New("archive root ownership conflict")
	}
	if err := os.MkdirAll(rec.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rec.meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(rec.dir, metaName+".tmp")
	if err = os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err = os.Rename(tmp, filepath.Join(rec.dir, metaName)); err != nil {
		return err
	}
	return nil
}

func (s *Store) Get(gid int, token string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	if rec == nil {
		return nil
	}
	rootGID, rootToken := s.rootIdentity(rec)
	activeGID, activeToken := s.activeIdentity(rec)
	var out map[string]any
	if gid == rootGID && token == rootToken || gid == activeGID && token == activeToken {
		out = cloneMap(rec.meta)
	} else if rec.legacy {
		out = cloneMap(rec.meta)
	} else {
		out = cloneMap(versionRecords(rec.meta)[key(gid, token)])
	}
	if out == nil {
		return nil
	}
	out["root_gid"], out["root_token"] = rootGID, rootToken
	out["active_gid"], out["active_token"] = activeGID, activeToken
	if gid == rootGID && token == rootToken {
		if update, ok := rec.meta["version_update"]; ok {
			out["version_update"] = update
		}
	}
	pathGID := gid
	if gid == rootGID && token == rootToken {
		pathGID = activeGID
	}
	if st, e := os.Stat(s.archivePathLocked(rec, pathGID)); e == nil {
		out["archive_size"] = st.Size()
	} else {
		out["archive_size"] = int64(0)
	}
	if gid == rootGID && token == rootToken {
		if st, e := os.Stat(filepath.Join(rec.dir, partName)); e == nil {
			out["part_size"] = st.Size()
		} else {
			out["part_size"] = int64(0)
		}
	} else if st, e := os.Stat(filepath.Join(s.Root, strconv.Itoa(gid), token, partName)); e == nil {
		out["part_size"] = st.Size()
	} else {
		out["part_size"] = int64(0)
	}
	return out
}

func (s *Store) Upsert(gid int, token string, patch map[string]any) (map[string]any, error) {
	if gid <= 0 || token == "" {
		return nil, errors.New("invalid archive identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := key(gid, token)
	if s.conflicts[id] {
		return nil, errors.New("archive identity conflicts with another shelf")
	}
	rec := s.rootLocked(gid, token)
	if rec == nil {
		requestedRootGID, requestedRootToken := toInt(patch["root_gid"]), toString(patch["root_token"])
		if requestedRootGID > 0 && requestedRootToken != "" {
			candidate := s.rootLocked(requestedRootGID, requestedRootToken)
			if candidate == nil || candidate.key != key(requestedRootGID, requestedRootToken) {
				return nil, errors.New("archive root identity not found")
			}
			rec = candidate
		}
	}
	if rec == nil {
		rootDir := filepath.Join(s.Root, strconv.Itoa(gid))
		if b, err := os.ReadFile(filepath.Join(rootDir, metaName)); err == nil {
			var disk map[string]any
			if json.Unmarshal(b, &disk) != nil || toInt(disk["gid"]) != gid || toString(disk["token"]) != token || disk["layout"] != "flat" {
				return nil, errors.New("archive root ownership conflict")
			}
		}
		for _, existing := range s.roots {
			if toInt(existing.meta["gid"]) == gid && toString(existing.meta["token"]) != token {
				return nil, errors.New("archive gid already belongs to another token")
			}
		}
		if _, err := os.Stat(filepath.Join(rootDir, strconv.Itoa(gid)+".zip")); err == nil {
			return nil, errors.New("archive file already exists without matching shelf ownership")
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		now := float64(time.Now().Unix())
		meta := map[string]any{"gid": gid, "token": token, "root_gid": gid, "root_token": token, "active_gid": gid, "active_token": token, "layout": "flat", "created_at": now, "versions": map[string]any{}}
		rec = &rootRecord{key: id, dir: rootDir, meta: meta}
		s.roots[id] = rec
		s.registerIdentityLocked(rec, gid, token)
	}
	if !s.flatOwnedLocked(rec) {
		return nil, errors.New("archive root ownership conflict")
	}
	rootGID, rootToken := s.rootIdentity(rec)
	if id != rec.key && s.identityRoot[id] != rec.key {
		if prior := s.rootLocked(gid, token); prior != nil && prior.key != rec.key {
			return nil, errors.New("target gallery already belongs to another shelf")
		}
		for identity, owner := range s.identityRoot {
			priorGID, priorToken, ok := parseKey(identity)
			if ok && priorGID == gid && priorToken != token && owner != rec.key {
				return nil, errors.New("target gallery gid already belongs to another token")
			}
		}
		if _, err := os.Stat(s.archivePathLocked(rec, gid)); err == nil {
			return nil, errors.New("target archive file already exists without matching shelf ownership")
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		s.registerIdentityLocked(rec, gid, token)
	}
	now := float64(time.Now().Unix())
	base := cloneMap(rec.meta)
	if base == nil {
		base = map[string]any{"created_at": now}
	}
	base["gid"], base["token"] = gid, token
	base["updated_at"] = now
	versions := versionRecords(rec.meta)
	state := cloneMap(versions[id])
	if state == nil {
		state = map[string]any{"gid": gid, "token": token, "created_at": now}
	}
	for k, v := range patch {
		state[k] = v
	}
	state["gid"], state["token"] = gid, token
	state["updated_at"] = now
	versions[id] = state
	if state["is_version_update"] == true {
		rec.meta["version_update"] = map[string]any{"gid": gid, "token": token, "status": state["status"], "error": state["error"]}
	}
	if id == key(toInt(rec.meta["active_gid"]), toString(rec.meta["active_token"])) {
		for k, v := range patch {
			base[k] = v
		}
	}
	base["gid"], base["token"] = rootGID, rootToken
	base["root_gid"], base["root_token"] = rootGID, rootToken
	if !rec.legacy {
		base["layout"] = rec.meta["layout"]
	}
	base[versionsName] = versionMapAny(versions)
	if summary, ok := rec.meta["version_update"]; ok {
		base["version_update"] = summary
	}
	if toInt(base["active_gid"]) <= 0 || toString(base["active_token"]) == "" {
		base["active_gid"], base["active_token"] = rootGID, rootToken
	}
	rec.meta = base
	if err := s.persistLocked(rec); err != nil {
		return nil, err
	}
	return cloneMap(state), nil
}

func versionMapAny(in map[string]map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

func (s *Store) ResolveActive(gid int, token string) (int, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	if rec == nil {
		return gid, token
	}
	rootGID, rootToken := s.rootIdentity(rec)
	if gid == rootGID && token == rootToken {
		return s.activeIdentity(rec)
	}
	return gid, token
}

// CanStart checks an archive identity and its flat path before the manager
// performs any potentially paid upstream submission.
func (s *Store) CanStart(gid int, token string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id := key(gid, token)
	if s.conflicts[id] {
		return errors.New("archive identity conflicts with another shelf")
	}
	if rec := s.rootLocked(gid, token); rec != nil {
		return nil
	}
	for _, rec := range s.roots {
		if toInt(rec.meta["gid"]) == gid && toString(rec.meta["token"]) != token {
			return errors.New("archive gid already belongs to another token")
		}
	}
	rootDir := filepath.Join(s.Root, strconv.Itoa(gid))
	if _, err := os.Stat(filepath.Join(rootDir, metaName)); err == nil {
		return errors.New("archive root metadata exists with unknown ownership")
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(filepath.Join(rootDir, strconv.Itoa(gid)+".zip")); err == nil {
		return errors.New("archive file already exists without matching shelf ownership")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) IsReady(gid int, token string) bool {
	m := s.Get(gid, token)
	return m != nil && m["status"] == "ready"
}

func (s *Store) ReadyCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, rec := range s.roots {
		if !s.flatOwnedLocked(rec) {
			continue
		}
		if rec.meta["status"] == "ready" {
			n++
		}
	}
	return n
}

func (s *Store) ListReady(page, perPage int) []eh.ArchiveInfo {
	if page < 1 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 100
	}
	s.mu.RLock()
	all := []eh.ArchiveInfo{}
	for _, rec := range s.roots {
		if !s.flatOwnedLocked(rec) {
			continue
		}
		m := rec.meta
		if m["status"] != "ready" {
			continue
		}
		rootGID, rootToken := s.rootIdentity(rec)
		activeGID, activeToken := s.activeIdentity(rec)
		item := eh.ArchiveInfo{GID: rootGID, Token: rootToken, ActiveGID: activeGID, ActiveToken: activeToken, Title: toString(m["title"]), PageCount: toInt(m["page_count"]), CreatedAt: toInt64(m["created_at"]), Quality: toString(m["quality"]), Status: "ready", MetadataAt: toInt64(m["metadata_at"])}
		if item.PageCount == 0 {
			item.PageCount = s.pageCountLocked(rec, activeGID)
		}
		all = append(all, item)
	}
	s.mu.RUnlock()
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt > all[j].CreatedAt })
	start := (page - 1) * perPage
	if start >= len(all) {
		return []eh.ArchiveInfo{}
	}
	end := start + perPage
	if end > len(all) {
		end = len(all)
	}
	return all[start:end]
}

func (s *Store) pageCountLocked(rec *rootRecord, gid int) int {
	r, err := zip.OpenReader(s.archivePathLocked(rec, gid))
	if err != nil {
		return 0
	}
	defer r.Close()
	return len(r.File)
}

func (s *Store) PageCount(gid int, token string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	if rec == nil {
		return 0
	}
	return s.pageCountLocked(rec, gid)
}

func (s *Store) GetPage(gid int, token string, page int) ([]byte, bool, error) {
	if page < 0 {
		return nil, false, nil
	}
	s.mu.RLock()
	rec := s.rootLocked(gid, token)
	if rec == nil || s.versionStateLocked(rec, gid, token)["status"] != "ready" {
		s.mu.RUnlock()
		return nil, false, nil
	}
	p := s.archivePathLocked(rec, gid)
	s.mu.RUnlock()
	r, err := zip.OpenReader(p)
	if err != nil {
		return nil, false, err
	}
	defer r.Close()
	if page >= len(r.File) {
		return nil, false, nil
	}
	f, err := r.File[page].Open()
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	return b, err == nil, err
}

func (s *Store) versionStateLocked(rec *rootRecord, gid int, token string) map[string]any {
	if rec == nil {
		return nil
	}
	if gid == toInt(rec.meta["gid"]) && token == toString(rec.meta["token"]) {
		return rec.meta
	}
	versions, _ := rec.meta[versionsName].(map[string]any)
	state, _ := versions[key(gid, token)].(map[string]any)
	return state
}

func (s *Store) writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) WriteMetadata(gid int, token string, payload any) error {
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.rootLocked(gid, token)
	path, ok := s.metadataPathLocked(rec, gid, token)
	if !ok {
		return errors.New("metadata identity is not the active archive version")
	}
	if !s.flatOwnedLocked(rec) {
		return errors.New("archive root ownership conflict")
	}
	return s.writeAtomic(path, b)
}

func (s *Store) ReadMetadata(gid int, token string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	path, ok := s.metadataPathLocked(rec, gid, token)
	if !ok {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	meta := map[string]any{}
	err = json.Unmarshal(b, &meta)
	return meta, err
}

func (s *Store) Cover(gid int, token string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	path, ok := s.metadataPathLocked(rec, gid, token)
	if !ok {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(path), coverName))
	return b, err == nil
}

func (s *Store) GetCover(gid int, token string) ([]byte, bool) { return s.Cover(gid, token) }
func (s *Store) GetMetadataSnapshot(gid int, token string) (map[string]any, bool) {
	m, err := s.ReadMetadata(gid, token)
	return m, err == nil
}

func (s *Store) WriteCover(gid int, token string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.rootLocked(gid, token)
	path, ok := s.metadataPathLocked(rec, gid, token)
	if !ok {
		return errors.New("cover identity is not the active archive version")
	}
	if !s.flatOwnedLocked(rec) {
		return errors.New("archive root ownership conflict")
	}
	return s.writeAtomic(filepath.Join(filepath.Dir(path), coverName), b)
}

func (s *Store) CurrentUpdate(gid int, token string) (int, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	if rec == nil || (gid != toInt(rec.meta["gid"]) || token != toString(rec.meta["token"])) {
		return 0, ""
	}
	activeGID, activeToken := s.activeIdentity(rec)
	path, ok := s.metadataPathLocked(rec, activeGID, activeToken)
	if !ok {
		return 0, ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, ""
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return 0, ""
	}
	targetGID, targetToken := toInt(m["current_gid"]), toString(m["current_key"])
	if targetGID <= 0 || targetToken == "" || targetGID == activeGID && targetToken == activeToken {
		return 0, ""
	}
	return targetGID, targetToken
}

// UpdateVersionLinks refreshes only gdata's relationship fields in the current
// snapshot. If no snapshot exists, it seeds a normalized metadata snapshot.
func (s *Store) UpdateVersionLinks(gid int, token string, metadata eh.GalleryMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.rootLocked(gid, token)
	if rec == nil {
		return errors.New("archive root not found")
	}
	activeGID, activeToken := s.activeIdentity(rec)
	if metadata.GID != activeGID || metadata.Token != activeToken {
		return nil
	}
	path, ok := s.metadataPathLocked(rec, activeGID, activeToken)
	if !ok {
		return nil
	}
	current := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &current)
	}
	if len(current) == 0 {
		b, _ := json.Marshal(metadata)
		_ = json.Unmarshal(b, &current)
		current["gid"], current["token"] = activeGID, activeToken
		current["saved_at"] = float64(time.Now().Unix())
	}
	current["parent_gid"], current["parent_key"] = metadata.ParentGID, metadata.ParentKey
	current["first_gid"], current["first_key"] = metadata.FirstGID, metadata.FirstKey
	current["current_gid"], current["current_key"] = metadata.CurrentGID, metadata.CurrentKey
	if !s.flatOwnedLocked(rec) {
		return errors.New("archive root ownership conflict")
	}
	b, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	if err = s.writeAtomic(path, b); err != nil {
		return err
	}
	return nil
}

func (s *Store) CanAddVersion(rootGID int, rootToken string, targetGID int, targetToken string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(rootGID, rootToken)
	if rec == nil || rec.key != key(rootGID, rootToken) {
		return errors.New("archive root identity not found")
	}
	activeGID, activeToken := s.activeIdentity(rec)
	if targetGID <= 0 || targetToken == "" || targetGID == activeGID && targetToken == activeToken {
		return errors.New("target gallery is already active")
	}
	if owner, exists := s.identityRoot[key(targetGID, targetToken)]; exists {
		if owner == rec.key {
			return errors.New("target gallery version is already present on this shelf")
		}
		return errors.New("target gallery is already archived under another shelf")
	}
	if prior := s.rootLocked(targetGID, targetToken); prior != nil && prior.key != rec.key {
		return errors.New("target gallery is already archived under another shelf")
	}
	if !s.flatOwnedLocked(rec) {
		return errors.New("archive root ownership conflict")
	}
	for identity := range s.identityRoot {
		priorGID, priorToken, ok := parseKey(identity)
		if ok && priorGID == targetGID && priorToken != targetToken {
			return errors.New("target gallery gid already belongs to another token")
		}
	}
	if _, err := os.Stat(s.archivePathLocked(rec, targetGID)); err == nil {
		return errors.New("target archive file already exists without matching shelf ownership")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ActivateVersion atomically publishes an already validated target ZIP and
// its matching snapshot as the shelf's new active version. Recovery uses the
// journal and staged sidecars to finish or roll back after process failure.
func (s *Store) ActivateVersion(rootGID int, rootToken string, targetGID int, targetToken string, snapshot map[string]any, cover []byte, hasCover bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.rootLocked(rootGID, rootToken)
	if rec == nil || rec.key != key(rootGID, rootToken) || !s.flatOwnedLocked(rec) {
		return errors.New("archive root identity or ownership conflict")
	}
	if s.identityRoot[key(targetGID, targetToken)] != rec.key {
		return errors.New("target gallery is not owned by this shelf")
	}
	version := versionRecords(rec.meta)[key(targetGID, targetToken)]
	if version == nil || version["status"] != "ready" {
		return errors.New("target archive is not ready")
	}
	archivePath := s.archivePathLocked(rec, targetGID)
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return errors.New("target archive failed validation")
	}
	if len(zr.File) == 0 {
		_ = zr.Close()
		return errors.New("target archive has no pages")
	}
	_ = zr.Close()
	snapshot = cloneMap(snapshot)
	snapshot["gid"], snapshot["token"] = targetGID, targetToken
	if snapshot["saved_at"] == nil {
		snapshot["saved_at"] = float64(time.Now().Unix())
	}
	metaBytes, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	metaPath := filepath.Join(rec.dir, metadataName)
	coverPath := filepath.Join(rec.dir, coverName)
	if err = s.writeAtomic(metaPath+".next", metaBytes); err != nil {
		return err
	}
	coverMarker := filepath.Join(rec.dir, ".cover.none")
	_ = os.Remove(coverMarker)
	if hasCover {
		if err = s.writeAtomic(coverPath+".next", cover); err != nil {
			return err
		}
		_ = os.Remove(coverMarker)
	} else {
		_ = os.Remove(coverPath + ".next")
		if err = s.writeAtomic(coverMarker, []byte("none")); err != nil {
			return err
		}
	}
	oldGID, oldToken := s.activeIdentity(rec)
	oldMeta := cloneMap(rec.meta)
	rec.meta["activation_pending"] = map[string]any{"target_gid": targetGID, "target_token": targetToken, "old_gid": oldGID, "old_token": oldToken}
	if err = s.persistLocked(rec); err != nil {
		rec.meta = oldMeta
		_ = os.Remove(metaPath + ".next")
		_ = os.Remove(coverPath + ".next")
		_ = os.Remove(coverMarker)
		return err
	}
	recoverAfterError := func(cause error) error {
		if recoveryErr := s.recoverActivationLocked(rec); recoveryErr != nil {
			return errors.Join(cause, recoveryErr)
		}
		if activeGID, activeToken := s.activeIdentity(rec); activeGID == targetGID && activeToken == targetToken {
			return nil
		}
		return cause
	}
	if _, err = os.Stat(metaPath); err == nil {
		_ = os.Remove(metaPath + ".previous")
		if err = os.Rename(metaPath, metaPath+".previous"); err != nil {
			return recoverAfterError(err)
		}
	}
	if _, err = os.Stat(coverPath); err == nil {
		_ = os.Remove(coverPath + ".previous")
		if err = os.Rename(coverPath, coverPath+".previous"); err != nil {
			return recoverAfterError(err)
		}
	}
	if err = os.Rename(metaPath+".next", metaPath); err != nil {
		return recoverAfterError(err)
	}
	if hasCover {
		if err = os.Rename(coverPath+".next", coverPath); err != nil {
			return recoverAfterError(err)
		}
	} else {
		_ = os.Remove(coverPath)
	}
	if err = s.finishActivationLocked(rec, targetGID, targetToken); err != nil {
		return recoverAfterError(err)
	}
	return nil
}

func (s *Store) finishActivationLocked(rec *rootRecord, targetGID int, targetToken string) error {
	oldMeta := cloneMap(rec.meta)
	versions := versionRecords(rec.meta)
	version := versions[key(targetGID, targetToken)]
	if version == nil {
		return errors.New("target version disappeared")
	}
	for _, field := range []string{"title", "quality", "or", "gp_price", "download_url", "page_count", "bytes", "total_bytes", "metadata_at", "status", "error"} {
		if value, ok := version[field]; ok {
			rec.meta[field] = value
		}
	}
	rec.meta["active_gid"], rec.meta["active_token"] = targetGID, targetToken
	rec.meta["activation_pending"] = nil
	if err := s.persistLocked(rec); err != nil {
		rec.meta = oldMeta
		return err
	}
	_ = os.Remove(filepath.Join(rec.dir, metadataName+".previous"))
	_ = os.Remove(filepath.Join(rec.dir, coverName+".previous"))
	_ = os.Remove(filepath.Join(rec.dir, metadataName+".next"))
	_ = os.Remove(filepath.Join(rec.dir, coverName+".next"))
	_ = os.Remove(filepath.Join(rec.dir, ".cover.none"))
	return nil
}

func (s *Store) recoverActivationLocked(rec *rootRecord) error {
	pending, _ := rec.meta["activation_pending"].(map[string]any)
	if pending == nil {
		return nil
	}
	targetGID, targetToken := toInt(pending["target_gid"]), toString(pending["target_token"])
	metaPath := filepath.Join(rec.dir, metadataName)
	coverPath := filepath.Join(rec.dir, coverName)
	nextMeta, nextCover := metaPath+".next", coverPath+".next"
	marker := filepath.Join(rec.dir, ".cover.none")
	zipPath := s.archivePathLocked(rec, targetGID)
	validZip := false
	if zr, err := zip.OpenReader(zipPath); err == nil {
		validZip = len(zr.File) > 0
		_ = zr.Close()
	}
	metaAvailable := false
	if b, err := os.ReadFile(metaPath); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil && toInt(m["gid"]) == targetGID && toString(m["token"]) == targetToken {
			metaAvailable = true
		}
	}
	if !metaAvailable {
		if _, err := os.Stat(nextMeta); err == nil && validZip {
			if _, oldErr := os.Stat(metaPath); oldErr == nil {
				_ = os.Remove(metaPath + ".previous")
				_ = os.Rename(metaPath, metaPath+".previous")
			}
			if err := os.Rename(nextMeta, metaPath); err != nil {
				return err
			}
			metaAvailable = true
		}
	}
	coverAvailable := false
	if _, err := os.Stat(marker); err == nil {
		if _, coverErr := os.Stat(coverPath); coverErr == nil {
			if _, backupErr := os.Stat(coverPath + ".previous"); os.IsNotExist(backupErr) {
				_ = os.Rename(coverPath, coverPath+".previous")
			}
		}
		_ = os.Remove(coverPath)
		_ = os.Remove(nextCover)
		coverAvailable = true
	} else if _, err := os.Stat(nextCover); err == nil {
		if _, oldErr := os.Stat(coverPath); oldErr == nil {
			_ = os.Remove(coverPath + ".previous")
			_ = os.Rename(coverPath, coverPath+".previous")
		}
		if err := os.Rename(nextCover, coverPath); err != nil {
			return err
		}
		coverAvailable = true
	} else if _, err := os.Stat(coverPath); err == nil {
		coverAvailable = true
	}
	if validZip && metaAvailable && coverAvailable {
		return s.finishActivationLocked(rec, targetGID, targetToken)
	}
	// Incomplete publication: restore the prior sidecars and keep the prior
	// active pointer. The validated target ZIP remains available as history.
	if _, err := os.Stat(metaPath + ".previous"); err == nil {
		_ = os.Remove(metaPath)
		if err := os.Rename(metaPath+".previous", metaPath); err != nil {
			return err
		}
	} else if b, readErr := os.ReadFile(metaPath); readErr == nil {
		var current map[string]any
		if json.Unmarshal(b, &current) == nil && toInt(current["gid"]) == targetGID && toString(current["token"]) == targetToken {
			_ = os.Remove(metaPath)
		}
	}
	if _, err := os.Stat(coverPath + ".previous"); err == nil {
		_ = os.Remove(coverPath)
		if err := os.Rename(coverPath+".previous", coverPath); err != nil {
			return err
		}
	} else {
		_ = os.Remove(coverPath)
	}
	rec.meta["active_gid"], rec.meta["active_token"] = pending["old_gid"], pending["old_token"]
	rec.meta["activation_pending"] = nil
	_ = os.Remove(nextMeta)
	_ = os.Remove(nextCover)
	_ = os.Remove(marker)
	oldMeta := cloneMap(rec.meta)
	if err := s.persistLocked(rec); err != nil {
		rec.meta = oldMeta
		return err
	}
	return nil
}

func (s *Store) Remove(gid int, token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conflicts[key(gid, token)] {
		return false
	}
	rec := s.rootLocked(gid, token)
	if rec == nil || rec.key != key(gid, token) {
		return false
	}
	if os.RemoveAll(rec.dir) != nil {
		return false
	}
	delete(s.roots, rec.key)
	for id, root := range s.identityRoot {
		if root == rec.key {
			delete(s.identityRoot, id)
		}
	}
	return true
}

func (s *Store) Stats() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	statuses := map[string]int{}
	var bytes int64
	ready, withMeta := 0, 0
	for _, rec := range s.roots {
		if !s.flatOwnedLocked(rec) {
			continue
		}
		status := toString(rec.meta["status"])
		statuses[status]++
		if status == "ready" {
			ready++
		}
		if toInt64(rec.meta["metadata_at"]) != 0 {
			withMeta++
		}
		versions := versionRecords(rec.meta)
		if len(versions) == 0 {
			versions[key(toInt(rec.meta["gid"]), toString(rec.meta["token"]))] = rec.meta
		}
		for _, version := range versions {
			gid := toInt(version["gid"])
			if st, err := os.Stat(s.archivePathLocked(rec, gid)); err == nil {
				bytes += st.Size()
			}
		}
	}
	return map[string]any{"entries": len(s.roots), "ready": ready, "by_status": statuses, "bytes": bytes, "with_metadata": withMeta}
}

func (s *Store) Entries() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []map[string]any{}
	for _, rec := range s.roots {
		if !s.flatOwnedLocked(rec) {
			continue
		}
		m := cloneMap(rec.meta)
		rootGID, rootToken := s.rootIdentity(rec)
		activeGID, activeToken := s.activeIdentity(rec)
		m["gid"], m["token"] = rootGID, rootToken
		m["root_gid"], m["root_token"] = rootGID, rootToken
		m["active_gid"], m["active_token"] = activeGID, activeToken
		if st, err := os.Stat(s.archivePathLocked(rec, activeGID)); err == nil {
			m["archive_size"] = st.Size()
		} else {
			m["archive_size"] = int64(0)
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return toInt64(out[i]["created_at"]) > toInt64(out[j]["created_at"]) })
	return out
}

// RecoveryEntries returns individual version jobs plus original roots for
// interrupted pre-version refreshes. It is internal manager state only.
func (s *Store) RecoveryEntries() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []map[string]any{}
	for _, rec := range s.roots {
		if !s.flatOwnedLocked(rec) {
			continue
		}
		rootGID, rootToken := s.rootIdentity(rec)
		versions := versionRecords(rec.meta)
		if len(versions) == 0 {
			versions[key(rootGID, rootToken)] = rec.meta
		}
		for _, state := range versions {
			status := toString(state["status"])
			if status != "pending" && status != "downloading" && status != "zipping" && status != "submitting" && !(status == "ready" && state["is_version_update"] == true) {
				continue
			}
			item := cloneMap(state)
			item["root_gid"], item["root_token"] = rootGID, rootToken
			out = append(out, item)
		}
	}
	return out
}

func toInt(v any) int { return int(toInt64(v)) }
func toInt64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}
func toString(v any) string { s, _ := v.(string); return s }

package archive

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
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
	metaName         = "meta.json"
	stateName        = "state.json"
	zipName          = "archive.zip"
	partName         = "archive.part"
	metadataName     = "metadata.json"
	galleryName      = "gallery.json"
	coverName        = "cover.jpg"
	versionsName     = "versions"
	versionDirLayout = "versioned"
)

type rootRecord struct {
	key           string
	dir           string
	stateFileName string
	legacy        bool
	meta          map[string]any
}

// Store keeps one shelf record per original gallery. New shelves use
// ARCHIVE_DIR/{original gid}/state.json and new physical versions use
// ARCHIVE_DIR/{actual gid}/state.json. Existing shelves keep their discovered
// filenames; all physical versions continue to use archive.zip.
type Store struct {
	Root              string
	DataRoot          string
	mu                sync.RWMutex
	roots             map[string]*rootRecord
	identityRoot      map[string]string
	conflicts         map[string]bool
	snapshotWake      chan struct{}
	snapshotStop      chan struct{}
	snapshotDone      chan struct{}
	zipPool           *zipPool
	closeOnce         sync.Once
	checkpointBlocked bool
	closed            bool
}

func NewStore(root string) (*Store, error) {
	return NewStoreWithState(root, root)
}

// NewStoreWithState keeps durable metadata beside configuration while archive
// payloads remain in the independently mounted archive volume.
func NewStoreWithState(dataRoot, stateRoot string) (*Store, error) {
	return newStoreWithState(dataRoot, stateRoot, true)
}

// newStoreForMigration loads durable state without applying archive recovery
// actions that can rename or delete ZIP payloads before migration preflight.
func newStoreForMigration(dataRoot, stateRoot string) (*Store, error) {
	return newStoreWithState(dataRoot, stateRoot, false)
}

func newStoreWithState(dataRoot, stateRoot string, recoverPayloads bool) (*Store, error) {
	if dataRoot == "" {
		dataRoot = "."
	}
	if stateRoot == "" {
		stateRoot = dataRoot
	}
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		return nil, err
	}
	s := &Store{Root: stateRoot, DataRoot: dataRoot, roots: map[string]*rootRecord{}, identityRoot: map[string]string{}, conflicts: map[string]bool{}, snapshotWake: make(chan struct{}, 1), snapshotStop: make(chan struct{}), snapshotDone: make(chan struct{}), zipPool: newZipPool(defaultZipPoolSize, defaultZipPoolIdleTTL)}
	type metaFile struct {
		path string
		meta map[string]any
	}
	rootFiles, versionFiles := []metaFile{}, []metaFile{}
	usedIndex := false
	if indexed, conflicts, ok := readIndexSnapshot(stateRoot); ok {
		usedIndex = true
		for _, id := range conflicts {
			s.conflicts[id] = true
		}
		for _, f := range indexed {
			rootFiles = append(rootFiles, metaFile{path: filepath.Join(stateRoot, f.Path), meta: f.Meta})
		}
	} else {
		_ = filepath.WalkDir(stateRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || (d.Name() != metaName && d.Name() != stateName) {
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
			rel, _ := filepath.Rel(stateRoot, path)
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) == 2 {
				declaredRootGID, declaredRootToken := toInt(meta["root_gid"]), toString(meta["root_token"])
				if declaredRootGID > 0 && (declaredRootGID != gid || declaredRootToken != token) {
					versionFiles = append(versionFiles, metaFile{path: path, meta: meta})
				} else {
					rootFiles = append(rootFiles, metaFile{path: path, meta: meta})
				}
				return nil
			}
			if len(parts) != 3 {
				return nil
			}
			rootFiles = append(rootFiles, metaFile{path: path, meta: meta})
			return nil
		})
	}
	for _, file := range rootFiles {
		meta := file.meta
		gid, token := toInt(meta["gid"]), toString(meta["token"])
		if toInt(meta["root_gid"]) == 0 {
			meta["root_gid"], meta["root_token"] = gid, token
		}
		layout := toString(meta["layout"])
		legacy := layout != "flat" && layout != versionDirLayout
		rec := &rootRecord{key: key(gid, token), dir: filepath.Dir(file.path), stateFileName: filepath.Base(file.path), legacy: legacy, meta: meta}
		if toInt(meta["active_gid"]) <= 0 || toString(meta["active_token"]) == "" {
			meta["active_gid"], meta["active_token"] = gid, token
		}
		if _, exists := s.roots[rec.key]; exists {
			s.conflicts[rec.key] = true
			continue
		}
		s.roots[rec.key] = rec
		s.registerIdentityLocked(rec, gid, token)
		for _, version := range versionRecords(meta) {
			vgid, vtoken := toInt(version["gid"]), toString(version["token"])
			if vgid > 0 && vtoken != "" {
				s.registerIdentityLocked(rec, vgid, vtoken)
			}
		}
	}
	for _, file := range versionFiles {
		meta := file.meta
		gid, token := toInt(meta["gid"]), toString(meta["token"])
		rootGID, rootToken := toInt(meta["root_gid"]), toString(meta["root_token"])
		rec := s.roots[key(rootGID, rootToken)]
		if rec == nil || gid <= 0 || token == "" {
			continue
		}
		id := key(gid, token)
		state := versionRecords(rec.meta)[id]
		if state == nil || toString(state["storage_dir"]) != strconv.Itoa(gid) {
			s.conflicts[id] = true
			delete(s.identityRoot, id)
			continue
		}
		state["state_file"] = filepath.Base(file.path)
		state["gallery_file"] = inferGalleryFile(filepath.Dir(file.path), filepath.Base(file.path))
		if toInt(state["page_count"]) == 0 && toInt(file.meta["page_count"]) > 0 {
			state["page_count"] = file.meta["page_count"]
		}
		if toInt64(state["archive_size"]) == 0 && toInt64(file.meta["archive_size"]) > 0 {
			state["archive_size"] = file.meta["archive_size"]
		}
	}
	for _, rec := range s.roots {
		if rec.stateFileName == "" {
			rec.stateFileName = metaName
		}
		rootID := key(toInt(rec.meta["gid"]), toString(rec.meta["token"]))
		rootState := versionRecords(rec.meta)[rootID]
		if rootState != nil {
			if toString(rootState["state_file"]) == "" {
				rootState["state_file"] = rec.stateFileName
			}
			if toString(rootState["gallery_file"]) == "" {
				rootState["gallery_file"] = inferGalleryFile(rec.dir, rec.stateFileName)
			}
		}
		for _, state := range versionRecords(rec.meta) {
			if toString(state["state_file"]) != "" {
				continue
			}
			gid := toInt(state["gid"])
			token := toString(state["token"])
			dir, ok := s.versionDirLocked(rec, gid, token)
			if !ok {
				continue
			}
			stateFile := stateName
			if dir == rec.dir {
				stateFile = rec.stateFileName
			} else if _, err := os.Stat(filepath.Join(dir, metaName)); err == nil {
				stateFile = metaName
			} else if _, err := os.Stat(filepath.Join(dir, stateName)); err == nil {
				stateFile = stateName
			}
			state["state_file"] = stateFile
			state["gallery_file"] = inferGalleryFile(dir, stateFile)
		}
	}
	// Finish or roll back a partially published active snapshot before exposing
	// the store to readers.
	for _, rec := range s.roots {
		if recoverPayloads {
			_ = s.recoverActivationLocked(rec)
			_ = s.cleanupPreviousZipLocked(rec)
		}
		for id, state := range versionRecords(rec.meta) {
			gid, token, ok := parseKey(id)
			if !usedIndex && ok && (gid != toInt(rec.meta["gid"]) || token != toString(rec.meta["token"])) && toString(state["storage_dir"]) == strconv.Itoa(gid) {
				_ = s.persistVersionLocked(rec, gid, token)
			}
		}
	}
	if !usedIndex {
		if err := s.writeIndexSnapshotLocked(); err != nil {
			return nil, err
		}
	}
	go s.snapshotLoop()
	return s, nil
}

func (s *Store) snapshotLoop() {
	defer close(s.snapshotDone)
	for {
		select {
		case <-s.snapshotWake:
			timer := time.NewTimer(300 * time.Millisecond)
		wait:
			for {
				select {
				case <-s.snapshotWake:
				case <-timer.C:
					break wait
				case <-s.snapshotStop:
					timer.Stop()
					s.flushIndex()
					return
				}
			}
			s.flushIndex()
		case <-s.snapshotStop:
			s.flushIndex()
			return
		}
	}
}

func (s *Store) flushIndex() {
	s.mu.Lock()
	if !s.checkpointBlocked {
		if _, err := os.Lstat(filepath.Join(s.Root, ".dirty")); err == nil {
			_ = s.writeIndexSnapshotLocked()
		}
	}
	s.mu.Unlock()
}

// Close flushes the coalesced local index checkpoint. It is safe to call more than once.
func (s *Store) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.zipPool != nil {
		s.zipPool.close()
	}
	s.closeOnce.Do(func() { close(s.snapshotStop); <-s.snapshotDone })
	if _, err := os.Lstat(filepath.Join(s.Root, ".dirty")); err == nil {
		return errors.New("archive index checkpoint remains dirty")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func key(gid int, token string) string { return strconv.Itoa(gid) + ":" + token }

func inferGalleryFile(dir, stateFile string) string {
	preferred := galleryName
	if stateFile == metaName {
		preferred = metadataName
	}
	if _, err := os.Stat(filepath.Join(dir, preferred)); err == nil {
		return preferred
	}
	other := metadataName
	if preferred == metadataName {
		other = galleryName
	}
	if _, err := os.Stat(filepath.Join(dir, other)); err == nil {
		return other
	}
	return preferred
}

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
	stateFile := rec.stateFileName
	if stateFile == "" {
		stateFile = metaName
	}
	b, err := os.ReadFile(filepath.Join(rec.dir, stateFile))
	if os.IsNotExist(err) {
		otherFile := metaName
		if stateFile == metaName {
			otherFile = stateName
		}
		b, err = os.ReadFile(filepath.Join(rec.dir, otherFile))
		if os.IsNotExist(err) {
			return true
		}
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
		return toString(disk["layout"]) != "flat" && toString(disk["layout"]) != versionDirLayout
	}
	return toString(disk["layout"]) == toString(rec.meta["layout"])
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
		if dir, ok := s.versionDirLocked(rec, gid, token); ok {
			return dir
		}
	}
	return filepath.Join(s.Root, strconv.Itoa(gid), token)
}

func (s *Store) versionDirLocked(rec *rootRecord, gid int, token string) (string, bool) {
	if rec == nil || !s.flatOwnedLocked(rec) {
		return "", false
	}
	rootGID, rootToken := s.rootIdentity(rec)
	if gid == rootGID && token == rootToken {
		return rec.dir, true
	}
	state := s.versionStateLocked(rec, gid, token)
	if state == nil {
		return "", false
	}
	storageDir := toString(state["storage_dir"])
	if storageDir == "" {
		if toString(rec.meta["layout"]) == "flat" {
			return rec.dir, true
		}
		return "", false
	}
	if storageDir == "." {
		return rec.dir, true
	}
	if storageDir != strconv.Itoa(gid) {
		return "", false
	}
	return filepath.Join(s.Root, storageDir), true
}

func (s *Store) versionDirOwnedLocked(rec *rootRecord, gid int, token string) bool {
	dir, ok := s.versionDirLocked(rec, gid, token)
	if !ok {
		return false
	}
	if dir == rec.dir {
		return true
	}
	stateFile := s.stateFileNameLocked(rec, gid, token)
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if os.IsNotExist(err) {
		entries, readErr := os.ReadDir(dir)
		if os.IsNotExist(readErr) {
			return true
		}
		if readErr != nil {
			return false
		}
		for _, entry := range entries {
			if entry.Name() != stateFile+".tmp" && entry.Name() != metaName+".tmp" && entry.Name() != stateName+".tmp" {
				return false
			}
		}
		return true
	}
	if err != nil {
		return false
	}
	var meta map[string]any
	if json.Unmarshal(b, &meta) != nil {
		return false
	}
	rootGID, rootToken := s.rootIdentity(rec)
	return toInt(meta["gid"]) == gid && toString(meta["token"]) == token && toInt(meta["root_gid"]) == rootGID && toString(meta["root_token"]) == rootToken
}

func (s *Store) archivePathLocked(rec *rootRecord, gid int, token string) string {
	dir, ok := s.versionDirLocked(rec, gid, token)
	if !ok || !s.versionDirOwnedLocked(rec, gid, token) {
		return ""
	}
	rel, err := filepath.Rel(s.Root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	dataDir := filepath.Join(s.DataRoot, rel)
	if toString(rec.meta["payload_layout"]) == "flat-gid" {
		return filepath.Join(s.DataRoot, strconv.Itoa(gid)+".zip")
	}
	if dir == rec.dir && toString(rec.meta["layout"]) == "flat" {
		return filepath.Join(dataDir, strconv.Itoa(gid)+".zip")
	}
	if dir == rec.dir && rec.legacy && gid == toInt(rec.meta["gid"]) && token == toString(rec.meta["token"]) {
		return filepath.Join(dataDir, zipName)
	}
	return filepath.Join(dataDir, zipName)
}

func (s *Store) stateFileNameLocked(rec *rootRecord, gid int, token string) string {
	if rec == nil {
		return stateName
	}
	if gid == toInt(rec.meta["gid"]) && token == toString(rec.meta["token"]) {
		if rec.stateFileName != "" {
			return rec.stateFileName
		}
		return stateName
	}
	state := s.versionStateLocked(rec, gid, token)
	if name := toString(state["state_file"]); name == metaName || name == stateName {
		return name
	}
	dir, ok := s.versionDirLocked(rec, gid, token)
	if ok {
		if _, err := os.Stat(filepath.Join(dir, metaName)); err == nil {
			return metaName
		}
	}
	return stateName
}

func (s *Store) galleryFileNameLocked(rec *rootRecord, gid int, token string) string {
	if rec == nil {
		return galleryName
	}
	state := s.versionStateLocked(rec, gid, token)
	if name := toString(state["gallery_file"]); name == metadataName || name == galleryName {
		return name
	}
	dir, ok := s.versionDirLocked(rec, gid, token)
	if !ok {
		return galleryName
	}
	return inferGalleryFile(dir, s.stateFileNameLocked(rec, gid, token))
}

func (s *Store) archivePath(gid int, token string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	return s.archivePathLocked(rec, gid, token)
}

func (s *Store) ArchivePublishPath(gid int, token string) (string, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	if rec == nil || s.versionStateLocked(rec, gid, token) == nil {
		return "", "", errors.New("archive identity or ownership conflict")
	}
	target := s.archivePathLocked(rec, gid, token)
	if target == "" {
		return "", "", errors.New("archive identity or ownership conflict")
	}
	return target, target + ".publish.tmp", nil
}

func (s *Store) PublishArchive(gid int, token, publishPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.rootLocked(gid, token)
	if rec == nil || s.versionStateLocked(rec, gid, token) == nil {
		return errors.New("archive identity or ownership conflict")
	}
	target := s.archivePathLocked(rec, gid, token)
	if target == "" || filepath.Clean(publishPath) != filepath.Clean(target+".publish.tmp") || !s.versionDirOwnedLocked(rec, gid, token) {
		return errors.New("archive publish path does not belong to this identity")
	}
	validated, err := zip.OpenReader(publishPath)
	if err != nil {
		return fmt.Errorf("published archive is not a valid ZIP: %w", err)
	}
	pages := len(validated.File)
	_ = validated.Close()
	if pages == 0 {
		return errors.New("published archive is empty")
	}
	st, err := os.Stat(publishPath)
	if err != nil {
		return err
	}
	if err := s.writeDirtyMarker(); err != nil {
		return err
	}
	if s.zipPool != nil {
		s.zipPool.invalidate(target)
	}
	if err := os.Rename(publishPath, target); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(target)); err != nil {
		return err
	}
	state := s.versionStateLocked(rec, gid, token)
	state["archive_size"], state["page_count"] = st.Size(), pages
	if err := s.persistLocked(rec); err != nil {
		return err
	}
	return s.persistVersionLocked(rec, gid, token)
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
		if !s.versionDirOwnedLocked(rec, gid, token) {
			return "", false
		}
		dir, _ := s.versionDirLocked(rec, gid, token)
		return filepath.Join(dir, s.galleryFileNameLocked(rec, gid, token)), true
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
	if rec.stateFileName == "" {
		rec.stateFileName = stateName
	}
	if err := s.writeDirtyMarker(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rec.meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(rec.dir, rec.stateFileName+".tmp")
	if err = os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err = os.Rename(tmp, filepath.Join(rec.dir, rec.stateFileName)); err != nil {
		return err
	}
	s.scheduleIndexSnapshotLocked()
	return nil
}

func (s *Store) scheduleIndexSnapshotLocked() {
	select {
	case s.snapshotWake <- struct{}{}:
	default:
	}
}

type indexFile struct {
	Path string         `json:"path"`
	Meta map[string]any `json:"meta"`
}
type indexSnapshot struct {
	Version   int         `json:"version"`
	Entries   []indexFile `json:"entries"`
	Conflicts []string    `json:"conflicts,omitempty"`
}

func readIndexSnapshot(root string) ([]indexFile, []string, bool) {
	if _, err := os.Lstat(filepath.Join(root, ".dirty")); err == nil {
		return nil, nil, false
	} else if !os.IsNotExist(err) {
		return nil, nil, false
	}
	path := filepath.Join(root, "index.snapshot")
	if st, err := os.Lstat(path); err != nil || st.Mode()&os.ModeSymlink != 0 {
		return nil, nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	var snap indexSnapshot
	if json.Unmarshal(b, &snap) != nil || snap.Version != 1 {
		return nil, nil, false
	}
	for _, f := range snap.Entries {
		clean := filepath.Clean(f.Path)
		if f.Path == "" || filepath.IsAbs(f.Path) || f.Meta == nil || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, nil, false
		}
		parts := strings.Split(filepath.ToSlash(clean), "/")
		if (len(parts) != 2 && len(parts) != 3) || (parts[len(parts)-1] != metaName && parts[len(parts)-1] != stateName) {
			return nil, nil, false
		}
		if gid, err := strconv.Atoi(parts[0]); err != nil || gid <= 0 {
			return nil, nil, false
		}
	}
	for _, id := range snap.Conflicts {
		if _, _, ok := parseKey(id); !ok {
			return nil, nil, false
		}
	}
	return snap.Entries, snap.Conflicts, true
}

func (s *Store) writeIndexSnapshotLocked() error {
	snap := indexSnapshot{Version: 1, Entries: make([]indexFile, 0, len(s.roots))}
	for id := range s.conflicts {
		snap.Conflicts = append(snap.Conflicts, id)
	}
	sort.Strings(snap.Conflicts)
	for _, rec := range s.roots {
		rel, err := filepath.Rel(s.Root, filepath.Join(rec.dir, rec.stateFileName))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("archive state path escapes root")
		}
		snap.Entries = append(snap.Entries, indexFile{Path: rel, Meta: rec.meta})
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	path := filepath.Join(s.Root, "index.snapshot")
	if err := s.writeAtomic(path, b); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.Root, ".dirty")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(s.Root)
}

func (s *Store) writeDirtyMarker() error {
	return writeDirtyMarkerAt(s.Root)
}

func writeDirtyMarkerAt(root string) error {
	f, err := os.OpenFile(filepath.Join(root, ".dirty"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write([]byte("1")); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return syncDirectory(root)
}

func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (s *Store) persistVersionLocked(rec *rootRecord, gid int, token string) error {
	rootGID, rootToken := s.rootIdentity(rec)
	if gid == rootGID && token == rootToken {
		return nil
	}
	dir, ok := s.versionDirLocked(rec, gid, token)
	if !ok || dir == rec.dir {
		return nil
	}
	if !s.versionDirOwnedLocked(rec, gid, token) {
		return errors.New("archive version directory ownership conflict")
	}
	state := versionRecords(rec.meta)[key(gid, token)]
	if state == nil {
		return errors.New("archive version state not found")
	}
	if toString(state["state_file"]) == "" {
		state["state_file"] = s.stateFileNameLocked(rec, gid, token)
	}
	if toString(state["gallery_file"]) == "" {
		state["gallery_file"] = s.galleryFileNameLocked(rec, gid, token)
	}
	meta := cloneMap(state)
	meta["gid"], meta["token"] = gid, token
	meta["root_gid"], meta["root_token"] = rootGID, rootToken
	meta["layout"] = "version"
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := s.writeDirtyMarker(); err != nil {
		return err
	}
	if err := s.writeAtomic(filepath.Join(dir, toString(state["state_file"])), b); err != nil {
		return err
	}
	s.scheduleIndexSnapshotLocked()
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
	} else {
		out = cloneMap(versionRecords(rec.meta)[key(gid, token)])
		if out == nil && rec.legacy && gid == rootGID && token == rootToken {
			out = cloneMap(rec.meta)
		}
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
	pathToken := token
	if gid == rootGID && token == rootToken {
		pathToken = activeToken
	}
	if st, e := os.Stat(s.archivePathLocked(rec, pathGID, pathToken)); e == nil {
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
	} else if st, e := os.Stat(filepath.Join(s.DataRoot, strconv.Itoa(gid), token, partName)); e == nil {
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
		if filepath.Clean(s.Root) != filepath.Clean(s.DataRoot) {
			if _, err := os.Lstat(filepath.Join(s.DataRoot, strconv.Itoa(gid)+".zip")); err == nil {
				return nil, errors.New("archive payload already exists without matching shelf ownership")
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
		if entries, err := os.ReadDir(rootDir); err == nil && len(entries) != 0 {
			return nil, errors.New("archive root directory already exists without matching shelf ownership")
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, existing := range s.roots {
			if toInt(existing.meta["gid"]) == gid && toString(existing.meta["token"]) != token {
				return nil, errors.New("archive gid already belongs to another token")
			}
		}
		now := float64(time.Now().Unix())
		meta := map[string]any{"gid": gid, "token": token, "root_gid": gid, "root_token": token, "active_gid": gid, "active_token": token, "layout": versionDirLayout, "created_at": now, versionsName: map[string]any{}}
		if filepath.Clean(s.Root) != filepath.Clean(s.DataRoot) {
			meta["payload_layout"] = "flat-gid"
		}
		rec = &rootRecord{key: id, dir: rootDir, stateFileName: stateName, meta: meta}
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
		for identity := range s.identityRoot {
			priorGID, priorToken, ok := parseKey(identity)
			if ok && priorGID == gid && priorToken != token {
				return nil, errors.New("target gallery gid already belongs to another token")
			}
		}
		collisionPath := filepath.Join(s.DataRoot, strconv.Itoa(gid))
		if filepath.Clean(s.Root) != filepath.Clean(s.DataRoot) {
			collisionPath = filepath.Join(s.DataRoot, strconv.Itoa(gid)+".zip")
		}
		if _, err := os.Lstat(collisionPath); err == nil {
			return nil, errors.New("target gallery directory already exists without matching shelf ownership")
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
		if id == rec.key {
			state["storage_dir"] = "."
			state["state_file"] = rec.stateFileName
			state["gallery_file"] = inferGalleryFile(rec.dir, rec.stateFileName)
		} else {
			state["storage_dir"] = strconv.Itoa(gid)
			state["state_file"] = stateName
			state["gallery_file"] = galleryName
		}
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
	if err := s.persistVersionLocked(rec, gid, token); err != nil {
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
	for identity := range s.identityRoot {
		priorGID, priorToken, ok := parseKey(identity)
		if ok && priorGID == gid && priorToken != token {
			return errors.New("archive gid already belongs to another token")
		}
	}
	rootDir := filepath.Join(s.DataRoot, strconv.Itoa(gid))
	if filepath.Clean(s.Root) != filepath.Clean(s.DataRoot) {
		if _, err := os.Lstat(filepath.Join(s.DataRoot, strconv.Itoa(gid)+".zip")); err == nil {
			return errors.New("archive payload already exists without matching shelf ownership")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if entries, err := os.ReadDir(rootDir); err == nil && len(entries) != 0 {
		return errors.New("archive root directory already exists without matching shelf ownership")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) IsReady(gid int, token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	return rec != nil && s.versionStateLocked(rec, gid, token)["status"] == "ready"
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
			item.PageCount = toInt(versionRecords(rec.meta)[key(activeGID, activeToken)]["page_count"])
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

func (s *Store) pageCountLocked(rec *rootRecord, gid int, token string) int {
	return toInt(s.versionStateLocked(rec, gid, token)["page_count"])
}

func (s *Store) PageCount(gid int, token string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.rootLocked(gid, token)
	if rec == nil {
		return 0
	}
	return s.pageCountLocked(rec, gid, token)
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
	p := s.archivePathLocked(rec, gid, token)
	s.mu.RUnlock()

	if s.zipPool == nil {
		r, err := zip.OpenReader(p)
		if err != nil {
			return nil, false, err
		}
		defer r.Close()
		return readZipPage(r, page)
	}

	r, release, reused, err := s.zipPool.acquire(p)
	if err != nil {
		return nil, false, err
	}
	b, ok, readErr := readZipPage(r, page)
	release()
	if readErr != nil && reused {
		// Invalidate stale pooled reader once in case the ZIP on disk was replaced.
		s.zipPool.invalidate(p)
		r, release, _, err = s.zipPool.acquire(p)
		if err != nil {
			return nil, false, err
		}
		defer release()
		return readZipPage(r, page)
	}
	return b, ok, readErr
}

func readZipPage(r *zip.ReadCloser, page int) ([]byte, bool, error) {
	if page < 0 || page >= len(r.File) {
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
	versions, _ := rec.meta[versionsName].(map[string]any)
	state, _ := versions[key(gid, token)].(map[string]any)
	if state == nil && gid == toInt(rec.meta["gid"]) && token == toString(rec.meta["token"]) {
		return rec.meta
	}
	return state
}

func (s *Store) writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
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
	if err := s.writeDirtyMarker(); err != nil {
		return err
	}
	if err := s.writeAtomic(path, b); err != nil {
		return err
	}
	s.scheduleIndexSnapshotLocked()
	return nil
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
	if err := s.writeDirtyMarker(); err != nil {
		return err
	}
	if err := s.writeAtomic(filepath.Join(filepath.Dir(path), coverName), b); err != nil {
		return err
	}
	s.scheduleIndexSnapshotLocked()
	return nil
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
	if err := s.writeDirtyMarker(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	if err = s.writeAtomic(path, b); err != nil {
		return err
	}
	s.scheduleIndexSnapshotLocked()
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
	targetCollision := filepath.Join(s.DataRoot, strconv.Itoa(targetGID))
	if filepath.Clean(s.Root) != filepath.Clean(s.DataRoot) {
		targetCollision = filepath.Join(s.DataRoot, strconv.Itoa(targetGID)+".zip")
	}
	if _, err := os.Lstat(targetCollision); err == nil {
		return errors.New("target gallery directory already exists without matching shelf ownership")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ActivateVersion publishes sidecars into the target's own physical directory,
// durably flips the stable root pointer, then removes only the previous ZIP.
func (s *Store) ActivateVersion(rootGID int, rootToken string, targetGID int, targetToken string, snapshot map[string]any, cover []byte, hasCover bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.rootLocked(rootGID, rootToken)
	if rec == nil || rec.key != key(rootGID, rootToken) || !s.flatOwnedLocked(rec) {
		return errors.New("archive root identity or ownership conflict")
	}
	if cleanup, _ := rec.meta["cleanup_previous_zip"].(map[string]any); cleanup != nil {
		if err := s.cleanupPreviousZipLocked(rec); err != nil {
			return errors.New("previous ZIP cleanup is incomplete; refusing another activation")
		}
	}
	if s.identityRoot[key(targetGID, targetToken)] != rec.key {
		return errors.New("target gallery is not owned by this shelf")
	}
	version := s.versionStateLocked(rec, targetGID, targetToken)
	if version == nil || version["status"] != "ready" || !s.versionDirOwnedLocked(rec, targetGID, targetToken) {
		return errors.New("target archive is not ready or its directory is not owned")
	}
	archivePath := s.archivePathLocked(rec, targetGID, targetToken)
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return errors.New("target archive failed validation")
	}
	if len(zr.File) == 0 {
		_ = zr.Close()
		return errors.New("target archive has no pages")
	}
	_ = zr.Close()
	dir, _ := s.versionDirLocked(rec, targetGID, targetToken)
	snapshot = cloneMap(snapshot)
	snapshot["gid"], snapshot["token"] = targetGID, targetToken
	if snapshot["saved_at"] == nil {
		snapshot["saved_at"] = float64(time.Now().Unix())
	}
	metaBytes, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	metaPath := filepath.Join(dir, s.galleryFileNameLocked(rec, targetGID, targetToken))
	coverPath := filepath.Join(dir, coverName)
	if err = s.writeDirtyMarker(); err != nil {
		return err
	}
	if err = s.writeAtomic(metaPath+".next", metaBytes); err != nil {
		return err
	}
	coverMarker := filepath.Join(dir, ".cover.none")
	_ = os.Remove(coverMarker)
	if hasCover {
		if err = s.writeAtomic(coverPath+".next", cover); err != nil {
			return err
		}
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
	if err = s.completeActivationLocked(rec, targetGID, targetToken); err != nil {
		if recoveryErr := s.recoverActivationLocked(rec); recoveryErr != nil {
			return errors.Join(err, recoveryErr)
		}
		activeGID, activeToken := s.activeIdentity(rec)
		if activeGID == targetGID && activeToken == targetToken {
			return nil
		}
		return err
	}
	_ = s.cleanupPreviousZipLocked(rec)
	return nil
}

func (s *Store) completeActivationLocked(rec *rootRecord, targetGID int, targetToken string) error {
	if err := s.writeDirtyMarker(); err != nil {
		return err
	}
	dir, ok := s.versionDirLocked(rec, targetGID, targetToken)
	if !ok || !s.versionDirOwnedLocked(rec, targetGID, targetToken) {
		return errors.New("target archive directory ownership conflict")
	}
	metaPath, coverPath := filepath.Join(dir, s.galleryFileNameLocked(rec, targetGID, targetToken)), filepath.Join(dir, coverName)
	nextMeta, nextCover := metaPath+".next", coverPath+".next"
	marker := filepath.Join(dir, ".cover.none")
	metaAvailable := false
	if b, err := os.ReadFile(metaPath); err == nil {
		var m map[string]any
		metaAvailable = json.Unmarshal(b, &m) == nil && toInt(m["gid"]) == targetGID && toString(m["token"]) == targetToken
	}
	if !metaAvailable {
		if _, err := os.Stat(nextMeta); err != nil {
			return errors.New("target metadata snapshot was not staged")
		}
		if err := os.Rename(nextMeta, metaPath); err != nil {
			return err
		}
	}
	if _, err := os.Stat(marker); err == nil {
		_ = os.Remove(coverPath)
		_ = os.Remove(nextCover)
	} else if _, err := os.Stat(nextCover); err == nil {
		if err := os.Rename(nextCover, coverPath); err != nil {
			return err
		}
	} else if _, err := os.Stat(coverPath); os.IsNotExist(err) {
		return errors.New("target cover publication was not staged")
	}
	return s.finishActivationLocked(rec, targetGID, targetToken)
}

func (s *Store) finishActivationLocked(rec *rootRecord, targetGID int, targetToken string) error {
	oldMeta := cloneMap(rec.meta)
	versions := versionRecords(rec.meta)
	version := versions[key(targetGID, targetToken)]
	if version == nil {
		return errors.New("target version disappeared")
	}
	oldGID, oldToken := s.activeIdentity(rec)
	for _, field := range []string{"title", "quality", "or", "gp_price", "download_url", "page_count", "bytes", "total_bytes", "metadata_at", "status", "error"} {
		if value, ok := version[field]; ok {
			rec.meta[field] = value
		}
	}
	rec.meta["active_gid"], rec.meta["active_token"] = targetGID, targetToken
	rec.meta["activation_pending"] = nil
	if oldGID != targetGID || oldToken != targetToken {
		rec.meta["cleanup_previous_zip"] = map[string]any{"gid": oldGID, "token": oldToken, "target_gid": targetGID, "target_token": targetToken}
	}
	if err := s.persistLocked(rec); err != nil {
		rec.meta = oldMeta
		return err
	}
	return nil
}

func (s *Store) cleanupPreviousZipLocked(rec *rootRecord) error {
	cleanup, _ := rec.meta["cleanup_previous_zip"].(map[string]any)
	if cleanup == nil {
		return nil
	}
	activeGID, activeToken := s.activeIdentity(rec)
	if activeGID != toInt(cleanup["target_gid"]) || activeToken != toString(cleanup["target_token"]) {
		return errors.New("previous ZIP cleanup target is no longer active")
	}
	targetPath := s.archivePathLocked(rec, activeGID, activeToken)
	zr, err := zip.OpenReader(targetPath)
	if err != nil {
		return errors.New("active target ZIP is missing; preserving previous ZIP")
	}
	if len(zr.File) == 0 {
		_ = zr.Close()
		return errors.New("active target ZIP is empty; preserving previous ZIP")
	}
	_ = zr.Close()
	oldGID, oldToken := toInt(cleanup["gid"]), toString(cleanup["token"])
	if oldGID != activeGID || oldToken != activeToken {
		oldPath := s.archivePathLocked(rec, oldGID, oldToken)
		if oldPath == "" || filepath.Clean(oldPath) == filepath.Clean(targetPath) {
			return errors.New("previous ZIP ownership conflict")
		}
		if err := s.writeDirtyMarker(); err != nil {
			return err
		}
		if s.zipPool != nil {
			s.zipPool.invalidate(oldPath)
		}
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if old := versionRecords(rec.meta)[key(oldGID, oldToken)]; old != nil {
			old["archive_size"], old["page_count"] = int64(0), 0
		}
	}
	oldMeta := cloneMap(rec.meta)
	delete(rec.meta, "cleanup_previous_zip")
	if err := s.persistLocked(rec); err != nil {
		rec.meta = oldMeta
		return err
	}
	return nil
}

func (s *Store) recoverActivationLocked(rec *rootRecord) error {
	pending, _ := rec.meta["activation_pending"].(map[string]any)
	if pending == nil {
		return nil
	}
	targetGID, targetToken := toInt(pending["target_gid"]), toString(pending["target_token"])
	zipPath := s.archivePathLocked(rec, targetGID, targetToken)
	validZip := false
	zr, zipErr := zip.OpenReader(zipPath)
	if zipErr != nil && !os.IsNotExist(zipErr) {
		return zipErr
	}
	if zipErr == nil {
		validZip = len(zr.File) > 0
		_ = zr.Close()
	}
	if validZip {
		if err := s.completeActivationLocked(rec, targetGID, targetToken); err == nil {
			return nil
		}
	}
	// Incomplete target publication stays hidden; keep the old active pointer
	// and remove only staged files owned by the target version.
	dir, ok := s.versionDirLocked(rec, targetGID, targetToken)
	if ok && s.versionDirOwnedLocked(rec, targetGID, targetToken) {
		_ = os.Remove(filepath.Join(dir, s.galleryFileNameLocked(rec, targetGID, targetToken)+".next"))
		_ = os.Remove(filepath.Join(dir, coverName+".next"))
		_ = os.Remove(filepath.Join(dir, ".cover.none"))
	}
	oldMeta := cloneMap(rec.meta)
	rec.meta["active_gid"], rec.meta["active_token"] = pending["old_gid"], pending["old_token"]
	rec.meta["activation_pending"] = nil
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
	if rec == nil || rec.key != key(gid, token) || !s.flatOwnedLocked(rec) {
		return false
	}
	if s.writeDirtyMarker() != nil {
		return false
	}
	versionDirs := map[string]bool{}
	payloads := map[string]bool{}
	for id, state := range versionRecords(rec.meta) {
		versionGID, versionToken, ok := parseKey(id)
		if !ok {
			continue
		}
		payload := s.archivePathLocked(rec, versionGID, versionToken)
		if payload != "" {
			rel, err := filepath.Rel(s.DataRoot, payload)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return false
			}
			payloads[payload] = true
		}
		if (versionGID == gid && versionToken == token) || toString(state["storage_dir"]) == "" || toString(state["storage_dir"]) == "." {
			continue
		}
		dir, ok := s.versionDirLocked(rec, versionGID, versionToken)
		if !ok || !s.versionDirOwnedLocked(rec, versionGID, versionToken) {
			return false
		}
		versionDirs[dir] = true
	}
	for payload := range payloads {
		if s.zipPool != nil {
			s.zipPool.invalidate(payload)
		}
		if err := os.Remove(payload); err != nil && !os.IsNotExist(err) {
			return false
		}
	}
	for dir := range versionDirs {
		if os.RemoveAll(dir) != nil {
			return false
		}
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
	s.scheduleIndexSnapshotLocked()
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
			bytes += toInt64(version["archive_size"])
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
		m["archive_size"] = toInt64(versionRecords(rec.meta)[key(activeGID, activeToken)]["archive_size"])
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

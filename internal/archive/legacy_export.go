package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LegacyVersionExport struct {
	GID           int
	Token         string
	State         map[string]any
	StatePath     string
	GalleryPath   string
	Gallery       json.RawMessage
	Cover         []byte
	ArchivePath   string
	SourceSize    int64
	SourceMTimeNS int64
	SourceSHA256  string
}
type LegacyWorkExport struct {
	GID         int
	Token       string
	ActiveGID   int
	ActiveToken string
	State       map[string]any
	StatePath   string
	Versions    []LegacyVersionExport
}

// ExportLegacyArchive reads legacy sidecars using the archive store's layout
// rules. It is strictly read-only: no index checkpoints, recovery actions, or
// directory creation occur. Importers may retry the returned rows safely.
func ExportLegacyArchive(dataRoot, stateRoot string) ([]LegacyWorkExport, error) {
	if err := validateLegacyMetadataTree(stateRoot); err != nil {
		return nil, err
	}
	s, err := newStoreForMigration(dataRoot, stateRoot)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.conflicts) != 0 {
		return nil, fmt.Errorf("legacy archive metadata contains %d conflicting identities", len(s.conflicts))
	}
	out := make([]LegacyWorkExport, 0, len(s.roots))
	for _, rec := range s.roots {
		rootGID, rootToken := s.rootIdentity(rec)
		w := LegacyWorkExport{GID: rootGID, Token: rootToken, ActiveGID: toInt(rec.meta["active_gid"]), ActiveToken: toString(rec.meta["active_token"]), State: cloneMap(rec.meta)}
		if w.ActiveGID <= 0 {
			w.ActiveGID = rootGID
		}
		if w.ActiveToken == "" {
			w.ActiveToken = rootToken
		}
		statePath := filepath.Join(rec.dir, rec.stateFileName)
		w.StatePath, err = filepath.Rel(s.Root, statePath)
		if err != nil {
			return nil, err
		}
		rootState := versionRecords(rec.meta)[key(rootGID, rootToken)]
		if rootState == nil {
			rootState = rec.meta
		}
		ids := map[string]map[string]any{}
		for id, v := range versionRecords(rec.meta) {
			ids[id] = v
		}
		if ids[key(rootGID, rootToken)] == nil {
			ids[key(rootGID, rootToken)] = rootState
		}
		for id, state := range ids {
			gid, token, ok := parseKey(id)
			if !ok {
				continue
			}
			dir, ok := s.versionDirLocked(rec, gid, token)
			if !ok {
				return nil, fmt.Errorf("legacy version %s has no safe state directory", id)
			}
			sf := s.stateFileNameLocked(rec, gid, token)
			gf := s.galleryFileNameLocked(rec, gid, token)
			statePath := filepath.Join(dir, sf)
			rel, e := filepath.Rel(s.Root, statePath)
			if e != nil {
				return nil, e
			}
			gpath := filepath.Join(dir, gf)
			gallery := json.RawMessage(nil)
			if info, e := os.Lstat(gpath); e == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("legacy gallery sidecar is a symlink: %s", gpath)
			} else if e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			if b, e := os.ReadFile(gpath); e == nil {
				var check any
				if e = json.Unmarshal(b, &check); e != nil {
					return nil, fmt.Errorf("decode legacy gallery sidecar %s: %w", gpath, e)
				}
				gallery = b
			} else if !os.IsNotExist(e) {
				return nil, e
			}
			coverPath := filepath.Join(dir, coverName)
			if info, e := os.Lstat(coverPath); e == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("legacy cover sidecar is a symlink: %s", coverPath)
			} else if e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			cover, e := os.ReadFile(coverPath)
			if e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			if os.IsNotExist(e) {
				cover = nil
			}
			archivePath := s.archivePathLocked(rec, gid, token)
			archiveRel := ""
			var sourceSize, sourceMTime int64
			var sourceHash string
			if archivePath != "" {
				archiveRel, e = filepath.Rel(s.DataRoot, archivePath)
				if e != nil {
					return nil, e
				}
				sourceSize, sourceMTime, sourceHash, e = payloadFingerprint(s.DataRoot, filepath.ToSlash(archiveRel))
				if e != nil {
					return nil, fmt.Errorf("fingerprint legacy payload %s: %w", archiveRel, e)
				}
			}
			w.Versions = append(w.Versions, LegacyVersionExport{GID: gid, Token: token, State: cloneMap(state), StatePath: rel, GalleryPath: gf, Gallery: gallery, Cover: cover, ArchivePath: archiveRel, SourceSize: sourceSize, SourceMTimeNS: sourceMTime, SourceSHA256: sourceHash})
		}
		sort.Slice(w.Versions, func(i, j int) bool {
			if w.Versions[i].GID != w.Versions[j].GID {
				return w.Versions[i].GID < w.Versions[j].GID
			}
			return w.Versions[i].Token < w.Versions[j].Token
		})
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GID != out[j].GID {
			return out[i].GID < out[j].GID
		}
		return out[i].Token < out[j].Token
	})
	return out, nil
}

func payloadFingerprint(rootPath, rel string) (int64, int64, string, error) {
	info, err := os.Lstat(rootPath)
	if err != nil {
		return 0, 0, "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, 0, "", fmt.Errorf("archive root is not a real directory")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return 0, 0, "", fmt.Errorf("unsafe archive path")
	}
	r, err := os.OpenRoot(rootPath)
	if err != nil {
		return 0, 0, "", err
	}
	defer r.Close()
	parts := strings.Split(clean, string(filepath.Separator))
	for i := range parts {
		p := filepath.Join(parts[:i+1]...)
		st, e := r.Lstat(p)
		if os.IsNotExist(e) {
			return 0, 0, "", nil
		}
		if e != nil {
			return 0, 0, "", e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return 0, 0, "", fmt.Errorf("archive path contains symlink")
		}
		if i < len(parts)-1 && !st.IsDir() {
			return 0, 0, "", fmt.Errorf("archive path component is not a directory")
		}
	}
	before, err := r.Lstat(clean)
	if os.IsNotExist(err) {
		return 0, 0, "", nil
	}
	if err != nil {
		return 0, 0, "", err
	}
	if !before.Mode().IsRegular() {
		return 0, 0, "", fmt.Errorf("archive payload is not a regular file")
	}
	return before.Size(), before.ModTime().UnixNano(), "", nil
}

// validateLegacyMetadataTree makes migration strict even though the runtime
// legacy index loader is intentionally tolerant of damaged historical files.
func validateLegacyMetadataTree(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("legacy state root is not a real directory")
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy state tree contains symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() != stateName && entry.Name() != metaName {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("legacy state sidecar is not a regular file: %s", path)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var state map[string]any
		if err = json.Unmarshal(body, &state); err != nil {
			return fmt.Errorf("decode legacy archive state %s: %w", path, err)
		}
		if toInt(state["gid"]) <= 0 || toString(state["token"]) == "" {
			return fmt.Errorf("legacy archive state has no identity: %s", path)
		}
		return nil
	})
}

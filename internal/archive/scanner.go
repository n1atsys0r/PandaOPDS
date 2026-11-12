package archive

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type importedRoot struct {
	record *rootRecord
	relDir string
}

// ScanLegacySidecars discovers only state-backed shelves. ZIP enumeration and
// validation happen in a temporary local tree before the store lock is acquired.
func (s *Store) ScanLegacySidecars() (ImportReport, error) {
	return s.ScanLegacySidecarsContext(context.Background())
}

func (s *Store) ScanLegacySidecarsContext(ctx context.Context) (ImportReport, error) {
	if err := ctx.Err(); err != nil {
		return ImportReport{}, err
	}
	stageRoot, err := os.MkdirTemp("", "pandaopds-archive-scan-")
	if err != nil {
		return ImportReport{}, err
	}
	defer os.RemoveAll(stageRoot)
	// The potentially remote ARCHIVE_DIR walk and ZIP inspection stay outside
	// Store.mu so reads and active archive jobs are not held behind storage I/O.
	staged, err := ImportSidecarsContext(ctx, s.DataRoot, stageRoot, s.DataRoot)
	if err != nil {
		return ImportReport{}, err
	}
	candidates, err := discoverImportedRoots(stageRoot)
	if err != nil {
		return ImportReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return ImportReport{}, err
	}
	payloadReady := make(map[string]bool, len(candidates))
	var unavailable int
	for rootKey, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return ImportReport{}, err
		}
		if candidatePayloadAvailable(s.DataRoot, candidate) {
			payloadReady[rootKey] = true
		} else {
			unavailable += countRootFiles(stageRoot, candidate.relDir)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ImportReport{}, errors.New("archive store is closed")
	}
	allowed := map[string]bool{}
	conflicts := 0
	candidateConflicts := map[string]bool{}
	identityClaims := map[string]string{}
	gidTokens := map[int]string{}
	gidRoots := map[int]map[string]bool{}
	existingGIDTokens := map[int]string{}
	for identity := range s.identityRoot {
		gid, token, ok := parseKey(identity)
		if !ok {
			continue
		}
		if prior, exists := existingGIDTokens[gid]; !exists {
			existingGIDTokens[gid] = token
		} else if prior != token {
			existingGIDTokens[gid] = "!conflict!"
		}
	}
	for rootKey, candidate := range candidates {
		if !payloadReady[rootKey] {
			continue
		}
		claims := map[string]bool{}
		gid, token := toInt(candidate.record.meta["gid"]), toString(candidate.record.meta["token"])
		claims[key(gid, token)] = true
		for id := range versionRecords(candidate.record.meta) {
			claims[id] = true
		}
		for identity := range claims {
			if owner := s.identityRoot[identity]; owner != "" && owner != key(gid, token) {
				candidateConflicts[rootKey] = true
			}
			if prior, exists := identityClaims[identity]; exists && prior != rootKey {
				candidateConflicts[prior], candidateConflicts[rootKey] = true, true
			}
			identityClaims[identity] = rootKey
			claimGID, claimToken, ok := parseKey(identity)
			if !ok {
				continue
			}
			if prior, exists := existingGIDTokens[claimGID]; exists && prior != claimToken {
				candidateConflicts[rootKey] = true
			}
			if prior, exists := gidTokens[claimGID]; exists && prior != claimToken {
				candidateConflicts[rootKey] = true
				for other := range gidRoots[claimGID] {
					candidateConflicts[other] = true
				}
			}
			gidTokens[claimGID] = claimToken
			if gidRoots[claimGID] == nil {
				gidRoots[claimGID] = map[string]bool{}
			}
			gidRoots[claimGID][rootKey] = true
		}
	}
	for rootKey, candidate := range candidates {
		if !payloadReady[rootKey] {
			continue
		}
		root := candidate.record
		gid, token := toInt(root.meta["gid"]), toString(root.meta["token"])
		id := key(gid, token)
		conflict := s.conflicts[id] || candidateConflicts[rootKey]
		for identity := range versionRecords(root.meta) {
			if s.conflicts[identity] || s.identityRoot[identity] != "" && s.identityRoot[identity] != id {
				conflict = true
			}
		}
		if conflict {
			conflicts += countRootFiles(stageRoot, candidate.relDir)
			continue
		}
		localDir := filepath.Join(s.Root, candidate.relDir)
		if entries, err := os.ReadDir(localDir); err == nil && len(entries) > 0 {
			_, alreadyLoaded := s.roots[id]
			if !alreadyLoaded && !sameRootIdentity(localDir, gid, token) && !importGroupCompatible(stageRoot, s.Root, candidate.relDir) {
				conflicts += countRootFiles(stageRoot, candidate.relDir)
				continue
			}
		} else if err != nil && !os.IsNotExist(err) {
			return ImportReport{}, err
		}
		allowed[rootKey] = true
	}

	// This second pass reads only the staged local sidecars. No ZIP volume is
	// touched while the store lock is held; importer commits with no-clobber.
	committed, err := importSidecars(ctx, stageRoot, s.Root, func(gid int, token string) bool { return allowed[key(gid, token)] })
	if err != nil {
		if _, dirtyErr := os.Lstat(filepath.Join(s.Root, ".dirty")); dirtyErr == nil {
			s.checkpointBlocked = true
			for rootKey, candidate := range candidates {
				if allowed[rootKey] {
					if s.addImportedRootLocked(candidate.record, candidate.relDir) {
						committed.State++
					}
				}
			}
			err = errors.New(err.Error() + "; partial sidecar import remains dirty and requires a rescan or restart")
		}
		committed.Skipped += staged.Skipped
		committed.Skipped += unavailable
		return committed, err
	}
	for rootKey, candidate := range candidates {
		if !allowed[rootKey] {
			continue
		}
		if !s.addImportedRootLocked(candidate.record, candidate.relDir) {
			committed.Conflicts++
			continue
		}
		_ = candidate
	}
	if len(allowed) > 0 {
		s.scheduleIndexSnapshotLocked()
	}
	if !s.checkpointBlocked || len(allowed) > 0 {
		s.checkpointBlocked = false
	} else {
		return committed, errors.New("archive scan completed without resolving a prior partial commit; snapshot remains dirty")
	}
	committed.Skipped += staged.Skipped
	committed.Skipped += unavailable
	committed.Conflicts += conflicts
	return committed, nil
}

// candidatePayloadAvailable checks the exact payload path selected by the
// persisted layout metadata. A stale sidecar without a readable active ZIP
// must not recreate a shelf that the user deleted from split storage.
func candidatePayloadAvailable(dataRoot string, candidate importedRoot) bool {
	meta := candidate.record.meta
	rootGID := toInt(meta["root_gid"])
	if rootGID <= 0 {
		rootGID = toInt(meta["gid"])
	}
	rootToken := toString(meta["root_token"])
	if rootToken == "" {
		rootToken = toString(meta["token"])
	}
	activeGID, activeToken := toInt(meta["active_gid"]), toString(meta["active_token"])
	if activeGID <= 0 || activeToken == "" {
		activeGID, activeToken = rootGID, rootToken
	}
	version := versionRecords(meta)[key(activeGID, activeToken)]
	var zipPath string
	switch {
	case toString(meta["payload_layout"]) == "flat-gid":
		zipPath = filepath.Join(dataRoot, strconv.Itoa(activeGID)+".zip")
	case toString(meta["layout"]) == "flat" && (activeGID == rootGID || toString(version["storage_dir"]) == "."):
		zipPath = filepath.Join(dataRoot, candidate.relDir, strconv.Itoa(activeGID)+".zip")
	case toString(version["storage_dir"]) != "" && toString(version["storage_dir"]) != ".":
		zipPath = filepath.Join(dataRoot, toString(version["storage_dir"]), zipName)
	case toString(meta["layout"]) == "flat":
		zipPath = filepath.Join(dataRoot, candidate.relDir, strconv.Itoa(activeGID)+".zip")
	default:
		zipPath = filepath.Join(dataRoot, candidate.relDir, zipName)
	}
	st, err := os.Stat(zipPath)
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		return false
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return false
	}
	defer zr.Close()
	return len(zr.File) > 0
}

func (s *Store) addImportedRootLocked(staged *rootRecord, relDir string) bool {
	dir := filepath.Join(s.Root, relDir)
	statePath := filepath.Join(dir, staged.stateFileName)
	b, err := os.ReadFile(statePath)
	if err != nil {
		return false
	}
	meta := map[string]any{}
	if json.Unmarshal(b, &meta) != nil || !validStateIdentity(meta) {
		return false
	}
	gid, token := toInt(meta["gid"]), toString(meta["token"])
	id := key(gid, token)
	if id != staged.key {
		return false
	}
	if _, exists := s.roots[id]; exists {
		return true
	}
	if s.conflicts[id] || s.identityRoot[id] != "" {
		return false
	}
	for existing := range s.identityRoot {
		oldGID, oldToken, ok := parseKey(existing)
		if ok && oldGID == gid && oldToken != token {
			return false
		}
	}
	for versionID := range versionRecords(meta) {
		if s.conflicts[versionID] || s.identityRoot[versionID] != "" && s.identityRoot[versionID] != id {
			return false
		}
	}
	layout := toString(meta["layout"])
	rec := &rootRecord{key: id, dir: dir, stateFileName: staged.stateFileName, legacy: layout != "flat" && layout != versionDirLayout, meta: meta}
	rootGID, rootToken := s.rootIdentity(rec)
	if toInt(rec.meta["root_gid"]) == 0 {
		rec.meta["root_gid"], rec.meta["root_token"] = rootGID, rootToken
	}
	if toInt(rec.meta["active_gid"]) <= 0 || toString(rec.meta["active_token"]) == "" {
		rec.meta["active_gid"], rec.meta["active_token"] = rootGID, rootToken
	}
	if !s.flatOwnedLocked(rec) {
		return false
	}
	if root := versionRecords(rec.meta)[key(rootGID, rootToken)]; root != nil {
		if toString(root["state_file"]) == "" {
			root["state_file"] = rec.stateFileName
		}
		if toString(root["gallery_file"]) == "" {
			root["gallery_file"] = inferGalleryFile(dir, rec.stateFileName)
		}
	}
	for _, version := range versionRecords(rec.meta) {
		vgid, vtoken := toInt(version["gid"]), toString(version["token"])
		if toString(version["state_file"]) == "" {
			version["state_file"] = stateName
		}
		if toString(version["gallery_file"]) == "" {
			version["gallery_file"] = inferGalleryFile(filepath.Join(s.Root, strconv.Itoa(vgid)), toString(version["state_file"]))
		}
		if vgid > 0 && vtoken != "" {
			s.registerIdentityLocked(rec, vgid, vtoken)
		}
	}
	s.roots[id] = rec
	s.registerIdentityLocked(rec, rootGID, rootToken)
	return true
}

func discoverImportedRoots(root string) (map[string]importedRoot, error) {
	out := map[string]importedRoot{}
	versionFiles := []struct {
		path string
		meta map[string]any
	}{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 || d.IsDir() || (d.Name() != metaName && d.Name() != stateName) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		meta := map[string]any{}
		if json.Unmarshal(b, &meta) != nil || !validStateIdentity(meta) {
			return nil
		}
		gid, token := toInt(meta["gid"]), toString(meta["token"])
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) == 2 && toInt(meta["root_gid"]) > 0 && (toInt(meta["root_gid"]) != gid || toString(meta["root_token"]) != token) {
			versionFiles = append(versionFiles, struct {
				path string
				meta map[string]any
			}{path, meta})
			return nil
		}
		if len(parts) != 2 && len(parts) != 3 {
			return nil
		}
		if toInt(meta["root_gid"]) == 0 {
			meta["root_gid"], meta["root_token"] = gid, token
		}
		layout := toString(meta["layout"])
		rec := &rootRecord{key: key(gid, token), dir: filepath.Dir(path), stateFileName: filepath.Base(path), legacy: layout != "flat" && layout != versionDirLayout, meta: meta}
		id := rec.key
		if _, exists := out[id]; exists {
			return errors.New("duplicate archive root state in scan source")
		}
		out[id] = importedRoot{record: rec, relDir: filepath.Dir(rel)}
		return nil
	})
	for _, file := range versionFiles {
		gid, token := toInt(file.meta["gid"]), toString(file.meta["token"])
		rootID := key(toInt(file.meta["root_gid"]), toString(file.meta["root_token"]))
		candidate, ok := out[rootID]
		if !ok {
			continue
		}
		version := versionRecords(candidate.record.meta)[key(gid, token)]
		if version == nil || toString(version["storage_dir"]) != strconv.Itoa(gid) {
			continue
		}
		version["state_file"] = filepath.Base(file.path)
		version["gallery_file"] = inferGalleryFile(filepath.Dir(file.path), filepath.Base(file.path))
		if toInt(version["page_count"]) == 0 && toInt(file.meta["page_count"]) > 0 {
			version["page_count"] = file.meta["page_count"]
		}
		if toInt64(version["archive_size"]) == 0 && toInt64(file.meta["archive_size"]) > 0 {
			version["archive_size"] = file.meta["archive_size"]
		}
	}
	for _, candidate := range out {
		rec := candidate.record
		rootID := key(toInt(rec.meta["gid"]), toString(rec.meta["token"]))
		if state := versionRecords(rec.meta)[rootID]; state != nil {
			if toString(state["state_file"]) == "" {
				state["state_file"] = rec.stateFileName
			}
			if toString(state["gallery_file"]) == "" {
				state["gallery_file"] = inferGalleryFile(rec.dir, rec.stateFileName)
			}
		}
		for id, state := range versionRecords(rec.meta) {
			if toString(state["state_file"]) == "" {
				state["state_file"] = stateName
			}
			if toString(state["gallery_file"]) == "" {
				gid, _, _ := parseKey(id)
				state["gallery_file"] = inferGalleryFile(filepath.Join(root, strconv.Itoa(gid)), toString(state["state_file"]))
			}
		}
	}
	return out, err
}

func countRootFiles(root, relDir string) int {
	n := 0
	_ = filepath.WalkDir(filepath.Join(root, relDir), func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func sameRootIdentity(dir string, gid int, token string) bool {
	for _, name := range []string{stateName, metaName} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(b, &m) == nil && validStateIdentity(m) && toInt(m["gid"]) == gid && toString(m["token"]) == token {
			return true
		}
	}
	return false
}

func importGroupCompatible(sourceRoot, targetRoot, relDir string) bool {
	sourceDir, targetDir := filepath.Join(sourceRoot, relDir), filepath.Join(targetRoot, relDir)
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return os.IsNotExist(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".archive-import-") {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return false
		}
		switch entry.Name() {
		case stateName, metaName, galleryName, metadataName, coverName:
		default:
			return false
		}
		source := filepath.Join(sourceDir, entry.Name())
		if st, err := os.Stat(source); err != nil || !st.Mode().IsRegular() {
			return false
		}
		same, err := sameFile(source, filepath.Join(targetDir, entry.Name()))
		if err != nil || !same {
			return false
		}
	}
	return true
}

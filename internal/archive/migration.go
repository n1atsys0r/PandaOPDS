package archive

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const payloadMigrationPrefix = ".archive-payload-migration-"

type payloadMove struct {
	GID    int    `json:"gid"`
	Token  string `json:"token"`
	Source string `json:"source"`
	Target string `json:"target"`
	Size   int64  `json:"size"`
	Pages  int    `json:"pages"`
	SHA256 string `json:"sha256"`
}

type payloadCacheUpdate struct {
	GID   int    `json:"gid"`
	Token string `json:"token"`
	Size  int64  `json:"size"`
	Pages int    `json:"pages"`
}

type payloadStateUpdate struct {
	Path       string               `json:"path"`
	GID        int                  `json:"gid"`
	Token      string               `json:"token"`
	RootGID    int                  `json:"root_gid"`
	RootToken  string               `json:"root_token"`
	RootRecord bool                 `json:"root_record,omitempty"`
	Caches     []payloadCacheUpdate `json:"caches"`
}

type payloadMigrationJournal struct {
	Version   int                  `json:"version"`
	RootGID   int                  `json:"root_gid"`
	RootToken string               `json:"root_token"`
	Moves     []payloadMove        `json:"moves"`
	States    []payloadStateUpdate `json:"states"`
}

type payloadMigrationPlan struct {
	journal     payloadMigrationJournal
	journalPath string
}

// MigrateLegacyPayloads moves only ZIPs resolved by an existing local shelf
// record. An unowned flat target is always a conflict, even when its bytes
// happen to match the nested source.
func MigrateLegacyPayloads(dataRoot, stateRoot string) (migrated, conflicts int, err error) {
	recovered, err := recoverPayloadMigrationJournals(dataRoot, stateRoot)
	if err != nil {
		return 0, 0, err
	}
	store, err := newStoreForMigration(dataRoot, stateRoot)
	if err != nil {
		return 0, 0, err
	}
	plans, blocked := buildPayloadMigrationPlans(store, dataRoot, stateRoot)
	if closeErr := store.Close(); closeErr != nil {
		return recovered, blocked, closeErr
	}
	conflicts += blocked
	for _, plan := range plans {
		if err := executePayloadMigration(dataRoot, stateRoot, plan); err != nil {
			return recovered + migrated, conflicts, err
		}
		migrated += len(plan.journal.Moves)
	}
	return recovered + migrated, conflicts, nil
}

// CheckpointArchiveIndex rebuilds the local snapshot without running payload
// recovery actions. The server performs those actions at normal startup.
func CheckpointArchiveIndex(dataRoot, stateRoot string) error {
	store, err := newStoreForMigration(dataRoot, stateRoot)
	if err != nil {
		return err
	}
	return store.Close()
}

func buildPayloadMigrationPlans(store *Store, dataRoot, stateRoot string) ([]payloadMigrationPlan, int) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	var candidates []payloadMigrationPlan
	blocked := 0
	claimedGID := map[int][]int{} // gid -> candidate plan indexes
	gidOwners := map[int]map[string]bool{}
	gidClaims := map[int]map[string]bool{}
	for _, rec := range store.roots {
		rootGID, rootToken := store.rootIdentity(rec)
		identities := map[string]bool{key(rootGID, rootToken): true}
		for id := range versionRecords(rec.meta) {
			identities[id] = true
		}
		for id := range identities {
			gid, token, ok := parseKey(id)
			if !ok {
				continue
			}
			if gidOwners[gid] == nil {
				gidOwners[gid] = map[string]bool{}
			}
			gidOwners[gid][rec.key] = true
			if gidClaims[gid] == nil {
				gidClaims[gid] = map[string]bool{}
			}
			gidClaims[gid][token] = true
		}
	}
	for _, rec := range store.roots {
		if toString(rec.meta["payload_layout"]) == "flat-gid" {
			continue
		}
		if store.conflicts[rec.key] || !store.flatOwnedLocked(rec) {
			blocked++
			continue
		}
		rootGID, rootToken := store.rootIdentity(rec)
		if rootGID <= 0 || rootToken == "" {
			blocked++
			continue
		}
		versions := versionRecords(rec.meta)
		rootID := key(rootGID, rootToken)
		if versions[rootID] == nil {
			versions[rootID] = map[string]any{"gid": rootGID, "token": rootToken, "storage_dir": ".", "state_file": rec.stateFileName}
		}
		activeGID, activeToken := store.activeIdentity(rec)
		activeID := key(activeGID, activeToken)
		if activeID != rootID && versions[activeID] == nil {
			blocked++
			continue
		}
		activeState := versions[activeID]
		activeReady := strings.EqualFold(toString(activeState["status"]), "ready") || strings.EqualFold(toString(rec.meta["status"]), "ready")
		ids := make([]string, 0, len(versions))
		for id := range versions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		journal := payloadMigrationJournal{Version: 1, RootGID: rootGID, RootToken: rootToken}
		blockedShelf := false
		caches := make([]payloadCacheUpdate, 0, len(versions))
		planGIDs := map[int]bool{}
		sourceOwners := map[string]string{}
		for _, id := range ids {
			gid, token, ok := parseKey(id)
			if !ok {
				blockedShelf = true
				break
			}
			state := versions[id]
			if len(gidOwners[gid]) > 1 || len(gidClaims[gid]) > 1 || toInt(state["gid"]) != gid || toString(state["token"]) != token {
				blockedShelf = true
				break
			}
			if store.conflicts[id] || store.identityRoot[id] != "" && store.identityRoot[id] != rec.key {
				blockedShelf = true
				break
			}
			targetRel := strconv.Itoa(gid) + ".zip"
			target, pathErr := validateImportTarget(dataRoot, targetRel)
			if pathErr != nil {
				blockedShelf = true
				break
			}
			// The whole shelf must be collision-free before moving any version,
			// including a pending version that will publish into the flat root.
			if _, e := os.Lstat(target); e == nil {
				blockedShelf = true
				break
			} else if !os.IsNotExist(e) {
				blockedShelf = true
				break
			}
			source := store.archivePathLocked(rec, gid, token)
			if source == "" {
				if id == activeID && activeReady {
					blockedShelf = true
					break
				}
				continue
			}
			sourceRel, relErr := filepath.Rel(dataRoot, source)
			if relErr != nil || sourceRel == ".." || strings.HasPrefix(sourceRel, ".."+string(filepath.Separator)) {
				blockedShelf = true
				break
			}
			source, pathErr = validateImportTarget(dataRoot, sourceRel)
			if pathErr != nil {
				blockedShelf = true
				break
			}
			st, e := os.Lstat(source)
			if os.IsNotExist(e) {
				if id == activeID && activeReady {
					blockedShelf = true
					break
				}
				continue
			}
			if e != nil || !st.Mode().IsRegular() {
				blockedShelf = true
				break
			}
			size, pages, digest, e := inspectPayload(source)
			if e != nil {
				blockedShelf = true
				break
			}
			if prior := sourceOwners[filepath.Clean(source)]; prior != "" && prior != id {
				blockedShelf = true
				break
			}
			sourceOwners[filepath.Clean(source)] = id
			journal.Moves = append(journal.Moves, payloadMove{GID: gid, Token: token, Source: sourceRel, Target: targetRel, Size: size, Pages: pages, SHA256: digest})
			caches = append(caches, payloadCacheUpdate{GID: gid, Token: token, Size: size, Pages: pages})
			planGIDs[gid] = true
		}
		if blockedShelf || len(journal.Moves) == 0 {
			if blockedShelf {
				blocked++
			}
			continue
		}
		rootState := filepath.Join(rec.dir, rec.stateFileName)
		rootRel, e := filepath.Rel(stateRoot, rootState)
		if e != nil || rootRel == ".." || strings.HasPrefix(rootRel, ".."+string(filepath.Separator)) {
			blocked++
			continue
		}
		if !migrationStateIdentityMatches(rootState, rootGID, rootToken, rootGID, rootToken) {
			blocked++
			continue
		}
		journal.States = append(journal.States, payloadStateUpdate{Path: rootRel, GID: rootGID, Token: rootToken, RootGID: rootGID, RootToken: rootToken, RootRecord: true, Caches: caches})
		for _, cache := range caches {
			if cache.GID == rootGID && cache.Token == rootToken {
				continue
			}
			state := versions[key(cache.GID, cache.Token)]
			dir, ok := store.versionDirLocked(rec, cache.GID, cache.Token)
			if !ok {
				blockedShelf = true
				break
			}
			name := toString(state["state_file"])
			if name != stateName && name != metaName {
				blockedShelf = true
				break
			}
			path := filepath.Join(dir, name)
			if filepath.Clean(path) == filepath.Clean(rootState) {
				continue
			}
			rel, e := filepath.Rel(stateRoot, path)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				blockedShelf = true
				break
			}
			if !migrationStateIdentityMatches(path, cache.GID, cache.Token, rootGID, rootToken) {
				blockedShelf = true
				break
			}
			journal.States = append(journal.States, payloadStateUpdate{Path: rel, GID: cache.GID, Token: cache.Token, RootGID: rootGID, RootToken: rootToken, Caches: []payloadCacheUpdate{cache}})
		}
		if blockedShelf {
			blocked++
			continue
		}
		nameHash := sha256.Sum256([]byte(key(rootGID, rootToken)))
		plan := payloadMigrationPlan{journal: journal, journalPath: filepath.Join(stateRoot, payloadMigrationPrefix+hex.EncodeToString(nameHash[:8])+".json")}
		planIndex := len(candidates)
		for gid := range planGIDs {
			claimedGID[gid] = append(claimedGID[gid], planIndex)
		}
		candidates = append(candidates, plan)
	}
	// Reject every shelf sharing an actual gid before the first rename.
	conflicted := map[int]bool{}
	for _, indexes := range claimedGID {
		unique := map[int]bool{}
		for _, i := range indexes {
			unique[i] = true
		}
		if len(unique) > 1 {
			for i := range unique {
				conflicted[i] = true
			}
		}
	}
	out := make([]payloadMigrationPlan, 0, len(candidates))
	for i, plan := range candidates {
		if conflicted[i] {
			blocked++
			continue
		}
		out = append(out, plan)
	}
	return out, blocked
}

func migrationStateIdentityMatches(path string, gid int, token string, rootGID int, rootToken string) bool {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	meta := map[string]any{}
	if json.Unmarshal(b, &meta) != nil || !validStateIdentity(meta) || toInt(meta["gid"]) != gid || toString(meta["token"]) != token {
		return false
	}
	storedRootGID, storedRootToken := toInt(meta["root_gid"]), toString(meta["root_token"])
	if storedRootGID == 0 && storedRootToken == "" {
		return gid == rootGID && token == rootToken
	}
	return storedRootGID == rootGID && storedRootToken == rootToken
}

func inspectPayload(path string) (int64, int, string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, 0, "", err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 {
		return 0, 0, "", errors.New("payload is not a nonempty regular file")
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return 0, 0, "", err
	}
	defer zr.Close()
	pages := len(zr.File)
	if pages == 0 {
		return 0, 0, "", errors.New("payload ZIP is empty")
	}
	h := sha256.New()
	writeFingerprintUint64(h, uint64(st.Size()))
	writeFingerprintUint64(h, uint64(len(zr.File)))
	for _, entry := range zr.File {
		writeFingerprintString(h, entry.Name)
		writeFingerprintUint64(h, uint64(entry.CRC32))
		writeFingerprintUint64(h, entry.CompressedSize64)
		writeFingerprintUint64(h, entry.UncompressedSize64)
		writeFingerprintUint64(h, uint64(entry.Method))
		writeFingerprintUint64(h, uint64(entry.Flags))
		writeFingerprintUint64(h, uint64(entry.Modified.UnixNano()))
	}
	return st.Size(), pages, hex.EncodeToString(h.Sum(nil)), nil
}

func writeFingerprintUint64(h hash.Hash, n uint64) {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], n)
	_, _ = h.Write(data[:])
}

func writeFingerprintString(h hash.Hash, value string) {
	writeFingerprintUint64(h, uint64(len(value)))
	_, _ = h.Write([]byte(value))
}

func executePayloadMigration(dataRoot, stateRoot string, plan payloadMigrationPlan) error {
	if err := writeDirtyMarkerAt(stateRoot); err != nil {
		return err
	}
	b, err := json.Marshal(plan.journal)
	if err != nil {
		return err
	}
	if err := writeAtomicImport(plan.journalPath, b); err != nil {
		return err
	}
	return recoverOnePayloadMigration(dataRoot, stateRoot, plan.journalPath, plan.journal)
}

func recoverPayloadMigrationJournals(dataRoot, stateRoot string) (int, error) {
	entries, err := os.ReadDir(stateRoot)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), payloadMigrationPrefix) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(stateRoot, entry.Name())
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
			return recovered, fmt.Errorf("invalid archive payload migration journal file %s", entry.Name())
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return recovered, err
		}
		var journal payloadMigrationJournal
		if json.Unmarshal(b, &journal) != nil || journal.Version != 1 || journal.RootGID <= 0 || journal.RootToken == "" || len(journal.Moves) == 0 {
			return recovered, fmt.Errorf("invalid archive payload migration journal %s", entry.Name())
		}
		if err := recoverOnePayloadMigration(dataRoot, stateRoot, path, journal); err != nil {
			return recovered, err
		}
		recovered += len(journal.Moves)
	}
	return recovered, nil
}

func recoverOnePayloadMigration(dataRoot, stateRoot, journalPath string, journal payloadMigrationJournal) error {
	if err := writeDirtyMarkerAt(stateRoot); err != nil {
		return err
	}
	for _, move := range journal.Moves {
		if move.GID <= 0 || move.Token == "" || filepath.Clean(move.Target) != strconv.Itoa(move.GID)+".zip" || move.SHA256 == "" {
			return errors.New("invalid archive payload migration intent")
		}
		source, err := validateImportTarget(dataRoot, move.Source)
		if err != nil {
			return err
		}
		target, err := validateImportTarget(dataRoot, move.Target)
		if err != nil {
			return err
		}
		if filepath.Clean(source) == filepath.Clean(target) {
			return errors.New("archive migration source and target are identical")
		}
		if err := moveJournaledPayload(source, target, move.SHA256); err != nil {
			return err
		}
	}
	for _, update := range journal.States {
		if err := applyPayloadStateUpdate(stateRoot, update); err != nil {
			return err
		}
	}
	if err := os.Remove(journalPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(stateRoot)
}

func moveJournaledPayload(source, target, expectedHash string) error {
	verify := func(path string) (bool, error) {
		st, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("archive migration path is not a regular file")
		}
		_, _, digest, err := inspectPayload(path)
		if err != nil {
			return false, err
		}
		if digest != expectedHash {
			return false, errors.New("archive migration payload hash mismatch")
		}
		return true, nil
	}
	sourceExists, err := verify(source)
	if err != nil {
		return err
	}
	targetExists, err := verify(target)
	if err != nil {
		return err
	}
	if !sourceExists && !targetExists {
		return errors.New("archive migration source and target are both missing")
	}
	if sourceExists && targetExists {
		return errors.New("archive migration source and target both exist")
	}
	if sourceExists {
		// The CLI is run with ARCHIVE_DIR exclusively offline. FUSE does not
		// consistently expose renameat2(RENAME_NOREPLACE), so recheck the target
		// immediately before a same-volume rename and verify the result.
		if _, err := os.Lstat(target); err == nil {
			return errors.New("archive migration target collision")
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(source, target); err != nil {
			return err
		}
		targetExists, err = verify(target)
		if err != nil {
			return err
		}
		if !targetExists {
			return errors.New("archive migration target disappeared after rename")
		}
	}
	return nil
}

func applyPayloadStateUpdate(stateRoot string, update payloadStateUpdate) error {
	path, err := validateImportTarget(stateRoot, update.Path)
	if err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("archive migration state path is not a regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	meta := map[string]any{}
	if json.Unmarshal(b, &meta) != nil || !validStateIdentity(meta) || toInt(meta["gid"]) != update.GID || toString(meta["token"]) != update.Token {
		return errors.New("archive migration state identity changed")
	}
	if update.RootRecord {
		if (toInt(meta["root_gid"]) > 0 && toInt(meta["root_gid"]) != update.RootGID) || (toString(meta["root_token"]) != "" && toString(meta["root_token"]) != update.RootToken) {
			return errors.New("archive migration root identity changed")
		}
		meta["payload_layout"] = "flat-gid"
		if versions, ok := meta[versionsName].(map[string]any); ok {
			for _, cache := range update.Caches {
				if raw, ok := versions[key(cache.GID, cache.Token)].(map[string]any); ok {
					raw["archive_size"], raw["page_count"] = cache.Size, cache.Pages
				}
			}
		}
		activeGID, activeToken := toInt(meta["active_gid"]), toString(meta["active_token"])
		if activeGID <= 0 || activeToken == "" {
			activeGID, activeToken = update.RootGID, update.RootToken
		}
		activeID := key(activeGID, activeToken)
		for _, cache := range update.Caches {
			if key(cache.GID, cache.Token) == activeID {
				meta["archive_size"], meta["page_count"] = cache.Size, cache.Pages
			}
		}
	}
	if update.RootGID != toInt(meta["root_gid"]) || update.RootToken != toString(meta["root_token"]) {
		if !(update.RootRecord && toInt(meta["root_gid"]) == 0 && toString(meta["root_token"]) == "") {
			return errors.New("archive migration shelf ownership changed")
		}
		meta["root_gid"], meta["root_token"] = update.RootGID, update.RootToken
	}
	if !update.RootRecord {
		for _, cache := range update.Caches {
			if cache.GID == update.GID && cache.Token == update.Token {
				meta["archive_size"], meta["page_count"] = cache.Size, cache.Pages
			}
		}
	}
	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicImport(path, encoded)
}

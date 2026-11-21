package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pandaopds/internal/storage"
)

const archiveDocumentLinksMarker = "archive_document_links"

// LinkLegacyDocuments reconciles imported archive path evidence with documents
// produced by a completed explicit scan. Call only after Scan succeeds.
func LinkLegacyDocuments(db *storage.DB, archiveRoot string) (int, error) {
	if db == nil || db.DB == nil {
		return 0, errors.New("legacy document linking requires SQLite")
	}
	var imported int
	if err := db.QueryRow(`SELECT COUNT(*) FROM legacy_imports WHERE name='archive_sidecars'`).Scan(&imported); err != nil {
		return 0, err
	}
	if imported == 0 {
		return 0, nil
	}
	type evidence struct {
		path   string
		status string
		gid    int
		token  string
		size   int64
		mtime  int64
		sha    string
	}
	rows, err := db.Query(`SELECT v.archive_path,v.status,p.gid,p.token,v.source_size,v.source_mtime_ns,v.source_sha256 FROM archive_versions v JOIN publications p ON p.id=v.publication_id WHERE v.archive_path<>'' ORDER BY v.archive_path,p.gid,p.token`)
	if err != nil {
		return 0, err
	}
	byPath := map[string]evidence{}
	for rows.Next() {
		var e evidence
		if err = rows.Scan(&e.path, &e.status, &e.gid, &e.token, &e.size, &e.mtime, &e.sha); err != nil {
			rows.Close()
			return 0, err
		}
		e.path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(e.path)))
		if e.path == "." || filepath.IsAbs(filepath.FromSlash(e.path)) || e.path == ".." || strings.HasPrefix(e.path, "../") {
			rows.Close()
			return 0, fmt.Errorf("legacy archive path is unsafe: %q", e.path)
		}
		if prior, ok := byPath[e.path]; ok {
			if prior.gid != e.gid || !strings.EqualFold(prior.token, e.token) {
				rows.Close()
				return 0, fmt.Errorf("legacy archive path %s is claimed by multiple EH identities", e.path)
			}
			continue
		}
		byPath[e.path] = e
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	type link struct {
		evidence
		id               string
		pub              sql.NullInt64
		source           sql.NullString
		target           string
		frozenGID        int
		frozenToken      string
		hasLocalProgress bool
	}
	links := []link{}
	missingAtImport := 0
	unverified := 0
	conflicts := 0
	alreadyLinked := 0
	reasons := map[string]int{}
	for _, ev := range byPath {
		if ev.size <= 0 {
			// No payload existed when the legacy source was imported. Keep its
			// sidecar history in SQLite, but do not make it a reason to block
			// linking other scanned files or opening the service.
			missingAtImport++
			reasons["source_missing_at_import"]++
			continue
		} // no source payload existed when sidecars were imported
		var x link
		x.evidence = ev
		err = db.QueryRow(`SELECT id,publication_id,association_source,progression_target,progression_gid,progression_token FROM documents WHERE archive_path=? AND status='available'`, ev.path).Scan(&x.id, &x.pub, &x.source, &x.target, &x.frozenGID, &x.frozenToken)
		if errors.Is(err, sql.ErrNoRows) {
			unverified++
			reasons["no_document"]++
			continue
		}
		if err != nil {
			return 0, err
		}
		var size, mtime int64
		var hash string
		if err = db.QueryRow(`SELECT size,mtime_ns,COALESCE(content_sha256,'') FROM documents WHERE id=?`, x.id).Scan(&size, &mtime, &hash); err != nil {
			return 0, err
		}
		if size != ev.size {
			unverified++
			reasons["stale_size"]++
			continue
		}
		if mtime != ev.mtime {
			unverified++
			reasons["stale_mtime"]++
			continue
		}
		if hash != "" && ev.sha != "" && !strings.EqualFold(hash, ev.sha) {
			unverified++
			reasons["hash_mismatch"]++
			continue
		}
		actualSize, actualMTime, actualHash, e := fingerprintArchivePath(archiveRoot, ev.path)
		if e != nil {
			unverified++
			reasons["stat_error"]++
			continue
		}
		if actualSize != ev.size {
			unverified++
			reasons["stale_size"]++
			continue
		}
		if actualMTime != ev.mtime {
			unverified++
			reasons["stale_mtime"]++
			continue
		}
		if hash != "" && ev.sha != "" && actualHash != "" && !strings.EqualFold(actualHash, ev.sha) {
			unverified++
			reasons["hash_mismatch"]++
			continue
		}
		if x.pub.Valid {
			var oldGID int
			var oldToken string
			if err = db.QueryRow(`SELECT gid,token FROM publications WHERE id=?`, x.pub.Int64).Scan(&oldGID, &oldToken); err != nil {
				return 0, err
			}
			if oldGID != ev.gid || !strings.EqualFold(oldToken, ev.token) {
				conflicts++
				reasons["conflict"]++
				continue
			}
			alreadyLinked++
		} else if x.source.Valid && x.source.String == "manual" {
			conflicts++
			reasons["conflict"]++
			continue
		}
		if x.target == "eh" && (x.frozenGID != ev.gid || !strings.EqualFold(x.frozenToken, ev.token)) {
			conflicts++
			reasons["conflict"]++
			continue
		}
		if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM progressions WHERE target_type='document' AND target_id=?)`, x.id).Scan(&x.hasLocalProgress); err != nil {
			return 0, err
		}
		links = append(links, x)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO legacy_imports(name,completed_at,details_json) VALUES(?,?,?) ON CONFLICT(name) DO UPDATE SET completed_at=excluded.completed_at,details_json=excluded.details_json`, archiveDocumentLinksMarker, time.Now().UTC().Format(time.RFC3339Nano), `{"status":"linking"}`); err != nil {
		return 0, err
	}
	linked := 0
	for _, x := range links {
		var currentPub sql.NullInt64
		var currentSource sql.NullString
		var target string
		var frozenGID int
		var frozenToken string
		if err = tx.QueryRow(`SELECT publication_id,association_source,progression_target,progression_gid,progression_token FROM documents WHERE id=? AND status='available'`, x.id).Scan(&currentPub, &currentSource, &target, &frozenGID, &frozenToken); err != nil {
			return 0, err
		}
		if currentPub.Valid {
			var currentGID int
			var currentToken string
			if err = tx.QueryRow(`SELECT gid,token FROM publications WHERE id=?`, currentPub.Int64).Scan(&currentGID, &currentToken); err != nil {
				return 0, err
			}
			if currentGID != x.gid || !strings.EqualFold(currentToken, x.token) {
				reasons["conflict"]++
				conflicts++
				continue
			}
		} else if currentSource.Valid && currentSource.String == "manual" {
			// An explicit user unlink is a manual decision too; a later scan
			// must not silently restore the legacy association.
			reasons["conflict"]++
			conflicts++
			continue
		}
		if target == "eh" && (frozenGID != x.gid || !strings.EqualFold(frozenToken, x.token)) {
			reasons["conflict"]++
			conflicts++
			continue
		}
		var pubID int64
		if currentPub.Valid {
			pubID = currentPub.Int64
		} else if err = tx.QueryRow(`SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, x.gid, strings.ToLower(x.token)).Scan(&pubID); err != nil {
			return 0, err
		}
		if !currentPub.Valid {
			var res sql.Result
			res, err = tx.Exec(`UPDATE documents SET publication_id=?,association_source='legacy_sidecar',association_evidence='archive_version_path',updated_at=? WHERE id=? AND publication_id IS NULL AND association_source IS NULL`, pubID, time.Now().UTC().Format(time.RFC3339Nano), x.id)
			if err != nil {
				return 0, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				reasons["conflict"]++
				conflicts++
				continue
			}
			linked++
		} else if !currentSource.Valid || (currentSource.String != "manual" && currentSource.String != "download_job" && currentSource.String != "legacy_sidecar") {
			if _, err = tx.Exec(`UPDATE documents SET association_source='legacy_sidecar',association_evidence='archive_version_path',updated_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), x.id); err != nil {
				return 0, err
			}
		}
		var hasLocalProgress bool
		if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM progressions WHERE target_type='document' AND target_id=?)`, x.id).Scan(&hasLocalProgress); err != nil {
			return 0, err
		}
		if !hasLocalProgress && !x.hasLocalProgress && target != "eh" && (!currentSource.Valid || currentSource.String != "manual") {
			if _, err = tx.Exec(`UPDATE documents SET progression_target='eh',progression_gid=?,progression_token=? WHERE id=? AND progression_target='document' AND association_source='legacy_sidecar' AND publication_id=? AND NOT EXISTS(SELECT 1 FROM progressions WHERE target_type='document' AND target_id=?)`, x.gid, strings.ToLower(x.token), x.id, pubID, x.id); err != nil {
				return 0, err
			}
		}
	}
	reasons["linked"] = linked
	reasons["verified_paths"] = len(links)
	reasons["already_linked"] = alreadyLinked
	reasonsJSON, _ := json.Marshal(reasons)
	details := fmt.Sprintf(`{"linked":%d,"verified_paths":%d,"missing_at_import":%d,"unverified":%d,"conflicts":%d,"already_linked":%d,"reasons":%s}`, linked, len(links), missingAtImport, unverified, conflicts, alreadyLinked, reasonsJSON)
	if _, err = tx.Exec(`INSERT INTO legacy_imports(name,completed_at,details_json) VALUES(?,?,?) ON CONFLICT(name) DO UPDATE SET completed_at=excluded.completed_at,details_json=excluded.details_json`, archiveDocumentLinksMarker, time.Now().UTC().Format(time.RFC3339Nano), details); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return linked, nil
}

// LegacyDocumentLinkDiagnostics returns bounded reason counts from the latest
// reconciliation run. No raw metadata, path, token, or URL is exposed.
func LegacyDocumentLinkDiagnostics(db *storage.DB) (map[string]int, error) {
	if db == nil || db.DB == nil {
		return nil, errors.New("legacy document linking requires SQLite")
	}
	var raw string
	err := db.QueryRow(`SELECT details_json FROM legacy_imports WHERE name=?`, archiveDocumentLinksMarker).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	var summary struct {
		Reasons map[string]int `json:"reasons"`
	}
	if err = json.Unmarshal([]byte(raw), &summary); err != nil {
		return nil, err
	}
	if summary.Reasons == nil {
		summary.Reasons = map[string]int{}
	}
	return summary.Reasons, nil
}

func fingerprintArchivePath(rootPath, rel string) (int64, int64, string, error) {
	rootInfo, err := os.Lstat(rootPath)
	if err != nil {
		return 0, 0, "", err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return 0, 0, "", errors.New("archive root is not a real directory")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return 0, 0, "", errors.New("unsafe archive relative path")
	}
	r, err := os.OpenRoot(rootPath)
	if err != nil {
		return 0, 0, "", err
	}
	defer r.Close()
	parts := strings.Split(clean, string(filepath.Separator))
	for i := range parts {
		path := filepath.Join(parts[:i+1]...)
		st, e := r.Lstat(path)
		if e != nil {
			return 0, 0, "", e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return 0, 0, "", errors.New("archive path contains symlink")
		}
		if i < len(parts)-1 && !st.IsDir() {
			return 0, 0, "", errors.New("archive path component is not a directory")
		}
	}
	before, err := r.Lstat(clean)
	if err != nil {
		return 0, 0, "", err
	}
	if !before.Mode().IsRegular() {
		return 0, 0, "", errors.New("archive payload is not regular")
	}
	return before.Size(), before.ModTime().UnixNano(), "", nil
}

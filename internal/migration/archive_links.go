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
		path, status            string
		gid                     int
		token                   string
		sourceSize, sourceMTime int64
	}
	rows, err := db.Query(`SELECT v.archive_path,v.status,p.gid,p.token,v.source_size,v.source_mtime_ns FROM archive_versions v JOIN publications p ON p.id=v.publication_id WHERE v.archive_path<>'' ORDER BY v.archive_path,p.gid,p.token`)
	if err != nil {
		return 0, err
	}
	byPath := map[string][]evidence{}
	for rows.Next() {
		var e evidence
		if err = rows.Scan(&e.path, &e.status, &e.gid, &e.token, &e.sourceSize, &e.sourceMTime); err != nil {
			rows.Close()
			return 0, err
		}
		e.path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(e.path)))
		if e.path == "." || filepath.IsAbs(filepath.FromSlash(e.path)) || e.path == ".." || strings.HasPrefix(e.path, "../") {
			rows.Close()
			return 0, fmt.Errorf("legacy archive path is unsafe: %q", e.path)
		}
		byPath[e.path] = append(byPath[e.path], e)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	links := []evidence{}
	reasons := map[string]int{}
	conflicts, alreadyLinked, missingAtImport, unverified := 0, 0, 0, 0
	for path, candidates := range byPath {
		identity := map[string]evidence{}
		for _, ev := range candidates {
			identity[fmt.Sprintf("%d:%s", ev.gid, strings.ToLower(ev.token))] = ev
		}
		if len(identity) == 0 {
			reasons["metadata_not_ready"]++
			continue
		}
		if len(identity) != 1 {
			reasons["identity_conflict"]++
			conflicts++
			continue
		}
		var ev evidence
		for _, ev = range identity {
		}
		_, _, _, e := fingerprintArchivePath(archiveRoot, path)
		if e != nil {
			reasons["file_unavailable"]++
			unverified++
			continue
		}
		var totalDocs int
		if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE archive_path=?`, path).Scan(&totalDocs); err != nil {
			return 0, err
		}
		if totalDocs == 0 {
			reasons["no_document"]++
			unverified++
			continue
		}
		// The scan normally leaves a single active row. When older runs created
		// duplicates, prefer the active row carrying the strongest user state.
		var id string
		err = db.QueryRow(`SELECT d.id FROM documents d WHERE d.archive_path=? ORDER BY CASE WHEN d.status='available' AND (d.publication_id IS NOT NULL OR d.association_source='manual' OR EXISTS(SELECT 1 FROM progressions q WHERE q.target_type='document' AND q.target_id=d.id)) THEN 0 WHEN d.association_source='manual' THEN 1 WHEN d.publication_id IS NOT NULL THEN 2 WHEN EXISTS(SELECT 1 FROM progressions q WHERE q.target_type='document' AND q.target_id=d.id) THEN 3 WHEN d.status='available' THEN 4 ELSE 5 END,d.created_at,d.id LIMIT 1`, path).Scan(&id)
		if err != nil {
			return 0, err
		}
		var manualDecisions, conflicting int
		if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE archive_path=? AND association_source='manual'`, path).Scan(&manualDecisions); err != nil {
			return 0, err
		}
		if manualDecisions > 0 {
			reasons["manual_decision"]++
			continue
		}
		if err = db.QueryRow(`SELECT COUNT(*) FROM documents d JOIN publications p ON p.id=d.publication_id WHERE d.archive_path=? AND (p.gid<>? OR lower(p.token)<>lower(?))`, path, ev.gid, ev.token).Scan(&conflicting); err != nil {
			return 0, err
		}
		if conflicting > 0 {
			reasons["identity_conflict"]++
			conflicts++
			continue
		}
		var pub sql.NullInt64
		var source sql.NullString
		var oldGID sql.NullInt64
		var oldToken sql.NullString
		if err = db.QueryRow(`SELECT d.publication_id,d.association_source,p.gid,p.token FROM documents d LEFT JOIN publications p ON p.id=d.publication_id WHERE d.id=?`, id).Scan(&pub, &source, &oldGID, &oldToken); err != nil {
			return 0, err
		}
		if source.Valid && source.String == "manual" {
			reasons["manual_decision"]++
			continue
		}
		if pub.Valid {
			if oldGID.Valid && int(oldGID.Int64) == ev.gid && strings.EqualFold(oldToken.String, ev.token) {
				alreadyLinked++
				reasons["already_linked"]++
				continue
			}
			reasons["identity_conflict"]++
			conflicts++
			continue
		}
		links = append(links, ev)
		if ev.sourceSize <= 0 {
			reasons["restored_from_metadata"]++
		} else if ev.sourceMTime != 0 {
			_, actualMTime, _, statErr := fingerprintArchivePath(archiveRoot, path)
			if statErr == nil && actualMTime != ev.sourceMTime {
				reasons["metadata_mtime_changed"]++
			}
		}
		// Preserve the chosen id separately through an ephemeral path lookup in
		// the transaction below; no archive bytes are opened here.
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
		var id string
		if err = tx.QueryRow(`SELECT d.id FROM documents d WHERE d.archive_path=? ORDER BY CASE WHEN d.status='available' AND (d.publication_id IS NOT NULL OR d.association_source='manual' OR EXISTS(SELECT 1 FROM progressions q WHERE q.target_type='document' AND q.target_id=d.id)) THEN 0 WHEN d.association_source='manual' THEN 1 WHEN d.publication_id IS NOT NULL THEN 2 WHEN EXISTS(SELECT 1 FROM progressions q WHERE q.target_type='document' AND q.target_id=d.id) THEN 3 WHEN d.status='available' THEN 4 ELSE 5 END,d.created_at,d.id LIMIT 1`, x.path).Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				reasons["no_document"]++
				continue
			}
			return 0, err
		}
		var currentPub sql.NullInt64
		var currentSource sql.NullString
		var target string
		if err = tx.QueryRow(`SELECT publication_id,association_source,progression_target FROM documents WHERE id=?`, id).Scan(&currentPub, &currentSource, &target); err != nil {
			return 0, err
		}
		if currentPub.Valid {
			var currentGID int
			var currentToken string
			if err = tx.QueryRow(`SELECT gid,token FROM publications WHERE id=?`, currentPub.Int64).Scan(&currentGID, &currentToken); err != nil {
				return 0, err
			}
			if currentGID != x.gid || !strings.EqualFold(currentToken, x.token) {
				reasons["identity_conflict"]++
				conflicts++
				continue
			}
		} else if currentSource.Valid && currentSource.String == "manual" {
			// An explicit user unlink is a manual decision too; a later scan
			// must not silently restore the legacy association.
			reasons["manual_decision"]++
			conflicts++
			continue
		}
		// A previously frozen progression destination is independent of this
		// publication association; preserve it unless local user action changes it.
		var pubID int64
		if currentPub.Valid {
			pubID = currentPub.Int64
		} else if err = tx.QueryRow(`SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, x.gid, strings.ToLower(x.token)).Scan(&pubID); err != nil {
			return 0, err
		}
		if !currentPub.Valid {
			var res sql.Result
			res, err = tx.Exec(`UPDATE documents SET publication_id=?,association_source='legacy_sidecar',association_evidence='archive_version_path',updated_at=? WHERE id=? AND publication_id IS NULL AND association_source IS NULL`, pubID, time.Now().UTC().Format(time.RFC3339Nano), id)
			if err != nil {
				return 0, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				reasons["identity_conflict"]++
				conflicts++
				continue
			}
			linked++
		}
		var hasLocalProgress bool
		if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM progressions WHERE target_type='document' AND target_id=?)`, id).Scan(&hasLocalProgress); err != nil {
			return 0, err
		}
		if !hasLocalProgress && target != "eh" && (!currentSource.Valid || currentSource.String != "manual") {
			if _, err = tx.Exec(`UPDATE documents SET progression_target='eh',progression_gid=?,progression_token=? WHERE id=? AND progression_target='document' AND association_source='legacy_sidecar' AND publication_id=? AND NOT EXISTS(SELECT 1 FROM progressions WHERE target_type='document' AND target_id=?)`, x.gid, strings.ToLower(x.token), id, pubID, id); err != nil {
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

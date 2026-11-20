package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pandaopds/internal/storage"
)

const archiveDocumentLinksMarker = "archive_document_links"

// LinkLegacyDocuments associates one-time imported archive path evidence with
// the documents produced by a completed archive scan. Call only after Scan
// returns success and before download workers are enabled.
func LinkLegacyDocuments(db *storage.DB, archiveRoot string) (int, error) {
	if db == nil || db.DB == nil {
		return 0, errors.New("legacy document linking requires SQLite")
	}
	var done int
	if err := db.QueryRow(`SELECT COUNT(*) FROM legacy_imports WHERE name=?`, archiveDocumentLinksMarker).Scan(&done); err != nil {
		return 0, err
	}
	if done > 0 {
		return 0, nil
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
	for _, ev := range byPath {
		if ev.sha == "" {
			// No payload existed when the legacy source was imported. Keep its
			// sidecar history in SQLite, but do not make it a reason to block
			// linking other scanned files or opening the service.
			missingAtImport++
			continue
		} // no source payload existed when sidecars were imported
		var x link
		x.evidence = ev
		err = db.QueryRow(`SELECT id,publication_id,association_source,progression_target,progression_gid,progression_token FROM documents WHERE archive_path=? AND status='available'`, ev.path).Scan(&x.id, &x.pub, &x.source, &x.target, &x.frozenGID, &x.frozenToken)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("legacy archive payload %s has no scanned document", ev.path)
		}
		if err != nil {
			return 0, err
		}
		var size int64
		var hash string
		if err = db.QueryRow(`SELECT size,COALESCE(content_sha256,'') FROM documents WHERE id=?`, x.id).Scan(&size, &hash); err != nil {
			return 0, err
		}
		if size != ev.size || !strings.EqualFold(hash, ev.sha) {
			return 0, fmt.Errorf("legacy payload fingerprint differs from scanned document at %s", ev.path)
		}
		actualSize, actualMTime, actualHash, e := fingerprintArchivePath(archiveRoot, ev.path)
		if e != nil {
			return 0, fmt.Errorf("verify legacy payload %s: %w", ev.path, e)
		}
		if actualSize != ev.size || actualMTime != ev.mtime || !strings.EqualFold(actualHash, ev.sha) {
			return 0, fmt.Errorf("legacy payload changed after import at %s", ev.path)
		}
		if x.pub.Valid {
			var oldGID int
			var oldToken string
			if err = db.QueryRow(`SELECT gid,token FROM publications WHERE id=?`, x.pub.Int64).Scan(&oldGID, &oldToken); err != nil {
				return 0, err
			}
			if oldGID != ev.gid || !strings.EqualFold(oldToken, ev.token) {
				return 0, fmt.Errorf("document %s is associated with a different EH identity", x.id)
			}
		} else if x.source.Valid && x.source.String == "manual" {
			return 0, fmt.Errorf("document %s has a manual association without a publication", x.id)
		}
		if x.target == "eh" && (x.frozenGID != ev.gid || !strings.EqualFold(x.frozenToken, ev.token)) {
			return 0, fmt.Errorf("document %s has a different frozen EH progression identity", x.id)
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
	if _, err = tx.Exec(`INSERT INTO legacy_imports(name,completed_at,details_json) VALUES(?,?,?)`, archiveDocumentLinksMarker, time.Now().UTC().Format(time.RFC3339Nano), `{"status":"linking"}`); err != nil {
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
		if target == "eh" && (frozenGID != x.gid || !strings.EqualFold(frozenToken, x.token)) {
			return 0, fmt.Errorf("document %s progression target changed during linking", x.id)
		}
		var pubID int64
		if currentPub.Valid {
			pubID = currentPub.Int64
		} else if err = tx.QueryRow(`SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, x.gid, strings.ToLower(x.token)).Scan(&pubID); err != nil {
			return 0, err
		}
		if !currentPub.Valid {
			if _, err = tx.Exec(`UPDATE documents SET publication_id=?,association_source='legacy_sidecar',association_evidence='archive_version_path',updated_at=? WHERE id=? AND publication_id IS NULL`, pubID, time.Now().UTC().Format(time.RFC3339Nano), x.id); err != nil {
				return 0, err
			}
			linked++
		} else if !currentSource.Valid || (currentSource.String != "manual" && currentSource.String != "download_job" && currentSource.String != "legacy_sidecar") {
			if _, err = tx.Exec(`UPDATE documents SET association_source='legacy_sidecar',association_evidence='archive_version_path',updated_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), x.id); err != nil {
				return 0, err
			}
		}
		if !x.hasLocalProgress && target != "eh" && (!currentSource.Valid || currentSource.String != "manual") {
			if _, err = tx.Exec(`UPDATE documents SET progression_target='eh',progression_gid=?,progression_token=? WHERE id=? AND progression_target='document'`, x.gid, strings.ToLower(x.token), x.id); err != nil {
				return 0, err
			}
		}
	}
	details := fmt.Sprintf(`{"linked":%d,"verified_paths":%d,"missing_at_import":%d}`, linked, len(links), missingAtImport)
	if _, err = tx.Exec(`UPDATE legacy_imports SET completed_at=?,details_json=? WHERE name=?`, time.Now().UTC().Format(time.RFC3339Nano), details, archiveDocumentLinksMarker); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return linked, nil
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
	f, err := r.Open(clean)
	if err != nil {
		return 0, 0, "", err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return 0, 0, "", err
	}
	if !before.Mode().IsRegular() {
		return 0, 0, "", errors.New("archive payload is not regular")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return 0, 0, "", err
	}
	after, err := f.Stat()
	if err != nil {
		return 0, 0, "", err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return 0, 0, "", errors.New("archive payload changed during verification")
	}
	return before.Size(), before.ModTime().UnixNano(), hex.EncodeToString(h.Sum(nil)), nil
}

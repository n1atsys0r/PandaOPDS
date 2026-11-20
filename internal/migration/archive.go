package migration

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pandaopds/internal/archive"
	"pandaopds/internal/storage"
)

// ImportLegacyArchive resolves sidecar layouts through archive.Store's
// strictly read-only importer, then commits one work at a time so interruption
// is cheap to retry and never alters ZIP or sidecar files.
func ImportLegacyArchive(db *storage.DB, archiveDir, stateDir string) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM legacy_imports WHERE name='archive_sidecars'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	works, err := archive.ExportLegacyArchive(archiveDir, stateDir)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	imported := 0
	for _, w := range works {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		rootState := cloneMap(w.State)
		delete(rootState, "versions")
		rootJSON, err := json.Marshal(rootState)
		if err != nil {
			tx.Rollback()
			return err
		}
		status := stringValue(w.State["status"])
		if status == "" {
			status = "unknown"
		}
		_, err = tx.Exec(`INSERT INTO archive_works(provider,gid,token,active_gid,active_token,status,created_at,state_path,raw_state_json) VALUES('eh',?,?,?,?,?,?,?,?) ON CONFLICT(provider,gid,token) DO UPDATE SET active_gid=excluded.active_gid,active_token=excluded.active_token,state_path=excluded.state_path,raw_state_json=excluded.raw_state_json`, w.GID, strings.ToLower(w.Token), w.ActiveGID, strings.ToLower(w.ActiveToken), status, now, w.StatePath, string(rootJSON))
		if err != nil {
			tx.Rollback()
			return err
		}
		var workID int
		if err = tx.QueryRow(`SELECT id FROM archive_works WHERE provider='eh' AND gid=? AND token=?`, w.GID, strings.ToLower(w.Token)).Scan(&workID); err != nil {
			tx.Rollback()
			return err
		}
		for _, v := range w.Versions {
			meta := string(v.Gallery)
			if meta == "" {
				meta = "{}"
			}
			fields := storage.DecodePublicationFields(meta)
			_, err = tx.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,title,title_jpn,category,uploader,cover_url,page_count,rating,publish_time,language,tags_json,created_at,updated_at) VALUES('eh',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(provider,gid,token) DO UPDATE SET metadata_json=CASE WHEN publications.metadata_json='{}' THEN excluded.metadata_json ELSE publications.metadata_json END,title=CASE WHEN publications.metadata_json='{}' THEN excluded.title ELSE publications.title END,title_jpn=CASE WHEN publications.metadata_json='{}' THEN excluded.title_jpn ELSE publications.title_jpn END,category=CASE WHEN publications.metadata_json='{}' THEN excluded.category ELSE publications.category END,uploader=CASE WHEN publications.metadata_json='{}' THEN excluded.uploader ELSE publications.uploader END,cover_url=CASE WHEN publications.metadata_json='{}' THEN excluded.cover_url ELSE publications.cover_url END,page_count=CASE WHEN publications.metadata_json='{}' THEN excluded.page_count ELSE publications.page_count END,rating=CASE WHEN publications.metadata_json='{}' THEN excluded.rating ELSE publications.rating END,publish_time=CASE WHEN publications.metadata_json='{}' THEN excluded.publish_time ELSE publications.publish_time END,language=CASE WHEN publications.metadata_json='{}' THEN excluded.language ELSE publications.language END,tags_json=CASE WHEN publications.metadata_json='{}' THEN excluded.tags_json ELSE publications.tags_json END,updated_at=excluded.updated_at`, v.GID, strings.ToLower(v.Token), meta, fields.Title, fields.TitleJPN, fields.Category, fields.Uploader, fields.CoverURL, fields.PageCount, fields.Rating, fields.PublishTime, fields.Language, fields.TagsJSON, now, now)
			if err != nil {
				tx.Rollback()
				return err
			}
			var pubID int
			if err = tx.QueryRow(`SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, v.GID, strings.ToLower(v.Token)).Scan(&pubID); err != nil {
				tx.Rollback()
				return err
			}
			stateJSON, e := json.Marshal(v.State)
			if e != nil {
				tx.Rollback()
				return e
			}
			vs := stringValue(v.State["status"])
			if vs == "" {
				vs = "unknown"
			}
			quality := stringValue(v.State["or"])
			size := int64Value(v.State["archive_size"])
			pages := intValue(v.State["page_count"])
			_, err = tx.Exec(`INSERT INTO archive_versions(publication_id,work_id,status,quality,archive_path,state_path,page_count,archive_size,state_json,source_size,source_mtime_ns,source_sha256) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(publication_id) DO UPDATE SET work_id=excluded.work_id,status=excluded.status,quality=excluded.quality,archive_path=excluded.archive_path,state_path=excluded.state_path,page_count=excluded.page_count,archive_size=excluded.archive_size,state_json=excluded.state_json,source_size=excluded.source_size,source_mtime_ns=excluded.source_mtime_ns,source_sha256=excluded.source_sha256`, pubID, workID, vs, quality, filepath.ToSlash(v.ArchivePath), v.StatePath, pages, size, string(stateJSON), v.SourceSize, v.SourceMTimeNS, v.SourceSHA256)
			if err != nil {
				tx.Rollback()
				return err
			}
			if len(v.Cover) > 0 {
				sum := sha256.Sum256(v.Cover)
				mt := http.DetectContentType(v.Cover)
				_, err = tx.Exec(`INSERT INTO covers(publication_id,mime,data,sha256,source,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(publication_id) DO UPDATE SET mime=excluded.mime,data=excluded.data,sha256=excluded.sha256,source=excluded.source,updated_at=excluded.updated_at`, pubID, mt, v.Cover, hex.EncodeToString(sum[:]), "legacy_sidecar", now)
				if err != nil {
					tx.Rollback()
					return err
				}
			}
			if err = importLegacyDownloadJob(tx, w, v, now); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		imported++
	}
	details, _ := json.Marshal(map[string]any{"works": imported, "source": stateDir})
	_, err = db.Exec(`INSERT INTO legacy_imports(name,completed_at,details_json) VALUES('archive_sidecars',?,?)`, now, string(details))
	return err
}

func importLegacyDownloadJob(tx *sql.Tx, work archive.LegacyWorkExport, version archive.LegacyVersionExport, now string) error {
	state := cloneMap(version.State)
	if state == nil {
		state = map[string]any{}
	}
	if stringValue(state["status"]) == "" && version.GID == work.GID && strings.EqualFold(version.Token, work.Token) {
		for _, key := range []string{"status", "error", "download_url", "or", "quality", "is_version_update"} {
			if v, ok := work.State[key]; ok {
				state[key] = v
			}
		}
	}
	status := stringValue(state["status"])
	sourceStatus := status
	jobStatus, stage, errText := "", "", ""
	switch status {
	case "pending", "queued", "downloading", "zipping", "submitting", "staging", "verifying":
		jobStatus, stage = "needs_review", "needs_review"
		errText = "legacy download was interrupted; manual review required"
	case "failed":
		jobStatus, stage = "failed", "failed"
		errText = stringValue(state["error"])
	case "ready":
		if state["is_version_update"] == true || state["upgrade_pending"] == true {
			jobStatus, stage = "needs_review", "needs_review"
			errText = stringValue(state["error"])
			if errText == "" {
				errText = "legacy version update state is ambiguous; manual review required"
			}
		}
	}
	if jobStatus == "" {
		return nil
	}
	originalError := stringValue(state["error"])
	if originalError != "" {
		errText = originalError
	}
	quality := stringValue(state["or"])
	if quality == "" {
		quality = stringValue(state["quality"])
	}
	identity := fmt.Sprintf("eh:%d:%s:%s:%s", version.GID, strings.ToLower(version.Token), sourceStatus, quality)
	intent := map[string]any{"source": "legacy_sidecar", "original_status": sourceStatus, "original_state": state, "download_url": state["download_url"], "quality": quality, "error": originalError}
	if workState := work.State["version_update"]; workState != nil {
		intent["root_version_update"] = workState
	}
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(append([]byte(identity+":"), intentJSON...))
	jobID := "legacy-" + hex.EncodeToString(sum[:16])
	if _, err = tx.Exec(`INSERT INTO download_jobs(id,provider,gid,token,quality,status,stage,automatic,force,intent_json,error,created_at,updated_at) VALUES(?,'eh',?,?,?,?,?,0,0,?,?,?,?) ON CONFLICT(id) DO NOTHING`, jobID, version.GID, strings.ToLower(version.Token), quality, jobStatus, stage, string(intentJSON), errText, now, now); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO download_job_sources(job_id,subscription_id,source,source_key,requested_quality) VALUES(?,NULL,'legacy_import',?,?) ON CONFLICT(job_id,source_key) DO NOTHING`, jobID, "legacy_import:"+hex.EncodeToString(sum[:16]), quality)
	return err
}

func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringValue(v any) string { s, _ := v.(string); return s }
func intValue(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}
func int64Value(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

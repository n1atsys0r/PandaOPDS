package archive

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pandaopds/internal/storage"
)

// NewStoreWithSQLite uses SQLite as the authoritative archive metadata store.
// ZIP payloads remain at their existing paths; sidecar JSON is not loaded or written.
func NewStoreWithSQLite(dataRoot, stateRoot string, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("archive store requires SQLite")
	}
	if dataRoot == "" {
		dataRoot = "."
	}
	if stateRoot == "" {
		stateRoot = dataRoot
	}
	if _, e := os.Stat(dataRoot); os.IsNotExist(e) {
		if e = os.MkdirAll(dataRoot, 0755); e != nil {
			return nil, e
		}
	}
	s := &Store{Root: stateRoot, DataRoot: dataRoot, db: db, roots: map[string]*rootRecord{}, identityRoot: map[string]string{}, conflicts: map[string]bool{}, snapshotWake: make(chan struct{}, 1), snapshotStop: make(chan struct{}), snapshotDone: make(chan struct{}), zipPool: newZipPool(defaultZipPoolSize, defaultZipPoolIdleTTL)}
	rows, e := db.Query(`SELECT gid,token,active_gid,active_token,status,state_path,raw_state_json FROM archive_works ORDER BY id`)
	if e != nil {
		return nil, e
	}
	type root struct {
		gid, active                           int
		token, activeToken, status, path, raw string
	}
	all := []root{}
	for rows.Next() {
		var x root
		if e = rows.Scan(&x.gid, &x.token, &x.active, &x.activeToken, &x.status, &x.path, &x.raw); e != nil {
			rows.Close()
			return nil, e
		}
		all = append(all, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, x := range all {
		meta := map[string]any{}
		if x.raw != "" {
			if e = json.Unmarshal([]byte(x.raw), &meta); e != nil {
				return nil, e
			}
		}
		if meta == nil {
			meta = map[string]any{}
		}
		meta["gid"], meta["token"] = x.gid, x.token
		meta["root_gid"], meta["root_token"] = x.gid, x.token
		meta["active_gid"], meta["active_token"] = x.active, x.activeToken
		if x.status != "" {
			meta["status"] = x.status
		}
		if _, ok := meta["layout"]; !ok {
			meta["layout"] = versionDirLayout
		}
		statePath := filepath.Join(stateRoot, filepath.FromSlash(x.path))
		dir, name := filepath.Dir(statePath), filepath.Base(statePath)
		rec := &rootRecord{key: key(x.gid, x.token), dir: dir, stateFileName: name, meta: meta}
		if _, ok := s.roots[rec.key]; ok {
			s.conflicts[rec.key] = true
			continue
		}
		s.roots[rec.key] = rec
		s.registerIdentityLocked(rec, x.gid, x.token)
		vr, e := db.Query(`SELECT p.gid,p.token,v.state_json,v.archive_path,v.state_path FROM archive_versions v JOIN publications p ON p.id=v.publication_id WHERE v.work_id=(SELECT id FROM archive_works WHERE provider='eh' AND gid=? AND token=?)`, x.gid, strings.ToLower(x.token))
		if e != nil {
			return nil, e
		}
		versions := versionRecords(rec.meta)
		for vr.Next() {
			var gid int
			var token, raw, path, statePath string
			if e = vr.Scan(&gid, &token, &raw, &path, &statePath); e != nil {
				vr.Close()
				return nil, e
			}
			m := map[string]any{}
			if e = json.Unmarshal([]byte(raw), &m); e != nil {
				vr.Close()
				return nil, e
			}
			m["gid"], m["token"] = gid, token
			if statePath != "" {
				m["state_file"] = filepath.Base(statePath)
				if filepath.Dir(statePath) != "." {
					m["storage_dir"] = filepath.ToSlash(filepath.Dir(statePath))
				}
			}
			if path != "" {
				m["archive_path"] = filepath.FromSlash(path)
			}
			versions[key(gid, token)] = m
			s.registerIdentityLocked(rec, gid, token)
		}
		e = vr.Err()
		vr.Close()
		if e != nil {
			return nil, e
		}
		rec.meta[versionsName] = versionMapAny(versions)
	}
	for _, rec := range s.roots {
		if err := s.recoverActivationLocked(rec); err != nil {
			s.zipPool.close()
			return nil, err
		}
		if err := s.cleanupPreviousZipLocked(rec); err != nil {
			s.zipPool.close()
			return nil, err
		}
	}
	if err := s.recoverDeleteIntentsLocked(); err != nil {
		s.zipPool.close()
		return nil, err
	}
	close(s.snapshotDone)
	return s, nil
}

func (s *Store) recoverDeleteIntentsLocked() error {
	rows, err := s.db.Query(`SELECT id,root_gid,root_token,status FROM archive_delete_intents WHERE status IN ('pending','needs_review') ORDER BY created_at,id`)
	if err != nil {
		return err
	}
	type intent struct {
		id            string
		gid           int
		token, status string
	}
	list := []intent{}
	for rows.Next() {
		var x intent
		if err = rows.Scan(&x.id, &x.gid, &x.token, &x.status); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, x := range list {
		if x.status == "needs_review" {
			continue
		}
		rec := s.roots[key(x.gid, x.token)]
		if rec == nil {
			continue
		}
		if err = s.resumeDeleteIntentLocked(rec, x.id); err != nil { // Keep other shelves available; the pending intent remains durable.
			continue
		}
	}
	return nil
}

func (s *Store) persistSQLiteLocked(rec *rootRecord) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.persistSQLiteTxLocked(tx, rec); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) rejectPendingDeleteIntentLocked(rec *rootRecord) error {
	if rec == nil {
		return nil
	}
	gid, token := s.rootIdentity(rec)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM archive_delete_intents WHERE provider='eh' AND root_gid=? AND root_token=? AND status IN ('pending','needs_review')`, gid, strings.ToLower(token)).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errors.New("archive work has an incomplete deletion intent")
	}
	return nil
}

func (s *Store) persistSQLiteTxLocked(tx *sql.Tx, rec *rootRecord) error {
	rootGID, rootToken := s.rootIdentity(rec)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	raw := cloneMap(rec.meta)
	delete(raw, versionsName)
	rootJSON, e := json.Marshal(raw)
	if e != nil {
		return e
	}
	activeGID, activeToken := s.activeIdentity(rec)
	status := toString(rec.meta["status"])
	if status == "" {
		status = "unknown"
	}
	statePath, _ := filepath.Rel(s.Root, filepath.Join(rec.dir, rec.stateFileName))
	if statePath == "." {
		statePath = filepath.ToSlash(filepath.Join(strconv.Itoa(rootGID), stateName))
	}
	if _, e = tx.Exec(`INSERT INTO archive_works(provider,gid,token,active_gid,active_token,status,created_at,state_path,raw_state_json) VALUES('eh',?,?,?,?,?,?,?,?) ON CONFLICT(provider,gid,token) DO UPDATE SET active_gid=excluded.active_gid,active_token=excluded.active_token,status=excluded.status,state_path=excluded.state_path,raw_state_json=excluded.raw_state_json`, rootGID, strings.ToLower(rootToken), activeGID, strings.ToLower(activeToken), status, now, filepath.ToSlash(statePath), string(rootJSON)); e != nil {
		return e
	}
	var workID int
	if e = tx.QueryRow(`SELECT id FROM archive_works WHERE provider='eh' AND gid=? AND token=?`, rootGID, strings.ToLower(rootToken)).Scan(&workID); e != nil {
		return e
	}
	for id, state := range versionRecords(rec.meta) {
		gid, token, ok := parseKey(id)
		if !ok {
			continue
		}
		versionJSON, e := json.Marshal(state)
		if e != nil {
			return e
		}
		created := now
		if _, e = tx.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,created_at,updated_at) VALUES('eh',?,?,?, ?,?) ON CONFLICT(provider,gid,token) DO NOTHING`, gid, strings.ToLower(token), "{}", created, now); e != nil {
			return e
		}
		archivePath := toString(state["archive_path"])
		if archivePath == "" {
			if p := s.archivePathLocked(rec, gid, token); p != "" {
				if rel, x := filepath.Rel(s.DataRoot, p); x == nil {
					archivePath = filepath.ToSlash(rel)
				}
			}
		}
		versionStatePath := filepath.ToSlash(filepath.Join(toString(state["storage_dir"]), toString(state["state_file"])))
		if toString(state["state_file"]) == "" {
			versionStatePath = filepath.ToSlash(filepath.Join(strconv.Itoa(gid), stateName))
		}
		if versionStatePath == "." {
			versionStatePath = statePath
		}
		quality := toString(state["or"])
		if quality == "" {
			quality = toString(state["quality"])
		}
		if _, e = tx.Exec(`INSERT INTO archive_versions(publication_id,work_id,status,quality,archive_path,state_path,page_count,archive_size,state_json) VALUES((SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?),?,?,?,?,?,?,?,?) ON CONFLICT(publication_id) DO UPDATE SET work_id=excluded.work_id,status=excluded.status,quality=excluded.quality,archive_path=excluded.archive_path,state_path=excluded.state_path,page_count=excluded.page_count,archive_size=excluded.archive_size,state_json=excluded.state_json`, gid, strings.ToLower(token), workID, toString(state["status"]), quality, filepath.ToSlash(archivePath), versionStatePath, toInt(state["page_count"]), toInt64(state["archive_size"]), string(versionJSON)); e != nil {
			return e
		}
	}
	return nil
}

func upsertPublicationMetadata(tx *sql.Tx, gid int, token, raw, createdAt, updatedAt string) error {
	f := storage.DecodePublicationFields(raw)
	_, err := tx.Exec(`INSERT INTO publications(provider,gid,token,metadata_json,title,title_jpn,category,uploader,cover_url,page_count,rating,publish_time,language,tags_json,created_at,updated_at) VALUES('eh',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(provider,gid,token) DO UPDATE SET metadata_json=excluded.metadata_json,title=excluded.title,title_jpn=excluded.title_jpn,category=excluded.category,uploader=excluded.uploader,cover_url=excluded.cover_url,page_count=excluded.page_count,rating=excluded.rating,publish_time=excluded.publish_time,language=excluded.language,tags_json=excluded.tags_json,updated_at=excluded.updated_at`, gid, strings.ToLower(token), raw, f.Title, f.TitleJPN, f.Category, f.Uploader, f.CoverURL, f.PageCount, f.Rating, f.PublishTime, f.Language, f.TagsJSON, createdAt, updatedAt)
	return err
}

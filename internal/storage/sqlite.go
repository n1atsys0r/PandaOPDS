package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DB is the process-wide SQLite database. A single connection serializes
// reads and writes; WAL supports durable checkpoints and external readers.
type DB struct{ *sql.DB }

type PublicationFields struct {
	Title, TitleJPN, Category, Uploader, CoverURL, PublishTime, Language, TagsJSON string
	PageCount                                                                      int
	Rating                                                                         float64
}

func DecodePublicationFields(raw string) PublicationFields {
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)
	get := func(keys ...string) string {
		for _, key := range keys {
			if v, ok := m[key].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	integer := func(keys ...string) int {
		for _, key := range keys {
			switch v := m[key].(type) {
			case float64:
				if v > 0 {
					return int(v)
				}
			case int:
				if v > 0 {
					return v
				}
			}
		}
		return 0
	}
	f := PublicationFields{Title: get("title"), TitleJPN: get("title_jpn"), Category: get("category"), Uploader: get("uploader"), CoverURL: get("cover_url", "thumb"), PageCount: integer("page_count", "filecount"), PublishTime: get("publish_time"), Language: get("language"), TagsJSON: "[]"}
	if rating, ok := m["rating"].(float64); ok {
		f.Rating = rating
	}
	if f.PublishTime == "" {
		if posted := integer("posted"); posted > 0 {
			f.PublishTime = time.Unix(int64(posted), 0).UTC().Format(time.RFC3339)
		}
	}
	if tags, ok := m["tags"]; ok && tags != nil {
		if b, err := json.Marshal(tags); err == nil {
			f.TagsJSON = string(b)
		}
	}
	return f
}

func Open(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	privateFile, err := os.OpenFile(absolutePath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = privateFile.Close(); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: absolutePath}).String() + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db}, nil
}

func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > 10 {
		return fmt.Errorf("database schema version %d is newer than this application supports", version)
	}
	if version == 10 {
		return tx.Commit()
	}
	if version == 9 {
		if err := addColumnIfMissing(tx, "subscriptions", "exclude_from_all", `ALTER TABLE subscriptions ADD COLUMN exclude_from_all INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if _, err := tx.Exec(`PRAGMA user_version=10`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(10,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return tx.Commit()
	}
	if version == 8 {
		if err := migrateArchiveAssociationIdentity(tx); err != nil {
			return err
		}
		version = 9
	}
	if version == 1 {
		for _, col := range []struct{ table, name, stmt string }{
			{"documents", "progression_gid", `ALTER TABLE documents ADD COLUMN progression_gid INTEGER NOT NULL DEFAULT 0`},
			{"documents", "progression_token", `ALTER TABLE documents ADD COLUMN progression_token TEXT NOT NULL DEFAULT ''`},
			{"download_jobs", "force", `ALTER TABLE download_jobs ADD COLUMN force INTEGER NOT NULL DEFAULT 0`},
			{"subscription_items", "favcat", `ALTER TABLE subscription_items ADD COLUMN favcat INTEGER`},
			{"publications", "title", `ALTER TABLE publications ADD COLUMN title TEXT NOT NULL DEFAULT ''`},
			{"publications", "title_jpn", `ALTER TABLE publications ADD COLUMN title_jpn TEXT NOT NULL DEFAULT ''`},
			{"publications", "category", `ALTER TABLE publications ADD COLUMN category TEXT NOT NULL DEFAULT ''`},
			{"publications", "uploader", `ALTER TABLE publications ADD COLUMN uploader TEXT NOT NULL DEFAULT ''`},
			{"publications", "cover_url", `ALTER TABLE publications ADD COLUMN cover_url TEXT NOT NULL DEFAULT ''`},
			{"publications", "page_count", `ALTER TABLE publications ADD COLUMN page_count INTEGER NOT NULL DEFAULT 0`},
			{"publications", "rating", `ALTER TABLE publications ADD COLUMN rating REAL NOT NULL DEFAULT 0`},
			{"publications", "publish_time", `ALTER TABLE publications ADD COLUMN publish_time TEXT NOT NULL DEFAULT ''`},
			{"publications", "language", `ALTER TABLE publications ADD COLUMN language TEXT NOT NULL DEFAULT ''`},
			{"publications", "tags_json", `ALTER TABLE publications ADD COLUMN tags_json TEXT NOT NULL DEFAULT '[]'`},
		} {
			if err := addColumnIfMissing(tx, col.table, col.name, col.stmt); err != nil {
				return err
			}
		}
		version = 2
	}
	if version == 2 {
		if err := addColumnIfMissing(tx, "download_job_sources", "requested_quality", `ALTER TABLE download_job_sources ADD COLUMN requested_quality TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
		if _, err := tx.Exec(`PRAGMA user_version=3`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES(3,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		version = 3
	}
	if version == 3 {
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS archive_delete_intents(id TEXT PRIMARY KEY,provider TEXT NOT NULL,root_gid INTEGER NOT NULL,root_token TEXT NOT NULL,operation TEXT NOT NULL CHECK(operation IN ('remove','reset')),status TEXT NOT NULL CHECK(status IN ('pending','complete','failed','needs_review')),created_at TEXT NOT NULL,updated_at TEXT NOT NULL,error TEXT);
CREATE TABLE IF NOT EXISTS archive_delete_payloads(intent_id TEXT NOT NULL REFERENCES archive_delete_intents(id) ON DELETE CASCADE,archive_path TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('pending','deleted','needs_review')),expected_size INTEGER NOT NULL,expected_mtime_ns INTEGER NOT NULL,expected_sha256 TEXT NOT NULL,error TEXT,PRIMARY KEY(intent_id,archive_path));`); err != nil {
			return err
		}
		if _, err := tx.Exec(`PRAGMA user_version=4`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at)VALUES(4,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		version = 4
	}
	if version == 4 {
		for _, col := range []struct{ name, stmt string }{{"source_size", `ALTER TABLE archive_versions ADD COLUMN source_size INTEGER NOT NULL DEFAULT 0`}, {"source_mtime_ns", `ALTER TABLE archive_versions ADD COLUMN source_mtime_ns INTEGER NOT NULL DEFAULT 0`}, {"source_sha256", `ALTER TABLE archive_versions ADD COLUMN source_sha256 TEXT NOT NULL DEFAULT ''`}} {
			if err := addColumnIfMissing(tx, "archive_versions", col.name, col.stmt); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`PRAGMA user_version=5`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at)VALUES(5,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		version = 5
	}
	if version == 5 {
		if err := addColumnIfMissing(tx, "publications", "metadata_level", `ALTER TABLE publications ADD COLUMN metadata_level INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if _, err := tx.Exec(`PRAGMA user_version=6`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at)VALUES(6,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		version = 6
	}
	if version == 6 {
		if err := addColumnIfMissing(tx, "subscription_runs", "coverage", `ALTER TABLE subscription_runs ADD COLUMN coverage TEXT NOT NULL DEFAULT 'unknown'`); err != nil {
			return err
		}
		if err := addColumnIfMissing(tx, "download_jobs", "intent_json", `ALTER TABLE download_jobs ADD COLUMN intent_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return err
		}
		if err := addColumnIfMissing(tx, "subscription_items", "purged", `ALTER TABLE subscription_items ADD COLUMN purged INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if _, err := tx.Exec(`PRAGMA user_version=7`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at)VALUES(7,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		version = 7
	}
	if version == 7 {
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS archive_versions_work_id ON archive_versions(work_id)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`PRAGMA user_version=8`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at)VALUES(8,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if err := migrateArchiveAssociationIdentity(tx); err != nil {
			return err
		}
		version = 9
	}
	if version == 9 {
		if err := addColumnIfMissing(tx, "subscriptions", "exclude_from_all", `ALTER TABLE subscriptions ADD COLUMN exclude_from_all INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if _, err := tx.Exec(`PRAGMA user_version=10`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at)VALUES(10,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return tx.Commit()
	}
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS publications(id INTEGER PRIMARY KEY, provider TEXT NOT NULL, gid INTEGER NOT NULL, token TEXT NOT NULL, metadata_json TEXT NOT NULL DEFAULT '{}', title TEXT NOT NULL DEFAULT '', title_jpn TEXT NOT NULL DEFAULT '', category TEXT NOT NULL DEFAULT '', uploader TEXT NOT NULL DEFAULT '', cover_url TEXT NOT NULL DEFAULT '', page_count INTEGER NOT NULL DEFAULT 0, rating REAL NOT NULL DEFAULT 0, publish_time TEXT NOT NULL DEFAULT '', language TEXT NOT NULL DEFAULT '', tags_json TEXT NOT NULL DEFAULT '[]', metadata_level INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(provider,gid,token));
CREATE TABLE IF NOT EXISTS documents(id TEXT PRIMARY KEY, archive_path TEXT NOT NULL, format TEXT NOT NULL, size INTEGER NOT NULL DEFAULT 0, mtime_ns INTEGER NOT NULL DEFAULT 0, content_sha256 TEXT, status TEXT NOT NULL, publication_id INTEGER REFERENCES publications(id), association_source TEXT, association_evidence TEXT, progression_target TEXT NOT NULL DEFAULT 'document', progression_gid INTEGER NOT NULL DEFAULT 0, progression_token TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', page_count INTEGER NOT NULL DEFAULT 0, scan_run_id TEXT, missing_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS documents_status_path ON documents(status,archive_path);
CREATE UNIQUE INDEX IF NOT EXISTS documents_available_path ON documents(archive_path) WHERE status='available';
CREATE TABLE IF NOT EXISTS document_pages(document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,page_index INTEGER NOT NULL,central_index INTEGER NOT NULL,entry_name TEXT NOT NULL,mime TEXT NOT NULL,size INTEGER NOT NULL,PRIMARY KEY(document_id,page_index));
CREATE TABLE IF NOT EXISTS archive_scan_runs(id TEXT PRIMARY KEY,status TEXT NOT NULL,started_at TEXT NOT NULL,finished_at TEXT,files_seen INTEGER NOT NULL DEFAULT 0,documents_found INTEGER NOT NULL DEFAULT 0,error TEXT);
CREATE TABLE IF NOT EXISTS covers(publication_id INTEGER PRIMARY KEY REFERENCES publications(id) ON DELETE CASCADE, mime TEXT NOT NULL, data BLOB NOT NULL, sha256 TEXT NOT NULL, source TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS archive_works(id INTEGER PRIMARY KEY, provider TEXT NOT NULL, gid INTEGER NOT NULL, token TEXT NOT NULL, active_gid INTEGER NOT NULL, active_token TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, state_path TEXT NOT NULL DEFAULT '', raw_state_json TEXT NOT NULL DEFAULT '{}', UNIQUE(provider,gid,token));
CREATE TABLE IF NOT EXISTS archive_versions(publication_id INTEGER PRIMARY KEY REFERENCES publications(id), work_id INTEGER NOT NULL REFERENCES archive_works(id) ON DELETE CASCADE, status TEXT NOT NULL, quality TEXT NOT NULL DEFAULT '', archive_path TEXT NOT NULL DEFAULT '', state_path TEXT NOT NULL DEFAULT '', page_count INTEGER NOT NULL DEFAULT 0, archive_size INTEGER NOT NULL DEFAULT 0, state_json TEXT NOT NULL DEFAULT '{}',source_size INTEGER NOT NULL DEFAULT 0,source_mtime_ns INTEGER NOT NULL DEFAULT 0,source_sha256 TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS archive_versions_work_id ON archive_versions(work_id);
CREATE TABLE IF NOT EXISTS progressions(target_type TEXT NOT NULL, target_id TEXT NOT NULL, gid INTEGER NOT NULL DEFAULT 0, token TEXT NOT NULL DEFAULT '', modified TEXT NOT NULL, device_json TEXT NOT NULL, progression REAL NOT NULL, title TEXT, references_json TEXT NOT NULL DEFAULT '[]', written INTEGER NOT NULL, snapshot_json TEXT, snapshot_category TEXT NOT NULL DEFAULT '', legacy_raw_json BLOB, PRIMARY KEY(target_type,target_id));
CREATE INDEX IF NOT EXISTS progressions_written ON progressions(written DESC);
CREATE TABLE IF NOT EXISTS progression_aliases(alias TEXT PRIMARY KEY, target_id TEXT NOT NULL, target_type TEXT NOT NULL DEFAULT 'eh');
CREATE INDEX IF NOT EXISTS progression_alias_target ON progression_aliases(target_type,target_id);
CREATE TABLE IF NOT EXISTS progression_protected(target_type TEXT NOT NULL, target_id TEXT NOT NULL, PRIMARY KEY(target_type,target_id));
CREATE TABLE IF NOT EXISTS subscriptions(id TEXT PRIMARY KEY, name TEXT NOT NULL, source TEXT NOT NULL, query_json TEXT NOT NULL, interval_seconds INTEGER NOT NULL DEFAULT 3600, enabled INTEGER NOT NULL DEFAULT 1, auto_download INTEGER NOT NULL DEFAULT 0, download_rules_json TEXT NOT NULL DEFAULT '{}', revision INTEGER NOT NULL DEFAULT 1, baseline_complete INTEGER NOT NULL DEFAULT 0, exclude_from_all INTEGER NOT NULL DEFAULT 0, last_run_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS subscription_items(subscription_id TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE, publication_id INTEGER NOT NULL REFERENCES publications(id), first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, last_seen_run TEXT NOT NULL DEFAULT '', inspected INTEGER NOT NULL DEFAULT 0, hidden INTEGER NOT NULL DEFAULT 0, remote_member INTEGER NOT NULL DEFAULT 1, first_seen_revision INTEGER NOT NULL DEFAULT 1, baseline_item INTEGER NOT NULL DEFAULT 1, pending_auto INTEGER NOT NULL DEFAULT 0, favcat INTEGER, purged INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(subscription_id,publication_id));
CREATE INDEX IF NOT EXISTS subscription_items_order ON subscription_items(subscription_id,first_seen_at DESC,publication_id DESC);
CREATE TABLE IF NOT EXISTS subscription_runs(id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE, started_at TEXT NOT NULL, finished_at TEXT, status TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 1, cursor TEXT NOT NULL DEFAULT '', pages INTEGER NOT NULL DEFAULT 0, results INTEGER NOT NULL DEFAULT 0, error TEXT, complete INTEGER NOT NULL DEFAULT 0, coverage TEXT NOT NULL DEFAULT 'unknown');
CREATE TABLE IF NOT EXISTS download_jobs(id TEXT PRIMARY KEY, provider TEXT NOT NULL, gid INTEGER NOT NULL, token TEXT NOT NULL, quality TEXT NOT NULL, status TEXT NOT NULL, stage TEXT NOT NULL, automatic INTEGER NOT NULL DEFAULT 0, force INTEGER NOT NULL DEFAULT 0, suppress_auto INTEGER NOT NULL DEFAULT 0, intent_json TEXT NOT NULL DEFAULT '{}', error TEXT, document_id TEXT REFERENCES documents(id), created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS download_jobs_active_identity ON download_jobs(provider,gid,token) WHERE status IN ('queued','running','needs_review');
CREATE INDEX IF NOT EXISTS download_jobs_history ON download_jobs(created_at DESC,status);
CREATE TABLE IF NOT EXISTS download_job_sources(job_id TEXT NOT NULL REFERENCES download_jobs(id) ON DELETE CASCADE, subscription_id TEXT REFERENCES subscriptions(id) ON DELETE SET NULL, source TEXT NOT NULL, source_key TEXT NOT NULL, requested_quality TEXT NOT NULL DEFAULT '', PRIMARY KEY(job_id,source_key));
CREATE TABLE IF NOT EXISTS download_suppressions(provider TEXT NOT NULL,gid INTEGER NOT NULL,token TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(provider,gid,token));
CREATE TABLE IF NOT EXISTS archive_delete_intents(id TEXT PRIMARY KEY,provider TEXT NOT NULL,root_gid INTEGER NOT NULL,root_token TEXT NOT NULL,operation TEXT NOT NULL CHECK(operation IN ('remove','reset')),status TEXT NOT NULL CHECK(status IN ('pending','complete','failed','needs_review')),created_at TEXT NOT NULL,updated_at TEXT NOT NULL,error TEXT);
CREATE TABLE IF NOT EXISTS archive_delete_payloads(intent_id TEXT NOT NULL REFERENCES archive_delete_intents(id) ON DELETE CASCADE,archive_path TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('pending','deleted','needs_review')),expected_size INTEGER NOT NULL,expected_mtime_ns INTEGER NOT NULL,expected_sha256 TEXT NOT NULL,error TEXT,PRIMARY KEY(intent_id,archive_path));
CREATE TABLE IF NOT EXISTS legacy_imports(name TEXT PRIMARY KEY, completed_at TEXT NOT NULL, details_json TEXT NOT NULL DEFAULT '{}');
CREATE TABLE IF NOT EXISTS app_state(state_key TEXT PRIMARY KEY,value_json TEXT NOT NULL,updated_at TEXT NOT NULL);
PRAGMA user_version=8;`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES(8,?)`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if err := migrateArchiveAssociationIdentity(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`PRAGMA user_version=10; INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(10,?);`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateArchiveAssociationIdentity(tx *sql.Tx) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Keep the unique trusted candidate matching the ready archive path first,
	// otherwise newest trusted record then ID. If none is trusted, unlink all.
	_, err := tx.Exec(`WITH ranked AS (
 SELECT d.id, ROW_NUMBER() OVER (PARTITION BY d.publication_id ORDER BY
 CASE WHEN d.association_source='download_job' AND EXISTS(SELECT 1 FROM archive_versions av WHERE av.publication_id=d.publication_id AND av.status='ready' AND av.archive_path=d.archive_path) THEN 0 WHEN d.association_source='download_job' THEN 1 ELSE 2 END,
 CASE WHEN d.association_source='download_job' THEN d.updated_at END DESC,d.id) rn,
 SUM(CASE WHEN d.association_source='download_job' THEN 1 ELSE 0 END) OVER (PARTITION BY d.publication_id) trusted
 FROM documents d WHERE d.publication_id IS NOT NULL
), losers AS (SELECT id FROM ranked WHERE rn>1 AND trusted>0)
UPDATE documents SET publication_id=NULL,association_source='manual',association_evidence='duplicate_association_migration',updated_at=? WHERE id IN (SELECT id FROM losers);
WITH untrusted AS (SELECT publication_id,id,SUM(CASE WHEN association_source='download_job' THEN 1 ELSE 0 END) OVER(PARTITION BY publication_id) trusted,COUNT(*) OVER(PARTITION BY publication_id) n FROM documents WHERE publication_id IS NOT NULL)
UPDATE documents SET publication_id=NULL,association_source='manual',association_evidence='duplicate_association_migration',updated_at=? WHERE id IN (SELECT id FROM untrusted WHERE n>1 AND trusted=0);`, now, now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS documents_publication_unique ON documents(publication_id) WHERE publication_id IS NOT NULL; PRAGMA user_version=9; INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(9,?);`, now)
	return err
}

func addColumnIfMissing(tx *sql.Tx, table, column, statement string) error {
	rows, err := tx.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.Exec(statement)
	return err
}

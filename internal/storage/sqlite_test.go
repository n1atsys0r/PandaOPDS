package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesPrivateDatabaseAndMigratesVersionOneAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pandaopds.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("new DB mode=%#o want 0600", info.Mode().Perm())
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,applied_at TEXT NOT NULL);
CREATE TABLE documents(id TEXT PRIMARY KEY);
CREATE TABLE download_jobs(id TEXT PRIMARY KEY);
CREATE TABLE subscription_items(id INTEGER PRIMARY KEY);
CREATE TABLE publications(id INTEGER PRIMARY KEY);
CREATE TABLE download_job_sources(id INTEGER PRIMARY KEY);
CREATE TABLE archive_versions(id INTEGER PRIMARY KEY,work_id INTEGER NOT NULL DEFAULT 0);
CREATE TABLE subscription_runs(id TEXT PRIMARY KEY);
INSERT INTO schema_migrations(version,applied_at)VALUES(1,'old');PRAGMA user_version=1;`)
	if err != nil {
		t.Fatal(err)
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := Open(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	var version int
	if err = migrated.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 8 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	for _, check := range []struct{ table, column string }{{"documents", "progression_gid"}, {"download_jobs", "force"}, {"download_jobs", "intent_json"}, {"subscription_items", "favcat"}, {"subscription_items", "purged"}, {"publications", "metadata_level"}, {"archive_versions", "source_sha256"}, {"download_job_sources", "requested_quality"}, {"subscription_runs", "coverage"}} {
		rows, err := migrated.Query(`PRAGMA table_info(` + check.table + `)`)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var dflt any
			if err = rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if name == check.column {
				found = true
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		if !found {
			t.Fatalf("%s.%s missing after migration", check.table, check.column)
		}
	}
	var indexCount int
	if err = migrated.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='archive_versions_work_id'`).Scan(&indexCount); err != nil || indexCount != 1 {
		t.Fatalf("archive_versions work_id index count=%d err=%v", indexCount, err)
	}
}

func TestOpenDoesNotChangeExistingDatabaseMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("existing DB mode changed to %#o", info.Mode().Perm())
	}
}

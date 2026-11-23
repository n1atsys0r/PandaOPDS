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
CREATE TABLE documents(id TEXT PRIMARY KEY,publication_id INTEGER,association_source TEXT,association_evidence TEXT,updated_at TEXT NOT NULL DEFAULT '',archive_path TEXT NOT NULL DEFAULT '',status TEXT NOT NULL DEFAULT 'missing');
CREATE TABLE download_jobs(id TEXT PRIMARY KEY);
CREATE TABLE subscriptions(id TEXT PRIMARY KEY);
CREATE TABLE subscription_items(id INTEGER PRIMARY KEY);
CREATE TABLE publications(id INTEGER PRIMARY KEY,provider TEXT NOT NULL DEFAULT 'eh',gid INTEGER NOT NULL DEFAULT 0,token TEXT NOT NULL DEFAULT '');
CREATE TABLE download_job_sources(id INTEGER PRIMARY KEY);
CREATE TABLE archive_versions(id INTEGER PRIMARY KEY,publication_id INTEGER,work_id INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT '',archive_path TEXT NOT NULL DEFAULT '');
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
	var version int
	if err = migrated.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 10 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	if err = migrated.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(legacyPath)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer reopened.Close()
	for _, check := range []struct{ table, column string }{{"documents", "progression_gid"}, {"download_jobs", "force"}, {"download_jobs", "intent_json"}, {"subscription_items", "favcat"}, {"subscription_items", "purged"}, {"publications", "metadata_level"}, {"archive_versions", "source_sha256"}, {"download_job_sources", "requested_quality"}, {"subscription_runs", "coverage"}, {"subscriptions", "exclude_from_all"}} {
		rows, err := reopened.Query(`PRAGMA table_info(` + check.table + `)`)
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
	if err = reopened.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='archive_versions_work_id'`).Scan(&indexCount); err != nil || indexCount != 1 {
		t.Fatalf("archive_versions work_id index count=%d err=%v", indexCount, err)
	}
	if err = reopened.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='documents_publication_unique'`).Scan(&indexCount); err != nil || indexCount != 1 {
		t.Fatalf("document association index count=%d err=%v", indexCount, err)
	}
}

func TestAssociationMigrationKeepsOneTrustedDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicates.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP INDEX documents_publication_unique`); err != nil {
		t.Fatal(err)
	}
	now := "2026-11-21T00:00:00Z"
	var pub int64
	if err = db.QueryRow(`INSERT INTO publications(provider,gid,token,created_at,updated_at) VALUES('eh',77,'tok77',?,?) RETURNING id`, now, now).Scan(&pub); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, source string }{{"trusted", "download_job"}, {"manual", "manual"}} {
		if _, err = db.Exec(`INSERT INTO documents(id,archive_path,format,status,publication_id,association_source,created_at,updated_at) VALUES(?,?,'zip','available',?,?,?,?)`, row.id, row.id+".zip", pub, row.source, now, now); err != nil {
			t.Fatal(err)
		}
	}
	var pub2 int64
	if err = db.QueryRow(`INSERT INTO publications(provider,gid,token,created_at,updated_at) VALUES('eh',78,'tok78',?,?) RETURNING id`, now, now).Scan(&pub2); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"manual-a", "manual-b"} {
		if _, err = db.Exec(`INSERT INTO documents(id,archive_path,format,status,publication_id,association_source,created_at,updated_at) VALUES(?,?,'zip','available',?,'manual',?,?)`, id, id+".zip", pub2, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`DELETE FROM schema_migrations WHERE version>=9;PRAGMA user_version=8;`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var linked, unlinked int
	if err = db.QueryRow(`SELECT SUM(publication_id IS NOT NULL),SUM(publication_id IS NULL) FROM documents`).Scan(&linked, &unlinked); err != nil || linked != 1 || unlinked != 3 {
		t.Fatalf("linked=%d unlinked=%d err=%v", linked, unlinked, err)
	}
	var retained string
	if err = db.QueryRow(`SELECT id FROM documents WHERE publication_id=?`, pub).Scan(&retained); err != nil || retained != "trusted" {
		t.Fatalf("retained=%q err=%v", retained, err)
	}
	var untrustedLinked int
	if err = db.QueryRow(`SELECT COUNT(*) FROM documents WHERE publication_id=?`, pub2).Scan(&untrustedLinked); err != nil || untrustedLinked != 0 {
		t.Fatalf("untrusted duplicates retained=%d err=%v", untrustedLinked, err)
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

package catalog

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenRecordsSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := userVersion(t, c.db); got != len(migrations) {
		t.Fatalf("user_version %d, want %d", got, len(migrations))
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	if got := userVersion(t, c.db); got != len(migrations) {
		t.Fatalf("reopened user_version %d", got)
	}
}

func TestPragmasSurviveNewConnection(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	// No idle connection is kept, so each query below runs on a
	// connection the pool opened after Open returned.
	c.db.SetMaxIdleConns(0)
	for _, p := range []struct{ name, want string }{
		{"busy_timeout", "5000"},
		{"foreign_keys", "1"},
		{"journal_mode", "wal"},
		{"synchronous", "2"},
	} {
		var got string
		if err := c.db.QueryRow(`PRAGMA ` + p.name).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != p.want {
			t.Errorf("%s = %s, want %s", p.name, got, p.want)
		}
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations)+1)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	c, err := Open(path)
	if err == nil {
		c.Close()
		t.Fatal("opened a catalog from a newer binary")
	}
	want := fmt.Sprintf("schema version %d; this binary knows up to %d", len(migrations)+1, len(migrations))
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not name the versions", err)
	}
}

func TestOpenMigratesUnversionedCatalog(t *testing.T) {
	// A file an older binary wrote: the schema before relations and the
	// normalize queue, one session in it, and user_version 0.
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE sessions (
			session_uid TEXT PRIMARY KEY,
			harness TEXT NOT NULL,
			native_session_id TEXT NOT NULL,
			head_sha256 TEXT NOT NULL,
			manifest_json TEXT NOT NULL,
			ingested_at TEXT NOT NULL,
			UNIQUE (harness, native_session_id)
		);
		CREATE TABLE provenance (
			session_uid TEXT NOT NULL,
			machine_id TEXT NOT NULL,
			PRIMARY KEY (session_uid, machine_id)
		);
		CREATE TABLE artifacts (
			artifact_id TEXT PRIMARY KEY,
			session_uid TEXT NOT NULL,
			kind TEXT NOT NULL,
			relpath TEXT NOT NULL,
			sha256 TEXT NOT NULL,
			size INTEGER NOT NULL,
			UNIQUE (session_uid, relpath, sha256)
		);
		INSERT INTO sessions VALUES ('uid-1', 'terva', 'sess-1', 'aa', '{}', '2026-09-01T00:00:00Z');
		INSERT INTO artifacts VALUES ('art-1', 'uid-1', 'transcript_jsonl', 'sessions/s.jsonl', 'aa', 2);`); err != nil {
		t.Fatal(err)
	}
	if got := userVersion(t, db); got != 0 {
		t.Fatalf("fixture user_version %d", got)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if got := userVersion(t, c.db); got != len(migrations) {
		t.Fatalf("user_version %d, want %d", got, len(migrations))
	}
	list, err := c.ListSessions(t.Context())
	if state, err := c.NormalizationState(t.Context(), "uid-1"); err != nil || state != "unknown" {
		t.Fatalf("legacy state %q: %v", state, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].UID != "uid-1" {
		t.Fatalf("sessions after migration: %+v", list)
	}
	arts, err := c.Artifacts(t.Context(), "uid-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 || !arts[0].Current {
		t.Fatalf("the old head is not current: %+v", arts)
	}
	jobs, err := c.ListNormalizeJobs(t.Context())
	if err != nil || len(jobs) != 0 {
		t.Fatalf("normalize_jobs: %v %v", jobs, err)
	}
}

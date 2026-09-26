package catalog

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestDashboardMigrationAcrossBatches(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalid=%t", invalid), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			for _, migrate := range migrations[:2] {
				if err := migrate(tx); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(`PRAGMA user_version=2`); err != nil {
				t.Fatal(err)
			}
			const n = 1537
			expected := make(map[string]int64, n)
			for i := 0; i < n; i++ {
				uid := fmt.Sprintf("uid-%05d", i)
				if i == 0 {
					uid = ""
				} // The initial query must not skip the smallest key.
				when := time.Date(2026, 9, 26, 1, 0, 0, i*12345, time.FixedZone("offset", 3600))
				raw := when.Format(time.RFC3339Nano)
				expected[uid] = when.UnixNano()
				if invalid && i == n-1 {
					raw = "bad-timestamp"
				}
				if _, err := tx.Exec(`INSERT INTO sessions(session_uid,harness,native_session_id,head_sha256,manifest_json,ingested_at) VALUES(?,'terva',?,'head','{}',?)`, uid, uid, raw); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			c, err := Open(path)
			if invalid {
				if err == nil {
					c.Close()
					t.Fatal("invalid timestamp accepted")
				}
				if userVersion(t, db) != 2 {
					t.Fatal("failed migration advanced version")
				}
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				exists, err := columnExists(tx, "sessions", "web_updated_ns")
				if err != nil || exists {
					t.Fatalf("partial migration survived rollback: %v %v", exists, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			rows, err := c.db.Query(`SELECT session_uid,web_updated_ns FROM sessions`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			count := 0
			for rows.Next() {
				var uid string
				var ns int64
				if err := rows.Scan(&uid, &ns); err != nil {
					t.Fatal(err)
				}
				if want, ok := expected[uid]; !ok || ns != want {
					t.Fatalf("bad migrated timestamp for %q", uid)
				}
				count++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if count != n {
				t.Fatalf("migrated %d rows", count)
			}
		})
	}
}

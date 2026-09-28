package catalog

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// catalogAt writes a catalog at schema version v, as the release that
// shipped migrations[:v] left it, with one session row once the table
// exists.
func catalogAt(t *testing.T, path string, v int) {
	t.Helper()
	if err := CreateAtVersion(path, v); err != nil {
		t.Fatalf("build version %d: %v", v, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v > 0 {
		if _, err := db.Exec(`INSERT INTO sessions (session_uid, harness, native_session_id, head_sha256, manifest_json, ingested_at)
			VALUES ('s1', 'claude', 'n1', 'h', '{}', '2026-09-28T00:00:00Z')`); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenUpgradesEachEarlierVersionAndKeepsABackup(t *testing.T) {
	for v := 1; v < len(migrations); v++ {
		t.Run(fmt.Sprintf("from%d", v), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			catalogAt(t, path, v)
			c, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			m := c.Migrated()
			if m.From != v || m.To != len(migrations) || len(m.Steps) != len(migrations)-v {
				t.Fatalf("migrated %+v", m)
			}
			if m.Steps[len(m.Steps)-1] != stepName(migrations[len(migrations)-1]) {
				t.Fatalf("last step %q", m.Steps[len(m.Steps)-1])
			}
			var n int
			if err := c.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n); err != nil || n != 1 {
				t.Fatalf("sessions after upgrade: %d %v", n, err)
			}
			if m.Backup == "" || filepath.Dir(m.Backup) != filepath.Join(filepath.Dir(path), BackupDir) {
				t.Fatalf("backup %q", m.Backup)
			}
			st, err := os.Stat(m.Backup)
			if err != nil || st.Mode().Perm() != 0o600 {
				t.Fatalf("backup file %v %v", st, err)
			}
			if got, err := FileVersion(m.Backup); err != nil || got != v {
				t.Fatalf("backup is at version %d (%v), want %d", got, err, v)
			}
		})
	}
}

func TestOpenMakesNoBackupOfANewOrCurrentCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.db")
	for i := 0; i < 2; i++ {
		c, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		m := c.Migrated()
		c.Close()
		if m.Backup != "" || m.Created != (i == 0) {
			t.Fatalf("open %d: %+v", i, m)
		}
		if i == 1 && (m.From != m.To || len(m.Steps) != 0) {
			t.Fatalf("a current catalog migrated: %+v", m)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, BackupDir)); !os.IsNotExist(err) {
		t.Fatalf("backup dir: %v", err)
	}
}

func TestMigrationBackupsKeepTheNewestThree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	catalogAt(t, path, len(migrations)-1)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var made []string
	for i := 0; i < 5; i++ {
		b, err := backupBeforeMigrating(db, path, len(migrations)-1, start.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, filepath.Base(b))
	}
	// Another file in the directory is not the package's to remove.
	other := filepath.Join(filepath.Dir(path), BackupDir, "operator-notes.txt")
	if err := os.WriteFile(other, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backupBeforeMigrating(db, path, len(migrations)-1, start.Add(6*time.Hour)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(other))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if len(got) != 4 || got[0] != made[3] || got[1] != made[4] || !strings.HasSuffix(got[2], ".db") || got[3] != "operator-notes.txt" {
		t.Fatalf("left %v", got)
	}
}

func TestOpenCurrentRefusesAnOlderSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	catalogAt(t, path, len(migrations)-1)
	if c, err := OpenCurrent(path); err == nil {
		c.Close()
		t.Fatal("OpenCurrent migrated an older catalog")
	} else if !strings.Contains(err.Error(), "serve migrate") {
		t.Fatalf("error does not say how to upgrade: %v", err)
	}
	if v, err := FileVersion(path); err != nil || v != len(migrations)-1 {
		t.Fatalf("the refused open changed the file: %d %v", v, err)
	}
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	c, err = OpenCurrent(path)
	if err != nil {
		t.Fatalf("OpenCurrent on a current catalog: %v", err)
	}
	c.Close()
}

func TestNewerSchemaErrorNamesTheRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations)+1)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for name, open := range map[string]func(string) (*Catalog, error){"Open": Open, "OpenCurrent": OpenCurrent} {
		_, err := open(path)
		if err == nil || !strings.Contains(err.Error(), BackupDir) || !strings.Contains(err.Error(), "serve backup") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

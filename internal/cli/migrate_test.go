package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/lakelock"
)

// olderLake is a lake directory whose catalog is one schema version
// behind this binary.
func olderLake(t *testing.T) (dir string, from int) {
	t.Helper()
	dir = t.TempDir()
	from = catalog.SchemaVersion() - 1
	if err := catalog.CreateAtVersion(filepath.Join(dir, "catalog.db"), from); err != nil {
		t.Fatal(err)
	}
	return dir, from
}

func TestServeMigrateCheckChangesNothing(t *testing.T) {
	dir, from := olderLake(t)
	// --check reads beside a running serve, so a held lock is no bar.
	lock, err := lakelock.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	var stdout bytes.Buffer
	if err := Run([]string{"serve", "migrate", "--check", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("catalog schema %d, this binary writes %d: 1 migrations pending\n", from, catalog.SchemaVersion())
	if stdout.String() != want {
		t.Fatalf("check printed %q, want %q", stdout.String(), want)
	}
	if v, err := catalog.FileVersion(filepath.Join(dir, "catalog.db")); err != nil || v != from {
		t.Fatalf("check changed the version to %d (%v)", v, err)
	}
}

func TestServeMigrateUpgradesUnderTheLock(t *testing.T) {
	dir, from := olderLake(t)
	lock, err := lakelock.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = Run([]string{"serve", "migrate", "--data", dir}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), "stop serve first") {
		t.Fatalf("migrate while the lock is held: %v", err)
	}
	lock.Release()

	var stdout bytes.Buffer
	if err := Run([]string{"serve", "migrate", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{
		"catalog backup before migrating: " + filepath.Join(dir, catalog.BackupDir, "catalog-"),
		fmt.Sprintf("catalog migration %d: ", catalog.SchemaVersion()),
		fmt.Sprintf("catalog schema %d -> %d\n", from, catalog.SchemaVersion()),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("migrate output lacks %q:\n%s", want, out)
		}
	}
	stdout.Reset()
	if err := Run([]string{"serve", "migrate", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("catalog schema %d, up to date\n", catalog.SchemaVersion()); stdout.String() != want {
		t.Fatalf("second run printed %q", stdout.String())
	}
}

func TestServeMigrateNeedsACatalog(t *testing.T) {
	err := Run([]string{"serve", "migrate", "--check", "--data", t.TempDir()}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), "no catalog") {
		t.Fatalf("err %v", err)
	}
}

// Commands that open the catalog without lake.lock run beside a serve,
// possibly an older one, and must not change the schema under it. Those
// that write refuse; those that only read may run.
func TestLocklessCommandsDoNotMigrate(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		writes bool
	}{
		{[]string{"serve", "devices"}, false},
		{[]string{"serve", "devices", "revoke", "laptop"}, true},
		{[]string{"serve", "register", "--list"}, true},
		{[]string{"serve", "normalize", "--status"}, false},
		{[]string{"serve", "normalize", "--failed"}, true},
		{[]string{"serve", "compact", "--dry-run"}, true},
	} {
		t.Run(strings.Join(tc.args[1:], " "), func(t *testing.T) {
			dir, from := olderLake(t)
			err := Run(append(tc.args, "--data", dir), Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
			if tc.writes && (err == nil || !strings.Contains(err.Error(), "serve migrate")) {
				t.Fatalf("err %v", err)
			}
			if v, err := catalog.FileVersion(filepath.Join(dir, "catalog.db")); err != nil || v != from {
				t.Fatalf("version %d (%v), want %d", v, err, from)
			}
			if _, err := os.Stat(filepath.Join(dir, catalog.BackupDir)); !os.IsNotExist(err) {
				t.Fatalf("made a backup: %v", err)
			}
		})
	}
}

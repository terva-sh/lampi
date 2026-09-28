package cli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"terva.sh/lampi/internal/lakelock"
)

// Only a lock another process holds says to stop serve. A lake
// directory that cannot be created, as when --data is left off and the
// default sits under a home that does not exist, names that directory
// instead (TKT-01M3M54QK).
func TestLockErrorsSayWhatWentWrong(t *testing.T) {
	held := t.TempDir()
	lock, err := lakelock.Acquire(held)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	_, err = lockLake("compact", held, "--dry-run")
	if !errors.Is(err, lakelock.ErrHeld) || !strings.HasSuffix(err.Error(), "stop serve first, or pass --dry-run") {
		t.Fatalf("held: %v", err)
	}

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o700) })
	missing := filepath.Join(parent, "home", "lake")
	_, err = lockLake("compact", missing, "--dry-run")
	if err == nil || errors.Is(err, lakelock.ErrHeld) || strings.Contains(err.Error(), "stop serve") || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "--data") {
		t.Fatalf("unwritable: %v", err)
	}
}

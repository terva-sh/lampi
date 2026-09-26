// Package lakestate places the client's per-lake sync state and moves a
// single-lake state directory into it once.
//
// Before a machine could report to several lakes, the watermarks, the
// outbox, and the last sync and attempt records sat directly in the
// state directory. Each lake now has its own directory under lakes/,
// named after the lake's local name. What does not depend on the lake
// stays at the top: the quarantine records and agent.pid.
//
// A lake started without --data shares the state directory with the
// client, so the migration moves the files it names and nothing else.
// catalog.db, cas/ and identity.json belong to that lake and stay.
package lakestate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	_ "modernc.org/sqlite"
)

// Root is the directory of per-lake state inside the state directory.
func Root(stateDir string) string { return filepath.Join(stateDir, "lakes") }

// Dir is one lake's state directory. name is a validated lake name, so
// it is one path segment.
func Dir(stateDir, name string) string { return filepath.Join(Root(stateDir), name) }

// The legacy files, by kind. A SQLite file is copied with VACUUM INTO so
// the copy holds what its write-ahead log held. Its sidecars are left
// with the original and removed with it.
var (
	sqliteFiles = []string{"watermarks.db", "outbox.db"}
	plainFiles  = []string{"last_sync.json", "last_attempt.json"}
	sidecars    = []string{"-wal", "-shm", "-journal"}
)

// Legacy reports whether any single-lake state file is in stateDir.
func Legacy(stateDir string) (bool, error) {
	for _, name := range append(append([]string{}, sqliteFiles...), plainFiles...) {
		_, err := os.Stat(filepath.Join(stateDir, name))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

// Migrate moves the legacy state in stateDir into the directory of the
// lake called name, and reports whether it copied anything.
//
// The copy is built in a hidden directory beside the target and renamed
// into place, so the target appears whole or not at all. The legacy
// files are removed only after that rename. A crash before it leaves the
// legacy files and a partial hidden directory, which the next call
// discards and builds again. A crash after it leaves both; the next call
// sees the target and removes the legacy files. The caller holds
// whatever keeps another writer off the legacy files while this runs.
func Migrate(stateDir, name string) (bool, error) {
	dst := Dir(stateDir, name)
	if _, err := os.Stat(dst); err == nil {
		// An earlier call may have stopped on a failed sync after the
		// rename, so make the target durable again before cleanup.
		for _, d := range []string{Root(stateDir), stateDir} {
			if err := syncDir(d); err != nil {
				return false, err
			}
		}
		return false, removeLegacy(stateDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("lakestate: %w", err)
	}
	has, err := Legacy(stateDir)
	if err != nil || !has {
		return false, err
	}
	root := Root(stateDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return false, fmt.Errorf("lakestate: %w", err)
	}
	// The lakes/ entry must be durable before any legacy file goes, or a
	// crash during cleanup can keep the deletions and lose the copy.
	if err := syncDir(stateDir); err != nil {
		return false, err
	}
	tmp := filepath.Join(root, "."+name+".migrating")
	if err := os.RemoveAll(tmp); err != nil {
		return false, fmt.Errorf("lakestate: %w", err)
	}
	if err := os.Mkdir(tmp, 0o700); err != nil {
		return false, fmt.Errorf("lakestate: %w", err)
	}
	for _, f := range sqliteFiles {
		if err := vacuumInto(filepath.Join(stateDir, f), filepath.Join(tmp, f)); err != nil {
			return false, err
		}
	}
	for _, f := range plainFiles {
		if err := copyFile(filepath.Join(stateDir, f), filepath.Join(tmp, f)); err != nil {
			return false, err
		}
	}
	if err := syncDir(tmp); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return false, fmt.Errorf("lakestate: %w", err)
	}
	if err := syncDir(root); err != nil {
		return false, err
	}
	return true, removeLegacy(stateDir)
}

// vacuumInto writes a consistent copy of the SQLite file src to dst. A
// missing src is skipped.
func vacuumInto(src, dst string) error {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("lakestate: %w", err)
	}
	db, err := sql.Open("sqlite", src)
	if err != nil {
		return fmt.Errorf("lakestate: %s: %w", src, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("lakestate: %s: %w", src, err)
	}
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("lakestate: copy %s: %w", src, err)
	}
	if err := os.Chmod(dst, 0o600); err != nil {
		return fmt.Errorf("lakestate: %w", err)
	}
	return syncFile(dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lakestate: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("lakestate: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("lakestate: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return fmt.Errorf("lakestate: %w", err)
	}
	return out.Close()
}

// removeLegacy removes the legacy files and their SQLite sidecars, by
// name. Nothing else in stateDir is touched.
func removeLegacy(stateDir string) error {
	var names []string
	for _, f := range sqliteFiles {
		names = append(names, f)
		for _, s := range sidecars {
			names = append(names, f+s)
		}
	}
	names = append(names, plainFiles...)
	for _, n := range names {
		if err := os.Remove(filepath.Join(stateDir, n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("lakestate: %w", err)
		}
	}
	return syncDir(stateDir)
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("lakestate: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("lakestate: %w", err)
	}
	return f.Close()
}

// syncDir makes dir's entries durable. Windows cannot fsync a directory,
// and some filesystems answer EINVAL or ENOTSUP; those are not failures,
// since there is nothing more the caller could do. Any other error is
// returned, so the migration stops before it removes the originals.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("lakestate: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !syncUnsupported(err) {
		return fmt.Errorf("lakestate: sync %s: %w", dir, err)
	}
	return nil
}

func syncUnsupported(err error) bool {
	return runtime.GOOS == "windows" || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP)
}

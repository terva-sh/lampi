package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"
)

// BackupDir is the directory beside catalog.db that holds the copies
// Open makes before it migrates.
const BackupDir = "migration-backups"

// keepBackups is how many pre-migration copies Open leaves. The catalog
// is small next to the CAS, and three outlast a few quick upgrades.
const keepBackups = 3

// Migration is what Open did to bring a file to this binary's schema.
// From equals To when nothing ran.
type Migration struct {
	From, To int
	// Backup is the copy taken before the first step, or empty.
	Backup string
	// Steps names each migration applied, in order.
	Steps []string
	// Created is set when the file had no tables: a new lake, with
	// nothing to back up.
	Created bool
}

// SchemaVersion is the catalog schema this binary writes.
func SchemaVersion() int {
	return len(migrations)
}

// FileVersion reads a catalog's schema version without writing to it
// or taking the lake lock, so it works beside a running serve.
func FileVersion(path string) (int, error) {
	c, err := OpenReadOnly(path)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	var v int
	if err := c.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	return v, nil
}

// OpenCurrent opens an existing catalog for writing only when its
// schema is this binary's. It is for commands that do not hold
// lake.lock: a serve may be running on the file, and migrating it under
// that serve would change the schema the running process reads.
func OpenCurrent(path string) (*Catalog, error) {
	v, err := FileVersion(path)
	if err != nil {
		return nil, err
	}
	if v > len(migrations) {
		return nil, newerError(path, v)
	}
	if v < len(migrations) {
		return nil, fmt.Errorf("catalog: %s has schema version %d and this binary writes %d; start serve from this version, or run terva-lampi serve migrate with serve stopped, to upgrade it first", path, v, len(migrations))
	}
	return Open(path)
}

// Migrated reports what Open did to the file.
func (c *Catalog) Migrated() Migration {
	return c.migrated
}

func newerError(path string, v int) error {
	return fmt.Errorf("catalog: %s has schema version %d; this binary knows up to %d. Run a newer terva-lampi, or roll back: stop serve, restore catalog.db from %s or a serve backup taken before the upgrade, then start this version", path, v, len(migrations), BackupDir)
}

// isEmpty reports whether the file has no tables yet: a new catalog.
func isEmpty(db *sql.DB) (bool, error) {
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	return tables == 0, nil
}

// backupBeforeMigrating copies the catalog with VACUUM INTO before any
// step runs.
func backupBeforeMigrating(db *sql.DB, path string, v int, now time.Time) (string, error) {
	dir := filepath.Join(filepath.Dir(path), BackupDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("catalog: backup before migrating: %w", err)
	}
	// The time leads the name so the names sort in the order they were
	// taken, whatever version a restore went back to.
	dest := filepath.Join(dir, fmt.Sprintf("catalog-%s-v%d.db", now.UTC().Format("20060102T150405.000000000Z"), v))
	if _, err := db.Exec(`VACUUM INTO ?`, dest); err != nil {
		os.Remove(dest)
		return "", fmt.Errorf("catalog: backup before migrating from version %d: %w", v, err)
	}
	if err := os.Chmod(dest, 0o600); err != nil {
		return "", fmt.Errorf("catalog: backup before migrating: %w", err)
	}
	if err := pruneBackups(dir, keepBackups); err != nil {
		return "", err
	}
	return dest, nil
}

// pruneBackups removes all but the newest keep copies. Only names this
// package writes are touched.
func pruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && strings.HasPrefix(n, "catalog-") && strings.HasSuffix(n, ".db") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("catalog: %w", err)
		}
		names = names[1:]
	}
	return nil
}

// stepName is a migration's function name without its package, for the
// line serve logs per step.
func stepName(f func(*sql.Tx) error) string {
	name := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	return name[strings.LastIndex(name, ".")+1:]
}

// CreateAtVersion writes a new catalog at schema version v, as the
// release that shipped the first v migrations left it, for tests in
// other packages that upgrade an older lake.
func CreateAtVersion(path string, v int) error {
	if v < 0 || v > len(migrations) {
		return fmt.Errorf("catalog: no schema version %d", v)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("catalog: %s exists", path)
	}
	dsn, err := dataSource(path)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	defer db.Close()
	// A new file has no tables, so upgrade makes no backup.
	_, err = upgrade(db, path, migrations[:v], time.Now())
	return err
}

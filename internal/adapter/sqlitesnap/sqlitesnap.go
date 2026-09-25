// Package sqlitesnap takes a consistent copy of a SQLite database that
// another program has open, without opening the live file.
//
// The Cursor IDE and Cursor CLI readers use it. A read-only SQLite open
// of the live database is not used: with no other connection open, it
// creates -wal and -shm beside the live file and rewrites -shm on every
// open. The watcher matches those sidecars, so each sync would start
// the next one.
//
// Take copies the main file and, when it exists, the -wal. The -shm is
// not copied. It is an index of the WAL, and the first connection to
// the copy rebuilds it. A checkpoint between the two copies can pair an
// old main file with a WAL that no longer holds the pages it moved, so
// Take hashes the main file as it copies it, and after the WAL copy
// hashes the live main file again, re-stats it, and re-reads the WAL
// header. A main file whose bytes, size, or identity moved, or a WAL
// whose header changed or that disappeared, is a torn copy, and Take
// copies again. mtime is checked too but is not enough on its own: the
// kernel stamps it from a coarse clock, so a commit and a checkpoint
// inside one tick leave it unchanged. Frames appended to the WAL during the copy are not a
// change: SQLite stops at the last commit frame it can verify. The copy
// is then opened read-only and PRAGMA quick_check must return ok, or
// that attempt is retried too. Take gives up after Tries attempts.
package sqlitesnap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Tries is how many copies Take makes before it reports ErrChanged.
const Tries = 5

// walHeaderSize is the WAL file header. It holds the checkpoint
// sequence and the salts, which change when a checkpoint resets the WAL.
const walHeaderSize = 32

// ErrChanged is a database that moved under every copy Take made.
var ErrChanged = errors.New("sqlitesnap: database changed during every copy")

// betweenCopies runs after the main file is copied and before the WAL
// is. Tests use it to checkpoint in that window. Nil does nothing.
var betweenCopies func(src string)

// Snapshot is a private copy of a database, open read-only.
type Snapshot struct {
	DB  *sql.DB
	dir string
}

// Close closes the database and removes the copy. A nil Snapshot and
// a second call are safe.
func (s *Snapshot) Close() error {
	if s == nil || s.dir == "" {
		return nil
	}
	err := s.DB.Close()
	if rmErr := os.RemoveAll(s.dir); err == nil {
		err = rmErr
	}
	s.dir = ""
	return err
}

// Take copies src into a new directory under the system temp directory
// named with prefix, checks the copy, and opens it read-only. A missing
// src is an error that wraps os.ErrNotExist. When every attempt fails,
// the error is the last one: ErrChanged, or why the copy did not open
// or pass quick_check.
func Take(ctx context.Context, src, prefix string) (*Snapshot, error) {
	var last error
	for range Tries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir, stable, err := copyOnce(src, prefix)
		if err != nil {
			return nil, err
		}
		if !stable {
			_ = os.RemoveAll(dir)
			last = ErrChanged
			continue
		}
		db, err := openChecked(ctx, filepath.Join(dir, filepath.Base(src)))
		if err != nil {
			_ = os.RemoveAll(dir)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			last = err
			continue
		}
		return &Snapshot{DB: db, dir: dir}, nil
	}
	return nil, fmt.Errorf("%s: %w", filepath.Base(src), last)
}

// copyOnce copies the main file and the WAL. stable is false when the
// live files show a checkpoint or a write to the main file during the
// copy. The directory is returned either way; the caller removes it.
func copyOnce(src, prefix string) (string, bool, error) {
	before, err := os.Stat(src)
	if err != nil {
		return "", false, err
	}
	walBefore, walExists, err := walHeader(src + "-wal")
	if err != nil {
		return "", false, err
	}
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", false, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	base := filepath.Join(dir, filepath.Base(src))
	copiedSum, err := copyFile(src, base)
	if err != nil {
		return "", false, err
	}
	if betweenCopies != nil {
		betweenCopies(src)
	}
	stable := true
	if walExists {
		_, err := copyFile(src+"-wal", base+"-wal")
		switch {
		case errors.Is(err, os.ErrNotExist):
			// The last connection checkpointed and removed the WAL.
			stable = false
		case err != nil:
			return "", false, err
		default:
			copied, _, err := walHeader(base + "-wal")
			if err != nil {
				return "", false, err
			}
			stable = bytes.Equal(copied, walBefore)
		}
	}
	liveSum, err := hashFile(src)
	if err != nil {
		return "", false, err
	}
	if !bytes.Equal(liveSum, copiedSum) {
		stable = false
	}
	after, err := os.Stat(src)
	if err != nil {
		return "", false, err
	}
	walAfter, walStill, err := walHeader(src + "-wal")
	if err != nil {
		return "", false, err
	}
	if walStill != walExists || !bytes.Equal(walAfter, walBefore) {
		stable = false
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, after) {
		stable = false
	}
	ok = true
	return dir, stable, nil
}

// walHeader reads up to the first 32 bytes of a WAL. A missing file is
// not an error; exists reports it. A short file returns what it holds.
func walHeader(path string) (header []byte, exists bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	buf := make([]byte, walHeaderSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, true, err
	}
	return buf[:n], true, nil
}

// copyFile reads from and writes to, and returns the SHA-256 of the
// bytes it wrote. The source is opened read-only.
func copyFile(from, to string) ([]byte, error) {
	in, err := os.Open(from)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), in)
	closeErr := out.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return h.Sum(nil), nil
}

// hashFile is the SHA-256 of path, read-only.
func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// openChecked opens the copy read-only and runs quick_check on it.
func openChecked(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var verdict string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&verdict); err != nil {
		_ = db.Close()
		return nil, err
	}
	if verdict != "ok" {
		_ = db.Close()
		return nil, fmt.Errorf("quick_check: %s", verdict)
	}
	return db, nil
}

// readOnlyURI is a read-only file URI. mode=ro refuses a write. The
// WAL copied beside the database is applied. immutable is not set:
// that flag tells SQLite to ignore the WAL.
func readOnlyURI(path string) string {
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	u := &url.URL{Scheme: "file", Path: slash, RawQuery: "mode=ro"}
	return u.String()
}

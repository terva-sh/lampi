package sqlitesnap

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// openWriter opens a WAL database with automatic checkpoints off, so
// rows stay in the WAL until the test checkpoints.
func openWriter(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA wal_autocheckpoint=0`,
		`CREATE TABLE a (id INTEGER PRIMARY KEY, body TEXT)`,
		`CREATE TABLE b (id INTEGER PRIMARY KEY, body TEXT)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return db
}

// commit writes one row into a and b in one transaction. A consistent
// snapshot always has the same count in both.
func commit(db *sql.DB, id int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	body := string(make([]byte, 2000))
	for _, table := range []string{"a", "b"} {
		if _, err := tx.Exec(`INSERT INTO `+table+` (id, body) VALUES (?, ?)`, id, body); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func counts(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	var a, b int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM a), (SELECT count(*) FROM b)`).Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func fileBytes(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		out[suffix] = string(b)
	}
	return out
}

func TestTakeReadsWALAndLeavesLiveFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	w := openWriter(t, path)
	for i := 1; i <= 3; i++ {
		if err := commit(w, i); err != nil {
			t.Fatal(err)
		}
	}
	before := fileBytes(t, path)
	if before["-wal"] == "" || before["-shm"] == "" {
		t.Fatalf("fixture has no live wal or shm: %v", len(before))
	}
	snap, err := Take(context.Background(), path, "lampi-test-snap-")
	if err != nil {
		t.Fatal(err)
	}
	if a, b := counts(t, snap.DB); a != 3 || b != 3 {
		t.Fatalf("rows a=%d b=%d", a, b)
	}
	dir := snap.dir
	if err := snap.Close(); err != nil {
		t.Fatal(err)
	}
	if err := snap.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot dir left behind: %v", err)
	}
	after := fileBytes(t, path)
	for name, body := range before {
		if after[name] != body {
			t.Fatalf("live %q changed", name)
		}
	}
}

func TestTakeRollbackJournalAndMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE a (id INTEGER PRIMARY KEY, body TEXT); INSERT INTO a (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	snap, err := Take(context.Background(), path, "lampi-test-snap-")
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	var n int
	if err := snap.DB.QueryRow(`SELECT count(*) FROM a`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("live %s appeared: %v", suffix, err)
		}
	}

	if _, err := Take(context.Background(), filepath.Join(t.TempDir(), "gone.db"), "lampi-test-snap-"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing database: %v", err)
	}
	junk := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(junk, []byte("not a database, and long enough to have a header............................................................"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Take(context.Background(), junk, "lampi-test-snap-"); err == nil || errors.Is(err, ErrChanged) {
		t.Fatalf("junk file: %v", err)
	}
}

// A checkpoint that resets the WAL between the two copies pairs the
// old main file with new frames. Take sees it and copies again.
func TestTakeRetriesAfterCheckpointBetweenCopies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	w := openWriter(t, path)
	id := 0
	for ; id < 20; id++ {
		if err := commit(w, id); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	betweenCopies = func(string) {
		calls++
		if calls > 1 {
			return
		}
		if _, err := w.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			t.Error(err)
		}
		for end := id + 5; id < end; id++ {
			if err := commit(w, id); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { betweenCopies = nil })

	snap, err := Take(context.Background(), path, "lampi-test-snap-")
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	if calls != 2 {
		t.Fatalf("copies %d, want a retry", calls)
	}
	if a, b := counts(t, snap.DB); a != id || b != id {
		t.Fatalf("rows a=%d b=%d want %d", a, b, id)
	}
}

func TestTakeGivesUpOnADatabaseThatAlwaysMoves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	w := openWriter(t, path)
	if err := commit(w, 0); err != nil {
		t.Fatal(err)
	}
	id := 1
	calls := 0
	betweenCopies = func(string) {
		calls++
		if err := commit(w, id); err != nil {
			t.Error(err)
		}
		id++
		if _, err := w.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { betweenCopies = nil })
	if _, err := Take(context.Background(), path, "lampi-test-snap-"); !errors.Is(err, ErrChanged) {
		t.Fatalf("err %v", err)
	}
	if calls != Tries {
		t.Fatalf("copies %d want %d", calls, Tries)
	}
}

// A main file rewritten between the copies, with its size and mtime put
// back, is what a commit and checkpoint inside one coarse clock tick
// look like on disk. The copy must not be trusted.
func TestTakeRetriesWhenMainChangesWithinOneTick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	w := openWriter(t, path)
	if err := commit(w, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	betweenCopies = func(src string) {
		calls++
		if calls > 1 {
			return
		}
		st, err := os.Stat(src)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(src, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		// Past the 100-byte header, inside a page SQLite owns. The copy
		// is never opened, so the damage only has to differ.
		if _, err := f.WriteAt([]byte("moved"), st.Size()-64); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(src, st.ModTime(), st.ModTime()); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { betweenCopies = nil })

	dir, stable, err := copyOnce(path, "lampi-test-snap-")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(dir)
	if stable {
		t.Fatal("copy of a main file that changed within one mtime tick was stable")
	}
	if calls != 1 {
		t.Fatalf("copies %d want 1", calls)
	}
}

// A writer that commits and checkpoints while snapshots are taken never
// yields a copy with a half-applied transaction.
func TestTakeIsConsistentUnderAWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	w := openWriter(t, path)
	if err := commit(w, 0); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for id := 1; !stop.Load(); id++ {
			if err := commit(w, id); err != nil {
				t.Error(err)
				return
			}
			if id%7 == 0 {
				if _, err := w.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
					t.Error(err)
					return
				}
			}
		}
	}()
	defer func() {
		stop.Store(true)
		wg.Wait()
	}()

	ok := 0
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < 40 && time.Now().Before(deadline); i++ {
		snap, err := Take(context.Background(), path, "lampi-test-snap-")
		if errors.Is(err, ErrChanged) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		a, b := counts(t, snap.DB)
		snap.Close()
		if a != b || a == 0 {
			t.Fatalf("torn snapshot a=%d b=%d", a, b)
		}
		ok++
	}
	if ok == 0 {
		t.Fatal("no snapshot succeeded")
	}
}

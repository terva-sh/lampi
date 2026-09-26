package lakestate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watermark"
)

var mark = watermark.Mark{
	MachineID: "m1", Harness: "terva", Root: "/r", RelPath: "a.jsonl",
	Size: 10, ModTime: time.Unix(1, 0).UTC(), SHA256: strings.Repeat("ab", 32), Offset: 10,
}

// legacyState writes a single-lake state directory that also holds a
// lake's own files, as the default layout does when serve and the agent
// share the directory. The stores stay open, so their last writes sit in
// the write-ahead log when the migration runs.
func legacyState(t *testing.T) (dir string, closeAll func()) {
	t.Helper()
	dir = t.TempDir()
	ctx := context.Background()
	wm, err := watermark.Open(watermark.File(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := wm.Commit(ctx, mark, protocol.ManifestAck{SessionUID: "s1"}); err != nil {
		t.Fatal(err)
	}
	q, err := outbox.Open(outbox.File(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, outbox.Item{Digest: strings.Repeat("cd", 32)}); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"last_sync.json":        `{"uploaded":1}`,
		"last_attempt.json":     `{"ok":true}`,
		"quarantine.jsonl":      "q\n",
		"quarantine_allow.json": "{}",
		"agent.pid":             "123\n",
		"catalog.db":            "lake catalog",
		"identity.json":         "lake identity",
		"lake.lock":             "",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "cas", "sha256"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cas", "sha256", "obj"), []byte("blob"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, func() { wm.Close(); q.Close() }
}

func requireMigrated(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	lake := Dir(dir, "default")
	wm, err := watermark.Open(watermark.File(lake))
	if err != nil {
		t.Fatal(err)
	}
	defer wm.Close()
	if _, ok, err := wm.Get(ctx, mark); err != nil || !ok {
		t.Fatalf("watermark after migration ok=%v err=%v", ok, err)
	}
	q, err := outbox.Open(outbox.File(lake))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if n, err := q.Depth(ctx); err != nil || n != 1 {
		t.Fatalf("outbox depth %d %v", n, err)
	}
	for _, name := range []string{"last_sync.json", "last_attempt.json"} {
		if _, err := os.Stat(filepath.Join(lake, name)); err != nil {
			t.Fatalf("%s not moved: %v", name, err)
		}
	}
	for _, name := range []string{"watermarks.db", "watermarks.db-wal", "watermarks.db-shm", "outbox.db", "outbox.db-wal", "outbox.db-shm", "last_sync.json", "last_attempt.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("legacy %s left behind: %v", name, err)
		}
	}
	// Everything else in the directory is the lake's or shared, and
	// stays exactly as it was.
	for name, body := range map[string]string{
		"quarantine.jsonl":      "q\n",
		"quarantine_allow.json": "{}",
		"agent.pid":             "123\n",
		"catalog.db":            "lake catalog",
		"identity.json":         "lake identity",
		"cas/sha256/obj":        "blob",
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != body {
			t.Fatalf("%s changed: %q %v", name, got, err)
		}
	}
	st, err := os.Stat(lake)
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("lake state dir %v %v", st, err)
	}
}

func TestMigrateMovesOnlyTheClientFiles(t *testing.T) {
	dir, closeAll := legacyState(t)
	moved, err := Migrate(dir, "default")
	closeAll()
	if err != nil || !moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	requireMigrated(t, dir)
	if has, _ := Legacy(dir); has {
		t.Fatal("legacy still reported")
	}
	// A second call does nothing.
	if moved, err := Migrate(dir, "default"); err != nil || moved {
		t.Fatalf("second call moved=%v err=%v", moved, err)
	}
	requireMigrated(t, dir)
}

func TestMigrateDiscardsAPartialCopy(t *testing.T) {
	dir, closeAll := legacyState(t)
	defer closeAll()
	// A crash before the rename left half a copy.
	tmp := filepath.Join(Root(dir), ".default.migrating")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "watermarks.db"), []byte("torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if moved, err := Migrate(dir, "default"); err != nil || !moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	closeAll()
	requireMigrated(t, dir)
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("partial copy left behind")
	}
}

func TestMigrateAfterACrashPastTheRenameRemovesTheLegacyFiles(t *testing.T) {
	dir, closeAll := legacyState(t)
	if _, err := Migrate(dir, "default"); err != nil {
		t.Fatal(err)
	}
	closeAll()
	// Put a legacy file back, as if the crash came before the removal.
	if err := os.WriteFile(filepath.Join(dir, "last_sync.json"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if moved, err := Migrate(dir, "default"); err != nil || moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	requireMigrated(t, dir)
	got, _ := os.ReadFile(filepath.Join(Dir(dir, "default"), "last_sync.json"))
	if string(got) != `{"uploaded":1}` {
		t.Fatalf("migrated copy replaced by the stale one: %q", got)
	}
}

func TestMigrateWithNothingToMove(t *testing.T) {
	dir := t.TempDir()
	if moved, err := Migrate(dir, "default"); err != nil || moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	if _, err := os.Stat(Root(dir)); !os.IsNotExist(err) {
		t.Fatal("empty migration made a directory")
	}
}

func TestSyncFailuresThatMeanNotSupportedAreNotErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("every directory sync error is ignored on windows")
	}
	for _, err := range []error{syscall.EINVAL, syscall.ENOTSUP, &os.PathError{Op: "sync", Err: syscall.EINVAL}} {
		if !syncUnsupported(err) {
			t.Errorf("syncUnsupported(%v) = false, want true", err)
		}
	}
	if syncUnsupported(&os.PathError{Op: "sync", Err: syscall.EIO}) {
		t.Error("an I/O error from sync was treated as unsupported")
	}
}

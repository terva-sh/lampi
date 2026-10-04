package cursorcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter/sqlitesnap"
	"terva.sh/lampi/internal/protocol"
)

// An ACP session is acp-sessions/<session>/store.db with its cwd in the
// sibling meta.json. Its session id keeps the acp-sessions/ prefix, so
// a chat with the same uuid is a different session.
func TestACPSessionManifest(t *testing.T) {
	root := t.TempDir()
	acp := filepath.Join(root, "acp-sessions", "sid-1", "store.db")
	chat := filepath.Join(root, "chats", "ab12", "sid-1", "store.db")
	for _, db := range []string{acp, chat} {
		if err := seedClosed(db); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(filepath.Dir(db), "meta.json"), `{"schemaVersion":1,"cwd":"/work/app"}`)
	}
	mustWrite(t, filepath.Join(root, "acp-sessions", "meta-only", "meta.json"), `{"schemaVersion":1,"cwd":"/work/app"}`)

	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cleanup()
	if len(b.Manifests) != 2 {
		t.Fatalf("manifests %+v", b.Manifests)
	}
	ids := map[string]protocol.Manifest{}
	for _, m := range b.Manifests {
		ids[m.NativeSessionID] = m
	}
	m, ok := ids["acp-sessions/sid-1"]
	if !ok || ids["chats/ab12/sid-1"].NativeSessionID == "" {
		t.Fatalf("ids %v", ids)
	}
	a := m.Artifacts[0]
	if m.Harness != protocol.HarnessCursorCLI || m.Project.CWD != "/work/app" || a.RelPath != "acp-sessions/sid-1/store.json" || a.SHA256 == "" || b.Paths[a.RelPath] == "" {
		t.Fatalf("acp manifest %+v", m)
	}
}

// With a settle time, a permitted session written within it is held:
// not snapshotted, listed on Held with no digest, and HeldUntil is when
// it may be read. A refused session is still a manifest. Once the
// settle time has passed the session is exported.
func TestManifestsHoldASessionStillBeingWritten(t *testing.T) {
	root := t.TempDir()
	busy := filepath.Join(root, "acp-sessions", "busy", "store.db")
	refused := filepath.Join(root, "acp-sessions", "refused", "store.db")
	for _, db := range []string{busy, refused} {
		if err := seedClosed(db); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(filepath.Dir(busy), "meta.json"), `{"cwd":"/work/app"}`)
	mustWrite(t, filepath.Join(filepath.Dir(refused), "meta.json"), `{"cwd":"/work/other"}`)
	written := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for _, p := range []string{busy, refused} {
		if err := os.Chtimes(p, written, written); err != nil {
			t.Fatal(err)
		}
	}
	// The WAL is newer than store.db, and it is the last write.
	mustWrite(t, busy+"-wal", "")
	walAt := written.Add(2 * time.Minute)
	if err := os.Chtimes(busy+"-wal", walAt, walAt); err != nil {
		t.Fatal(err)
	}

	copies := 0
	takeSnapshot = func(ctx context.Context, src, prefix string) (*sqlitesnap.Snapshot, error) {
		copies++
		return sqlitesnap.Take(ctx, src, prefix)
	}
	t.Cleanup(func() { takeSnapshot = sqlitesnap.Take; now = time.Now })
	permit := func(m protocol.Manifest) bool { return m.Project.CWD == "/work/app" }

	now = func() time.Time { return walAt.Add(time.Minute) }
	b, err := ManifestsMemo(root, "machine-1", nil, permit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cleanup()
	if copies != 0 || len(b.Held) != 1 || len(b.Manifests) != 1 || len(b.Paths) != 0 {
		t.Fatalf("copies %d held %+v manifests %+v", copies, b.Held, b.Manifests)
	}
	if h := b.Held[0]; h.NativeSessionID != "acp-sessions/busy" || h.Artifacts[0].SHA256 != "" {
		t.Fatalf("held %+v", h)
	}
	if b.Manifests[0].NativeSessionID != "acp-sessions/refused" {
		t.Fatalf("refused session not kept: %+v", b.Manifests)
	}
	if want := walAt.Add(5 * time.Minute); !b.HeldUntil.Equal(want) {
		t.Fatalf("held until %v, want %v", b.HeldUntil, want)
	}

	now = func() time.Time { return walAt.Add(5 * time.Minute) }
	b2, err := ManifestsMemo(root, "machine-1", nil, permit, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Cleanup()
	if copies != 1 || len(b2.Held) != 0 || !b2.HeldUntil.IsZero() || len(b2.Paths) != 1 {
		t.Fatalf("settled session: copies %d held %+v until %v", copies, b2.Held, b2.HeldUntil)
	}

	b3, err := ManifestsMemo(root, "machine-1", nil, permit, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer b3.Cleanup()
	if len(b3.Held) != 0 {
		t.Fatalf("zero settle held %+v", b3.Held)
	}
}

// A store over the size cap is skipped with a line that names it and
// its size, and is not snapshotted. A refused one is still a manifest.
func TestManifestsSkipAStoreOverTheCap(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "acp-sessions", "big", "store.db")
	small := filepath.Join(root, "acp-sessions", "small", "store.db")
	for _, db := range []string{big, small} {
		if err := seedClosed(db); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(filepath.Dir(db), "meta.json"), `{"cwd":"/work/app"}`)
	}
	info, err := os.Stat(small)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, big+"-wal", "x")
	maxStoreBytes = info.Size()
	copies := 0
	takeSnapshot = func(ctx context.Context, src, prefix string) (*sqlitesnap.Snapshot, error) {
		copies++
		return sqlitesnap.Take(ctx, src, prefix)
	}
	t.Cleanup(func() { takeSnapshot = sqlitesnap.Take; maxStoreBytes = 256 << 20 })

	b, err := ManifestsMemo(root, "machine-1", nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cleanup()
	if copies != 1 || len(b.Manifests) != 1 || b.Manifests[0].NativeSessionID != "acp-sessions/small" {
		t.Fatalf("copies %d manifests %+v", copies, b.Manifests)
	}
	if len(b.Skipped) != 1 || !strings.Contains(b.Skipped[0].Error(), "acp-sessions/big/store.db") || !strings.Contains(b.Skipped[0].Error(), "MiB") {
		t.Fatalf("skipped %v", b.Skipped)
	}

	refused, err := ManifestsMemo(root, "machine-1", nil, func(protocol.Manifest) bool { return false }, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer refused.Cleanup()
	if len(refused.Manifests) != 2 || len(refused.Skipped) != 0 {
		t.Fatalf("refused: manifests %d skipped %v", len(refused.Manifests), refused.Skipped)
	}
}

package outbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCrashLeavesWorkRetryable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	ctx := context.Background()
	digest := repeat('a', 64)
	manifest := []byte(`{"capture_protocol":1,"native_session_id":"s"}`)

	q, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Item{
		Identity: "session:s",
		Digest:   digest,
		Manifest: manifest,
		Version:  1,
	}); err != nil {
		t.Fatal(err)
	}
	// The upload started: the row was read. The process dies before Ack.
	pending, err := q.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Digest != digest || string(pending[0].Manifest) != string(manifest) {
		t.Fatalf("before crash: %+v", pending)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}

	q, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	pending, err = q.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Version != 1 {
		t.Fatalf("after reopen: %+v", pending)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestAckIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	ctx := context.Background()
	q, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()

	digest := repeat('b', 64)
	if err := q.Enqueue(ctx, Item{Digest: digest}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Item{Digest: digest}); err != nil {
		t.Fatal(err)
	}
	pending, err := q.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("duplicate enqueue: %+v", pending)
	}
	if err := q.Ack(ctx, pending[0]); err != nil {
		t.Fatal(err)
	}
	if err := q.Ack(ctx, pending[0]); err != nil {
		t.Fatal(err)
	}
	// A peer that only knows the digest, not the row id, is also done.
	if err := q.Ack(ctx, Item{Digest: digest}); err != nil {
		t.Fatal(err)
	}
	pending, err = q.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("still pending: %+v", pending)
	}
}

func TestManifestVersionReplacesPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	ctx := context.Background()
	q, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()

	id := "session:s"
	if err := q.Enqueue(ctx, Item{Identity: id, Manifest: []byte("v1"), Version: 1}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Item{Identity: id, Manifest: []byte("v1-again"), Version: 1}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Item{Identity: id, Manifest: []byte("v2"), Version: 2}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Item{Identity: id, Manifest: []byte("v0"), Version: 0}); err != nil {
		t.Fatal(err)
	}
	pending, err := q.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Version != 2 || string(pending[0].Manifest) != "v2" {
		t.Fatalf("pending: %+v %q", pending, pending[0].Manifest)
	}
}

func TestSharedByTwoOpeners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "outbox.db")
	if got := File(filepath.Dir(path)); got != path {
		t.Fatalf("File = %s", got)
	}
	ctx := context.Background()
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	digest := repeat('c', 64)
	if err := a.Enqueue(ctx, Item{Digest: digest, Manifest: []byte("m"), Version: 3, Identity: "blob-and-manifest"}); err != nil {
		t.Fatal(err)
	}
	pending, err := b.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Digest != digest || pending[0].Version != 3 {
		t.Fatalf("peer pending: %+v", pending)
	}
	if err := b.Ack(ctx, pending[0]); err != nil {
		t.Fatal(err)
	}
	pending, err = a.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("ack did not cross openers: %+v", pending)
	}
}

// TestEnqueueLostUpdate is the interleaving from review: one connection
// has already decided to write version 2, and before that write lands
// another connection commits version 3. The version compare has to be
// in the write itself. A stale read of version 1 would store 2 and drop 3.
func TestEnqueueLostUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	ctx := context.Background()
	low, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer low.Close()
	high, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer high.Close()

	id := "session:s"
	if err := low.Enqueue(ctx, Item{Identity: id, Manifest: []byte("v1"), Version: 1}); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { testEnqueueGate = nil })
	var gateErr error
	testEnqueueGate = func() {
		// Run once, on the version-2 enqueue. Commit 3 before that write.
		testEnqueueGate = nil
		gateErr = high.Enqueue(ctx, Item{Identity: id, Manifest: []byte("v3"), Version: 3})
	}
	if err := low.Enqueue(ctx, Item{Identity: id, Manifest: []byte("v2"), Version: 2}); err != nil {
		t.Fatal(err)
	}
	if gateErr != nil {
		t.Fatal(gateErr)
	}
	pending, err := high.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Version != 3 || string(pending[0].Manifest) != "v3" {
		t.Fatalf("lost update: %+v", pending)
	}
}

func TestWALSidecarsArePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "outbox.db")
	q, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if err := q.Enqueue(context.Background(), Item{Digest: repeat('d', 64)}); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("state dir mode %o", dirInfo.Mode().Perm())
	}
	// Loosen a sidecar and write again. Enqueue has to put it back.
	wal := path + "-wal"
	if _, err := os.Stat(wal); err != nil {
		t.Fatalf("wal sidecar: %v", err)
	}
	if err := os.Chmod(wal, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(context.Background(), Item{Digest: repeat('e', 64)}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %o", p, info.Mode().Perm())
		}
	}
}

func TestRejectsBadDigest(t *testing.T) {
	q, err := Open(filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if err := q.Enqueue(context.Background(), Item{Digest: "nope"}); err == nil {
		t.Fatal("expected invalid digest")
	}
}

func repeat(b byte, n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = b
	}
	return string(buf)
}

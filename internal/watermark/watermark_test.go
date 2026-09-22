package watermark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func TestCommitOnlyAfterAck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watermarks.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	key := Mark{
		MachineID: "machine-1",
		Harness:   protocol.HarnessTerva,
		Root:      "/home/drew/.local/state/terva",
		RelPath:   "sessions/abcd/s.jsonl",
	}
	body := []byte("0123456789")
	sum := sha256.Sum256(body)
	mark := key
	mark.Size = int64(len(body))
	mark.Offset = int64(len(body))
	mark.SHA256 = hex.EncodeToString(sum[:])
	mark.ModTime = time.Unix(1_700_000_000, 0).UTC()

	if err := s.Commit(ctx, mark, protocol.ManifestAck{}); !errors.Is(err, ErrNotAcked) {
		t.Fatalf("empty ack: %v", err)
	}
	if _, ok, err := s.Get(ctx, key); err != nil || ok {
		t.Fatalf("stored without ack: ok=%v err=%v", ok, err)
	}
	// A head hash without a session uid is still not an ACK.
	if err := s.Commit(ctx, mark, protocol.ManifestAck{HeadSHA256: mark.SHA256}); !errors.Is(err, ErrNotAcked) {
		t.Fatalf("head only: %v", err)
	}

	ack := protocol.ManifestAck{SessionUID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", HeadSHA256: mark.SHA256, ArtifactIDs: []string{"art"}}
	if err := s.Commit(ctx, mark, ack); err != nil {
		t.Fatal(err)
	}
	// A later failed ACK must not move the cursor.
	moved := mark
	moved.Offset = 99
	moved.Size = 99
	if err := s.Commit(ctx, moved, protocol.ManifestAck{}); !errors.Is(err, ErrNotAcked) {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, key)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Offset != mark.Offset || got.SHA256 != mark.SHA256 || got.Size != mark.Size {
		t.Fatalf("mark changed: %+v", got)
	}
	if !got.ModTime.Equal(mark.ModTime) {
		t.Fatalf("mtime %s", got.ModTime)
	}

	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err = s.Get(ctx, key)
	if err != nil || !ok || got.Offset != 10 {
		t.Fatalf("reopen: %+v ok=%v err=%v", got, ok, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestPlanDrivesTailOnlyUpload(t *testing.T) {
	prefix := []byte("0123456789")
	tail := []byte("ABCDE")
	file := append(append([]byte{}, prefix...), tail...)
	sum := sha256.Sum256(prefix)
	mark := Mark{
		MachineID: "m",
		Harness:   "terva",
		Root:      "/t",
		RelPath:   "sessions/abcd/s.jsonl",
		Size:      int64(len(prefix)),
		Offset:    int64(len(prefix)),
		SHA256:    hex.EncodeToString(sum[:]),
		ModTime:   time.Unix(10, 0).UTC(),
	}

	// Same stat: nothing to send.
	same := Plan(mark, Stat{Size: mark.Size, ModTime: mark.ModTime})
	if same.Kind != KindUnchanged || same.Length != 0 {
		t.Fatalf("unchanged: %+v", same)
	}

	// Grown, hash not computed yet. The caller hashes the stored prefix
	// only, then uploads the tail.
	probe := Plan(mark, Stat{Size: int64(len(file))})
	if probe.Kind != KindProbe || probe.Offset != mark.Offset || probe.Length != int64(len(tail)) {
		t.Fatalf("probe: %+v", probe)
	}
	var reads int
	prefixSHA, err := HashPrefix(countReader{r: bytes.NewReader(file), n: &reads}, probe.Offset)
	if err != nil {
		t.Fatal(err)
	}
	if reads != int(mark.Offset) {
		t.Fatalf("hashed %d bytes, want the prefix %d", reads, mark.Offset)
	}
	if prefixSHA != mark.SHA256 {
		t.Fatalf("prefix %s", prefixSHA)
	}
	dec := Plan(mark, Stat{Size: int64(len(file)), PrefixSHA: prefixSHA})
	if dec.Kind != KindTail || dec.Offset != 10 || dec.Length != 5 {
		t.Fatalf("tail: %+v", dec)
	}
	if string(file[dec.Offset:dec.Offset+dec.Length]) != string(tail) {
		t.Fatalf("uploaded %q", file[dec.Offset:dec.Offset+dec.Length])
	}

	// Full hash match, even if mtime moved, sends nothing.
	full := sha256.Sum256(prefix)
	still := Plan(mark, Stat{Size: mark.Size, ModTime: mark.ModTime.Add(time.Hour), FullSHA: hex.EncodeToString(full[:])})
	if still.Kind != KindUnchanged || still.Length != 0 {
		t.Fatalf("full hash: %+v", still)
	}
}

func TestPlanTruncateAndRewrite(t *testing.T) {
	mark := Mark{
		SHA256: repeatHex('a'),
		Size:   10,
		Offset: 10,
	}
	trunc := Plan(mark, Stat{Size: 4})
	if trunc.Kind != KindReplace || trunc.Offset != 0 || trunc.Length != 4 {
		t.Fatalf("truncate: %+v", trunc)
	}
	rewrite := Plan(mark, Stat{Size: 15, PrefixSHA: repeatHex('b')})
	if rewrite.Kind != KindReplace || rewrite.Offset != 0 || rewrite.Length != 15 {
		t.Fatalf("rewrite: %+v", rewrite)
	}
	fresh := Plan(Mark{}, Stat{Size: 3})
	if fresh.Kind != KindNew || fresh.Offset != 0 || fresh.Length != 3 {
		t.Fatalf("new: %+v", fresh)
	}
}

func TestFilePath(t *testing.T) {
	if got := File("/var/state"); got != filepath.Join("/var/state", "watermarks.db") {
		t.Fatalf("File = %s", got)
	}
}

type countReader struct {
	r ioReader
	n *int
}

type ioReader interface {
	Read(p []byte) (int, error)
}

func (c countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	*c.n += n
	return n, err
}

func repeatHex(b byte) string {
	return string(bytes.Repeat([]byte{b}, 64))
}

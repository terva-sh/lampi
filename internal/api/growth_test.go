package api

import (
	"bytes"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

// A transcript that grows through tail uploads keeps every version
// readable, and the CAS holds its bytes about once rather than once per
// version (TKT-01M3K38AA). Before prefix records, 100 appends stored
// about 51 times the final file.
func TestGrowingTranscriptIsStoredOnce(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	const appends = 100
	var file []byte
	var versions [][]byte
	var prevSize int64
	for i := 0; i < appends; i++ {
		var tail bytes.Buffer
		for l := 0; l < 20; l++ {
			fmt.Fprintf(&tail, "{\"i\":%d,\"l\":%d,\"text\":%q}\n", i, l, bytes.Repeat([]byte("x"), 200))
		}
		file = append(file, tail.Bytes()...)
		sum := sha256Hex(file)
		if i == 0 {
			putRaw(t, h, file)
			postManifest(t, h, manifest("m", "sid", file, sum, 0, sum))
		} else {
			tailSHA := putRaw(t, h, tail.Bytes())
			postManifest(t, h, manifest("m", "sid", file, sum, prevSize, tailSHA))
		}
		prevSize = int64(len(file))
		versions = append(versions, append([]byte(nil), file...))
	}

	for i, v := range versions {
		got, err := s.CAS.Read(sha256Hex(v))
		if err != nil {
			t.Fatalf("version %d: %v", i, err)
		}
		if !bytes.Equal(got, v) {
			t.Fatalf("version %d reads %d bytes, want %d", i, len(got), len(v))
		}
	}

	// The objects left are the newest version and the tails, which
	// together are twice the file. The records are a few hundred bytes
	// each.
	var objects int64
	err := filepath.WalkDir(filepath.Join(s.dataDir, "cas", "sha256"), func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		objects += info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if limit := 2*int64(len(file)) + 1024; objects > limit {
		t.Fatalf("objects hold %d bytes for a %d-byte file; want at most %d", objects, len(file), limit)
	}
}

// chunkedManifest sends file as chunks of n bytes, the way the client
// sends a file past the object cap. It travels as the errors sidecar of
// a one-line transcript, so the test measures the store and not the
// normalizer reading a 32 MiB transcript at every version.
func chunkedManifest(t *testing.T, h http.Handler, native string, file []byte, n int) protocol.Manifest {
	t.Helper()
	head := []byte(`{"type":"meta","meta":{"id":"` + native + `"}}` + "\n")
	m := manifest("m", native, head, putRaw(t, h, head), 0, "")
	a := protocol.Artifact{
		Kind:    protocol.KindErrorsJSONL,
		RelPath: "sessions/x/" + native + ".errors.jsonl",
		Size:    int64(len(file)),
		SHA256:  sha256Hex(file),
	}
	for start := 0; start < len(file); start += n {
		end := min(start+n, len(file))
		piece := file[start:end]
		// A put of a chunk the lake holds as a record keeps the record.
		putRaw(t, h, piece)
		a.ChunkSHA256s = append(a.ChunkSHA256s, sha256Hex(piece))
		a.ChunkLengths = append(a.ChunkLengths, int64(len(piece)))
	}
	m.Artifacts = append(m.Artifacts, a)
	return m
}

// A file past the object cap is sent whole, as chunks, at every
// sync. The lake keeps its bytes about once: the previous version's
// last chunk becomes a record of the chunk that extends it
// (TKT-01M3KC2DA). Before, each sync kept another copy of that chunk.
func TestGrowingChunkedFileIsStoredOnce(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	const chunk = 8 << 20
	// Random bytes, so no two chunks share a digest.
	file := make([]byte, protocol.MaxBlobBytes+1)
	rand.NewChaCha8([32]byte{1}).Read(file)
	var versions [][]byte
	for i := 0; i < 4; i++ {
		file = append(file, bytes.Repeat([]byte(fmt.Sprintf("{\"i\":%d}\n", i)), 100_000)...)
		postManifest(t, h, chunkedManifest(t, h, "sid", file, chunk))
		versions = append(versions, append([]byte(nil), file...))
	}
	for i, v := range versions {
		got, err := s.CAS.Read(sha256Hex(v))
		if err != nil || !bytes.Equal(got, v) {
			t.Fatalf("version %d reads %d bytes, %v", i, len(got), err)
		}
	}
	// The transcript's object is under 100 bytes.
	if objects := objectBytes(t, s); objects > int64(len(file))+1024 {
		t.Fatalf("objects hold %d bytes for a %d-byte file", objects, len(file))
	}

	// A fork of an older version names a chunk that is now a record. It
	// is still accepted and reads back.
	fork := versions[1]
	postManifest(t, h, chunkedManifest(t, h, "fork", fork, chunk))
	if got, err := s.CAS.Read(sha256Hex(fork)); err != nil || !bytes.Equal(got, fork) {
		t.Fatalf("fork reads %d bytes, %v", len(got), err)
	}
}

package api

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
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

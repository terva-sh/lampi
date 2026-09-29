package cas

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
)

func TestFoldFreesTheObjectAndRefusesALoop(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	short, long := []byte("abc\n"), []byte("abc\ndef\n")
	ds, dl := mustPut(t, s, short), mustPut(t, s, long)
	stored, _, err := s.StoredSize(ds)
	if err != nil {
		t.Fatal(err)
	}

	freed, err := s.Fold(ds, dl, int64(len(short)))
	if err != nil || freed != stored {
		t.Fatalf("fold = %d %v", freed, err)
	}
	if got, err := s.Read(ds); err != nil || !bytes.Equal(got, short) {
		t.Fatalf("read = %q %v", got, err)
	}
	if term, err := s.Terminal(ds); err != nil || term != dl {
		t.Fatalf("terminal = %s %v", term, err)
	}
	// Folding again changes nothing and frees nothing.
	if freed, err := s.Fold(ds, dl, int64(len(short))); err != nil || freed != 0 {
		t.Fatalf("second fold = %d %v", freed, err)
	}

	// The long file cannot be folded into the short one, which reads
	// from it.
	if _, err := s.Fold(dl, ds, 3); !errors.Is(err, ErrWouldLoop) {
		t.Fatalf("looping fold: %v", err)
	}
	if ok, _ := s.Has(dl); !ok {
		t.Fatal("a refused fold removed the object")
	}
}

// bindChunks stores file as chunks of n bytes and binds its digest to
// them, as the lake does for a file past the object cap.
func bindChunks(t *testing.T, s *Store, file []byte, n int) string {
	t.Helper()
	var parts []string
	var lengths []int64
	for start := 0; start < len(file); start += n {
		end := min(start+n, len(file))
		parts = append(parts, mustPut(t, s, file[start:end]))
		lengths = append(lengths, int64(end-start))
	}
	sum := sha256.Sum256(file)
	d := hex.EncodeToString(sum[:])
	if _, err := s.BindLogical(d, parts, lengths); err != nil {
		t.Fatal(err)
	}
	return d
}

// A chunked file that grows keeps each version readable and stores its
// last chunk once: the previous version's last chunk becomes a record
// of the chunk that extends it (TKT-01M3KC2DA).
func TestFoldGrowthRecordsTheLastChunkOfAChunkedFile(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const chunk = 64
	// The file starts past one chunk, as a file does once it is past
	// the object cap; below it the lake grows the file from tails.
	file := bytes.Repeat([]byte("#"), chunk+10)
	var versions [][]byte
	var digests []string
	for i := 0; i < 12; i++ {
		file = append(file, []byte(fmt.Sprintf("line %02d of the file\n", i))...)
		d := bindChunks(t, s, file, chunk)
		if len(digests) > 0 {
			if _, err := s.FoldGrowth(digests[len(digests)-1], d); err != nil {
				t.Fatal(err)
			}
		}
		versions = append(versions, append([]byte(nil), file...))
		digests = append(digests, d)
	}
	for i, d := range digests {
		got, err := s.Read(d)
		if err != nil || !bytes.Equal(got, versions[i]) {
			t.Fatalf("version %d reads %q, %v", i, got, err)
		}
	}
	// Only the newest version's chunks remain objects.
	if held := objectBytes(t, s); held != int64(len(file)) {
		t.Fatalf("objects hold %d bytes for a %d-byte file", held, len(file))
	}
	var bad []Problem
	if _, err := s.Verify(func(p Problem) { bad = append(bad, p) }); err != nil || len(bad) != 0 {
		t.Fatalf("verify: %v %v", bad, err)
	}
}

// A version that is not an append of the one before folds nothing.
func TestFoldGrowthLeavesARewriteAlone(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := bindChunks(t, s, bytes.Repeat([]byte("a"), 100), 64)
	next := bindChunks(t, s, bytes.Repeat([]byte("b"), 120), 64)
	if freed, err := s.FoldGrowth(old, next); err != nil || freed != 0 {
		t.Fatalf("fold growth = %d %v", freed, err)
	}
	if got, err := s.Read(old); err != nil || !bytes.Equal(got, bytes.Repeat([]byte("a"), 100)) {
		t.Fatalf("old version reads %q %v", got, err)
	}
}

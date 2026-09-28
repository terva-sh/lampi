package cas

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustPut(t *testing.T, s *Store, b []byte) string {
	t.Helper()
	d := digestOf(b)
	if _, err := s.Put(d, bytes.NewReader(b), 0); err != nil {
		t.Fatal(err)
	}
	return d
}

// grow appends tail to file in s the way ingest does and returns the
// grown bytes and digest.
func grow(t *testing.T, s *Store, file []byte, tail string) ([]byte, string) {
	t.Helper()
	tailDigest := mustPut(t, s, []byte(tail))
	next := append(append([]byte(nil), file...), tail...)
	d := digestOf(next)
	if _, err := s.Grow(d, digestOf(file), int64(len(file)), tailDigest, 1<<20); err != nil {
		t.Fatal(err)
	}
	return next, d
}

func TestGrowKeepsEveryVersionReadableAndStoresOneCopy(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v1 := []byte("line one\n")
	mustPut(t, s, v1)
	v2, _ := grow(t, s, v1, "line two\n")
	v3, d3 := grow(t, s, v2, "line three\n")

	for _, v := range [][]byte{v1, v2, v3} {
		d := digestOf(v)
		got, err := s.Read(d)
		if err != nil {
			t.Fatalf("read %d bytes: %v", len(v), err)
		}
		if !bytes.Equal(got, v) {
			t.Fatalf("read %q, want %q", got, v)
		}
		n, err := s.Size(d)
		if err != nil || n != int64(len(v)) {
			t.Fatalf("size %d %v, want %d", n, err, len(v))
		}
	}
	for _, v := range [][]byte{v1, v2} {
		if ok, _ := s.Has(digestOf(v)); ok {
			t.Fatalf("superseded %q still has its own object", v)
		}
	}
	if ok, _ := s.Has(d3); !ok {
		t.Fatal("newest version is not an object")
	}
	// v1 was pointed at v2, which was then pointed at v3: a chain.
	base, n, ok, err := s.PrefixOf(digestOf(v1))
	if err != nil || !ok || base != digestOf(v2) || n != int64(len(v1)) {
		t.Fatalf("v1 record = %s %d %v %v", base, n, ok, err)
	}
	var problems []Problem
	if _, err := s.Verify(func(p Problem) { problems = append(problems, p) }); err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Fatalf("verify: %v", problems)
	}
}

func TestGrowRefusesWhatIsNotAGrowth(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v1 := []byte("head\n")
	d1 := mustPut(t, s, v1)
	tail := mustPut(t, s, []byte("tail\n"))

	// The digest claimed is not head+tail.
	wrong := digestOf([]byte("head\nother\n"))
	if _, err := s.Grow(wrong, d1, int64(len(v1)), tail, 0); !errors.Is(err, ErrNotGrown) {
		t.Fatalf("wrong digest: %v", err)
	}
	// The catalog size is not the stored prefix's size.
	right := digestOf([]byte("head\ntail\n"))
	if _, err := s.Grow(right, d1, int64(len(v1))-1, tail, 0); !errors.Is(err, ErrNotGrown) {
		t.Fatalf("short prefix size: %v", err)
	}
	// Over the cap.
	if _, err := s.Grow(right, d1, int64(len(v1)), tail, 6); !errors.Is(err, ErrRejected) {
		t.Fatalf("over the cap: %v", err)
	}
	if ok, _ := s.Has(d1); !ok {
		t.Fatal("a refused grow removed the prefix")
	}
	if ok, _ := s.Has(right); ok {
		t.Fatal("a refused grow installed the digest")
	}
	if _, _, ok, _ := s.PrefixOf(d1); ok {
		t.Fatal("a refused grow wrote a prefix record")
	}
}

func TestPrefixRecordThatCannotBeReadIsReported(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	v1 := []byte("aaaa\n")
	mustPut(t, s, v1)
	v2, d2 := grow(t, s, v1, "bbbb\n")
	d1 := digestOf(v1)

	object, logical, chunks, err := s.Stored(d1)
	if err != nil || object || !logical || len(chunks) != 1 || chunks[0] != d2 {
		t.Fatalf("stored = %v %v %v %v", object, logical, chunks, err)
	}

	// The base shrinks: reading the prefix must fail, not come back short.
	p, _ := s.Path(d2)
	if err := os.WriteFile(p, v2[:3], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(d1); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read over a short base: %v", err)
	}

	// The base goes: fsck names the record and repair keeps it.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	var problems []Problem
	if _, err := s.Verify(func(p Problem) { problems = append(problems, p) }); err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].Digest != d1 || !problems[0].Logical || !strings.Contains(problems[0].Reason, "not in the store") {
		t.Fatalf("verify: %v", problems)
	}
	if fixed, err := s.Repair(problems[0]); err != nil || fixed {
		t.Fatalf("repair = %v %v", fixed, err)
	}
	if ok, err := s.Present(d1); err != nil || ok {
		t.Fatalf("present with no base = %v %v", ok, err)
	}
	// A put of the digest restores it, Open prefers the object, and
	// fsck is clean again.
	mustPut(t, s, v1)
	if got, err := s.Read(d1); err != nil || !bytes.Equal(got, v1) {
		t.Fatalf("read after put: %q %v", got, err)
	}
	problems = nil
	if _, err := s.Verify(func(p Problem) { problems = append(problems, p) }); err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("verify after the put: %v", problems)
	}
}

func TestPresentNeedsABaseThatHoldsTheBytes(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v1 := []byte("first line\n")
	mustPut(t, s, v1)
	v2, d2 := grow(t, s, v1, "second line\n")
	d1 := digestOf(v1)
	if ok, err := s.Present(d1); err != nil || !ok {
		t.Fatalf("present = %v %v", ok, err)
	}
	// The base is cut shorter than the record: the version is missing,
	// and a put of it is kept rather than discarded.
	p, _ := s.Path(d2)
	if err := os.WriteFile(p, v2[:4], 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Present(d1); err != nil || ok {
		t.Fatalf("present over a short base = %v %v", ok, err)
	}
	if exists, err := s.Put(d1, bytes.NewReader(v1), 0); err != nil || exists {
		t.Fatalf("put over a short base = %v %v", exists, err)
	}
	if ok, _ := s.Has(d1); !ok {
		t.Fatal("the put was discarded")
	}

	// A chunk list missing a chunk does not hold the bytes either.
	c1, c2 := []byte("chunk one\n"), []byte("chunk two\n")
	dc1, dc2 := mustPut(t, s, c1), mustPut(t, s, c2)
	whole := append(append([]byte(nil), c1...), c2...)
	dw := digestOf(whole)
	if _, err := s.BindLogical(dw, []string{dc1, dc2}, []int64{int64(len(c1)), int64(len(c2))}); err != nil {
		t.Fatal(err)
	}
	dp := digestOf(whole[:3])
	if err := s.writeLogical(dp, logicalIndex{PrefixOf: dw, Length: 3}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Present(dp); err != nil || !ok {
		t.Fatalf("present over a chunk list = %v %v", ok, err)
	}
	cp, _ := s.Path(dc2)
	if err := os.Remove(cp); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Present(dp); err != nil || ok {
		t.Fatalf("present over a chunk list missing a chunk = %v %v", ok, err)
	}
}

func TestPrefixRecordsThatLoopAreAnError(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, b := digestOf([]byte("a")), digestOf([]byte("b"))
	if err := s.writeLogical(a, logicalIndex{PrefixOf: b, Length: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.writeLogical(b, logicalIndex{PrefixOf: a, Length: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(a); err == nil {
		t.Fatal("a loop of records read")
	}
	// A record whose base is not longer is refused the same way.
	c := digestOf([]byte("c"))
	d := mustPut(t, s, []byte("dd"))
	if err := s.writeLogical(c, logicalIndex{PrefixOf: b, Length: 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.writeLogical(b, logicalIndex{PrefixOf: d, Length: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(c); err == nil || !strings.Contains(err.Error(), "not longer") {
		t.Fatalf("a record over a shorter one: %v", err)
	}
}

func TestBackupRecopiesARepointedRecord(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v1 := []byte("one\n")
	mustPut(t, s, v1)
	v2, _ := grow(t, s, v1, "two\n")
	dest := t.TempDir()
	if _, err := s.Backup(dest); err != nil {
		t.Fatal(err)
	}
	// Grow again, then point v1 straight at v3, as compact will. The
	// record keeps its size and changes its base.
	_, d3 := grow(t, s, v2, "three\n")
	s.mu.Lock()
	err = s.supersedeLocked(digestOf(v1), d3, int64(len(v1)))
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(dest); err != nil {
		t.Fatal(err)
	}
	b := &Store{Root: dest}
	if got, err := b.Read(digestOf(v1)); err != nil || !bytes.Equal(got, v1) {
		t.Fatalf("backup read: %q %v", got, err)
	}
	src, _ := s.logicalPath(digestOf(v1))
	want, _ := os.ReadFile(src)
	got, _ := os.ReadFile(filepath.Join(dest, "logical", digestOf(v1)[:2], digestOf(v1)[2:]))
	if !bytes.Equal(got, want) {
		t.Fatalf("backup record %s, want %s", got, want)
	}
}

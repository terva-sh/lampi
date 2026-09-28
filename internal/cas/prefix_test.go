package cas

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	if _, err := s.Size(d1); err == nil {
		t.Fatal("size over a short base")
	}
	var problems []Problem
	if _, err := s.Verify(func(p Problem) { problems = append(problems, p) }); err != nil {
		t.Fatal(err)
	}
	// The cut object is reported, and so is the record over it.
	var record bool
	for _, p := range problems {
		record = record || (p.Digest == d1 && p.Logical)
	}
	if len(problems) != 2 || !record {
		t.Fatalf("verify over a short base: %v", problems)
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
	// A chunk that grew into a longer file is a record, and still reads.
	c1x := append(append([]byte(nil), c1...), "more\n"...)
	mustPut(t, s, c1x)
	s.mu.Lock()
	err = s.supersedeLocked(dc1, digestOf(c1x), int64(len(c1)))
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Present(dp); err != nil || !ok {
		t.Fatalf("present over a chunk that is a record = %v %v", ok, err)
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

// A backup whose walk reached a record but passed its base before
// ingest wrote it still ends with every chain resolving.
func TestBackupCopiesWhatARecordReadsFrom(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v1 := []byte("one\n")
	mustPut(t, s, v1)
	v2, _ := grow(t, s, v1, "two\n")
	v3, _ := grow(t, s, v2, "three\n")
	dest := t.TempDir()
	// Only the records reach the copy, as when the object walk ran
	// before the grows.
	for _, v := range [][]byte{v1, v2} {
		d := digestOf(v)
		src, _ := s.logicalPath(d)
		if _, err := copyIfMissing(src, filepath.Join(dest, "logical", d[:2], d[2:]), true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.closeRecords(dest); err != nil {
		t.Fatal(err)
	}
	b := &Store{Root: dest}
	for _, v := range [][]byte{v1, v2, v3} {
		if got, err := b.Read(digestOf(v)); err != nil || !bytes.Equal(got, v) {
			t.Fatalf("backup read of %q: %q %v", v, got, err)
		}
	}
}

// Every link of a chain must be longer than the one before it, not only
// longer than the first.
func TestPrefixChainWithAShorterMiddleLinkIsRefused(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := digestOf([]byte("a")), digestOf([]byte("b")), digestOf([]byte("c"))
	obj := mustPut(t, s, bytes.Repeat([]byte("x"), 200))
	if err := s.writeLogical(a, logicalIndex{PrefixOf: b, Length: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.writeLogical(b, logicalIndex{PrefixOf: c, Length: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.writeLogical(c, logicalIndex{PrefixOf: obj, Length: 2}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Present(a); err != nil || ok {
		t.Fatalf("present = %v %v", ok, err)
	}
	if _, err := s.Size(a); err == nil || !strings.Contains(err.Error(), "not longer") {
		t.Fatalf("size: %v", err)
	}
}

// A record whose chain ends at a chunk list whose chunks the backup
// walk missed gets those chunks copied too.
func TestBackupCopiesTheChunksARecordReadsThrough(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c1, c2 := []byte("chunk one\n"), []byte("chunk two\n")
	dc1, dc2 := mustPut(t, s, c1), mustPut(t, s, c2)
	whole := append(append([]byte(nil), c1...), c2...)
	dw := digestOf(whole)
	if _, err := s.BindLogical(dw, []string{dc1, dc2}, []int64{int64(len(c1)), int64(len(c2))}); err != nil {
		t.Fatal(err)
	}
	dp := digestOf(whole[:5])
	if err := s.writeLogical(dp, logicalIndex{PrefixOf: dw, Length: 5}); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	// The logical walk copied the record and the list; the object walk
	// had passed before the chunks were installed.
	for _, d := range []string{dp, dw} {
		src, _ := s.logicalPath(d)
		if _, err := copyIfMissing(src, filepath.Join(dest, "logical", d[:2], d[2:]), true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.closeRecords(dest); err != nil {
		t.Fatal(err)
	}
	b := &Store{Root: dest}
	if got, err := b.Read(dp); err != nil || !bytes.Equal(got, whole[:5]) {
		t.Fatalf("backup read: %q %v", got, err)
	}
}

// A chain that grew many times while the backup walked is closed link
// by link, however long it is.
func TestBackupClosesALongChain(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := []byte("line 0\n")
	mustPut(t, s, file)
	first := digestOf(file)
	var versions [][]byte
	versions = append(versions, file)
	for i := 1; i <= 100; i++ {
		file, _ = grow(t, s, file, fmt.Sprintf("line %d\n", i))
		versions = append(versions, file)
	}
	dest := t.TempDir()
	src, _ := s.logicalPath(first)
	if _, err := copyIfMissing(src, filepath.Join(dest, "logical", first[:2], first[2:]), true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.closeRecords(dest); err != nil {
		t.Fatal(err)
	}
	b := &Store{Root: dest}
	for _, v := range []int{0, 50, 100} {
		if got, err := b.Read(digestOf(versions[v])); err != nil || !bytes.Equal(got, versions[v]) {
			t.Fatalf("backup read of version %d: %v", v, err)
		}
	}
}

// A reader of the current head keeps working while Grow replaces that
// head's object with a record.
func TestReadsOfAHeadSurviveItsGrowth(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := []byte("line 0\n")
	mustPut(t, s, file)
	heads := make(chan []byte, 1)
	heads <- file
	done := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		defer close(errs)
		for {
			select {
			case <-done:
				return
			default:
			}
			v := <-heads
			heads <- v
			d := digestOf(v)
			got, err := s.Read(d)
			if err == nil && !bytes.Equal(got, v) {
				err = fmt.Errorf("read %d bytes, want %d", len(got), len(v))
			}
			if err == nil {
				_, err = s.Size(d)
			}
			if err != nil {
				errs <- err
				return
			}
		}
	}()
	for i := 1; i <= 300; i++ {
		file, _ = grow(t, s, file, fmt.Sprintf("line %d\n", i))
		<-heads
		heads <- file
	}
	close(done)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

// A grow whose record write failed after the grown object was
// installed is finished by the retry: the object is found, and the
// record is written.
func TestGrowRetryFinishesAnInterruptedSupersede(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v1 := []byte("first\n")
	d1 := mustPut(t, s, v1)
	tail := mustPut(t, s, []byte("second\n"))
	v2 := []byte("first\nsecond\n")
	d2 := digestOf(v2)
	// The state a failed record write leaves: v2 installed, v1 whole.
	mustPut(t, s, v2)
	exists, err := s.Grow(d2, d1, int64(len(v1)), tail, 0)
	if err != nil || !exists {
		t.Fatalf("retry = %v %v", exists, err)
	}
	if ok, _ := s.Has(d1); ok {
		t.Fatal("the retry left the old head whole")
	}
	if base, _, ok, _ := s.PrefixOf(d1); !ok || base != d2 {
		t.Fatalf("record = %s %v", base, ok)
	}
}

// GrowParts extends a chunked file's last piece with a tail, splits it
// at the limit, and keeps the old last piece as a record of its
// extension, so each version reads back and the bytes are held once
// (TKT-01M3KD7DK).
func TestGrowPartsExtendsAndSplitsTheLastPiece(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const limit = 64
	file := bytes.Repeat([]byte("0123456789"), 10) // 100 bytes: 64 + 36
	prev := bindChunks(t, s, file, limit)
	versions := map[string][]byte{prev: append([]byte(nil), file...)}
	for _, add := range []string{"short tail\n", strings.Repeat("t", 40)} {
		tail := mustPut(t, s, []byte(add))
		parts, lengths, err := s.GrowParts(prev, tail, limit)
		if err != nil {
			t.Fatal(err)
		}
		file = append(file, add...)
		sum := sha256.Sum256(file)
		next := hex.EncodeToString(sum[:])
		if _, err := s.BindLogical(next, parts, lengths); err != nil {
			t.Fatal(err)
		}
		for _, n := range lengths {
			if n > limit {
				t.Fatalf("a piece is %d bytes, past %d", n, limit)
			}
		}
		versions[next] = append([]byte(nil), file...)
		prev = next
	}
	for d, want := range versions {
		if got, err := s.Read(d); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("version of %d bytes reads %d bytes, %v", len(want), len(got), err)
		}
	}
	// 64 + 36 + 11 + 40 = 151 bytes of file; the tails stay stored too.
	var held int64
	err = s.Entries(func(e Entry) error {
		if !e.Logical {
			held += e.Size
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(len(file)) + 11 + 40; held != want {
		t.Fatalf("objects hold %d bytes, want %d", held, want)
	}
	var bad []Problem
	if _, err := s.Verify(func(p Problem) { bad = append(bad, p) }); err != nil || len(bad) != 0 {
		t.Fatalf("verify: %v %v", bad, err)
	}
}

// A last piece whose bytes are not its digest is not grown from.
func TestGrowPartsRefusesADamagedLastPiece(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prev := bindChunks(t, s, bytes.Repeat([]byte("x"), 100), 64)
	parts, _, _, _ := s.parts(prev)
	p, err := s.Path(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, bytes.Repeat([]byte("y"), 36), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GrowParts(prev, mustPut(t, s, []byte("tail")), 64); !errors.Is(err, ErrNotGrown) {
		t.Fatalf("grow from a damaged piece: %v", err)
	}
}

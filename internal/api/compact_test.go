package api

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/cas"
)

// storeEntries lists every object and logical entry with its size.
func storeEntries(t *testing.T, s *Server) []string {
	t.Helper()
	var out []string
	err := s.CAS.Entries(func(e cas.Entry) error {
		out = append(out, fmt.Sprintf("%s %v %d", e.Digest, e.Logical, e.Size))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// objectBytes is what the objects hold once decompressed.
func objectBytes(t *testing.T, s *Server) int64 {
	t.Helper()
	var n int64
	err := s.CAS.Entries(func(e cas.Entry) error {
		if !e.Logical {
			size, _, err := s.CAS.ObjectSize(e.Digest)
			n += size
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func ageObject(t *testing.T, s *Server, digest string, by time.Duration) {
	t.Helper()
	p, _, err := s.CAS.ObjectPath(digest)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-by)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
}

func verifyClean(t *testing.T, s *Server) {
	t.Helper()
	var bad []cas.Problem
	if _, err := s.CAS.Verify(func(p cas.Problem) { bad = append(bad, p) }); err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Fatalf("fsck: %v", bad)
	}
}

func readsBack(t *testing.T, s *Server, versions [][]byte) {
	t.Helper()
	for i, v := range versions {
		got, err := s.CAS.Read(sha256Hex(v))
		if err != nil {
			t.Fatalf("version %d: %v", i, err)
		}
		if !bytes.Equal(got, v) {
			t.Fatalf("version %d reads %d bytes, want %d", i, len(got), len(v))
		}
	}
}

func line(i int) string {
	return fmt.Sprintf("{\"line\":%d,\"pad\":%q}\n", i, strings.Repeat("y", 500))
}

// A lake from before prefix records holds every version whole, and the
// tails an older client put. Compact folds the versions, removes an
// old unreferenced tail and keeps a fresh one, and the dry run
// predicts exactly that without writing.
func TestCompactFoldsWholeCopiesAndDropsOldTails(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	var versions [][]byte
	var file []byte
	var reclaimed []string
	for i := 0; i < 5; i++ {
		file = append(file, line(i)...)
		v := append([]byte(nil), file...)
		d := putRaw(t, h, v)
		postManifest(t, h, manifest("m", "sid", v, d, 0, d))
		versions = append(versions, v)
		if i < 4 {
			reclaimed = append(reclaimed, d)
		}
	}
	oldTail := []byte("an assembled tail nothing names\n")
	fresh := []byte("a tail whose manifest is in flight\n")
	oldSHA := putRaw(t, h, oldTail)
	ageObject(t, s, oldSHA, 2*time.Hour)
	freshSHA := putRaw(t, h, fresh)
	// Reclaimed is disk bytes: the objects are compressed.
	var olderBytes int64
	for _, d := range append(reclaimed, oldSHA) {
		n, _, err := s.CAS.StoredSize(d)
		if err != nil {
			t.Fatal(err)
		}
		olderBytes += n
	}

	before := storeEntries(t, s)
	dry, err := s.Compact(t.Context(), CompactOptions{DryRun: true, MinAge: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if got := storeEntries(t, s); strings.Join(got, "\n") != strings.Join(before, "\n") {
		t.Fatalf("the dry run wrote:\nbefore %v\nafter  %v", before, got)
	}
	want := CompactReport{Paths: 1, Versions: 4, Folded: 4, Unreferenced: 1, Reclaimed: olderBytes}
	if fmt.Sprint(dry) != fmt.Sprint(want) {
		t.Fatalf("dry run %+v\nwant    %+v", dry, want)
	}

	rep, err := s.Compact(t.Context(), CompactOptions{MinAge: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rep) != fmt.Sprint(want) {
		t.Fatalf("compact %+v\nwant    %+v", rep, want)
	}
	readsBack(t, s, versions)
	if ok, _ := s.CAS.Has(freshSHA); !ok {
		t.Fatal("the fresh tail went")
	}
	if got, want := objectBytes(t, s), int64(len(file)+len(fresh)); got != want {
		t.Fatalf("objects hold %d bytes, want %d", got, want)
	}
	verifyClean(t, s)

	again, err := s.Compact(t.Context(), CompactOptions{MinAge: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if again.Folded+again.Flattened+again.Unreferenced != 0 || again.Reclaimed != 0 {
		t.Fatalf("second run changed something: %+v", again)
	}
}

// Versions that grew through tails form a chain of records. Compact
// points each at the newest and drops the tails the last manifest does
// not name.
func TestCompactFlattensChainsFromTails(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	var versions [][]byte
	var file []byte
	var prev int64
	for i := 0; i < 5; i++ {
		tail := []byte(line(i))
		file = append(file, tail...)
		sum := sha256Hex(file)
		if i == 0 {
			putRaw(t, h, file)
			postManifest(t, h, manifest("m", "sid", file, sum, 0, sum))
		} else {
			postManifest(t, h, manifest("m", "sid", file, sum, prev, putRaw(t, h, tail)))
		}
		prev = int64(len(file))
		versions = append(versions, append([]byte(nil), file...))
	}
	newest := sha256Hex(file)

	rep, err := s.Compact(t.Context(), CompactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// v0..v2 pointed at the next version and now point at v4; v3
	// already did. Tails 1..3 go; tail 4 is in the last manifest.
	if rep.Flattened != 3 || rep.Folded != 0 || rep.Unreferenced != 3 {
		t.Fatalf("compact %+v", rep)
	}
	for _, v := range versions[:4] {
		base, _, ok, err := s.CAS.PrefixOf(sha256Hex(v))
		if err != nil || !ok || base != newest {
			t.Fatalf("record of %d bytes points at %s %v %v", len(v), base, ok, err)
		}
	}
	readsBack(t, s, versions)
	verifyClean(t, s)
	again, err := s.Compact(t.Context(), CompactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Folded+again.Flattened+again.Unreferenced != 0 {
		t.Fatalf("second run changed something: %+v", again)
	}
}

// A divergent copy at the same path is its own branch: its older
// versions fold into it, and the main line's fold into the main line.
func TestCompactFoldsEachDivergentBranchIntoItsOwnNewest(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	a1 := []byte(line(1))
	a2 := append(append([]byte(nil), a1...), line(2)...)
	b1 := []byte("{\"other\":1}\n" + line(3) + line(4))
	b2 := append(append([]byte(nil), b1...), line(5)...)
	for _, v := range [][]byte{a1, a2, b1, b2} {
		d := putRaw(t, h, v)
		postManifest(t, h, manifest("m", "sid", v, d, 0, d))
	}
	rep, err := s.Compact(t.Context(), CompactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Folded != 2 || rep.Divergent != 1 || rep.Versions != 3 {
		t.Fatalf("compact %+v", rep)
	}
	for _, c := range []struct{ v, base []byte }{{a1, a2}, {b1, b2}} {
		base, _, ok, err := s.CAS.PrefixOf(sha256Hex(c.v))
		if err != nil || !ok || base != sha256Hex(c.base) {
			t.Fatalf("record of %q points at %s %v %v", c.v[:12], base, ok, err)
		}
	}
	readsBack(t, s, [][]byte{a1, a2, b1, b2})
	verifyClean(t, s)
}

// A newest version stored as a chunk list built from the older version
// cannot hold it: the record would loop. The dry run refuses the fold
// as the real run does, and both report the same.
func TestCompactDryRunRefusesAFoldThatWouldLoop(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	v1 := []byte(line(1))
	tail := []byte(line(2))
	v2 := append(append([]byte(nil), v1...), tail...)
	d1 := putRaw(t, h, v1)
	postManifest(t, h, manifest("m", "sid", v1, d1, 0, d1))
	d2 := putRaw(t, h, v2)
	postManifest(t, h, manifest("m", "sid", v2, d2, 0, d2))
	dt := putRaw(t, h, tail)
	p, _, err := s.CAS.ObjectPath(d2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CAS.BindLogical(d2, []string{d1, dt}, []int64{int64(len(v1)), int64(len(tail))}); err != nil {
		t.Fatal(err)
	}
	dry, err := s.Compact(t.Context(), CompactOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.Compact(t.Context(), CompactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Looped != 1 || dry.Folded != 0 || fmt.Sprint(dry) != fmt.Sprint(rep) {
		t.Fatalf("dry run %+v\nreal    %+v", dry, rep)
	}
	readsBack(t, s, [][]byte{v1, v2})
	verifyClean(t, s)
}

// The loop check follows folds already made as well as the store, so a
// fold that makes a base read from a version is seen before the store
// holds it, and one that stops a base doing so frees a later fold.
func TestCompactLoopCheckFollowsFoldsMadeSoFar(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	d := []byte("the version\n")
	x := []byte("a middle file\n")
	t2 := []byte("more\n")
	r := []byte("rest\n")
	dd, dx, dt2, dr := putRaw(t, h, d), putRaw(t, h, x), putRaw(t, h, t2), putRaw(t, h, r)
	top := sha256Hex(append(append([]byte(nil), x...), t2...))
	if _, err := s.CAS.BindLogical(top, []string{dx, dt2}, []int64{int64(len(x)), int64(len(t2))}); err != nil {
		t.Fatal(err)
	}
	viaD := sha256Hex(append(append([]byte(nil), d...), r...))
	if _, err := s.CAS.BindLogical(viaD, []string{dd, dr}, []int64{int64(len(d)), int64(len(r))}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		applied map[string]string
		want    bool
	}{
		{nil, false},
		{map[string]string{dx: viaD}, true},
		{map[string]string{dx: dr}, false},
	} {
		got, err := s.readsFrom(top, dd, c.applied)
		if err != nil || got != c.want {
			t.Fatalf("applied %v: reads from = %v %v, want %v", c.applied, got, err, c.want)
		}
	}
}

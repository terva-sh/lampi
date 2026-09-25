package cas

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func age(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSweepRemovesOldTempsAndPartials(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("kept object")
	putAll(t, s, body)
	obj, _ := s.Path(digestOf(body))
	now := time.Now()
	old := now.Add(-48 * time.Hour)

	shard := filepath.Dir(obj)
	oldPut := filepath.Join(shard, ".put-123")
	freshPut := filepath.Join(shard, ".put-456")
	oldLogical := filepath.Join(s.Root, "logical", "ab", ".logical-1")
	for _, p := range []string{oldPut, freshPut, oldLogical} {
		damage(t, p, []byte("temp"))
	}
	age(t, oldPut, old)
	age(t, oldLogical, old)

	stale := []byte("abandoned upload")
	sd := digestOf(stale)
	if _, _, err := s.PutRange(sd, 0, 3, int64(len(stale)), 0, bytes.NewReader(stale[:4])); err != nil {
		t.Fatal(err)
	}
	live := []byte("upload in progress")
	ld := digestOf(live)
	if _, _, err := s.PutRange(ld, 0, 3, int64(len(live)), 0, bytes.NewReader(live[:4])); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"data", "meta.json"} {
		age(t, filepath.Join(s.partialDir(sd), name), old)
	}
	age(t, filepath.Join(s.partialDir(ld), "data"), old)

	removed, err := s.Sweep(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 3 {
		t.Fatalf("removed %d, want 3", removed)
	}
	if exists(oldPut) || exists(oldLogical) || exists(s.partialDir(sd)) {
		t.Fatal("an old temp file or partial upload is still there")
	}
	// meta.json is fresh, so that upload is still being resumed.
	if !exists(freshPut) || !exists(s.partialDir(ld)) || !exists(obj) {
		t.Fatal("sweep removed a fresh temp file, a live upload, or an object")
	}
}

func TestVerifyNamesBadObjectsAndRepairRemovesThem(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	good, flipped := []byte("good object"), []byte("flipped object")
	a, b := []byte("chunk a"), []byte("chunk b")
	putAll(t, s, good, flipped, a, b)
	ab := append(append([]byte{}, a...), b...)
	if _, err := s.BindLogical(digestOf(ab), []string{digestOf(a), digestOf(b)}, []int64{7, 7}); err != nil {
		t.Fatal(err)
	}
	fd := digestOf(flipped)
	fp, _ := s.Path(fd)
	damage(t, fp, []byte("flipped objecT"))
	// An index whose chunk was lost, and an index that does not parse.
	lost := digestOf([]byte("lost"))
	idxLost, _ := s.logicalPath(lost)
	damage(t, idxLost, []byte(`{"chunk_sha256s":["`+lost+`"],"chunk_lengths":[4]}`))
	junk := digestOf([]byte("junk"))
	idxJunk, _ := s.logicalPath(junk)
	damage(t, idxJunk, []byte("{"))
	damage(t, filepath.Join(filepath.Dir(fp), ".put-9"), []byte("temp"))

	var bad []Problem
	checked, err := s.Verify(func(p Problem) { bad = append(bad, p) })
	if err != nil {
		t.Fatal(err)
	}
	if checked != 7 {
		t.Fatalf("checked %d, want 4 objects and 3 indexes", checked)
	}
	if len(bad) != 3 {
		t.Fatalf("problems: %v", bad)
	}
	want := map[string]bool{"object " + fd: true, "logical " + lost: true, "logical " + junk: true}
	for _, p := range bad {
		head, _, _ := strings.Cut(p.String(), ":")
		if !want[head] {
			t.Fatalf("unexpected problem %s", p)
		}
	}

	for _, p := range bad {
		if _, err := s.Repair(p); err != nil {
			t.Fatal(err)
		}
	}
	if ok, _ := s.Has(fd); ok {
		t.Fatal("repaired object still reported present")
	}
	if exists(idxJunk) || !exists(idxLost) {
		t.Fatal("repair should drop the unreadable index and keep the one missing a chunk")
	}
	putAll(t, s, flipped)
	got, err := s.Read(fd)
	if err != nil || !bytes.Equal(got, flipped) {
		t.Fatalf("re-put after repair: %q %v", got, err)
	}
}

func TestBackupCopiesObjectsAndIndexesOnce(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	one, a, b := []byte("one"), []byte("chunk a"), []byte("chunk b")
	putAll(t, s, one, a, b)
	ab := append(append([]byte{}, a...), b...)
	if _, err := s.BindLogical(digestOf(ab), []string{digestOf(a), digestOf(b)}, []int64{7, 7}); err != nil {
		t.Fatal(err)
	}
	p1, _ := s.Path(digestOf(one))
	damage(t, filepath.Join(filepath.Dir(p1), ".put-1"), []byte("temp"))
	half := []byte("half")
	if _, _, err := s.PutRange(digestOf(half), 0, 1, 4, 0, bytes.NewReader(half[:2])); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	n, err := s.Backup(dest)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("copied %d, want 3 objects and 1 index", n)
	}
	if exists(filepath.Join(dest, "sha256", filepath.Base(filepath.Dir(p1)), ".put-1")) || exists(filepath.Join(dest, "partial")) {
		t.Fatal("backup copied a temp file or a partial upload")
	}
	restored, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restored.Read(digestOf(ab))
	if err != nil || !bytes.Equal(got, ab) {
		t.Fatalf("logical file from the backup: %q %v", got, err)
	}
	var bad []Problem
	if _, err := restored.Verify(func(p Problem) { bad = append(bad, p) }); err != nil || len(bad) > 0 {
		t.Fatalf("backup verify: %v %v", bad, err)
	}
	if n, err := s.Backup(dest); err != nil || n != 0 {
		t.Fatalf("second backup copied %d: %v", n, err)
	}
}

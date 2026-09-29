package cas

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An object is stored compressed, and every read returns its bytes:
// the empty object, one below the size the encoder records in the
// frame header, and ones above it (TKT-01M3K45MV).
func TestObjectsAreStoredCompressed(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{
		nil,
		[]byte("short\n"),
		bytes.Repeat([]byte("x"), 255),
		bytes.Repeat([]byte("y"), 256),
		bytes.Repeat([]byte(`{"type":"assistant","text":"the same line again"}`+"\n"), 4000),
	} {
		d := mustPut(t, s, body)
		installedAs(t, s, d, body)
		if ok, err := s.Has(d); err != nil || !ok {
			t.Fatalf("%d bytes: has = %v %v", len(body), ok, err)
		}
		if n, err := s.Size(d); err != nil || n != int64(len(body)) {
			t.Fatalf("%d bytes: size = %d %v", len(body), n, err)
		}
		stored, _, err := s.StoredSize(d)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) > 1000 && stored*10 > int64(len(body)) {
			t.Fatalf("%d repetitive bytes stored in %d", len(body), stored)
		}
		if exists, err := s.Put(d, bytes.NewReader(body), 0); err != nil || !exists {
			t.Fatalf("%d bytes: second put exists %v %v", len(body), exists, err)
		}
	}
}

// An object installed before compression is the raw bytes under the
// name without .zst. It still reads, has its size, and counts as
// present, and a put of the same bytes keeps it.
func TestRawObjectsStillRead(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("installed by an older lake\n")
	d := digestOf(body)
	raw, _ := s.Path(d)
	damage(t, raw, body)

	if got, err := s.Read(d); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read %q %v", got, err)
	}
	if n, err := s.Size(d); err != nil || n != int64(len(body)) {
		t.Fatalf("size %d %v", n, err)
	}
	if ok, err := s.Present(d); err != nil || !ok {
		t.Fatalf("present %v %v", ok, err)
	}
	if exists, err := s.Put(d, bytes.NewReader(body), 0); err != nil || !exists {
		t.Fatalf("put over an intact raw object: exists %v %v", exists, err)
	}
	var entries []Entry
	if err := s.Entries(func(e Entry) error { entries = append(entries, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Digest != d || entries[0].Compressed {
		t.Fatalf("entries %+v", entries)
	}

	// A version that grows from it is stored compressed, and the raw
	// object becomes a record of it.
	next, dn := grow(t, s, body, "and grown by a new one\n")
	installedAs(t, s, dn, next)
	if _, err := os.Lstat(raw); !os.IsNotExist(err) {
		t.Fatalf("superseded raw object kept: %v", err)
	}
	if got, err := s.Read(d); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read the grown-from version %q %v", got, err)
	}
}

// fsck decodes each frame: one that does not decode, and one that
// decodes to the wrong bytes, are both problems. A raw copy left beside
// an intact frame is repaired by removing the copy alone.
func TestVerifyDecodesFrames(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	junk, wrong, kept := []byte("junk frame\n"), []byte("wrong frame\n"), []byte("kept frame\n")
	dj, dw, dk := mustPut(t, s, junk), mustPut(t, s, wrong), mustPut(t, s, kept)
	pj, _ := s.zstPath(dj)
	damage(t, pj, []byte("not zstd at all"))
	storeFrame(t, s, dw, []byte("other bytes\n"))
	rawKept, _ := s.Path(dk)
	damage(t, rawKept, []byte("a damaged raw copy"))

	var bad []Problem
	checked, err := s.Verify(func(p Problem) { bad = append(bad, p) })
	if err != nil {
		t.Fatal(err)
	}
	if checked != 4 || len(bad) != 3 {
		t.Fatalf("checked %d, problems %v", checked, bad)
	}
	reasons := map[string]string{}
	for _, p := range bad {
		reasons[p.Digest] += p.Reason + ";"
	}
	if !strings.Contains(reasons[dj], "does not decompress") || !strings.Contains(reasons[dw], "bytes hash to") || !strings.Contains(reasons[dk], "bytes hash to") {
		t.Fatalf("reasons %v", reasons)
	}
	for _, p := range bad {
		if fixed, err := s.Repair(p); err != nil || !fixed {
			t.Fatalf("repair %s: %v %v", p, fixed, err)
		}
	}
	for _, d := range []string{dj, dw} {
		if ok, _ := s.Has(d); ok {
			t.Fatalf("%s still present after repair", d)
		}
	}
	installedAs(t, s, dk, kept)
}

// Backup copies frames as they are stored. A raw object with a frame
// beside it is not copied, and a raw copy an earlier backup made of an
// object since compressed is removed from the backup.
func TestBackupCopiesTheStoredForm(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy, fresh := []byte("from an older lake\n"), []byte("from this one\n")
	dl := digestOf(legacy)
	raw, _ := s.Path(dl)
	damage(t, raw, legacy)
	df := mustPut(t, s, fresh)

	dest := t.TempDir()
	if n, err := s.Backup(dest); err != nil || n != 2 {
		t.Fatalf("first backup copied %d: %v", n, err)
	}
	backup := &Store{Root: dest}
	installedAs(t, backup, df, fresh)
	if got, err := backup.Read(dl); err != nil || !bytes.Equal(got, legacy) {
		t.Fatalf("raw object in the backup reads %q %v", got, err)
	}
	src, _ := s.zstPath(df)
	a, _ := os.ReadFile(src)
	b, _ := os.ReadFile(filepath.Join(dest, "sha256", df[:2], df[2:]+zstSuffix))
	if !bytes.Equal(a, b) {
		t.Fatal("the backup's frame is not the stored one")
	}

	// The legacy object is compressed, leaving its raw file behind for
	// a moment, as a re-encode does.
	if err := s.RemoveEntry(Entry{Digest: dl}); err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, legacy)
	damage(t, raw, legacy)
	if n, err := s.Backup(dest); err != nil || n != 1 {
		t.Fatalf("second backup copied %d: %v", n, err)
	}
	installedAs(t, backup, dl, legacy)
}

// A damaged frame beside an intact raw copy is removed on its own, so
// repair leaves the object readable rather than missing. StoredSize
// counts both files, since removing the object frees both.
func TestRepairKeepsAnIntactRawCopy(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("the raw copy is the good one\n")
	d := mustPut(t, s, body)
	raw, _ := s.Path(d)
	damage(t, raw, body)
	frame, _ := s.zstPath(d)
	fs, _ := os.Lstat(frame)
	if n, _, err := s.StoredSize(d); err != nil || n != fs.Size()+int64(len(body)) {
		t.Fatalf("stored size %d %v, want both files", n, err)
	}
	damage(t, frame, []byte("not a frame"))

	var bad []Problem
	if _, err := s.Verify(func(p Problem) { bad = append(bad, p) }); err != nil || len(bad) != 1 {
		t.Fatalf("verify %v %v", bad, err)
	}
	if fixed, err := s.Repair(bad[0]); err != nil || !fixed {
		t.Fatalf("repair %v %v", fixed, err)
	}
	if _, err := os.Lstat(frame); !os.IsNotExist(err) {
		t.Fatalf("damaged frame kept: %v", err)
	}
	if got, err := s.Read(d); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read after repair %q %v", got, err)
	}
	if fixed, err := s.Repair(bad[0]); err != nil || fixed {
		t.Fatalf("second repair %v %v", fixed, err)
	}
}

// Reencode rewrites a raw object as a frame and removes the raw file.
// A raw object that is not its digest is left, and a raw copy beside a
// frame is removed with nothing written.
func TestReencodeCompressesRawObjects(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte(`{"type":"user","text":"compress me"}`+"\n"), 500)
	d := digestOf(body)
	raw, _ := s.Path(d)
	damage(t, raw, body)

	before, after, err := s.Reencode(d)
	if err != nil {
		t.Fatal(err)
	}
	if before != int64(len(body)) || after <= 0 || after >= before/10 {
		t.Fatalf("reencode %d -> %d", before, after)
	}
	installedAs(t, s, d, body)
	if before, after, err := s.Reencode(d); err != nil || before != 0 || after != 0 {
		t.Fatalf("second reencode %d -> %d %v", before, after, err)
	}

	// A raw copy beside the frame, as a re-encode that stopped leaves.
	damage(t, raw, body)
	if before, after, err := s.Reencode(d); err != nil || before != int64(len(body)) || after != 0 {
		t.Fatalf("reencode beside a frame %d -> %d %v", before, after, err)
	}
	installedAs(t, s, d, body)

	rotten := []byte("rotten bytes\n")
	dr := digestOf(rotten)
	rawRotten, _ := s.Path(dr)
	damage(t, rawRotten, []byte("rotted bytes\n"))
	if _, _, err := s.Reencode(dr); !errors.Is(err, ErrNotItsDigest) {
		t.Fatalf("reencode of a damaged object: %v", err)
	}
	if got, err := os.ReadFile(rawRotten); err != nil || string(got) != "rotted bytes\n" {
		t.Fatalf("damaged object changed: %q %v", got, err)
	}
	if _, ok, _ := s.object(dr); !ok {
		t.Fatal("damaged object removed")
	}
	zr, _ := s.zstPath(dr)
	if _, err := os.Lstat(zr); !os.IsNotExist(err) {
		t.Fatalf("frame written for a damaged object: %v", err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(zr), ".put-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}
}

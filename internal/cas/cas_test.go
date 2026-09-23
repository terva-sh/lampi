package cas

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestPutLayoutAndIdempotent(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("session-line\n")
	d := digestOf(body)

	exists, err := s.Put(d, bytes.NewReader(body), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("first put reported exists")
	}
	want := filepath.Join(root, "sha256", d[:2], d[2:])
	got, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("stored %q", got)
	}
	st, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}

	exists, err = s.Put(d, bytes.NewReader(body), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("second put should be a no-op hit")
	}

	ok, err := s.Has(d)
	if err != nil || !ok {
		t.Fatalf("has %v %v", ok, err)
	}
	ok, err = s.Has(digestOf([]byte("other")))
	if err != nil || ok {
		t.Fatalf("missing digest has %v %v", ok, err)
	}
}

func TestPutRejectsMismatchAndOversize(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("abc")
	claimed := digestOf([]byte("nope"))
	if _, err := s.Put(claimed, bytes.NewReader(body), 1024); !errors.Is(err, ErrRejected) {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
	if _, err := s.Put("not-a-digest", bytes.NewReader(body), 1024); err == nil {
		t.Fatal("expected invalid digest")
	}
	d := digestOf(body)
	if _, err := s.Put(d, bytes.NewReader(body), 2); !errors.Is(err, ErrRejected) {
		t.Fatalf("expected oversize refusal, got %v", err)
	}
	ok, err := s.Has(d)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("refused put left an object")
	}
}

func TestBindLogicalDoesNotInstallAssembly(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	left := []byte("hello ")
	right := []byte("world")
	whole := append(append([]byte{}, left...), right...)
	d0 := digestOf(left)
	d1 := digestOf(right)
	full := digestOf(whole)
	if _, err := s.Put(d0, bytes.NewReader(left), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(d1, bytes.NewReader(right), 0); err != nil {
		t.Fatal(err)
	}
	exists, err := s.BindLogical(full, []string{d0, d1}, []int64{int64(len(left)), int64(len(right))})
	if err != nil || exists {
		t.Fatalf("bind exists %v err %v", exists, err)
	}
	ok, err := s.Has(full)
	if err != nil || ok {
		t.Fatalf("logical digest installed: has %v %v", ok, err)
	}
	got, err := s.Read(full)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, whole) {
		t.Fatalf("read %q", got)
	}
	exists, err = s.BindLogical(full, []string{d0, d1}, []int64{int64(len(left)), int64(len(right))})
	if err != nil || !exists {
		t.Fatalf("second bind exists %v err %v", exists, err)
	}
	if _, err := s.BindLogical(digestOf([]byte("nope")), []string{d0, d1}, []int64{int64(len(left)), int64(len(right))}); !errors.Is(err, ErrRejected) {
		t.Fatalf("hash mismatch: %v", err)
	}
	if _, err := s.BindLogical(full, []string{d0, d1}, []int64{1, int64(len(right))}); !errors.Is(err, ErrRejected) {
		t.Fatalf("length mismatch: %v", err)
	}
	if ok, err := s.Has(full); err != nil || ok {
		t.Fatalf("failed bind installed the digest: %v %v", ok, err)
	}
}

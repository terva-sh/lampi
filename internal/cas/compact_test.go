package cas

import (
	"bytes"
	"errors"
	"testing"
)

func TestFoldFreesTheObjectAndRefusesALoop(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	short, long := []byte("abc\n"), []byte("abc\ndef\n")
	ds, dl := mustPut(t, s, short), mustPut(t, s, long)

	freed, err := s.Fold(ds, dl, int64(len(short)))
	if err != nil || freed != int64(len(short)) {
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

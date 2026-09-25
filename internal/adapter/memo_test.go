package adapter

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type mapMemo map[string]struct {
	st FileStat
	s  Seen
}

func (m mapMemo) Recall(path string, st FileStat) (Seen, bool) {
	e, ok := m[path]
	if !ok || e.st != st {
		return Seen{}, false
	}
	return e.s, true
}

func (m mapMemo) Remember(path string, st FileStat, s Seen) {
	m[path] = struct {
		st FileStat
		s  Seen
	}{st, s}
}

// A recalled file is not opened and its reader is not called. A stat
// that moved reads and hashes again.
func TestIdentifyUsesTheMemoAtTheSameStat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := Ref{AbsPath: path, Size: 4, ModTime: time.Unix(10, 0), Inode: 7}
	memo := mapMemo{}
	reads := 0
	read := func() (string, error) { reads++; return "id-1", nil }
	sum, id, err := Identify(memo, ref, read)
	if err != nil || id != "id-1" || reads != 1 {
		t.Fatalf("first %s %s %v reads=%d", sum, id, err, reads)
	}
	// The file is gone. A recall does not notice, because it does not
	// open it.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	again, id, err := Identify(memo, ref, read)
	if err != nil || again != sum || id != "id-1" || reads != 1 {
		t.Fatalf("recall %s %s %v reads=%d", again, id, err, reads)
	}
	ref.Inode = 8
	if _, _, err := Identify(memo, ref, read); err == nil || reads != 2 {
		t.Fatalf("a new inode was recalled: %v reads=%d", err, reads)
	}
	if _, _, err := Identify[string](nil, ref, read); err == nil {
		t.Fatal("a nil memo did not read")
	}
}

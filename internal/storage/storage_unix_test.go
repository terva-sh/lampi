//go:build unix

package storage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TKT-01M3JV45Y: a directory's own blocks count toward the component it
// holds, as du counts them, and add no file.
func TestMeasureCountsDirectories(t *testing.T) {
	dir := t.TempDir()
	shard := filepath.Join(dir, "cas", "sha256", "ab")
	if err := os.MkdirAll(shard, 0o700); err != nil {
		t.Fatal(err)
	}
	var st syscall.Stat_t
	if err := syscall.Stat(shard, &st); err != nil {
		t.Fatal(err)
	}
	if st.Blocks == 0 {
		t.Skip("this filesystem allocates no blocks to a directory")
	}
	got, err := Measure(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got[CAS].Files != 0 || got[CAS].Bytes < 3*st.Blocks*512 {
		t.Fatalf("cas %+v, want 0 files and at least three directories of %d bytes", got[CAS], st.Blocks*512)
	}
}

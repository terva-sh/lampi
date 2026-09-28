package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	for rel, want := range map[string]string{
		"cas/sha256/ab/cdef":             CAS,
		"cas/logical/ab/cdef":            CAS,
		"cas/sha256/ab/.put-123":         Uploads,
		"cas/partial/ab/upload/0":        Uploads,
		"catalog.db":                     Catalog,
		"catalog.db-wal":                 Catalog,
		"search.db-shm":                  Search,
		"audit.jsonl":                    Audit,
		"audit.jsonl.lock":               Audit,
		"normalized/uid.jsonl":           Normalized,
		"parquet/date=2026-09-28/h/x.pq": Parquet,
		"identity.json":                  Other,
		"tokens/laptop.token":            Other,
		"catalog.db/nested":              Other,
	} {
		if got := classify(rel); got != want {
			t.Errorf("classify(%q) = %s, want %s", rel, got, want)
		}
	}
}

// TKT-01M3JV45Y: every file lands in one component, and a symlink is
// neither followed nor counted.
func TestMeasure(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string, n int) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Repeat("x", n)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("cas/sha256/ab/one", 5000)
	write("cas/sha256/ab/two", 10)
	write("cas/partial/ab/up/0", 10)
	write("catalog.db", 8192)
	write("identity.json", 10)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "big"), make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(dir, "normalized")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Measure(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(Components) {
		t.Fatalf("components %v", got)
	}
	for c, files := range map[string]int64{CAS: 2, Uploads: 1, Catalog: 1, Other: 1, Normalized: 0, Parquet: 0} {
		if got[c].Files != files {
			t.Errorf("%s files %d, want %d", c, got[c].Files, files)
		}
	}
	if got[CAS].Bytes < 5010 {
		t.Errorf("cas bytes %d, less than the data written", got[CAS].Bytes)
	}
	if got[Normalized].Bytes != 0 {
		t.Errorf("followed a symlink: normalized %d bytes", got[Normalized].Bytes)
	}
	if _, err := Measure(t.Context(), filepath.Join(dir, "missing")); err == nil {
		t.Error("measuring a missing directory succeeded")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Measure(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled walk: %v", err)
	}
}

func TestCapacity(t *testing.T) {
	fs, err := Capacity(t.TempDir())
	if err == ErrUnsupported {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if fs.Total == 0 || fs.Free > fs.Total {
		t.Fatalf("capacity %+v", fs)
	}
}

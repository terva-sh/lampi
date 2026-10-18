package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
	"terva.sh/lampi/internal/normalize"
)

func TestProjectionMeasuresRealOutput(t *testing.T) {
	o := options{out: t.TempDir(), workload: "projection", sizeMiB: 1, sessions: 2, keepData: true}
	r, err := work(o)
	if err != nil {
		t.Fatal(err)
	}
	if r.InputBytes < 2<<20 || len(r.Stages) != 8 || r.OutputBytes["parquet"] == 0 {
		t.Fatalf("incomplete projection: %+v", r)
	}
	path := filepath.Join(o.out, "data", "parquet", "date=2026-10-17", "harness=claude", "synthetic-0000.parquet")
	rows, err := parquet.ReadFile[normalize.ParquetRow](path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(o.out, "data", "claude", "projects", "synthetic-lampi", "synthetic-0000.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if want := bytes.Count(raw, []byte{'\n'}); len(rows) != want || len(rows) != r.Stages[3].Events {
		t.Fatalf("rows=%d, source records=%d, reported=%d", len(rows), want, r.Stages[3].Events)
	}
	if r.Stages[1].Allocated == 0 || r.Stages[1].SampledHeap == 0 {
		t.Fatal("missing memory measurements")
	}
}

func TestSyncUsesOnlySyntheticHomesAndLake(t *testing.T) {
	// These would break a caller that accidentally resolved live config.
	t.Setenv("LAMPI_SERVER", "http://127.0.0.1:1")
	t.Setenv("LAMPI_TOKEN_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	o := options{out: t.TempDir(), workload: "sync", sizeMiB: 1, sessions: 2, unchanged: 1, appends: 2, appendKiB: 8}
	r, err := work(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Stages) != 4 {
		t.Fatalf("stages=%d", len(r.Stages))
	}
	for i, s := range r.Stages {
		if s.Sync == nil || s.Sync.Refused != 0 || s.Sync.Quarantined != 0 || len(s.Sync.Skipped) != 0 {
			t.Fatalf("stage %d: %+v", i, s)
		}
		if i == 1 {
			if s.Sync.Manifests != 0 || s.Sync.Uploaded != 0 || s.Sync.Unchanged != 2 {
				t.Fatalf("unchanged pass did work: %+v", s.Sync)
			}
		} else if s.Sync.Manifests != 2 {
			t.Fatalf("stage %d lost sessions: %+v", i, s.Sync)
		}
	}
	if _, err := os.Stat(filepath.Join(o.out, "data")); !os.IsNotExist(err) {
		t.Fatalf("synthetic data was not removed: %v", err)
	}
}

func TestRunnerRefusesExistingOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep")
	if err := os.WriteFile(path, []byte("existing user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--out", dir}, io.Discard); err == nil {
		t.Fatal("accepted an existing output directory")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "existing user data" {
		t.Fatalf("modified existing data: %q, %v", b, err)
	}
}

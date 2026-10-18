package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
	"terva.sh/lampi/internal/normalize"
)

func TestMain(m *testing.M) {
	// Exercise the real separate-process lake protocol using this test binary.
	if len(os.Args) > 1 && os.Args[1] == "--lake-worker" {
		main()
		return
	}
	os.Exit(m.Run())
}

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
	for _, workload := range []string{"sync", "agent"} {
		t.Run(workload, func(t *testing.T) { testSyncWorkload(t, workload) })
	}
}

func testSyncWorkload(t *testing.T, workload string) {
	// These would break a caller that accidentally resolved live config.
	t.Setenv("LAMPI_SERVER", "http://127.0.0.1:1")
	t.Setenv("LAMPI_TOKEN_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	o := options{out: t.TempDir(), workload: workload, sizeMiB: 1, sessions: 2, unchanged: 1, appends: 2, appendKiB: 8}
	r, err := work(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Stages) != 4 {
		t.Fatalf("stages=%d", len(r.Stages))
	}
	if workload == "agent" && !strings.Contains(r.Scope, "agent process only") {
		t.Fatalf("incorrect measurement scope: %s", r.Scope)
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

func TestBurstDrainsLatestGenerations(t *testing.T) {
	for _, workload := range []string{"sync", "agent"} {
		t.Run(workload, func(t *testing.T) {
			r, err := work(options{out: t.TempDir(), workload: workload, sizeMiB: 1, sessions: 2, burst: 4, appendKiB: 8, normalizeWorkers: 2, normalizeBudgetMiB: 1})
			if err != nil {
				t.Fatal(err)
			}
			last := r.Stages[len(r.Stages)-1]
			if last.Name != "burst-drain" || last.Lake == nil || last.Lake.Admission.Bytes != 0 || last.Lake.Results["failed"] != 0 || last.Lake.Results["ok"] < 4 {
				t.Fatalf("incomplete burst: %+v", last)
			}
		})
	}
}
func TestParseInvalidAdmission(t *testing.T) {
	for _, args := range [][]string{{"--normalize-workers", "-1"}, {"--normalize-workers", "65"}, {"--normalize-input-budget-mib", "-1"}, {"--burst-passes", "-1"}} {
		if _, err := parse(append([]string{"--out", "unused"}, args...)); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestStreamingComponentsAndSearch(t *testing.T) {
	for _, workload := range []string{"stream", "sync"} {
		t.Run(workload, func(t *testing.T) {
			r, err := work(options{out: t.TempDir(), workload: workload, sizeMiB: 1, sessions: 1, appends: 1, appendKiB: 8, search: workload == "sync"})
			if err != nil {
				t.Fatal(err)
			}
			if workload == "stream" && (len(r.Stages) != 3 || r.Stages[0].Events == 0 || r.OutputBytes["parquet"] == 0) {
				t.Fatalf("incomplete components: %+v", r)
			}
			if workload == "sync" && (len(r.Stages) != 4 || !strings.HasSuffix(r.Stages[3].Name, "/search")) {
				t.Fatalf("missing append search: %+v", r.Stages)
			}
		})
	}
}

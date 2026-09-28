package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// TKT-01M3HKYFC: serve normalize queues stale and failed sessions,
// names them, and --dry-run queues nothing.
func TestServeNormalizeQueuesStaleAndFailed(t *testing.T) {
	data := t.TempDir()
	cat, err := catalog.Open(filepath.Join(data, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	ingest := func(id string) string {
		ack, err := cat.Ingest(t.Context(), protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       "m",
			Harness:         protocol.HarnessTerva,
			NativeSessionID: id,
			Artifacts: []protocol.Artifact{{
				Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/x/" + id + ".jsonl",
				Size: 1, SHA256: strings.Repeat("a", 64),
			}},
		}, time.Now(), []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return ack.SessionUID
	}
	stale, failed, ready := ingest("stale"), ingest("failed"), ingest("ready")
	if err := cat.SetNormalizeError(t.Context(), failed, "normalize: boom"); err != nil {
		t.Fatal(err)
	}
	gen, head, _, err := cat.NormalizeVersion(t.Context(), ready)
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.MarkPublished(t.Context(), ready, gen, head); err != nil {
		t.Fatal(err)
	}
	cat.Close()

	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		env := Env{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }}
		if err := Run(append([]string{"serve", "normalize", "--data", data}, args...), env); err != nil {
			t.Fatalf("serve normalize %v: %v\n%s", args, err, out.String())
		}
		return out.String()
	}
	out := run("--stale", "--failed", "--dry-run")
	if !strings.Contains(out, "would queue 2 sessions: 1 unknown, 1 failed, 0 other") || strings.Contains(out, ready) {
		t.Fatalf("dry run:\n%s", out)
	}
	cat, err = catalog.OpenReadOnly(filepath.Join(data, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	if jobs, _ := cat.ListNormalizeJobs(t.Context()); len(jobs) != 0 {
		t.Fatalf("--dry-run queued %d jobs", len(jobs))
	}
	cat.Close()

	out = run("--stale", "--failed", "--session", stale)
	for _, want := range []string{"queued " + stale + " (was unknown)", "queued " + failed + " (was failed)", "queued 2 sessions", "kill -s HUP"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	cat, err = catalog.OpenReadOnly(filepath.Join(data, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cat.Close()
	for uid, want := range map[string]string{stale: "pending", failed: "pending", ready: "ready"} {
		if got, err := cat.NormalizationState(t.Context(), uid); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", uid, got, err, want)
		}
	}
	if err := Run([]string{"serve", "normalize", "--data", data}, Env{Stdout: io.Discard, Stderr: io.Discard, Getenv: func(string) string { return "" }}); err == nil {
		t.Error("serve normalize with no selector succeeded")
	}
	cat.Close()

	// --all takes the ready session too, and the two already queued
	// once each.
	out = run("--all", "--stale")
	for _, want := range []string{"queued " + ready + " (was ready)", "queued 3 sessions"} {
		if !strings.Contains(out, want) {
			t.Errorf("--all output lacks %q:\n%s", want, out)
		}
	}
	cat, err = catalog.OpenReadOnly(filepath.Join(data, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cat.Close()
	if got, err := cat.NormalizationState(t.Context(), ready); err != nil || got != "pending" {
		t.Errorf("ready after --all: %q %v, want pending", got, err)
	}
}

func TestAgeStringKeepsFractions(t *testing.T) {
	for secs, want := range map[float64]string{0: "0s", 0.25: "250ms", 2.5: "3s", 150: "2m30s"} {
		if got := ageString(secs); got != want {
			t.Errorf("ageString(%v) = %q, want %q", secs, got, want)
		}
	}
}

// TKT-01M3KA702: --status reads the catalog with no serve running and
// names failed sessions with their messages; --json prints the
// /v1/stats normalization object.
func TestServeNormalizeStatus(t *testing.T) {
	data := t.TempDir()
	cat, err := catalog.Open(filepath.Join(data, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	ingest := func(id string) string {
		ack, err := cat.Ingest(t.Context(), protocol.Manifest{
			CaptureProtocol: protocol.Version, MachineID: "m", Harness: protocol.HarnessTerva, NativeSessionID: id,
			Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/x/" + id + ".jsonl", Size: 1, SHA256: strings.Repeat("a", 64)}},
		}, time.Now(), []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return ack.SessionUID
	}
	bad, waiting := ingest("bad"), ingest("waiting")
	ingest("unknown")
	if err := cat.SetNormalizeError(t.Context(), bad, "normalize: line 3 is not JSON"); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.EnqueueNormalize(t.Context(), waiting, time.Now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	cat.Close()

	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		env := Env{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }}
		err := Run(append([]string{"serve", "normalize", "--data", data}, args...), env)
		return out.String(), err
	}
	out, err := run("--status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"sessions: ready=0 pending=1 failed=1 unknown=1\n",
		"jobs: 1 waiting, oldest 2m",
		"failed " + bad + ": normalize: line 3 is not JSON\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--status lacks %q:\n%s", want, out)
		}
	}
	out, err = run("--status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var st protocol.NormalizationStats
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("--json: %v\n%s", err, out)
	}
	if st.Jobs != 1 || st.Sessions["failed"] != 1 || st.OldestPendingSeconds < 110 || st.Queued != 0 {
		t.Fatalf("--json %+v", st)
	}
	if _, err := run("--status", "--all"); err == nil {
		t.Error("--status with a selector succeeded")
	}
	if _, err := run("--json", "--all"); err == nil {
		t.Error("--json without --status succeeded")
	}
}

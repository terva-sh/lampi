package cli

import (
	"bytes"
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
}

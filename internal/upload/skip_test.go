package upload

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/claude"
	"terva.sh/lampi/internal/config"
)

// One harness that cannot be walked and one file with a line too long
// to find its session are skipped and named. Every other session in
// the run still reaches the lake.
func TestSyncSkipsWhatItCannotReadAndUploadsTheRest(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	tervaHome := t.TempDir()
	// sessions is a file, so the whole terva harness fails its walk.
	mustFile(t, filepath.Join(tervaHome, "sessions"), "not a directory")

	claudeHome := t.TempDir()
	dir := filepath.Join(claudeHome, "projects", "-work-app")
	mustFile(t, filepath.Join(dir, "good.jsonl"), `{"type":"user","sessionId":"good","cwd":"/work/app"}`+"\n")
	huge := `{"type":"user","blob":"` + strings.Repeat("x", 8<<20) + "\"}\n"
	mustFile(t, filepath.Join(dir, "lost.jsonl"), huge+`{"type":"user"}`+"\n")

	codexHome := t.TempDir()
	mustFile(t, filepath.Join(codexHome, "sessions", "2026", "09", "23", "rollout-a.jsonl"),
		`{"type":"session_meta","payload":{"id":"thread-1","cwd":"/work/app"}}`+"\n")

	opt := allowAll(srv, tervaHome, t.TempDir(), "/work/app")
	opt.ClaudeHome = claudeHome
	opt.CodexHome = codexHome
	res, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Manifests != 2 {
		t.Fatalf("manifests %d, want claude good and codex: %+v", res.Manifests, res)
	}
	joined := strings.Join(res.Skipped, "\n")
	if len(res.Skipped) != 2 || !strings.Contains(joined, "terva: ") || !strings.Contains(joined, "lost.jsonl") {
		t.Fatalf("skipped:\n%s", joined)
	}
	if _, ok, err := ReadLastSync(opt.StateDir); err != nil || !ok {
		t.Fatalf("a run with skips is still a finished run: ok=%v err=%v", ok, err)
	}
}

// A file that cannot be read after its digest was taken skips that
// session only.
func TestPrepareSkipsAnUnreadableFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "-work-app")
	mustFile(t, filepath.Join(dir, "a.jsonl"), `{"type":"user","sessionId":"a","cwd":"/work/app"}`+"\n")
	mustFile(t, filepath.Join(dir, "b.jsonl"), `{"type":"user","sessionId":"b","cwd":"/work/app"}`+"\n")
	bundle, err := claude.Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	// a.jsonl is now a directory: the read fails the way a removed or
	// unreadable file does, whoever runs the test.
	a := filepath.Join(dir, "a.jsonl")
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	wm, q := openQueues(t, state)
	opt := Options{
		MachineID: "machine-1",
		StateDir:  state,
		Projects:  config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}
	work, res, err := prepare(context.Background(), opt, wm, q, []adapter.Bundle{bundle})
	if err != nil {
		t.Fatal(err)
	}
	if len(work) != 1 || work[0].manifest.NativeSessionID != "b" {
		t.Fatalf("work %+v", work)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "projects/-work-app/a.jsonl") {
		t.Fatalf("skipped %v", res.Skipped)
	}
}

//go:build golive

package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/testharness"
)

func TestGoLive20KSync(t *testing.T) {
	f := newGoLiveFixture(t)
	lake, err := api.Open(filepath.Join(f.root, "lake"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	var manifests, puts atomic.Int64
	h := lake.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/manifests" {
			manifests.Add(1)
		}
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	for id, plant := range map[string]func(string, string, []testharness.SessionSpec) (testharness.PlantResult, error){"terva": testharness.PlantTerva, "claude": testharness.PlantClaude, "codex": testharness.PlantCodex, "opencode": testharness.PlantOpenCode} {
		specs := make([]testharness.SessionSpec, 5000)
		for i := range specs {
			specs[i].ID = fmt.Sprintf("scale-%s-%05d", id, i)
		}
		if _, err := plant(f.homes[id], filepath.Join(f.root, "allowed"), specs); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	out, errs, err := f.run("sync", "--server", srv.URL)
	first := time.Since(start)
	if err != nil {
		t.Fatalf("first sync: %v %s", err, errs)
	}
	counts, err := lake.Catalog.Counts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if counts.Sessions != 20000 || counts.Artifacts != 20000 || manifests.Load() != 20000 {
		t.Fatalf("first counts: %+v manifests=%d output=%s", counts, manifests.Load(), out)
	}
	t.Logf("first sync: %s; counts=%+v; manifests=%d puts=%d", first, counts, manifests.Load(), puts.Load())
	manifests.Store(0)
	puts.Store(0)
	start = time.Now()
	out, errs, err = f.run("sync", "--server", srv.URL)
	second := time.Since(start)
	if err != nil || manifests.Load() != 0 || puts.Load() != 0 || !strings.Contains(out, "unchanged 20000") {
		t.Fatalf("second sync: %v %s %s manifests=%d puts=%d", err, out, errs, manifests.Load(), puts.Load())
	}
	if second > 15*time.Second {
		t.Fatalf("unchanged sync took %s; expected seconds", second)
	}
	t.Logf("unchanged sync: %s; zero manifests and blob PUTs; %s", second, strings.TrimSpace(out))
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestGoLiveServeProcess(t *testing.T) {
	data := os.Getenv("LAMPI_GOLIVE_PROCESS_DATA")
	if data == "" {
		t.Skip("subprocess helper")
	}
	err := Run([]string{"serve", "--data", data, "--addr", "127.0.0.1:0"}, Env{Stdout: os.Stdout, Stderr: os.Stderr, Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
}

func goLiveServe(t *testing.T, data string) (string, *exec.Cmd) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestGoLiveServeProcess$", "-test.timeout=20m")
	cmd.Env = []string{"LAMPI_GOLIVE_PROCESS_DATA=" + data}
	pipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	ready := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(pipe)
		for scan.Scan() {
			if addr, ok := strings.CutPrefix(scan.Text(), "terva-lampi serve: listening on "); ok {
				ready <- "http://" + addr
			}
		}
		close(ready)
	}()
	select {
	case url := <-ready:
		if url == "" {
			t.Fatal("serve exited before listening")
		}
		return url, cmd
	case <-time.After(20 * time.Second):
		t.Fatal("serve startup timeout")
		return "", nil
	}
}

func TestGoLiveRestore(t *testing.T) {
	f := newGoLiveFixture(t)
	source := filepath.Join(f.root, "source")
	url, _ := goLiveServe(t, source)
	for id, plant := range map[string]func(string, string, []testharness.SessionSpec) (testharness.PlantResult, error){"terva": testharness.PlantTerva, "claude": testharness.PlantClaude, "codex": testharness.PlantCodex, "opencode": testharness.PlantOpenCode} {
		if _, err := plant(f.homes[id], filepath.Join(f.root, "allowed"), []testharness.SessionSpec{{ID: "restore-" + id}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := f.run("sync", "--server", url); err != nil {
		t.Fatal(err)
	}
	// Export beside serve reports pending derived files. Wait for all four.
	var original string
	deadline := time.Now().Add(20 * time.Second)
	for {
		out, errs, err := f.run("export", "--data", source)
		if err != nil {
			t.Fatal(err)
		}
		if errs == "" && out != "" {
			original = out
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("normalization did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	backup := filepath.Join(f.root, "backup")
	if _, _, err := f.run("serve", "backup", "--data", source, "--out", backup); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(f.root, "restored")
	if err := os.CopyFS(restored, os.DirFS(backup)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(restored, "normalized")); !os.IsNotExist(err) {
		t.Fatal("backup unexpectedly contains derived data")
	}
	if _, _, err := f.run("serve", "fsck", "--data", restored); err != nil {
		t.Fatal(err)
	}
	rebuilt, errs, err := f.run("export", "--data", restored)
	if err != nil || errs != "" {
		t.Fatalf("rebuild: %v %s", err, errs)
	}
	assertRestoredEvents(t, original, rebuilt)

	restoredURL, _ := goLiveServe(t, restored)
	for _, endpoint := range []string{url, restoredURL} {
		out, errs, err := f.run("status", "--server", endpoint)
		if err != nil || errs != "" || !strings.Contains(out, "health: ok") || !strings.Contains(out, "catalog_sessions: 4\ncatalog_artifacts: 4\ncatalog_machines: 1") {
			t.Fatalf("status: %v %s %s", err, errs, out)
		}
	}
	after, errs, err := f.run("export", "--data", restored)
	if err != nil || errs != "" || after != rebuilt {
		t.Fatal("export changed after restored serve started")
	}
	t.Logf("live backup restored to fresh directory; fsck clean; both status reports 4 sessions/4 artifacts/1 machine; export content matches (%d source bytes; regenerated event IDs and projection timestamps)", len(original))
}

// Explicit roots and a closed environment keep these drills away from real
// harness homes, credentials, agent state and the workstation lake.
type goLiveFixture struct {
	root   string
	getenv func(string) string
	homes  map[string]string
}

func newGoLiveFixture(t *testing.T) *goLiveFixture {
	t.Helper()
	f := &goLiveFixture{root: t.TempDir(), homes: map[string]string{}}
	harnesses := map[string]any{}
	for _, id := range []string{"terva", "claude", "codex", "opencode", "cursor", "cursor-cli"} {
		f.homes[id] = filepath.Join(f.root, "homes", id)
		harnesses[id] = map[string]any{"root": f.homes[id], "enabled": id != "cursor" && id != "cursor-cli"}
	}
	cfg := filepath.Join(f.root, "config", "terva-lampi")
	if err := os.MkdirAll(cfg, 0700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(map[string]any{"harnesses": harnesses, "projects": map[string]any{"allow": []any{map[string]string{"cwd_prefix": filepath.Join(f.root, "allowed")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	f.getenv = func(k string) string {
		switch k {
		case "XDG_CONFIG_HOME":
			return filepath.Join(f.root, "config")
		case "XDG_STATE_HOME":
			return filepath.Join(f.root, "state")
		default:
			return ""
		}
	}
	return f
}

func (f *goLiveFixture) run(args ...string) (string, string, error) {
	var out, errs bytes.Buffer
	err := Run(args, Env{Stdout: &out, Stderr: &errs, Getenv: f.getenv})
	return out.String(), errs.String(), err
}

func TestGoLiveCanaries(t *testing.T) {
	f := newGoLiveFixture(t)
	data := filepath.Join(f.root, "lake")
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	// These are deliberately synthetic rule-shaped values, never credentials.
	canaries := []string{"AKIA" + "Z7Q4M2X9K3W8N5R1", "ghp_" + strings.Repeat("Z", 36), "AKIA" + "Z2X5QW7RT3LK9PMN"}
	allowed := filepath.Join(f.root, "allowed")
	remote := filepath.Join(allowed, "remote")
	for _, args := range [][]string{{"init", "--quiet", remote}, {"-C", remote, "remote", "add", "origin", "https://fake-user:" + canaries[1] + "@example.invalid/synthetic.git"}} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatal("synthetic git fixture:", err)
		}
	}
	for _, spec := range []struct{ id, cwd, prompt string }{
		{"escaped", allowed, canaries[0]},
		{"remote", remote, "clean remote control"},
		{"denied", filepath.Join(f.root, "denied"), canaries[2]},
		{"clean", allowed, "clean transcript control"},
	} {
		res, err := testharness.PlantTerva(f.homes["terva"], spec.cwd, []testharness.SessionSpec{{ID: spec.id, Prompt: spec.prompt}})
		if err != nil {
			t.Fatal(err)
		}
		if spec.id == "escaped" {
			b, err := os.ReadFile(res.Files[0])
			if err != nil {
				t.Fatal(err)
			}
			b = bytes.ReplaceAll(b, []byte(canaries[0]), []byte(`\u0041KIA`+canaries[0][4:]))
			if err := os.WriteFile(res.Files[0], b, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, stderr, err := f.run("sync", "--server", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "quarantin") || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("expected quarantine and allowlist refusals; err=%v stderr=%s", err, stderr)
	}
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	sessions, err := lake.Catalog.ListSessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected clean and sanitized-remote sessions, got %d", len(sessions))
	}
	for _, s := range sessions {
		if s.NativeID != "clean" && s.NativeID != "remote" {
			t.Fatal("refused session reached catalog")
		}
	}
	counts := map[string]int{}
	err = filepath.WalkDir(data, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(data, path)
		if err != nil {
			return err
		}
		kind := ""
		switch {
		case strings.HasPrefix(rel, "cas"+string(filepath.Separator)):
			kind = "cas"
		case strings.HasPrefix(rel, "catalog.db"):
			kind = "catalog"
		case strings.HasPrefix(rel, "normalized"+string(filepath.Separator)):
			kind = "normalized"
		}
		if kind == "" {
			return nil
		}
		counts[kind]++
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, canary := range append(canaries, `\u0041KIA`+canaries[0][4:]) {
			if bytes.Contains(b, []byte(canary)) {
				return fmt.Errorf("canary reached %s", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"cas", "catalog", "normalized"} {
		if counts[kind] == 0 {
			t.Fatalf("no %s evidence scanned", kind)
		}
	}
	t.Logf("2 accepted controls; escaped canary quarantined; denied project refused; scanned files: %v", counts)
}

// Projection creates event IDs and ingestion timestamps anew. Preserve every
// other field, including recorded timestamps supplied by the original harness.
func assertRestoredEvents(t *testing.T, original, rebuilt string) {
	t.Helper()
	canonical := func(raw string) []byte {
		events, err := decodeEvents([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 0 {
			t.Fatal("empty export")
		}
		for i := range events {
			ev := &events[i]
			if ev.EventID == "" {
				t.Fatal("missing event id")
			}
			if _, err := time.Parse(time.RFC3339Nano, ev.IngestedAt); err != nil {
				t.Fatal(err)
			}
			if ev.RecordedAt == ev.IngestedAt {
				ev.RecordedAt = "projection-time-fallback"
			}
			ev.EventID = ""
			ev.IngestedAt = ""
		}
		b, err := json.Marshal(events)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if !bytes.Equal(canonical(original), canonical(rebuilt)) {
		t.Fatal("restored event content differs")
	}
}

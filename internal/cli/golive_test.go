//go:build golive

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/testharness"
)

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

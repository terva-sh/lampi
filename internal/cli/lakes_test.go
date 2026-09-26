package cli

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/auth"
)

// twoLakeFixture is a machine whose config.json names a legacy default
// lake and a second lake, work, each an in-process lake with its own
// token. One terva session sits in /work/app, which only work allows.
type twoLakeFixture struct {
	env        Env
	stdout     *bytes.Buffer
	stderr     *bytes.Buffer
	home, work *api.Server
	cfg        string
}

func newTwoLakeFixture(t *testing.T) *twoLakeFixture {
	t.Helper()
	open := func(tok string) (*api.Server, string) {
		lake, err := api.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { lake.Close() })
		lake.Allow(tok)
		srv := httptest.NewServer(lake.Handler())
		t.Cleanup(srv.Close)
		return lake, srv.URL
	}
	home, homeURL := open("home-token")
	work, workURL := open("work-token")

	terva := t.TempDir()
	cfg := t.TempDir()
	state := t.TempDir()
	dir := filepath.Join(terva, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"sess-1\",\"cwd\":\"/work/app\"}}\n")
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	homeTok := filepath.Join(cfg, "home.token")
	workTok := filepath.Join(cfg, "work.token")
	if err := auth.Write(homeTok, "home-token"); err != nil {
		t.Fatal(err)
	}
	if err := auth.Write(workTok, "work-token"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	conf := fmt.Sprintf(`{
  "server": %q,
  "token_file": %q,
  "projects": {"allow": [{"cwd_prefix": "/home/app"}]},
  "lakes": {
    "work": {
      "server": %q,
      "token_file": %q,
      "projects": {"allow": [{"cwd_prefix": "/work/app"}]}
    }
  }
}`, homeURL, homeTok, workURL, workTok)
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &twoLakeFixture{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, home: home, work: work, cfg: cfg}
	f.env = Env{Stdout: f.stdout, Stderr: f.stderr, Getenv: statusEnv(cfg, terva, state)}
	return f
}

func (f *twoLakeFixture) run(args ...string) error {
	f.stdout.Reset()
	f.stderr.Reset()
	return Run(args, f.env)
}

func TestSyncPushesOnlyToTheDefaultLakeUntilStateIsPerLake(t *testing.T) {
	f := newTwoLakeFixture(t)
	// The session is allowed for work only, so the default lake refuses
	// it, and the default lake's own allow does not leak to work.
	err := f.run("sync")
	if err == nil || !strings.Contains(f.stdout.String()+f.stderr.String(), "refused") {
		t.Fatalf("sync to default: %v\nstdout:\n%s\nstderr:\n%s", err, f.stdout, f.stderr)
	}
	if !strings.Contains(f.stderr.String(), "2 lakes configured; this release pushes to default only") {
		t.Fatalf("no warning about the other lake:\n%s", f.stderr)
	}
	n, err := f.home.Catalog.Counts(t.Context())
	if err != nil || n.Sessions != 0 {
		t.Fatalf("default lake got %d sessions (%v)", n.Sessions, err)
	}
	err = f.run("sync", "--lake", "work")
	if err == nil || !strings.Contains(err.Error(), perLakeStateTicket) {
		t.Fatalf("sync --lake work: %v", err)
	}
	if n, _ := f.work.Catalog.Counts(t.Context()); n.Sessions != 0 {
		t.Fatal("work lake got a session before it has its own state")
	}
}

func TestStatusAndConflictsSelectALake(t *testing.T) {
	f := newTwoLakeFixture(t)
	if err := f.run("status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.stdout.String(), "lake: default\nother_lakes: 1 (pass --lake to show one)\n") {
		t.Fatalf("status:\n%s", f.stdout)
	}
	if err := f.run("status", "--lake", "work"); err != nil {
		t.Fatal(err)
	}
	out := f.stdout.String()
	if !strings.Contains(out, "lake: work\n") || strings.Contains(out, "other_lakes") || !strings.Contains(out, "source=config") {
		t.Fatalf("status --lake work:\n%s", out)
	}
	if !strings.Contains(out, "catalog_sessions: 0") {
		t.Fatalf("status --lake work did not reach the work lake with its token:\n%s", out)
	}
	if err := f.run("status", "--lake", "nope"); err == nil || !strings.Contains(err.Error(), "configured: default, work") {
		t.Fatalf("unknown lake: %v", err)
	}
	if err := f.run("conflicts", "--lake", "work"); err != nil {
		t.Fatalf("conflicts --lake work: %v\n%s", err, f.stderr)
	}
	// --server alone is ambiguous with two lakes: whose token?
	if err := f.run("conflicts", "--server", "http://127.0.0.1:1"); err == nil || !strings.Contains(err.Error(), "pass --lake") {
		t.Fatalf("conflicts --server with two lakes: %v", err)
	}
	if err := f.run("conflicts", "--lake", "work", "--data", t.TempDir()); err == nil {
		t.Fatal("conflicts took both --lake and --data")
	}
}

func TestAgentConfigListsEveryLake(t *testing.T) {
	f := newTwoLakeFixture(t)
	if err := f.run("agent", "config"); err != nil {
		t.Fatal(err)
	}
	out := f.stdout.String()
	for _, want := range []string{
		"lake default server=http://127.0.0.1:",
		"source=config token_file=" + filepath.Join(f.cfg, "home.token") + " token_source=config lake_id=- projects_allow=1 projects_deny=0",
		"lake work server=http://127.0.0.1:",
		"token_file=" + filepath.Join(f.cfg, "work.token"),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("agent config missing %q:\n%s", want, out)
		}
	}
}

func TestAgentRefusesAConfigWithNoDefaultLakeYet(t *testing.T) {
	cfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	conf := `{"lakes":{"work":{"server":"http://127.0.0.1:1"}}}`
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	env := Env{Stdout: ioDiscard(), Stderr: ioDiscard(), Getenv: statusEnv(cfg, t.TempDir(), t.TempDir())}
	if _, _, _, _, err := loadAgent(env, "", ""); err == nil || !strings.Contains(err.Error(), perLakeStateTicket) {
		t.Fatalf("agent with only a lakes map: %v", err)
	}
}

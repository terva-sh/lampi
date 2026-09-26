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
	"terva.sh/lampi/internal/config"
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

func TestSyncPushesToTheSelectedLakeWithItsOwnState(t *testing.T) {
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
	if n, err := f.home.Catalog.Counts(t.Context()); err != nil || n.Sessions != 0 {
		t.Fatalf("default lake got %d sessions (%v)", n.Sessions, err)
	}
	if err := f.run("sync", "--lake", "work"); err != nil {
		t.Fatalf("sync --lake work: %v\n%s", err, f.stderr)
	}
	if n, _ := f.work.Catalog.Counts(t.Context()); n.Sessions != 1 || n.Machines != 1 {
		t.Fatalf("work lake counts %+v", n)
	}
	// A second sync to work finds its own watermarks and sends nothing.
	if err := f.run("sync", "--lake", "work"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.stdout.String(), "uploaded 0") {
		t.Fatalf("second sync to work:\n%s", f.stdout)
	}
	state := f.env.Getenv("XDG_STATE_HOME")
	for _, name := range []string{"default", "work"} {
		if _, err := os.Stat(filepath.Join(state, "terva-lampi", "lakes", name, "last_attempt.json")); err != nil {
			t.Fatalf("lake %s has no state of its own: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(state, "terva-lampi", "watermarks.db")); !os.IsNotExist(err) {
		t.Fatal("a watermark store was written at the top of the state directory")
	}
	// Each lake has its own machine id for this machine.
	def, _ := config.EnsureLakeMachine(f.env.Getenv, "default")
	work, _ := config.EnsureLakeMachine(f.env.Getenv, "work")
	if def.MachineID == "" || def.MachineID == work.MachineID {
		t.Fatalf("machine ids default=%q work=%q", def.MachineID, work.MachineID)
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

func TestAgentUsesTheFirstLakeOfAMapOnlyConfig(t *testing.T) {
	cfg := t.TempDir()
	state := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	conf := `{"lakes":{"work":{"server":"http://127.0.0.1:1"},"archive":{"server":"http://127.0.0.1:2"}}}`
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	env := Env{Stdout: ioDiscard(), Stderr: &stderr, Getenv: statusEnv(cfg, t.TempDir(), state)}
	opt, _, _, _, err := loadAgent(env, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if opt.ServerURL != "http://127.0.0.1:2" || opt.LakeStateDir != filepath.Join(state, "terva-lampi", "lakes", "archive") {
		t.Fatalf("agent lake %s state %s", opt.ServerURL, opt.LakeStateDir)
	}
	if !strings.Contains(stderr.String(), "pushes to archive only") {
		t.Fatalf("no warning:\n%s", stderr.String())
	}
}

// legacyLayout moves a lake's state back to the top of the state
// directory, the layout a machine had before lakes/.
func legacyLayout(t *testing.T, state, lake string) {
	t.Helper()
	dir := filepath.Join(state, "lakes", lake)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Rename(filepath.Join(dir, e.Name()), filepath.Join(state, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(state, "lakes")); err != nil {
		t.Fatal(err)
	}
}

func TestSyncMigratesSingleLakeStateAndSendsNothingTwice(t *testing.T) {
	f := newTwoLakeFixture(t)
	// Make the session the default lake's, and sync it once.
	conf := filepath.Join(f.cfg, "terva-lampi", "config.json")
	raw, _ := os.ReadFile(conf)
	if err := os.WriteFile(conf, []byte(strings.Replace(string(raw), "/home/app", "/work/app", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.run("sync"); err != nil {
		t.Fatalf("first sync: %v\n%s", err, f.stderr)
	}
	state := filepath.Join(f.env.Getenv("XDG_STATE_HOME"), "terva-lampi")
	legacyLayout(t, state, "default")

	if err := f.run("status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.stdout.String(), "state: single-lake layout in ") || !strings.Contains(f.stdout.String(), "last_sync: ") {
		t.Fatalf("status before the move:\n%s", f.stdout)
	}
	if _, err := os.Stat(filepath.Join(state, "lakes")); !os.IsNotExist(err) {
		t.Fatal("status moved the state")
	}

	// An agent from before this release holds agent.pid: sync refuses to
	// move the files under it.
	release, err := writeAgentPID(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.run("sync"); err == nil || !strings.Contains(err.Error(), "stop that agent") {
		t.Fatalf("sync beside an old agent: %v", err)
	}
	release()

	if err := f.run("sync"); err != nil {
		t.Fatalf("sync after upgrade: %v\n%s", err, f.stderr)
	}
	if !strings.Contains(f.stderr.String(), "moved sync state to "+filepath.Join(state, "lakes", "default")) {
		t.Fatalf("no move reported:\n%s", f.stderr)
	}
	if !strings.Contains(f.stdout.String(), "uploaded 0") || !strings.Contains(f.stdout.String(), "manifests 0") {
		t.Fatalf("sync after the move sent again:\n%s", f.stdout)
	}
	if n, _ := f.home.Catalog.Counts(t.Context()); n.Sessions != 1 {
		t.Fatalf("default lake sessions %d", n.Sessions)
	}
}

// A crash between the rename and the cleanup leaves lakes/default and
// the legacy files both. The cleanup still waits for agent.pid, so an
// agent from before this release that started since keeps its files.
func TestMigrationCleanupWaitsForAnOldAgent(t *testing.T) {
	state := t.TempDir()
	if err := os.MkdirAll(filepath.Join(state, "lakes", "default"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(state, "last_sync.json")
	if err := os.WriteFile(legacy, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := writeAgentPID(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateDefault(Env{Stderr: ioDiscard()}, state, false); err == nil || !strings.Contains(err.Error(), "stop that agent") {
		t.Fatalf("cleanup beside an old agent: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("cleanup removed a file an agent holds")
	}
	release()
	if err := migrateDefault(Env{Stderr: ioDiscard()}, state, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("leftover legacy file kept")
	}
}

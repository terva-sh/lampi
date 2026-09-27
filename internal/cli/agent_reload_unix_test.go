//go:build unix

package cli

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
)

func TestAgentSIGHUPReloadsTheLakes(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home, cfg, state, _ := agentFixture(t, srv.URL)
	writeAgentConfig(t, cfg, standaloneConfig)
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runAgentLoop(ctx, Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}, "", "")
	}()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "\nwatching\n") })

	stateDir := filepath.Join(state, "terva-lampi")
	hup := func(raw string, want ...string) {
		t.Helper()
		writeAgentConfig(t, cfg, raw)
		before := len(buf.String())
		// reloadAgent is what register and lakes remove call.
		if line := reloadAgent(stateDir); !strings.Contains(line, "is reloading its lakes") {
			t.Fatalf("reloadAgent: %s", line)
		}
		waitOut(t, &buf, func(s string) bool {
			s = s[before:]
			for _, w := range want {
				if !strings.Contains(s, w) {
					return false
				}
			}
			return true
		})
	}
	work := func(allow string) string {
		return fmt.Sprintf(`{"lakes":{"work":{"server":%q,"projects":{"allow":[{"cwd_prefix":%q}]}}},"agent":{"debounce":"100ms"}}`, srv.URL, allow)
	}

	// A lake added to a standalone agent starts with a full pass.
	hup(work("/work/app"), "reload: added work", "checked 1, missing 1, uploaded 1")
	if n, _ := lake.Catalog.Counts(t.Context()); n.Sessions != 1 {
		t.Fatalf("sessions %d\n%s", n.Sessions, buf.String())
	}
	// The same settings keep the runner; new rules restart it.
	hup(work("/work/app"), "reload: unchanged work")
	hup(work("/work"), "reload: restarted work", "draining outbox", "\nchecked 0, missing 0")
	// A config that does not resolve leaves the lakes as they were.
	hup(`{"lakes":{"Bad":{"server":"x"}}}`, "the lakes are unchanged")
	// Removing the last lake drains it and says the agent is standalone.
	hup(standaloneConfig, "reload: removed work; no lake is configured")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("agent did not exit")
	}
	if strings.Count(buf.String(), "no lake is configured: watching") != 2 {
		t.Fatalf("agent output:\n%s", buf.String())
	}
}

func TestAReloadThatCannotPrepareALakeKeepsTheOldOneRunning(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home, cfg, state, _ := agentFixture(t, srv.URL)
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runAgentLoop(ctx, Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}, "", "")
	}()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "checked 1, missing 1, uploaded 1") })

	// A legacy file that cannot be moved or removed makes the default
	// lake's migration fail.
	stateDir := filepath.Join(state, "terva-lampi")
	legacy := filepath.Join(stateDir, "last_sync.json")
	if err := os.MkdirAll(filepath.Join(legacy, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	changed := fmt.Sprintf(`{"server":%q,"projects":{"allow":[{"cwd_prefix":"/work"}]},"agent":{"debounce":"100ms"}}`, srv.URL)
	writeAgentConfig(t, cfg, changed)
	before := len(buf.String())
	reloadAgent(stateDir)
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s[before:], "lake default: ") })
	if !strings.Contains(buf.String()[before:], "the lakes are unchanged") {
		t.Fatalf("agent output:\n%s", buf.String())
	}

	// The old runner is still there: once the migration can run, the
	// same config restarts it rather than adding it.
	if err := os.RemoveAll(legacy); err != nil {
		t.Fatal(err)
	}
	before = len(buf.String())
	reloadAgent(stateDir)
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s[before:], "reload: ") })
	if !strings.Contains(buf.String()[before:], "reload: restarted default") {
		t.Fatalf("agent output:\n%s", buf.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestShutdownWaitsForAReloadInProgress(t *testing.T) {
	home, cfg, state, _ := agentFixture(t, "http://127.0.0.1:9")
	stateDir := filepath.Join(state, "terva-lampi")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(stateDir, "last_sync.json")
	if err := os.WriteFile(legacy, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The reload stops in the middle of reading the config until the
	// test lets it go on.
	getenv := agentGetenv(home, cfg, state)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var buf memBuf
	env := Env{Stdout: &buf, Stderr: &buf, Getenv: func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			once.Do(func() {
				close(entered)
				<-release
			})
		}
		return getenv(k)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	set := &lakeSet{env: env, ctx: ctx, state: stateDir, runners: map[string]*lakeRunner{}}
	go set.reload()
	<-entered

	cancel()
	waited := make(chan struct{})
	go func() {
		set.wait()
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("wait returned while a reload was still running")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("wait did not return after the reload")
	}
	// Nothing started, and the single-lake state was not moved after
	// shutdown began.
	if len(set.snapshot()) != 0 || strings.Contains(buf.String(), "reload: ") {
		t.Fatalf("reload went on after shutdown:\n%s", buf.String())
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy state moved after shutdown: %v", err)
	}
}

func TestAReloadComparesMachineWideSettingsWithTheOnesTheAgentStartedWith(t *testing.T) {
	home, cfg, state, _ := agentFixture(t, "http://127.0.0.1:9")
	writeAgentConfig(t, cfg, standaloneConfig)
	var buf memBuf
	env := Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	set := &lakeSet{
		env: env, ctx: ctx, state: filepath.Join(state, "terva-lampi"), runners: map[string]*lakeRunner{},
		machine: machineFields(env),
	}
	const warning = "harnesses or debounce changed; restart the agent to apply them"
	reload := func(raw string, warns bool) {
		t.Helper()
		writeAgentConfig(t, cfg, raw)
		before := len(buf.String())
		if !set.reload() {
			t.Fatalf("reload failed:\n%s", buf.String())
		}
		if got := strings.Contains(buf.String()[before:], warning); got != warns {
			t.Fatalf("warned %v, want %v:\n%s", got, warns, buf.String()[before:])
		}
	}
	slower := `{"lakes":{},"agent":{"debounce":"200ms","debounce_max":"1s"}}`
	// A change still waiting for a restart is said at every reload, and
	// going back to what the agent runs with is not a change.
	reload(slower, true)
	reload(slower, true)
	reload(standaloneConfig, false)
}

func TestReloadAgentSignalsOnlyAHeldPIDFile(t *testing.T) {
	state := t.TempDir()
	if line := reloadAgent(state); !strings.Contains(line, "no agent is running") {
		t.Fatalf("no pid file: %s", line)
	}
	// A pid file nobody holds is left by a crash: its pid is not
	// signalled, whoever it names now.
	if err := os.WriteFile(filepath.Join(state, "agent.pid"), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if line := reloadAgent(state); !strings.Contains(line, "no agent is running") {
		t.Fatalf("stale pid file: %s", line)
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	defer signal.Stop(ch)
	release, err := writeAgentPID(state)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if line := reloadAgent(state); !strings.Contains(line, fmt.Sprintf("agent (pid %d) is reloading", os.Getpid())) {
		t.Fatalf("held pid file: %s", line)
	}
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no SIGHUP")
	}
}

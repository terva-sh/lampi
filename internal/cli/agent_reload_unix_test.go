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

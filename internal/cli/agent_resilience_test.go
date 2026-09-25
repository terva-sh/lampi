package cli

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
)

// No terva home at start is not a crash loop. The agent watches for the
// directory, and the first session written there is uploaded.
func TestAgentRunsWithNoTervaHome(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	_, cfg, state, _ := agentFixture(t, srv.URL)
	home := filepath.Join(t.TempDir(), "terva")
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runAgentLoop(ctx, Env{
			Stdout: &buf,
			Stderr: &buf,
			Getenv: agentGetenv(home, cfg, state),
		}, "", "")
	}()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "\nwatching\n") })

	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\"type\":\"meta\",\"meta\":{\"id\":\"late\",\"cwd\":\"/work/app\"}}\n"
	if err := os.WriteFile(filepath.Join(dir, "late.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "uploaded 1") })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("agent: %v\n%s", err, buf.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("agent did not exit")
	}
}

// A second agent on the same state directory stops and names the first.
// Once the first releases the file, the next one starts.
func TestAgentPIDRefusesASecondAgent(t *testing.T) {
	state := t.TempDir()
	release, err := writeAgentPID(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeAgentPID(state); err == nil || !strings.Contains(err.Error(), "another terva-lampi agent is running") {
		release()
		t.Fatalf("second agent: %v", err)
	}
	release()
	again, err := writeAgentPID(state)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

// A file left by an agent that died is not a lock. The next agent takes
// it and writes its own pid.
func TestAgentPIDReplacesAStaleFile(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, "agent.pid")
	if err := os.WriteFile(path, []byte("999999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := writeAgentPID(state)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("agent.pid %q", raw)
	}
}

//go:build unix

package cli

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
)

func TestAgentSIGUSR1SyncsAgain(t *testing.T) {
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
		done <- runAgentLoop(ctx, Env{
			Stdout: &buf,
			Stderr: &buf,
			Getenv: agentGetenv(home, cfg, state),
		}, "", "")
	}()

	waitOut(t, &buf, func(s string) bool {
		return strings.Contains(s, "\nwatching\n") && strings.Contains(s, "uploaded 1")
	})
	pidPath := filepath.Join(state, "terva-lampi", "agent.pid")
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Fatalf("agent.pid %d, process %d", pid, os.Getpid())
	}
	if strings.Count(buf.String(), "\nchecked ") != 1 {
		t.Fatalf("syncs before signal:\n%s", buf.String())
	}
	if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	waitOut(t, &buf, func(s string) bool {
		return strings.Count(s, "\nchecked ") >= 2
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("agent did not exit")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file left behind: %v", err)
	}
}

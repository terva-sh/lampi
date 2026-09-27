package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// standaloneConfig is a config.json with an empty lakes map: no lake.
const standaloneConfig = `{"lakes":{},"agent":{"debounce":"100ms","debounce_max":"1s"}}` + "\n"

func writeAgentConfig(t *testing.T, cfg, raw string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAgentWithNoLakeWatchesAndUploadsNothing(t *testing.T) {
	home, cfg, state, session := agentFixture(t, "http://127.0.0.1:9")
	writeAgentConfig(t, cfg, standaloneConfig)
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runAgentLoop(ctx, Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}, "", "")
	}()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "\nwatching\n") })
	f, err := os.OpenFile(session, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"type\":\"message\"}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "watch: ") })
	// Past the debounce ceiling: growth had no lake to wake.
	time.Sleep(1500 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "no lake is configured") != 1 {
		t.Fatalf("standalone line not said once:\n%s", out)
	}
	if strings.Contains(out, "checked ") || strings.Contains(out, "draining") {
		t.Fatalf("standalone agent pushed:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(state, "terva-lampi", "lakes")); !os.IsNotExist(err) {
		t.Fatalf("standalone agent made lake state: %v", err)
	}
}

func TestSyncAndStatusWithNoLake(t *testing.T) {
	home, cfg, state, _ := agentFixture(t, "http://127.0.0.1:9")
	writeAgentConfig(t, cfg, standaloneConfig)
	var out, errb memBuf
	env := Env{Stdout: &out, Stderr: &errb, Getenv: agentGetenv(home, cfg, state)}
	if err := runSync(env, nil); err == nil || !strings.Contains(err.Error(), "empty lakes map") {
		t.Fatalf("sync: %v", err)
	}
	if err := runStatus(env, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "lakes: none configured") || !strings.Contains(out.String(), "sessions: 1") {
		t.Fatalf("status:\n%s", out.String())
	}
}

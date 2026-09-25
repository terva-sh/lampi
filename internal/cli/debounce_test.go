package cli

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/config"
)

// A burst of events is one fire after the quiet window. Events that
// never pause still fire by the longest wait.
func TestDebouncerQuietAndLongest(t *testing.T) {
	var fired atomic.Int32
	d := newDebouncer(150*time.Millisecond, 600*time.Millisecond, func() { fired.Add(1) })
	defer d.stop()
	for range 5 {
		d.touch()
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond)
	if n := fired.Load(); n != 1 {
		t.Fatalf("burst fired %d times, want 1", n)
	}

	fired.Store(0)
	start := time.Now()
	for time.Since(start) < 1100*time.Millisecond {
		d.touch()
		time.Sleep(50 * time.Millisecond)
	}
	if n := fired.Load(); n < 1 {
		t.Fatalf("writes without a pause fired %d times in 1.1s, want at least 1 with a 600ms longest wait", n)
	}

	fired.Store(0)
	zero := newDebouncer(0, 0, func() { fired.Add(1) })
	zero.touch()
	zero.touch()
	if n := fired.Load(); n != 2 {
		t.Fatalf("a zero window fired %d times, want every event", n)
	}
}

func TestAgentWindowsFromConfig(t *testing.T) {
	w, l, err := config.AgentConfig{}.Windows()
	if err != nil || w != config.DefaultDebounce || l != config.DefaultDebounceMax {
		t.Fatalf("default %s %s %v", w, l, err)
	}
	w, l, err = config.AgentConfig{Debounce: "0s"}.Windows()
	if err != nil || w != 0 || l != config.DefaultDebounceMax {
		t.Fatalf("zero %s %s %v", w, l, err)
	}
	w, l, err = config.AgentConfig{Debounce: "10s", DebounceMax: "2s"}.Windows()
	if err != nil || w != 10*time.Second || l != 10*time.Second {
		t.Fatalf("longest below the window %s %s %v", w, l, err)
	}
	for _, bad := range []config.AgentConfig{{Debounce: "soon"}, {DebounceMax: "-1s"}} {
		if _, _, err := bad.Windows(); err == nil {
			t.Fatalf("%+v accepted", bad)
		}
	}
}

// The agent turns a burst of appends into one sync after the watch is
// quiet. The start pass does not wait.
func TestAgentDebouncesABurstOfAppends(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home, cfg, state, session := agentFixture(t, srv.URL)
	raw := `{"server":"` + srv.URL + `","projects":{"allow":[{"cwd_prefix":"/work/app"}]},"agent":{"debounce":"700ms","debounce_max":"5s"}}`
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runAgentLoop(ctx, Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}, "", "")
	}()
	waitOut(t, &buf, func(s string) bool {
		return strings.Contains(s, "\nwatching\n") && strings.Contains(s, "uploaded 1")
	})
	for i := range 5 {
		f, err := os.OpenFile(session, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("{\"type\":\"message\",\"n\":" + string(rune('0'+i)) + "}\n"); err != nil {
			t.Fatal(err)
		}
		f.Close()
		// Longer than the watcher's own 100ms per-path quiet, so each
		// append is its own event; shorter than the agent's window.
		time.Sleep(250 * time.Millisecond)
	}
	waitOut(t, &buf, func(s string) bool {
		return strings.Count(s, "\nchecked ") >= 2
	})
	time.Sleep(1500 * time.Millisecond)
	text := buf.String()
	if n := strings.Count(text, "\nchecked "); n != 2 {
		t.Fatalf("syncs %d, want the start pass and one for the burst:\n%s", n, text)
	}
	if strings.Count(text, "watch: append ") < 3 {
		t.Fatalf("the burst was not watched as separate appends:\n%s", text)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("agent did not exit")
	}
}

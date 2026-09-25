package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"terva.sh/lampi/internal/auth"
)

func TestBackoffGrowsCapsAndResets(t *testing.T) {
	high := &backoff{base: 2 * time.Second, max: 5 * time.Minute, rand: func(n int64) int64 { return n - 1 }}
	var got []time.Duration
	for i := 0; i < 10; i++ {
		got = append(got, high.next())
	}
	want := []time.Duration{
		2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
		32 * time.Second, 64 * time.Second, 128 * time.Second, 256 * time.Second,
		5 * time.Minute, 5 * time.Minute,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("waits %v, want %v", got, want)
		}
	}
	high.reset()
	if d := high.next(); d != 2*time.Second {
		t.Fatalf("after reset %s", d)
	}

	// Full jitter: the low end stays at the base however many failures.
	low := &backoff{base: 2 * time.Second, max: 5 * time.Minute, rand: func(int64) int64 { return 0 }}
	for i := 0; i < 50; i++ {
		if d := low.next(); d != 2*time.Second {
			t.Fatalf("failure %d waited %s", i, d)
		}
	}
	if d := low.capped(); d != 5*time.Minute {
		t.Fatalf("capped %s", d)
	}

	// The real source stays inside the window.
	live := newBackoff()
	for i := 0; i < 40; i++ {
		d := live.next()
		if d < syncRetryAfter || d > syncRetryCap {
			t.Fatalf("failure %d waited %s", i, d)
		}
	}
}

func TestAgentLogsUnauthorizedOnceAndWaitsTheCap(t *testing.T) {
	var mu sync.Mutex
	var hellos []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/hello" {
			mu.Lock()
			hellos = append(hellos, time.Now())
			mu.Unlock()
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	const wait = 150 * time.Millisecond
	prev := agentBackoff
	agentBackoff = func() *backoff {
		return &backoff{base: time.Millisecond, max: wait, rand: func(int64) int64 { return 0 }}
	}
	t.Cleanup(func() { agentBackoff = prev })

	home, cfg, state, _ := agentFixture(t, srv.URL)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("wrong-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runAgentLoop(ctx, Env{
			Stdout: &stdout,
			Stderr: &stderr,
			Getenv: agentGetenv(home, cfg, state),
		}, "", tokenFile)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		mu.Lock()
		n := len(hellos)
		mu.Unlock()
		if n >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hellos %d\n%s", n, stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
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

	text := stderr.String()
	if n := strings.Count(text, "Retrying every"); n != 1 {
		t.Fatalf("401 logged %d times\n%s", n, text)
	}
	if !strings.Contains(text, "401 Unauthorized to the device token in "+tokenFile) {
		t.Fatalf("log does not name the token file\n%s", text)
	}
	mu.Lock()
	defer mu.Unlock()
	// The first retry waits the cap, not the base.
	for i := 1; i < 3; i++ {
		if gap := hellos[i].Sub(hellos[i-1]); gap < wait-20*time.Millisecond {
			t.Fatalf("retry %d after %s, want the cap %s", i, gap, wait)
		}
	}
}

func TestAgentRefusesTokenOverPlainHTTP(t *testing.T) {
	home, cfg, state, _ := agentFixture(t, "http://127.0.0.1:1")
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := Env{Stdout: ioDiscard(), Stderr: ioDiscard(), Getenv: agentGetenv(home, cfg, state)}
	_, _, _, _, err := loadAgent(env, "http://lake.example:8787", tokenFile)
	if err == nil || !strings.Contains(err.Error(), "refusing to send the device token to lake.example:8787 over plain http") {
		t.Fatalf("err %v", err)
	}
	for _, ok := range []string{"http://127.0.0.1:8787", "http://localhost:8787", "http://[::1]:8787", "https://lake.example"} {
		if _, _, _, _, err := loadAgent(env, ok, tokenFile); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
}

func TestStatusAndConflictsRefuseTokenOverPlainHTTP(t *testing.T) {
	line := probeCatalog("http://lake.example:8787", "tok")
	if !strings.HasPrefix(line, "catalog: refusing to send the device token") {
		t.Fatalf("status %q", line)
	}
	if _, err := fetchConflicts("http://10.1.2.3:8787", "tok"); err == nil || !strings.Contains(err.Error(), "over plain http") {
		t.Fatalf("conflicts %v", err)
	}

	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"sessions":1,"artifacts":2,"machines":3}`))
	}))
	t.Cleanup(srv.Close)
	if line := probeCatalog(srv.URL, "tok"); !strings.Contains(line, "catalog_sessions: 1") || sent != "Bearer tok" {
		t.Fatalf("loopback %q auth %q", line, sent)
	}
}

func TestServeWarnsTokenOnNonLoopbackBind(t *testing.T) {
	devices := &auth.Devices{}
	devices.Allow("tok")
	if w := plaintextTokenWarning("0.0.0.0:8787", devices); !strings.Contains(w, "put TLS in front") {
		t.Fatalf("0.0.0.0: %q", w)
	}
	if w := plaintextTokenWarning(":8787", devices); w == "" {
		t.Fatal("empty host: no warning")
	}
	if w := plaintextTokenWarning("127.0.0.1:8787", devices); w != "" {
		t.Fatalf("loopback: %q", w)
	}
	if w := plaintextTokenWarning("0.0.0.0:8787", nil); w != "" {
		t.Fatalf("no tokens: %q", w)
	}
}

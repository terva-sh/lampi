package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
)

func TestAgentSIGTERMDrainsOutbox(t *testing.T) {
	if os.Getenv("LAMPI_AGENT_HELPER") == "1" {
		err := Run([]string{"agent"}, Env{
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Getenv: os.Getenv,
			Argv0:  "terva-lampi",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	held := &holdHello{next: lake.Handler(), held: make(chan struct{}), stop: make(chan struct{})}
	srv := httptest.NewServer(held)
	// Release runs before Close. LIFO: this cleanup is registered before
	// the process kill, so the kill runs first and this runs next.
	t.Cleanup(func() {
		releaseHold(held)
		srv.CloseClientConnections()
		srv.Close()
	})

	home, cfg, state, _ := agentFixture(t, srv.URL)
	outFile, err := os.CreateTemp(t.TempDir(), "agent-out")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAgentSIGTERMDrainsOutbox$", "-test.timeout=60s")
	cmd.Env = replaceEnv(map[string]string{
		"LAMPI_AGENT_HELPER": "1",
		"TERVA_HOME":         home,
		"XDG_CONFIG_HOME":    cfg,
		"XDG_STATE_HOME":     state,
		"XDG_DATA_HOME":      t.TempDir(),
		"HOME":               t.TempDir(),
	})
	cmd.Stdout = outFile
	cmd.Stderr = outFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	select {
	case <-held.held:
	case <-time.After(15 * time.Second):
		t.Fatal("agent did not reach the lake")
	}
	held.mu.Lock()
	if len(held.machines) != 0 {
		t.Fatalf("manifest landed before SIGTERM: %v", held.machines)
	}
	held.mu.Unlock()

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		out := readFile(t, outFile.Name())
		if err != nil {
			t.Fatalf("agent exit: %v\n%s", err, out)
		}
		assertDrained(t, cfg, state, out, held)
		// The first hello is still sitting in the handler. Closing the
		// test server will not cancel that request context, so release
		// the handler before Close waits on it.
		releaseHold(held)
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("agent did not exit after SIGTERM\n%s", readFile(t, outFile.Name()))
	}
}

func TestAgentRetriesFailedSyncWithoutGrowth(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	gate := &failFirstHello{next: lake.Handler()}
	srv := httptest.NewServer(gate)
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
		return strings.Contains(s, "uploaded 1")
	})
	gate.mu.Lock()
	n := len(gate.at)
	var gap time.Duration
	if n >= 2 {
		gap = gate.at[1].Sub(gate.at[0])
	}
	gate.mu.Unlock()
	if n < 2 {
		t.Fatalf("hellos %d\n%s", n, buf.String())
	}
	if gap < syncRetryAfter-500*time.Millisecond {
		t.Fatalf("retry gap %s, want at least %s\n%s", gap, syncRetryAfter, buf.String())
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

func TestAgentCancelSkipsFailedSyncRetry(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	var mu sync.Mutex
	var hellos int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/hello" {
			mu.Lock()
			hellos++
			mu.Unlock()
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		lake.Handler().ServeHTTP(w, r)
	}))
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
		return strings.Contains(s, "503")
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(syncRetryAfter / 2):
		t.Fatalf("cancel waited on the retry timer\n%s", buf.String())
	}
	text := buf.String()
	if strings.Count(text, "\nchecked ") != 1 {
		t.Fatalf("syncs before drain:\n%s", text)
	}
	if !strings.Contains(text, "drain: ") {
		t.Fatalf("no drain:\n%s", text)
	}
	mu.Lock()
	n := hellos
	mu.Unlock()
	// The failed push, then the shutdown drain. Not a third hello from the timer.
	if n != 2 {
		t.Fatalf("hellos %d\n%s", n, text)
	}
}

func TestAgentUploadsGrowthAndUsesMachineID(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	cap := &captureManifest{next: lake.Handler()}
	srv := httptest.NewServer(cap)
	t.Cleanup(srv.Close)

	home, cfg, state, session := agentFixture(t, srv.URL)
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
	id := readMachineID(t, cfg)
	if !strings.Contains(buf.String(), "machine_id: "+id) {
		t.Fatalf("banner machine_id:\n%s", buf.String())
	}

	f, err := os.OpenFile(session, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"type\":\"message\"}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	waitOut(t, &buf, func(s string) bool {
		return strings.Count(s, "uploaded 1") >= 2
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
	text := buf.String()
	if !strings.Contains(text, "drain: ") {
		t.Fatalf("no drain:\n%s", text)
	}
	if !strings.Contains(text, "watch: append ") {
		t.Fatalf("growth was not watched:\n%s", text)
	}
	assertOutboxEmpty(t, state)
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.machines) == 0 || cap.machines[0] != id {
		t.Fatalf("manifest machine_id %v, config %s", cap.machines, id)
	}
}

func assertDrained(t *testing.T, cfg, state, out string, held *holdHello) {
	t.Helper()
	id := readMachineID(t, cfg)
	if !strings.Contains(out, "machine_id: "+id) {
		t.Fatalf("stdout machine_id:\n%s", out)
	}
	if !strings.Contains(out, "drain: checked 1, missing 1, uploaded 1") {
		t.Fatalf("drain line:\n%s", out)
	}
	held.mu.Lock()
	machines := append([]string(nil), held.machines...)
	hellos := held.hellos
	held.mu.Unlock()
	if hellos < 2 {
		t.Fatalf("hellos %d, want the cancelled push and the drain", hellos)
	}
	if len(machines) != 1 || machines[0] != id {
		t.Fatalf("manifest machine_id %v, config %s", machines, id)
	}
	assertOutboxEmpty(t, state)
}

func assertOutboxEmpty(t *testing.T, state string) {
	t.Helper()
	q, err := outbox.Open(outbox.File(filepath.Join(state, "terva-lampi")))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	pending, err := q.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("outbox still has %d rows", len(pending))
	}
}

func agentFixture(t *testing.T, server string) (home, cfg, state, session string) {
	t.Helper()
	home = t.TempDir()
	cfg = t.TempDir()
	state = t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	session = filepath.Join(dir, "sess-1.jsonl")
	body := "{\"type\":\"meta\",\"meta\":{\"id\":\"sess-1\",\"cwd\":\"/work/app\"}}\n"
	if err := os.WriteFile(session, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A short debounce keeps these tests quick. The default is 5s.
	raw := fmt.Sprintf("{\"server\":%q,\"projects\":{\"allow\":[{\"cwd_prefix\":\"/work/app\"}]},\"agent\":{\"debounce\":\"100ms\",\"debounce_max\":\"1s\"}}\n", server)
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, cfg, state, session
}

func agentGetenv(home, cfg, state string) func(string) string {
	return func(k string) string {
		switch k {
		case "TERVA_HOME":
			return home
		case "XDG_CONFIG_HOME":
			return cfg
		case "XDG_STATE_HOME":
			return state
		default:
			return ""
		}
	}
}

func readMachineID(t *testing.T, cfg string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cfg, "terva-lampi", "machine.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m config.Machine
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.MachineID == "" {
		t.Fatal("machine.json has no machine_id")
	}
	return m.MachineID
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func replaceEnv(overrides map[string]string) []string {
	skip := make(map[string]bool, len(overrides))
	for k := range overrides {
		skip[k] = true
	}
	var out []string
	for _, e := range os.Environ() {
		k, _, ok := strings.Cut(e, "=")
		if !ok || skip[k] {
			continue
		}
		out = append(out, e)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func waitOut(t *testing.T, buf *memBuf, ok func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		last = buf.String()
		if ok(last) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout:\n%s", last)
}

type memBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (m *memBuf) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.b.Write(p)
}

func (m *memBuf) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.b.String()
}

// holdHello stalls the first hello until the client gives up or releaseHold
// closes stop. Later hellos reach the lake. An abandoned httptest client
// does not cancel the server request context, so Close waits until stop
// is closed.
type holdHello struct {
	next     http.Handler
	held     chan struct{}
	stop     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	hellos   int
	machines []string
}

func releaseHold(h *holdHello) {
	select {
	case <-h.stop:
	default:
		close(h.stop)
	}
}

func (h *holdHello) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/hello" {
		h.mu.Lock()
		h.hellos++
		n := h.hellos
		h.mu.Unlock()
		if n == 1 {
			h.once.Do(func() { close(h.held) })
			select {
			case <-r.Context().Done():
			case <-h.stop:
			}
			http.Error(w, "cancelled", http.StatusServiceUnavailable)
			return
		}
	}
	if r.URL.Path == "/v1/manifests" && r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var m protocol.Manifest
		if err := json.Unmarshal(body, &m); err == nil {
			h.mu.Lock()
			h.machines = append(h.machines, m.MachineID)
			h.mu.Unlock()
		}
	}
	h.next.ServeHTTP(w, r)
}

// failFirstHello answers the first hello with 503 and lets the rest through.
// at records when each hello arrived, so a test can see the retry wait.
type failFirstHello struct {
	next http.Handler
	mu   sync.Mutex
	at   []time.Time
}

func (f *failFirstHello) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/hello" {
		f.mu.Lock()
		f.at = append(f.at, time.Now())
		n := len(f.at)
		f.mu.Unlock()
		if n == 1 {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
	}
	f.next.ServeHTTP(w, r)
}

type captureManifest struct {
	next     http.Handler
	mu       sync.Mutex
	machines []string
}

func (c *captureManifest) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/manifests" && r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var m protocol.Manifest
		if err := json.Unmarshal(body, &m); err == nil {
			c.mu.Lock()
			c.machines = append(c.machines, m.MachineID)
			c.mu.Unlock()
		}
	}
	c.next.ServeHTTP(w, r)
}

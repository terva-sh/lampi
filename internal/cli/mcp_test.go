package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/testidp"
	"terva.sh/lampi/internal/web"
	"terva.sh/lampi/internal/webconfig"
)

// mcpLake serves a lake with a search index and one published session,
// and records the MCP headers of each request it gets.
func mcpLake(t *testing.T) (*api.Server, *httptest.Server, *[]http.Header) {
	t.Helper()
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	text := func(s string) *string { return &s }
	bash := "Bash"
	publishEvents(t, lake, "codex", "sid-a", "", []normalize.Event{
		{SchemaVersion: 1, Harness: "codex", Actor: "user", EventType: "message", RecordedAt: "2026-09-01T12:00:00Z", ContentText: text("push it")},
		{SchemaVersion: 1, Harness: "codex", Actor: "assistant", EventType: "tool_call", RecordedAt: "2026-09-01T12:00:01Z", Tool: normalize.Tool{Name: &bash}, ContentText: text("git push")},
	})
	idp := testidp.New()
	t.Cleanup(idp.Close)
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"owners": "admin"}}}
	reg := &web.Registrations{Lake: func() registrar.Lake { return registrar.Lake{Catalog: lake.Catalog, Dir: dir} }, Blobs: lake.CAS}
	reader := recall.NewReader(lake.Catalog, lake.Normalized)
	index, err := recall.OpenIndex(filepath.Join(t.TempDir(), recall.IndexFile), reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { index.Close() })
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if lake.Web, err = web.New(cfg, lake.Catalog, reader, index, reg, nil, idp.Client()); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := &[]http.Header{}
	h := lake.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*seen = append(*seen, r.Header.Clone())
		mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return lake, srv, seen
}

// runBridge runs terva-lampi mcp over the given lines and returns each
// line it wrote, by id.
func runBridge(t *testing.T, server, token string, lines ...string) (map[string]map[string]any, string) {
	t.Helper()
	var out, errw bytes.Buffer
	env := Env{Stdin: strings.NewReader(strings.Join(lines, "\n") + "\n"), Stdout: &out, Stderr: &errw, Getenv: func(k string) string {
		return map[string]string{"XDG_CONFIG_HOME": t.TempDir(), "XDG_STATE_HOME": t.TempDir()}[k]
	}}
	if err := Run([]string{"mcp", "--server", server, "--token-file", token}, env); err != nil {
		t.Fatalf("mcp: %v\n%s", err, errw.String())
	}
	replies := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stdout line is not JSON: %q", line)
		}
		id, _ := json.Marshal(m["id"])
		replies[string(id)] = m
	}
	return replies, errw.String()
}

func rpcLine(id int, method string, params any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return string(b)
}

// TKT-01M3FPWCFS: an agent that speaks MCP on stdio reaches the lake's
// tools through terva-lampi mcp, with the token from a file.
func TestMCPBridgeServesTheLakeTools(t *testing.T) {
	lake, srv, seen := mcpLake(t)
	token := tokenFile(t, lake, catalog.PermEventsRead)
	replies, stderr := runBridge(t, srv.URL, token,
		rpcLine(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		"",
		rpcLine(2, "tools/list", nil),
		rpcLine(3, "tools/call", map[string]any{"name": "search", "arguments": map[string]any{"q": "git push"}}),
		`{"jsonrpc":"2.0","id":4,"result":{}}`,
	)
	if len(replies) != 3 {
		t.Fatalf("replies %v", replies)
	}
	if result, _ := replies["1"]["result"].(map[string]any); result["protocolVersion"] != "2025-06-18" {
		t.Errorf("initialize: %v", replies["1"])
	}
	if result, _ := replies["2"]["result"].(map[string]any); len(result["tools"].([]any)) != 3 {
		t.Errorf("tools/list: %v", replies["2"])
	}
	result, _ := replies["3"]["result"].(map[string]any)
	content, _ := result["content"].([]any)
	// A result that is not an error may leave isError out.
	if result["isError"] == true || len(content) != 1 || !strings.Contains(content[0].(map[string]any)["text"].(string), `"snippet":"git push"`) {
		t.Errorf("search: %v", replies["3"])
	}
	// After initialize, every request names the negotiated version, and
	// every one carries the token, which nothing prints.
	for _, h := range *seen {
		if h.Get("Authorization") == "" || h.Get("Accept") != "application/json, text/event-stream" {
			t.Errorf("headers %v", h)
		}
	}
	versions := 0
	for _, h := range *seen {
		if h.Get("MCP-Protocol-Version") == "2025-06-18" {
			versions++
		}
	}
	if versions < 2 {
		t.Errorf("%d requests named the negotiated version", versions)
	}
	if strings.Contains(stderr, "synthetic-query-token") {
		t.Error("the token was printed")
	}
}

// A client that sends its version in every request gets the headers the
// lake checks against the body.
func TestMCPBridgeMirrorsPerRequestMetadata(t *testing.T) {
	lake, srv, seen := mcpLake(t)
	token := tokenFile(t, lake, catalog.PermEventsRead)
	meta := map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientInfo": map[string]any{"name": "t", "version": "1"}, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	replies, _ := runBridge(t, srv.URL, token,
		rpcLine(1, "server/discover", map[string]any{"_meta": meta}),
		rpcLine(2, "tools/call", map[string]any{"name": "read_events", "arguments": map[string]any{"session_uid": "no-such-session"}, "_meta": meta}),
	)
	for id, r := range replies {
		result, _ := r["result"].(map[string]any)
		if r["error"] != nil || result["resultType"] != "complete" {
			t.Errorf("reply %s: %v", id, r)
		}
	}
	result, _ := replies["2"]["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("a missing session is a tool error: %v", replies["2"])
	}
	names := map[string]string{}
	for _, h := range *seen {
		if h.Get("MCP-Protocol-Version") != "2026-07-28" {
			t.Errorf("version header %q", h.Get("MCP-Protocol-Version"))
		}
		names[h.Get("Mcp-Method")] = h.Get("Mcp-Name")
	}
	if len(names) != 2 || names["tools/call"] != "read_events" || names["server/discover"] != "" {
		t.Errorf("method and name headers %v", names)
	}
}

// A token the lake refuses becomes a JSON-RPC error that says what to do,
// and stdout carries nothing else.
func TestMCPBridgeExplainsRefusals(t *testing.T) {
	lake, srv, _ := mcpLake(t)
	raw := tokenFile(t, lake, catalog.PermRawRead)
	revoked := tokenFile(t, lake, catalog.PermEventsRead, catalog.PermRawRead)
	tokens, err := lake.Catalog.ReadTokens(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if len(tok.Permissions) == 2 {
			if _, err := lake.Catalog.RevokeReadToken(t.Context(), tok.ID, "test", time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, c := range map[string]struct{ token, want string }{
		"no events:read": {raw, "404"},
		"revoked":        {revoked, "401"},
	} {
		replies, _ := runBridge(t, srv.URL, c.token, rpcLine(1, "tools/list", nil), `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		e, _ := replies["1"]["error"].(map[string]any)
		if len(replies) != 1 || e == nil || !strings.Contains(e["message"].(string), c.want) || !strings.Contains(e["message"].(string), "token") {
			t.Errorf("%s: %v", name, replies)
		}
	}
	// A message the lake's server refuses in plain text gets that reason.
	events := tokenFile(t, lake, catalog.PermEventsRead)
	replies, _ := runBridge(t, srv.URL, events, `{"jsonrpc":"2.0","id":7,"method":"nope/nothing"}`)
	if e, _ := replies["7"]["error"].(map[string]any); e == nil || !strings.Contains(e["message"].(string), "the lake answered 400") || !strings.Contains(e["message"].(string), "nope/nothing") {
		t.Errorf("plain-text refusal: %v", replies)
	}
	if err := Run([]string{"mcp", "--server", "http://lake.example", "--token-file", raw}, Env{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "plain http") {
		t.Errorf("plain http to a remote lake: %v", err)
	}
	var out bytes.Buffer
	if err := Run([]string{"mcp", "--nope"}, Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &bytes.Buffer{}}); err == nil || out.Len() != 0 {
		t.Errorf("a bad flag wrote %q to stdout (err %v)", out.String(), err)
	}
}

func TestHeaderSafe(t *testing.T) {
	for in, want := range map[string]string{
		"search":          "search",
		" padded ":        "=?base64?IHBhZGRlZCA=?=",
		"Hello, 世界":       "=?base64?SGVsbG8sIOS4lueVjA==?=",
		"=?base64?abc?=":  "=?base64?PT9iYXNlNjQ/YWJjPz0=?=",
		"line1\nline2":    "=?base64?bGluZTEKbGluZTI=?=",
		"read_events-v.2": "read_events-v.2",
	} {
		if got := headerSafe(in); got != want {
			t.Errorf("headerSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

// failingWriter fails every write after the first ok ones, as stdout
// does once the agent has gone.
type failingWriter struct{ ok int }

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.ok > 0 {
		f.ok--
		return len(p), nil
	}
	return 0, errors.New("broken pipe")
}

// A write to the agent that fails ends the bridge with that error, rather
// than dropping answers while it goes on forwarding (review 2107).
func TestMCPBridgeStopsWhenTheAgentIsGone(t *testing.T) {
	lake, srv, seen := mcpLake(t)
	token := tokenFile(t, lake, catalog.PermEventsRead)
	lines := []string{rpcLine(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"})}
	for i := 2; i < 50; i++ {
		lines = append(lines, rpcLine(i, "ping", nil))
	}
	env := Env{Stdin: strings.NewReader(strings.Join(lines, "\n") + "\n"), Stdout: &failingWriter{}, Stderr: &bytes.Buffer{}}
	err := Run([]string{"mcp", "--server", srv.URL, "--token-file", token}, env)
	if err == nil || !strings.Contains(err.Error(), "writing to the agent") {
		t.Fatalf("mcp with a broken stdout: %v", err)
	}
	if len(*seen) > 2 {
		t.Errorf("the bridge sent %d requests after the agent was gone", len(*seen))
	}
}

// A failed write ends the bridge even when the agent keeps stdin open and
// sends nothing more: the answer to a concurrent request fails while the
// bridge waits for the next line (review 2111).
func TestMCPBridgeStopsWhileTheAgentIsSilent(t *testing.T) {
	lake, srv, _ := mcpLake(t)
	token := tokenFile(t, lake, catalog.PermEventsRead)
	stdin, feed := io.Pipe()
	t.Cleanup(func() { feed.Close() })
	env := Env{Stdin: stdin, Stdout: &failingWriter{ok: 1}, Stderr: &bytes.Buffer{}}
	done := make(chan error, 1)
	go func() { done <- Run([]string{"mcp", "--server", srv.URL, "--token-file", token}, env) }()
	// initialize is answered on the one write that works; the ping's
	// answer, written from its own goroutine, fails.
	if _, err := io.WriteString(feed, rpcLine(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"})+"\n"+rpcLine(2, "ping", nil)+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "writing to the agent") {
			t.Fatalf("mcp: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the bridge kept waiting for input after a write to the agent failed")
	}
}

// fakeMCPLake serves h as the lake, and returns its URL and a read token
// file the bridge accepts. h gets each request's id and method.
func fakeMCPLake(t *testing.T, h func(w http.ResponseWriter, r *http.Request, id json.RawMessage, method string)) (string, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		h(w, r, m.ID, m.Method)
	}))
	t.Cleanup(srv.Close)
	token := filepath.Join(t.TempDir(), "read-token")
	if err := os.WriteFile(token, []byte(web.ReadTokenPrefix+"fake\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return srv.URL, token
}

func answerMCP(w http.ResponseWriter, id json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{}}`, id)
}

// A notification has no answer to carry the lake's refusal, so the bridge
// logs it, rather than leaving a revoked token unseen until the next
// request (review 2115).
func TestMCPBridgeLogsARefusedNotification(t *testing.T) {
	lake, srv, _ := mcpLake(t)
	initialized := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	for _, tc := range []struct {
		name, perm, logged string
	}{
		{"accepted", catalog.PermEventsRead, ""},
		{"refused", catalog.PermRawRead, "terva-lampi mcp: notifications/initialized: the lake answered 404"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replies, stderr := runBridge(t, srv.URL, tokenFile(t, lake, tc.perm), initialized)
			if len(replies) != 0 {
				t.Errorf("the bridge answered a notification: %v", replies)
			}
			logged := strings.Contains(stderr, "notifications/initialized")
			if tc.logged == "" && logged {
				t.Errorf("an accepted notification was logged:\n%s", stderr)
			}
			if tc.logged != "" && !strings.Contains(stderr, tc.logged) {
				t.Errorf("stderr lacks %q:\n%s", tc.logged, stderr)
			}
		})
	}
}

// The bridge keeps at most mcpInFlight requests open to the lake, however
// many the agent sends at once, and answers each of them.
func TestMCPBridgeBoundsRequestsInFlight(t *testing.T) {
	var mu sync.Mutex
	inFlight, most := 0, 0
	release := make(chan struct{})
	server, token := fakeMCPLake(t, func(w http.ResponseWriter, r *http.Request, id json.RawMessage, _ string) {
		mu.Lock()
		inFlight++
		most = max(most, inFlight)
		mu.Unlock()
		select {
		case <-release:
		case <-r.Context().Done():
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		answerMCP(w, id)
	})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	const sent = 3 * mcpInFlight
	var lines []string
	for i := 1; i <= sent; i++ {
		lines = append(lines, rpcLine(i, "ping", nil))
	}
	// The lake holds every request until the bridge has filled its slots
	// and had time to open more than it should.
	observed := make(chan int, 1)
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := inFlight
			mu.Unlock()
			if n >= mcpInFlight {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		observed <- most
		mu.Unlock()
		once.Do(func() { close(release) })
	}()
	replies, stderr := runBridge(t, server, token, lines...)
	if n := <-observed; n != mcpInFlight {
		t.Errorf("the bridge had %d requests open to the lake at once, want %d", n, mcpInFlight)
	}
	if len(replies) != sent {
		t.Errorf("the bridge answered %d of %d requests:\n%s", len(replies), sent, stderr)
	}
}

// notifications/cancelled stops that request's call to the lake, and the
// bridge writes no answer for it. The notification goes no further.
func TestMCPBridgeCancelsARequest(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	started, cancelled, stop := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server, token := fakeMCPLake(t, func(w http.ResponseWriter, r *http.Request, id json.RawMessage, method string) {
		mu.Lock()
		methods = append(methods, method)
		mu.Unlock()
		if string(id) == "2" {
			close(started)
			select {
			case <-r.Context().Done():
				close(cancelled)
				return
			case <-stop:
			}
		}
		answerMCP(w, id)
	})
	t.Cleanup(func() { close(stop) })
	stdin, feed := io.Pipe()
	t.Cleanup(func() { feed.Close() })
	var out, errw bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- Run([]string{"mcp", "--server", server, "--token-file", token}, Env{Stdin: stdin, Stdout: &out, Stderr: &errw})
	}()
	send := func(line string) {
		t.Helper()
		if _, err := io.WriteString(feed, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(c chan struct{}, what string) {
		t.Helper()
		select {
		case <-c:
		case <-time.After(10 * time.Second):
			t.Fatal(what)
		}
	}
	send(rpcLine(2, "tools/call", map[string]any{"name": "search", "arguments": map[string]any{"query": "push"}}))
	wait(started, "the request never reached the lake")
	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2,"reason":"the user stopped it"}}`)
	wait(cancelled, "the bridge did not cancel the request to the lake")
	send(rpcLine(3, "ping", nil))
	feed.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("mcp: %v\n%s", err, errw.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the bridge did not stop at the end of its input")
	}
	if !strings.Contains(out.String(), `"id":3`) {
		t.Errorf("no answer to the request after the cancelled one:\n%s", out.String())
	}
	if strings.Contains(out.String(), `"id":2`) {
		t.Errorf("the bridge answered the cancelled request:\n%s", out.String())
	}
	if strings.Contains(errw.String(), "canceled") {
		t.Errorf("the bridge logged the cancellation as a failure:\n%s", errw.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if slices.Contains(methods, "notifications/cancelled") {
		t.Errorf("the bridge forwarded the cancellation to the lake: %v", methods)
	}
}

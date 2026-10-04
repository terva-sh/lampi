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
	"sync/atomic"
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
// many the agent sends at once, and answers each of them. An initialize
// sent once the slots are full waits for one too (review 2135).
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
		method := "ping"
		if i == mcpInFlight+1 {
			method = "initialize"
		}
		lines = append(lines, rpcLine(i, method, nil))
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
// bridge writes no answer for it. The notification goes no further. It
// names the request by the value of its id, however that is spelled
// (reviews 2135, 2136 and 2137).
func TestMCPBridgeCancelsARequest(t *testing.T) {
	for _, tc := range []struct{ name, id, requestID string }{
		{"number", `2`, `2`},
		{"string escaped differently", `"a"`, `"\u0061"`},
		{"number spelled as a float", `2`, `2.0`},
		{"number spelled with an exponent", `1000000`, `1e6`},
		{"number past the exponent boundary, as a float", `1000000`, `1000000.0`},
		{"number with a large exponent", `1e99999999`, `10e99999998`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var methods []string
			started, cancelled, stop := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server, token := fakeMCPLake(t, func(w http.ResponseWriter, r *http.Request, id json.RawMessage, method string) {
				mu.Lock()
				methods = append(methods, method)
				mu.Unlock()
				if method == "tools/call" {
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
			send, finish := startBridge(t, server, token, nil)
			send(`{"jsonrpc":"2.0","id":` + tc.id + `,"method":"tools/call","params":{"name":"search","arguments":{"query":"push"}}}`)
			waitFor(t, started, "the request never reached the lake")
			send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":` + tc.requestID + `,"reason":"the user stopped it"}}`)
			waitFor(t, cancelled, "the bridge did not cancel the request to the lake")
			send(rpcLine(3, "ping", nil))
			ids, stderr := finish()
			if !slices.Equal(ids, []string{"3"}) {
				t.Errorf("the bridge answered ids %v, want only 3", ids)
			}
			if strings.Contains(stderr, "canceled") {
				t.Errorf("the bridge logged the cancellation as a failure:\n%s", stderr)
			}
			mu.Lock()
			defer mu.Unlock()
			if slices.Contains(methods, "notifications/cancelled") {
				t.Errorf("the bridge forwarded the cancellation to the lake: %v", methods)
			}
		})
	}
}

// An answer that is ready when the bridge reads the request's
// cancellation is not written, even though it was ready first: the
// decision to write it waits on the same lock (review 2135).
func TestMCPBridgeWritesNoAnswerAfterACancellation(t *testing.T) {
	third := make(chan struct{})
	server, token := fakeMCPLake(t, func(w http.ResponseWriter, r *http.Request, id json.RawMessage, method string) {
		if string(id) == "3" {
			close(third)
		}
		answerMCP(w, id)
	})
	held, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	free := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(free)
	var answers atomic.Int32
	send, finish := startBridge(t, server, token, func() {
		// Only the first answer, to request 2, is held.
		if answers.Add(1) == 1 {
			close(held)
			<-release
		}
	})
	send(rpcLine(2, "tools/call", map[string]any{"name": "search", "arguments": map[string]any{"query": "push"}}))
	waitFor(t, held, "the answer to request 2 never came")
	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2}}`)
	// The bridge reads lines in order, so request 3 reaching the lake
	// means it has read the cancellation.
	send(rpcLine(3, "ping", nil))
	waitFor(t, third, "request 3 never reached the lake")
	free()
	if ids, _ := finish(); !slices.Equal(ids, []string{"3"}) {
		t.Errorf("the bridge answered ids %v, want only 3", ids)
	}
}

// startBridge serves a bridge to server on a pipe, with beforeReply set
// before it starts. send writes it one line. finish ends its input, waits
// for it to stop, and returns the ids it answered, in order, and what it
// logged.
func startBridge(t *testing.T, server, tokenPath string, beforeReply func()) (func(string), func() ([]string, string)) {
	t.Helper()
	token, err := readReadToken(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	b := &mcpBridge{url: server + mcpEndpoint, token: token, out: &out, log: &errw, beforeReply: beforeReply}
	stdin, feed := io.Pipe()
	t.Cleanup(func() { feed.Close() })
	done := make(chan error, 1)
	go func() { done <- b.serve(stdin) }()
	send := func(line string) {
		t.Helper()
		if _, err := io.WriteString(feed, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	finish := func() ([]string, string) {
		t.Helper()
		feed.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("serve: %v\n%s", err, errw.String())
			}
		case <-time.After(30 * time.Second):
			t.Fatal("the bridge did not stop at the end of its input")
		}
		var ids []string
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if line == "" {
				continue
			}
			var m struct {
				ID json.RawMessage `json:"id"`
			}
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("stdout line is not JSON: %q", line)
			}
			ids = append(ids, string(m.ID))
		}
		return ids, errw.String()
	}
	return send, finish
}

func waitFor(t *testing.T, c <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(10 * time.Second):
		t.Fatal(what)
	}
}

// Every spelling of an id's value shares a key, and values that differ
// do not. A large exponent is kept as a number, not expanded (review
// 2137).
func TestMCPIDKey(t *testing.T) {
	key := func(id string) string {
		t.Helper()
		k, ok := mcpIDKey(json.RawMessage(id))
		if !ok {
			t.Fatalf("mcpIDKey(%s) refused", id)
		}
		return k
	}
	for _, same := range [][2]string{
		{`2`, `2.0`}, {`1000000`, `1e6`}, {`1000000`, `1000000.0`}, {`0.5`, `5E-1`},
		{`-2`, `-2.00`}, {`0`, `-0.0`}, {`120`, `1.2e+2`}, {`"a"`, `"\u0061"`},
		{`1e99999999`, `10e99999998`},
		{`1e4611686018427387904`, `10e4611686018427387903`}, {`1e-4611686018427387904`, `0.1e-4611686018427387903`},
	} {
		if a, b := key(same[0]), key(same[1]); a != b {
			t.Errorf("%s and %s have keys %q and %q", same[0], same[1], a, b)
		}
	}
	for _, apart := range [][2]string{
		{`1`, `"1"`}, {`1`, `10`}, {`1.5`, `15`}, {`-1`, `1`}, {`0.01`, `0.1`}, {`"a"`, `"A"`},
	} {
		if a, b := key(apart[0]), key(apart[1]); a == b {
			t.Errorf("%s and %s share the key %q", apart[0], apart[1], a)
		}
	}
	if k := key(`1e999999999999`); len(k) > 32 {
		t.Errorf("the key of 1e999999999999 is %d bytes", len(k))
	}
	// An exponent past ±2^62 is refused, so the id cannot be cancelled.
	for _, id := range []string{`null`, `true`, `{}`, `[]`, `1e9999999999999999999`, `1e4611686018427387905`, `1e-4611686018427387905`, ``} {
		if k, ok := mcpIDKey(json.RawMessage(id)); ok {
			t.Errorf("mcpIDKey(%s) = %q, want refused", id, k)
		}
	}
}

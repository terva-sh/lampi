package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	if result["isError"] != false || len(content) != 1 || !strings.Contains(content[0].(map[string]any)["text"].(string), `"snippet":"git push"`) {
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

package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/toolorigin"
)

// codeModeTranscript is a Codex rollout whose exec cell calls a command
// and an MCP server from inside, as code mode records it.
const codeModeTranscript = `{"timestamp":"2026-09-22T16:10:02.000Z","type":"response_item","payload":{"type":"custom_tool_call","status":"completed","call_id":"call_cell1","name":"exec","input":"await tools.exec_command({cmd:'rg pond'})"}}
{"timestamp":"2026-09-22T16:10:02.500Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t1","turn_id":"u1","item":{"type":"CommandExecution","id":"exec-aaaa","command":"rg pond","cwd":"/work/app","status":"failed","aggregated_output":"","exit_code":1}}}
{"timestamp":"2026-09-22T16:10:03.000Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t1","turn_id":"u1","item":{"type":"McpToolCall","id":"exec-bbbb","server":"pond-docs","tool":"read","status":"completed","arguments":{"path":"main.go"},"result":{"content":[{"type":"text","text":"package main"}],"isError":false}}}}
{"timestamp":"2026-09-22T16:10:03.300Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call_cell1","output":"cell done"}}
`

// claudeMCPTranscript is a Claude Code session that calls an MCP
// server's tool and a built-in one.
const claudeMCPTranscript = `{"type":"assistant","sessionId":"s","cwd":"/work/app","timestamp":"2026-09-22T16:10:02Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"toolu_1","name":"mcp__pond__read","input":{"file_path":"secret-notes.md"}}]}}
{"type":"user","sessionId":"s","cwd":"/work/app","timestamp":"2026-09-22T16:10:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","is_error":false,"content":"private pond water"}]}}
{"type":"assistant","sessionId":"s","cwd":"/work/app","timestamp":"2026-09-22T16:10:04Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","sessionId":"s","cwd":"/work/app","timestamp":"2026-09-22T16:10:05Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","is_error":true,"content":"denied"}]}}
`

// normalizedSessions runs the two transcripts through their harnesses'
// normalizers, publishes them and indexes them, as the lake does.
func normalizedSessions(t *testing.T, lake *api.Server, index *recall.Index) (codex, claude string) {
	t.Helper()
	codex = seedHarness(t, lake, "codex", "sid-codex")
	claude = seedHarness(t, lake, "claude", "sid-claude")
	for uid, n := range map[string]normalize.Normalizer{codex: normalize.Codex{NativeID: "sid-codex"}, claude: normalize.Claude{NativeID: "sid-claude"}} {
		raw := codeModeTranscript
		if uid == claude {
			raw = claudeMCPTranscript
		}
		evs, err := n.Normalize(t.Context(), []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		publishNormalized(t, lake, uid, evs)
	}
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	return codex, claude
}

func decodeStats(t *testing.T, w *httptest.ResponseRecorder) recall.ToolStats {
	t.Helper()
	var v recall.ToolStats
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatalf("tool stats %d: %s", w.Code, w.Body.String())
	}
	return v
}

// TKT-01M61MZNC: real normalizer output reaches the read route with the
// Codex inner calls and the MCP origins placed, and no content.
func TestReadToolsFromNormalizedTranscripts(t *testing.T) {
	lake, idp, h, dir, index := mcpLake(t)
	codex, _ := normalizedSessions(t, lake, index)
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "stats", "", catalog.PermEventsRead)

	w := bearer(h, "GET", readToolsPath+"?all=true&largest=10", token)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("cache %q", w.Header().Get("Cache-Control"))
	}
	stats := decodeStats(t, w)
	type key struct {
		tool  string
		inner bool
	}
	groups := map[key]recall.ToolGroup{}
	for _, g := range stats.Groups {
		groups[key{g.Tool, g.Inner}] = g
	}
	for k, want := range map[key]toolorigin.Origin{
		{"exec", false}:                {Kind: toolorigin.Builtin},
		{"exec_command", true}:         {Kind: toolorigin.Builtin},
		{"mcp__pond-docs__read", true}: {Kind: toolorigin.MCP, Name: "pond-docs"},
		{"mcp__pond__read", false}:     {Kind: toolorigin.MCP, Name: "pond"},
		{"Bash", false}:                {Kind: toolorigin.Builtin},
	} {
		g, ok := groups[k]
		if !ok || g.Origin != want || g.Calls != 1 || g.Sessions != 1 {
			t.Errorf("group %v: %+v, want origin %+v and one call", k, g, want)
		}
	}
	if g := groups[key{"exec", false}]; g.LatencyNote != recall.LatencyCodeModeCell {
		t.Errorf("exec latency note %q", g.LatencyNote)
	}
	if g := groups[key{"Bash", false}]; g.ErrorRate == nil || *g.ErrorRate != 1 {
		t.Errorf("Bash error rate %v", g.ErrorRate)
	}
	if stats.Totals.Sessions != 2 || stats.Totals.InnerResultBytes == 0 || stats.Totals.ContextResultBytes == 0 || len(stats.Largest) == 0 {
		t.Errorf("totals %+v, %d largest", stats.Totals, len(stats.Largest))
	}
	for _, b := range stats.Largest {
		if !strings.HasPrefix(b.Link, "/sessions/") || !strings.Contains(b.Link, "#e-") {
			t.Errorf("largest link %q", b.Link)
		}
	}
	body := w.Body.String()
	for _, private := range []string{"package main", "private pond water", "secret-notes", "rg pond", "cell done", token} {
		if strings.Contains(body, private) {
			t.Errorf("the answer holds %q", private)
		}
	}

	// By origin: the Claude MCP server is its own group.
	byOrigin := decodeStats(t, bearer(h, "GET", readToolsPath+"?all=true&group_by=origin&harness=claude", token))
	found := false
	for _, g := range byOrigin.Groups {
		if g.Origin == (toolorigin.Origin{Kind: toolorigin.MCP, Name: "pond"}) {
			found = g.Calls == 1 && g.ResultShare > 0
		}
	}
	if !found {
		t.Errorf("by origin: %+v", byOrigin.Groups)
	}

	// The browser route answers the same.
	web := decodeStats(t, get(h, "/api/web/v1/tools?all=true&largest=10", admin))
	web.AsOf, stats.AsOf = "", ""
	a, _ := json.Marshal(web)
	b, _ := json.Marshal(stats)
	if string(a) != string(b) {
		t.Errorf("browser route differs:\n%s\n%s", a, b)
	}

	// A session-scoped token counts its session only.
	onlyCodex, _ := mintReadTokenWith(t, h, admin, "codex only", codex, catalog.PermEventsRead)
	if s := decodeStats(t, bearer(h, "GET", readToolsPath+"?all=true", onlyCodex)); s.Totals.Sessions != 1 || s.Coverage != (recall.Coverage{}) {
		t.Errorf("session scope: %+v", s.Totals)
	}

	log, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(log); !strings.Contains(s, `"tools query=all=true\u0026largest=10"`) || !strings.Contains(s, "(stats)") || strings.Contains(s, token) {
		t.Errorf("audit:\n%s", s)
	}
}

// The read route takes only a read token with events:read, refuses
// what it does not know, and reads the last seven days by default.
func TestReadToolsRefusals(t *testing.T) {
	lake, idp, h, _, index := mcpLake(t)
	normalizedSessions(t, lake, index)
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "stats", "", catalog.PermEventsRead)
	raw := mintReadToken(t, h, admin, "raw", "")

	for name, c := range map[string]struct {
		token, query string
		status       int
		body         string
	}{
		"no token":       {"", "", 401, "not_authenticated"},
		"raw-only token": {raw, "", 404, "not_found"},
		"unknown key":    {token, "?text=private", 400, `{"error":"invalid_request"}`},
		"repeated key":   {token, "?largest=1&largest=2", 400, `"param":"largest"`},
		"bad origin":     {token, "?origin=server", 400, `"param":"origin"`},
		"bad date":       {token, "?since=yesterday", 400, `"param":"since"`},
		"all and since":  {token, "?all=true&since=2026-09-01", 400, `"param":"all"`},
		"bad bucket":     {token, "?bucket=week", 400, `"param":"bucket"`},
		"session filter": {token, "?session_uid=x", 400, "invalid_request"},
	} {
		w := bearer(h, "GET", readToolsPath+c.query, c.token)
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.body) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private") {
			t.Errorf("%s: the refusal echoes a value", name)
		}
	}
	// The transcripts are from September 2026, outside the last 7 days.
	s := decodeStats(t, bearer(h, "GET", readToolsPath, token))
	if s.Since == nil || s.Until != nil || s.Totals.Calls != 0 {
		t.Errorf("default range: since %v until %v, %d calls", s.Since, s.Until, s.Totals.Calls)
	}
	if s := decodeStats(t, bearer(h, "GET", readToolsPath+"?since=2026-09-22&until=2026-09-23&context=true", token)); s.Totals.Calls == 0 || s.Context == nil || s.Context.Kind != recall.ToolContextKind || s.Context.Note == "" {
		t.Errorf("range with context: %+v %+v", s.Totals, s.Context)
	}
}

// With no search index the routes say so, and the browser route is
// behind sign-in.
func TestToolsWithoutAnIndex(t *testing.T) {
	_, idp, h, _ := rawLake(t, nil)
	if w := get(h, "/api/web/v1/tools", nil); w.Code != 401 {
		t.Errorf("unguarded: %d", w.Code)
	}
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "stats", "", catalog.PermEventsRead)
	for name, w := range map[string]*httptest.ResponseRecorder{
		"browser": get(h, "/api/web/v1/tools", admin),
		"read":    bearer(h, "GET", readToolsPath, token),
	} {
		if w.Code != 503 || !strings.Contains(w.Body.String(), "search_unavailable") {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

// A query that does not decode is refused before the browser handler
// reads it, so a malformed filter cannot fall away and widen the answer
// (review 2910 on #223).
func TestToolsRefuseMalformedQueries(t *testing.T) {
	lake, idp, h, _, index := mcpLake(t)
	normalizedSessions(t, lake, index)
	admin := signInAs(t, idp, h, "owners")
	for _, path := range []string{
		"/api/web/v1/tools?all=true&model=%ZZ",
		"/api/web/v1/tools?all=true&model=a&model=%ZZ",
	} {
		if w := get(h, path, admin); w.Code != 400 {
			t.Errorf("%s: %d, want 400", path, w.Code)
		}
	}
}

func TestToolsReadTheViewersBays(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	_, claude := normalizedSessions(t, lake, indexes[lake])
	ctx, now := t.Context(), time.Now()
	if _, err := lake.Catalog.CreateBay(ctx, "secret", "test", now); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: claude, Bay: "secret", Actor: "test", Via: catalog.ViaCLI}, now); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.RemoveFromBay(ctx, catalog.Membership{SessionUID: claude, Bay: catalog.DefaultBayName, Actor: "test", Via: catalog.ViaCLI}, now); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	for _, p := range []string{"/api/web/v1/tools?all=true&largest=20&context=true"} {
		w := get(h, p, cookie)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "exec_command") {
			t.Fatalf("%s: %d %s", p, w.Code, w.Body.String())
		}
		for _, leak := range []string{claude, "mcp__pond__read"} {
			if strings.Contains(w.Body.String(), leak) {
				t.Errorf("%s shows %q from a bay the viewer does not read", p, leak)
			}
		}
	}
}

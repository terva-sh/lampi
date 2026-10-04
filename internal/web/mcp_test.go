package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/testidp"
	"terva.sh/lampi/internal/webconfig"
)

// The revision and codes the tests hold the endpoint to, from the
// specification rather than the SDK.
const (
	mcpModern         = "2026-07-28"
	rpcNoMethod       = -32601
	rpcInvalidParams  = -32602
	mcpHeaderMismatch = -32020
	mcpBadVersion     = -32022
)

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// mcpLake is rawLake with a search index, which the search tool needs.
func mcpLake(t *testing.T) (*api.Server, *testidp.Server, http.Handler, string, *recall.Index) {
	t.Helper()
	idp := testidp.New()
	t.Cleanup(idp.Close)
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"owners": "admin"}}}
	reg := &Registrations{
		Lake:      func() registrar.Lake { return registrar.Lake{Catalog: lake.Catalog, Dir: dir} },
		Blobs:     lake.CAS,
		Normalize: lake.ReloadNormalizeJobs,
	}
	reader := recall.NewReader(lake.Catalog, lake.Normalized)
	index, err := recall.OpenIndex(filepath.Join(t.TempDir(), recall.IndexFile), reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { index.Close() })
	lake.Web, err = New(cfg, lake.Catalog, reader, index, reg, nil, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	return lake, idp, lake.Handler(), dir, index
}

// postRPC posts one JSON-RPC body to the MCP endpoint.
func postRPC(h http.Handler, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://lake.example"+mcpPath, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type rpcReplyBody struct {
	ID     json.RawMessage `json:"id"`
	Result map[string]any  `json:"result"`
	Error  *rpcError       `json:"error"`
}

func decodeRPC(t *testing.T, w *httptest.ResponseRecorder) rpcReplyBody {
	t.Helper()
	var out rpcReplyBody
	if w.Header().Get("Content-Type") != "application/json" || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("not a JSON-RPC reply: %d %s", w.Code, w.Body.String())
	}
	return out
}

func rpcBody(id int, method string, params any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return string(b)
}

// callTool calls a tool the initialize-era way and returns its text and
// whether it is a tool error.
func callTool(t *testing.T, h http.Handler, token, name string, args map[string]any) (string, bool) {
	t.Helper()
	w := postRPC(h, token, rpcBody(1, "tools/call", map[string]any{"name": name, "arguments": args}), nil)
	reply := decodeRPC(t, w)
	if w.Code != 200 || reply.Error != nil {
		t.Fatalf("%s %v: %d %s", name, args, w.Code, w.Body.String())
	}
	content, _ := reply.Result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("%s: content %v", name, reply.Result)
	}
	item, _ := content[0].(map[string]any)
	isError, _ := reply.Result["isError"].(bool)
	return item["text"].(string), isError
}

// asQuery is args as the browser route's query string.
func asQuery(args map[string]any) url.Values {
	q := url.Values{}
	for k, v := range args {
		switch v := v.(type) {
		case string:
			q.Set(k, v)
		case int:
			q.Set(k, strconv.Itoa(v))
		case bool:
			q.Set(k, strconv.FormatBool(v))
		}
	}
	return q
}

// TKT-01M3FPWCFS: each tool returns what its browser route returns for
// the same parameters, with links made absolute.
func TestMCPToolsMatchTheWebAPI(t *testing.T) {
	lake, idp, h, _, index := mcpLake(t)
	a := seedHarness(t, lake, "codex", "sid-a")
	b := seedHarness(t, lake, "claude", "sid-b")
	publishNormalized(t, lake, a, toolEvents("codex", "Bash git push", "Read README.md", "Bash git status"))
	publishNormalized(t, lake, b, toolEvents("claude", "Bash git push --force"))
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "agent", "", catalog.PermEventsRead)

	unify := func(v any) {
		switch v := v.(type) {
		case *recall.SearchPage:
			v.AsOf = ""
			for i := range v.Items {
				v.Items[i].Link = strings.TrimPrefix(v.Items[i].Link, "https://lake.example")
			}
		case *recall.EventPage:
			v.AsOf = ""
			for i := range v.Items {
				v.Items[i].Link = strings.TrimPrefix(v.Items[i].Link, "https://lake.example")
			}
		}
	}
	same := func(tool, path string, args map[string]any, fresh func() any) string {
		t.Helper()
		text, isError := callTool(t, h, token, tool, args)
		w := get(h, path, admin)
		if isError || w.Code != 200 {
			t.Fatalf("%s %v: tool error %v %s, web %d %s", tool, args, isError, text, w.Code, w.Body.String())
		}
		viaMCP, viaWeb := fresh(), fresh()
		if json.Unmarshal([]byte(text), viaMCP) != nil || json.Unmarshal(w.Body.Bytes(), viaWeb) != nil {
			t.Fatalf("%s: undecodable %s", tool, text)
		}
		if strings.Contains(text, `"link":"/`) || !strings.Contains(text, `"link":"https://lake.example/sessions/`) {
			t.Errorf("%s: links are not absolute: %s", tool, text)
		}
		unify(viaMCP)
		unify(viaWeb)
		if !reflect.DeepEqual(viaMCP, viaWeb) {
			t.Errorf("%s %v:\nmcp %+v\nweb %+v", tool, args, viaMCP, viaWeb)
		}
		return text
	}
	page := func() any { return &recall.SearchPage{} }
	for _, args := range []map[string]any{
		{"q": "git push"},
		{"tool": "Bash", "event_type": "tool_call"},
		{"q": "git", "since": "2026-09-02", "harness": "codex"},
		{"q": "said", "harness": "codex", "limit": 1, "actor": "user"},
		{"q": "git", "actor": "assistant", "until": "2026-09-01"},
	} {
		same("search", "/api/web/v1/search?"+asQuery(args).Encode(), args, page)
	}
	// A cursor carries over: the next page through either adapter is
	// the same page.
	first := map[string]any{"q": "said", "limit": 1}
	var p recall.SearchPage
	text := same("search", "/api/web/v1/search?"+asQuery(first).Encode(), first, page)
	if json.Unmarshal([]byte(text), &p) != nil || p.NextCursor == "" || len(p.Items) != 1 {
		t.Fatalf("first page %s", text)
	}
	first["cursor"] = p.NextCursor
	same("search", "/api/web/v1/search?"+asQuery(first).Encode(), first, page)

	events := map[string]any{"session_uid": a, "from": 1, "limit": 2}
	same("read_events", "/api/web/v1/sessions/"+a+"/events?from=1&limit=2", events, func() any { return &recall.EventPage{} })
	pinned := map[string]any{"session_uid": b, "gen": 1}
	same("read_events", "/api/web/v1/sessions/"+b+"/events?gen=1", pinned, func() any { return &recall.EventPage{} })
	ex := map[string]any{"session_uid": a, "from": 2, "count": 3}
	text = same("copy_excerpt", "/api/web/v1/sessions/"+a+"/excerpt?from=2&count=3", ex, func() any { return &recall.Excerpt{} })
	if !strings.Contains(text, "Read README.md") && !strings.Contains(text, "README.md") {
		t.Errorf("excerpt lacks its events: %s", text)
	}

	// A bad argument or a failed read is a tool error with the browser
	// route's code, so the agent can correct the call.
	for _, c := range []struct {
		tool string
		args map[string]any
		code string
	}{
		{"search", map[string]any{"q": "ab"}, "invalid_request"},
		{"search", map[string]any{"q": "push", "nope": "x"}, "invalid_request"},
		{"search", map[string]any{"q": "push", "limit": 1.5}, "invalid_request"},
		{"search", map[string]any{"q": "push", "limit": 201}, "invalid_request"},
		{"search", map[string]any{"q": []string{"push"}}, "invalid_request"},
		{"search", map[string]any{"q": "push", "cursor": "forged"}, "invalid_request"},
		{"search", map[string]any{"q": "push", "session_uid": a}, "invalid_request"},
		{"read_events", map[string]any{}, "invalid_request"},
		{"read_events", map[string]any{"session_uid": "no-such-session"}, "not_found"},
		{"read_events", map[string]any{"session_uid": a, "gen": 7}, "generation_changed"},
		{"copy_excerpt", map[string]any{"session_uid": a, "from": 99}, "invalid_request"},
	} {
		text, isError := callTool(t, h, token, c.tool, c.args)
		var body map[string]string
		if !isError || json.Unmarshal([]byte(text), &body) != nil || body["error"] != c.code || body["hint"] == "" {
			t.Errorf("%s %v: error=%v %s, want %s", c.tool, c.args, isError, text, c.code)
		}
	}
	w := postRPC(h, token, rpcBody(9, "tools/call", map[string]any{"name": "delete_everything"}), nil)
	if reply := decodeRPC(t, w); reply.Error == nil || reply.Error.Code != rpcInvalidParams {
		t.Errorf("unknown tool: %s", w.Body.String())
	}
}

// A token reads through MCP only what its scope holds, and only a read
// token holding events:read gets in.
func TestMCPReadsWithinTheTokenScope(t *testing.T) {
	lake, idp, h, dir, index := mcpLake(t)
	device := strings.Repeat("d", 64)
	lake.Allow(device)
	a := seedHarness(t, lake, "codex", "sid-a")
	b := seedHarness(t, lake, "claude", "sid-b")
	publishNormalized(t, lake, a, toolEvents("codex", "Bash git push"))
	publishNormalized(t, lake, b, toolEvents("claude", "Bash git push"))
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	onlyA, _ := mintReadTokenWith(t, h, admin, "one session", a, catalog.PermEventsRead)
	raw := mintReadToken(t, h, admin, "raw", "")

	text, isError := callTool(t, h, onlyA, "search", map[string]any{"q": "git push"})
	var page recall.SearchPage
	if isError || json.Unmarshal([]byte(text), &page) != nil || len(page.Items) != 2 || page.Coverage != (recall.Coverage{}) {
		t.Errorf("session-scoped search: %s", text)
	}
	for _, hit := range page.Items {
		if hit.SessionUID != a {
			t.Errorf("session-scoped search found %s", hit.SessionUID)
		}
	}
	for _, tool := range []string{"read_events", "copy_excerpt"} {
		if text, isError := callTool(t, h, onlyA, tool, map[string]any{"session_uid": b}); !isError || !strings.Contains(text, `"error":"not_found"`) {
			t.Errorf("%s outside the scope: %s", tool, text)
		}
		if _, isError := callTool(t, h, onlyA, tool, map[string]any{"session_uid": a}); isError {
			t.Errorf("%s inside the scope failed", tool)
		}
	}

	list := rpcBody(1, "tools/list", nil)
	for name, c := range map[string]struct {
		token string
		want  int
	}{"raw-only token": {raw, 404}, "device token": {device, 401}, "no token": {"", 401}} {
		if w := postRPC(h, c.token, list, nil); w.Code != c.want {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := postRPC(h, "", list, nil); w.Header().Get("WWW-Authenticate") != `Bearer realm="lampi-mcp"` {
		t.Errorf("challenge %q", w.Header().Get("WWW-Authenticate"))
	}
	r := httptest.NewRequest("POST", "https://lake.example"+mcpPath, strings.NewReader(list))
	r.AddCookie(admin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Errorf("browser session: %d", w.Code)
	}

	// A refused call is audited too, and a value under a wrong name is
	// counted, never written (review 2102).
	if text, isError := callTool(t, h, onlyA, "search", map[string]any{"q": "git push", "text": "private transcript"}); !isError {
		t.Errorf("an unknown argument was accepted: %s", text)
	}
	if _, isError := callTool(t, h, onlyA, "read_events", map[string]any{"from": 3}); !isError {
		t.Error("read_events without a session was accepted")
	}
	if reply := decodeRPC(t, postRPC(h, onlyA, rpcBody(9, "tools/call", map[string]any{"name": "private transcript"}), nil)); reply.Error == nil {
		t.Error("an unknown tool was accepted")
	}
	if text, isError := callTool(t, h, onlyA, "read_events", map[string]any{"session_uid": "private transcript"}); !isError {
		t.Errorf("a malformed session uid was accepted: %s", text)
	}

	// Each call is audited before its result leaves, without the search
	// text or the secret.
	log, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	s := string(log)
	if !strings.Contains(s, `"kind":"events.read"`) || !strings.Contains(s, "mcp tool=search q=8B") || !strings.Contains(s, "mcp tool=read_events session="+b) || !strings.Contains(s, "(one session)") {
		t.Errorf("audit lacks the calls:\n%s", s)
	}
	for _, want := range []string{"mcp tool=search q=8B unknown_args=1", "mcp tool=read_events from=3", "mcp tool=unknown", "mcp tool=read_events session=invalid"} {
		if !strings.Contains(s, want) {
			t.Errorf("audit lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "git push") || strings.Contains(s, "private") || strings.Contains(s, onlyA) {
		t.Error("audit holds search text, a refused argument or the secret")
	}
}

// A token scoped to a bay reaches that bay's sessions through every
// tool, and one scoped to a bay and to named sessions reaches only a
// named session in the bay. Neither is told search coverage, which
// counts the whole lake (TKT-01M445H1).
func TestMCPReadsWithinABayScopedToken(t *testing.T) {
	lake, _, h, _, index := mcpLake(t)
	a := seedHarness(t, lake, "codex", "sid-a")
	b := seedHarness(t, lake, "claude", "sid-b")
	c := seedHarness(t, lake, "codex", "sid-c")
	for _, uid := range []string{a, b, c} {
		publishNormalized(t, lake, uid, toolEvents("codex", "Bash git push"))
	}
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, now := t.Context(), time.Now()
	if _, err := lake.Catalog.CreateBay(ctx, "work", "admin", now); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{a, c} {
		if _, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: uid, Bay: "work", Actor: "admin", Via: catalog.ViaCLI}, now); err != nil {
			t.Fatal(err)
		}
	}
	mint := func(label string, sessions []string) string {
		secret := ReadTokenPrefix + label + strings.Repeat("0", 43-len(label))
		tok := catalog.ReadToken{Label: label, Permissions: []string{catalog.PermEventsRead}, BayScoped: true, Bays: []string{"work"}, Sessions: sessions, CreatedBy: "admin", Expires: now.Add(time.Hour)}
		if _, err := lake.Catalog.CreateReadToken(ctx, tok, hashReadToken(secret), now); err != nil {
			t.Fatal(err)
		}
		return secret
	}
	for name, c2 := range map[string]struct {
		token string
		want  map[string]bool
	}{
		"the bay":                       {mint("bay", nil), map[string]bool{a: true, b: false, c: true}},
		"the bay, a session in it":      {mint("bay-a", []string{a}), map[string]bool{a: true, b: false, c: false}},
		"the bay, a session outside it": {mint("bay-b", []string{b}), map[string]bool{a: false, b: false, c: false}},
	} {
		text, isError := callTool(t, h, c2.token, "search", map[string]any{"q": "git push"})
		var page recall.SearchPage
		if isError || json.Unmarshal([]byte(text), &page) != nil || page.Coverage != (recall.Coverage{}) {
			t.Fatalf("%s: search %s", name, text)
		}
		hits := map[string]bool{}
		for _, hit := range page.Items {
			hits[hit.SessionUID] = true
		}
		for uid, want := range c2.want {
			if hits[uid] != want {
				t.Errorf("%s: search hit %s = %v, want %v", name, uid, hits[uid], want)
			}
			for _, tool := range []string{"read_events", "copy_excerpt"} {
				text, isError := callTool(t, h, c2.token, tool, map[string]any{"session_uid": uid})
				if isError == want || (!want && !strings.Contains(text, `"not_found"`)) {
					t.Errorf("%s: %s %s: error=%v %s", name, tool, uid, isError, text)
				}
			}
		}
	}
}

// The endpoint answers an initialize-era client and a client that sends
// its version with every request, and refuses what neither allows. The
// protocol is the SDK's (TKT-01M44DVPVX). This holds it to the parts
// lampi relies on and the ones it sets, by the specification's numbers
// rather than the SDK's names, so that a change in the SDK shows here.
func TestMCPProtocolEras(t *testing.T) {
	lake, idp, h, _, index := mcpLake(t)
	a := seedHarness(t, lake, "codex", "sid-a")
	publishNormalized(t, lake, a, toolEvents("codex", "Bash git push"))
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "agent", "", catalog.PermEventsRead)
	toolsOnly := map[string]any{"tools": map[string]any{}}

	// Initialize era. A version the SDK does not serve gets the newest
	// one it does in that era. Capabilities are tools only, with no
	// list_changed and no logging.
	for asked, want := range map[string]string{"2025-11-25": "2025-11-25", "2025-06-18": "2025-06-18", "2025-03-26": "2025-03-26", "1999-01-01": "2025-11-25"} {
		reply := decodeRPC(t, postRPC(h, token, rpcBody(1, "initialize", map[string]any{"protocolVersion": asked, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}), nil))
		info, _ := reply.Result["serverInfo"].(map[string]any)
		if reply.Result["protocolVersion"] != want || info["name"] != "terva-lampi" || !reflect.DeepEqual(reply.Result["capabilities"], toolsOnly) || reply.Result["instructions"] != mcpInstructions || reply.Result["resultType"] != nil {
			t.Errorf("initialize %s: %+v", asked, reply)
		}
	}
	if w := postRPC(h, token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, map[string]string{"MCP-Protocol-Version": "2025-06-18"}); w.Code != 202 || w.Body.Len() != 0 {
		t.Errorf("notification: %d %q", w.Code, w.Body.String())
	}
	if w := postRPC(h, token, `{"jsonrpc":"2.0","id":"s-1","method":"ping"}`, nil); w.Code != 200 || string(decodeRPC(t, w).ID) != `"s-1"` {
		t.Errorf("string id: %d %s", w.Code, w.Body.String())
	}
	reply := decodeRPC(t, postRPC(h, token, rpcBody(2, "tools/list", nil), map[string]string{"MCP-Protocol-Version": "2025-06-18"}))
	tools, _ := reply.Result["tools"].([]any)
	var names []string
	for _, tool := range tools {
		m := tool.(map[string]any)
		names = append(names, m["name"].(string))
		ann, _ := m["annotations"].(map[string]any)
		schema, _ := m["inputSchema"].(map[string]any)
		if ann["readOnlyHint"] != true || ann["idempotentHint"] != true || ann["openWorldHint"] != false || schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("tool %v", m)
		}
	}
	// The SDK lists tools by name.
	if !slices.Equal(names, []string{"copy_excerpt", "read_events", "search"}) || string(reply.ID) != "2" {
		t.Errorf("tools/list: %v id %s", names, reply.ID)
	}
	if reply := decodeRPC(t, postRPC(h, token, rpcBody(3, "ping", nil), nil)); reply.Error != nil || len(reply.Result) != 0 {
		t.Errorf("ping: %+v", reply)
	}
	// A method the SDK does not know at all is a plain 400 in this era,
	// not a JSON-RPC error; docs/web-api.md lists it. Every method the
	// specification names is known to it.
	if w := postRPC(h, token, rpcBody(4, "nope/nothing", nil), nil); w.Code != 400 {
		t.Errorf("legacy unknown method: %d %s", w.Code, w.Body.String())
	}
	// A version header naming nothing served is refused, a
	// notification's too (review 2105) and initialize's (review 2112).
	// In this era the answer need not be JSON-RPC.
	for name, body := range map[string]string{
		"request":      rpcBody(5, "tools/list", nil),
		"notification": `{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		"initialize":   rpcBody(5, "initialize", map[string]any{"protocolVersion": "2025-06-18"}),
	} {
		if w := postRPC(h, token, body, map[string]string{"MCP-Protocol-Version": "1999-01-01"}); w.Code != 400 {
			t.Errorf("%s with an unsupported header: %d %s", name, w.Code, w.Body.String())
		}
	}
	// Batches ended with 2025-06-18.
	if w := postRPC(h, token, "["+rpcBody(6, "ping", nil)+"]", map[string]string{"MCP-Protocol-Version": "2025-06-18"}); w.Code != 400 {
		t.Errorf("batch: %d %s", w.Code, w.Body.String())
	}

	// Per-request metadata, 2026-07-28.
	meta := map[string]any{"io.modelcontextprotocol/protocolVersion": mcpModern, "io.modelcontextprotocol/clientInfo": map[string]any{"name": "t", "version": "1"}, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	modern := func(method string) map[string]string {
		return map[string]string{"MCP-Protocol-Version": mcpModern, "Mcp-Method": method}
	}
	served := func(t *testing.T, reply rpcReplyBody) {
		t.Helper()
		m, _ := reply.Result["_meta"].(map[string]any)
		info, _ := m["io.modelcontextprotocol/serverInfo"].(map[string]any)
		if reply.Error != nil || reply.Result["resultType"] != "complete" || info["name"] != "terva-lampi" {
			t.Errorf("modern result: %+v", reply)
		}
	}
	call := rpcBody(7, "tools/call", map[string]any{"name": "search", "arguments": map[string]any{"q": "git push"}, "_meta": meta})
	headers := modern("tools/call")
	// The transport's base64 form of Mcp-Name is decoded before the SDK
	// compares it to the body.
	for _, name := range []string{"search", "=?base64?" + base64.StdEncoding.EncodeToString([]byte("search")) + "?="} {
		headers["Mcp-Name"] = name
		w := postRPC(h, token, call, headers)
		reply := decodeRPC(t, w)
		served(t, reply)
		if w.Code != 200 || reply.Result["isError"] == true || reply.Result["content"] == nil {
			t.Errorf("modern call with Mcp-Name %s: %d %s", name, w.Code, w.Body.String())
		}
	}
	for name, bad := range map[string]map[string]string{
		"name mismatch":    {"MCP-Protocol-Version": mcpModern, "Mcp-Method": "tools/call", "Mcp-Name": "read_events"},
		"encoded mismatch": {"MCP-Protocol-Version": mcpModern, "Mcp-Method": "tools/call", "Mcp-Name": "=?base64?" + base64.StdEncoding.EncodeToString([]byte("read_events")) + "?="},
		"no name header":   {"MCP-Protocol-Version": mcpModern, "Mcp-Method": "tools/call"},
		"no method header": {"MCP-Protocol-Version": mcpModern, "Mcp-Name": "search"},
		"method mismatch":  {"MCP-Protocol-Version": mcpModern, "Mcp-Method": "tools/list", "Mcp-Name": "search"},
		"no version":       {"Mcp-Method": "tools/call", "Mcp-Name": "search"},
		"other version":    {"MCP-Protocol-Version": "2025-11-25", "Mcp-Method": "tools/call", "Mcp-Name": "search"},
	} {
		if w := postRPC(h, token, call, bad); w.Code != 400 || decodeRPC(t, w).Error.Code != mcpHeaderMismatch {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	// Every request carries the client's capabilities.
	noCaps := rpcBody(8, "tools/list", map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": mcpModern}})
	if w := postRPC(h, token, noCaps, modern("tools/list")); w.Code != 400 || decodeRPC(t, w).Error.Code != rpcInvalidParams {
		t.Errorf("no client capabilities: %d %s", w.Code, w.Body.String())
	}
	if w := postRPC(h, token, rpcBody(8, "tools/list", nil), modern("tools/list")); w.Code != 400 {
		t.Errorf("modern header, no _meta: %d %s", w.Code, w.Body.String())
	}
	future := maps.Clone(meta)
	future["io.modelcontextprotocol/protocolVersion"] = "2099-01-01"
	w := postRPC(h, token, rpcBody(9, "tools/list", map[string]any{"_meta": future}), map[string]string{"MCP-Protocol-Version": "2099-01-01", "Mcp-Method": "tools/list"})
	if reply := decodeRPC(t, w); w.Code != 400 || reply.Error.Code != mcpBadVersion || !strings.Contains(string(reply.Error.Data), `"supported":["2026-07-28","2025-11-25"`) {
		t.Errorf("unsupported modern version: %d %s", w.Code, w.Body.String())
	}
	reply = decodeRPC(t, postRPC(h, token, rpcBody(10, "server/discover", map[string]any{"_meta": meta}), modern("server/discover")))
	served(t, reply)
	if versions, _ := reply.Result["supportedVersions"].([]any); len(versions) == 0 || versions[0] != mcpModern || !reflect.DeepEqual(reply.Result["capabilities"], toolsOnly) {
		t.Errorf("discover: %+v", reply)
	}
	// A list result says how long it may be cached.
	reply = decodeRPC(t, postRPC(h, token, rpcBody(11, "tools/list", map[string]any{"_meta": meta}), modern("tools/list")))
	served(t, reply)
	if _, ok := reply.Result["ttlMs"]; !ok || reply.Result["cacheScope"] == nil {
		t.Errorf("modern tools/list: %+v", reply)
	}
	// 2026-07-28 removed ping, and a method not served is 404.
	for _, method := range []string{"ping", "nope/nothing"} {
		if w := postRPC(h, token, rpcBody(12, method, map[string]any{"_meta": meta}), modern(method)); w.Code != 404 || decodeRPC(t, w).Error.Code != rpcNoMethod {
			t.Errorf("modern %s: %d %s", method, w.Code, w.Body.String())
		}
	}

	// What neither era allows.
	for _, method := range []string{"GET", "DELETE"} {
		if w := bearer(h, method, mcpPath, token); w.Code != 405 || w.Header().Get("Allow") != "POST" {
			t.Errorf("%s: %d", method, w.Code)
		}
	}
	if w := postRPC(h, token, rpcBody(13, "ping", nil), map[string]string{"Origin": "https://evil.example"}); w.Code != 403 || decodeRPC(t, w).Error.Code != -32600 {
		t.Errorf("foreign origin: %d %s", w.Code, w.Body.String())
	}
	if w := postRPC(h, token, rpcBody(14, "ping", nil), map[string]string{"Origin": "https://lake.example"}); w.Code != 200 {
		t.Errorf("own origin: %d", w.Code)
	}
	// No answer is kept by a cache: the SDK's own Cache-Control is
	// replaced, on a result, an error and a refusal alike.
	for name, c := range map[string]struct {
		body    string
		headers map[string]string
		code    int
	}{
		"result":        {rpcBody(15, "ping", nil), nil, 200},
		"notification":  {`{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil, 202},
		"plain refusal": {rpcBody(16, "nope/nothing", nil), nil, 400},
		"modern error":  {rpcBody(17, "ping", map[string]any{"_meta": meta}), modern("ping"), 404},
		"tool call":     {call, headers, 200},
	} {
		if w := postRPC(h, token, c.body, c.headers); w.Code != c.code || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d Cache-Control %q", name, w.Code, w.Header().Get("Cache-Control"))
		}
	}
	for name, body := range map[string]string{
		"not json":   "{",
		"null id":    `{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		"object id":  `{"jsonrpc":"2.0","id":{"a":1},"method":"ping"}`,
		"array id":   `{"jsonrpc":"2.0","id":[1],"method":"ping"}`,
		"boolean id": `{"jsonrpc":"2.0","id":true,"method":"ping"}`,
		"no version": `{"id":1,"method":"ping"}`,
		"too large":  `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", mcpBodyMax) + `"}}`,
	} {
		if w := postRPC(h, token, body, nil); w.Code < 400 || w.Code >= 500 {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

// A tool reads under the context the SDK gives the call, not the HTTP
// request's, so a call the SDK cancels reads nothing (review on #195).
func TestMCPToolReadsUnderTheCallsContext(t *testing.T) {
	lake, idp, h, _, index := mcpLake(t)
	a := seedHarness(t, lake, "codex", "sid-a")
	publishNormalized(t, lake, a, toolEvents("codex", "Bash git push"))
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	secret, _ := mintReadTokenWith(t, h, admin, "agent", "", catalog.PermEventsRead)
	tok, found, err := lake.Catalog.ReadTokenBySecret(t.Context(), hashReadToken(secret))
	if err != nil || !found {
		t.Fatal("no token", err)
	}
	srv := &Server{catalog: lake.Catalog, events: recall.NewReader(lake.Catalog, lake.Normalized), index: index, origin: "https://lake.example", log: slog.New(slog.DiscardHandler)}
	// The request stays live; only the call's context is cancelled.
	caller := mcpCaller{r: httptest.NewRequest("POST", "https://lake.example"+mcpPath, nil), t: tok, now: time.Now()}
	call := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "search", Arguments: json.RawMessage(`{"q":"git push"}`)}}
	live, err := srv.mcpCall(context.WithValue(t.Context(), mcpCallerKey{}, caller), call)
	if err != nil || live.IsError {
		t.Fatalf("live call: %v %+v", err, live)
	}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), mcpCallerKey{}, caller))
	cancel()
	res, err := srv.mcpCall(ctx, call)
	if err != nil || !res.IsError {
		t.Errorf("a cancelled call read: %v %+v", err, res)
	}
}

// bearerTransport sends a read token with every request.
type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// The SDK's own client reads the lake in each era it speaks, which is
// the real client of 2026-07-28 that TKT-01M445H1 asked for.
func TestMCPServesTheSDKClient(t *testing.T) {
	lake, idp, h, _, index := mcpLake(t)
	a := seedHarness(t, lake, "codex", "sid-a")
	publishNormalized(t, lake, a, toolEvents("codex", "Bash git push", "Read README.md"))
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "agent", "", catalog.PermEventsRead)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	for _, version := range []string{mcpModern, "2025-11-25", "2025-03-26"} {
		client := mcp.NewClient(&mcp.Implementation{Name: "lampi-test", Version: "1"}, nil)
		transport := &mcp.StreamableClientTransport{Endpoint: srv.URL + mcpPath, HTTPClient: &http.Client{Transport: bearerTransport{token}}}
		cs, err := client.Connect(t.Context(), transport, &mcp.ClientSessionOptions{ProtocolVersion: version})
		if err != nil {
			t.Fatalf("%s: connect: %v", version, err)
		}
		if got := cs.InitializeResult().ProtocolVersion; got != version {
			t.Errorf("%s: negotiated %s", version, got)
		}
		list, err := cs.ListTools(t.Context(), nil)
		if err != nil || len(list.Tools) != len(mcpTools) {
			t.Fatalf("%s: tools/list: %v %+v", version, err, list)
		}
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"q": "git push"}})
		if err != nil || res.IsError || len(res.Content) != 1 {
			t.Fatalf("%s: search: %v %+v", version, err, res)
		}
		var page recall.SearchPage
		text, _ := res.Content[0].(*mcp.TextContent)
		if text == nil || json.Unmarshal([]byte(text.Text), &page) != nil || len(page.Items) == 0 || !strings.HasPrefix(page.Items[0].Link, "https://lake.example/sessions/"+a) {
			t.Errorf("%s: search result %+v", version, res.Content)
		}
		res, err = cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "read_events", Arguments: map[string]any{"session_uid": a, "limit": 2}})
		if err != nil || res.IsError {
			t.Errorf("%s: read_events: %v %+v", version, err, res)
		}
		if res, err = cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "read_events", Arguments: map[string]any{}}); err != nil || !res.IsError {
			t.Errorf("%s: read_events without a session: %v %+v", version, err, res)
		}
		if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "delete_everything"}); err == nil {
			t.Errorf("%s: an unknown tool was accepted", version)
		}
		if err := cs.Close(); err != nil {
			t.Errorf("%s: close: %v", version, err)
		}
	}
}

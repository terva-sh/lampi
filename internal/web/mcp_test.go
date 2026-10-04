package web

import (
	"encoding/base64"
	"encoding/json"
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

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/testidp"
	"terva.sh/lampi/internal/webconfig"
)

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

// The endpoint answers an initialize-era client and a client that sends
// its version with every request, and refuses what neither allows.
func TestMCPProtocolEras(t *testing.T) {
	lake, idp, h, _, index := mcpLake(t)
	a := seedHarness(t, lake, "codex", "sid-a")
	publishNormalized(t, lake, a, toolEvents("codex", "Bash git push"))
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "agent", "", catalog.PermEventsRead)

	// Initialize era.
	for asked, want := range map[string]string{"2025-06-18": "2025-06-18", "2025-03-26": "2025-03-26", "2024-11-05": "2025-11-25"} {
		reply := decodeRPC(t, postRPC(h, token, rpcBody(1, "initialize", map[string]any{"protocolVersion": asked, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}), nil))
		info, _ := reply.Result["serverInfo"].(map[string]any)
		if reply.Result["protocolVersion"] != want || info["name"] != "terva-lampi" || reply.Result["capabilities"] == nil || reply.Result["resultType"] != nil {
			t.Errorf("initialize %s: %+v", asked, reply)
		}
	}
	if w := postRPC(h, token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, map[string]string{"MCP-Protocol-Version": "2025-06-18"}); w.Code != 202 || w.Body.Len() != 0 {
		t.Errorf("notification: %d %q", w.Code, w.Body.String())
	}
	reply := decodeRPC(t, postRPC(h, token, rpcBody(2, "tools/list", nil), map[string]string{"MCP-Protocol-Version": "2025-06-18"}))
	tools, _ := reply.Result["tools"].([]any)
	var names []string
	for _, tool := range tools {
		m := tool.(map[string]any)
		names = append(names, m["name"].(string))
		ann, _ := m["annotations"].(map[string]any)
		schema, _ := m["inputSchema"].(map[string]any)
		if ann["readOnlyHint"] != true || schema["type"] != "object" {
			t.Errorf("tool %v", m)
		}
	}
	if !slices.Equal(names, []string{"search", "read_events", "copy_excerpt"}) || string(reply.ID) != "2" {
		t.Errorf("tools/list: %v id %s", names, reply.ID)
	}
	if reply := decodeRPC(t, postRPC(h, token, rpcBody(3, "ping", nil), nil)); reply.Error != nil || len(reply.Result) != 0 {
		t.Errorf("ping: %+v", reply)
	}
	if w := postRPC(h, token, rpcBody(4, "resources/list", nil), nil); w.Code != 200 || decodeRPC(t, w).Error.Code != rpcNoMethod {
		t.Errorf("legacy unknown method: %d %s", w.Code, w.Body.String())
	}
	if w := postRPC(h, token, rpcBody(5, "tools/list", nil), map[string]string{"MCP-Protocol-Version": "1999-01-01"}); w.Code != 400 || decodeRPC(t, w).Error.Code != mcpBadVersion {
		t.Errorf("unknown legacy header: %d %s", w.Code, w.Body.String())
	}

	// Per-request metadata.
	meta := map[string]any{"io.modelcontextprotocol/protocolVersion": mcpModern, "io.modelcontextprotocol/clientInfo": map[string]any{"name": "t", "version": "1"}, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	call := rpcBody(6, "tools/call", map[string]any{"name": "search", "arguments": map[string]any{"q": "git push"}, "_meta": meta})
	headers := map[string]string{"MCP-Protocol-Version": mcpModern, "Mcp-Method": "tools/call", "Mcp-Name": "search"}
	for _, name := range []string{"search", "=?base64?" + base64.StdEncoding.EncodeToString([]byte("search")) + "?="} {
		headers["Mcp-Name"] = name
		w := postRPC(h, token, call, headers)
		reply := decodeRPC(t, w)
		if w.Code != 200 || reply.Error != nil || reply.Result["resultType"] != "complete" || reply.Result["isError"] != false {
			t.Errorf("modern call with Mcp-Name %s: %d %s", name, w.Code, w.Body.String())
		}
	}
	for name, bad := range map[string]map[string]string{
		"name mismatch":    {"MCP-Protocol-Version": mcpModern, "Mcp-Method": "tools/call", "Mcp-Name": "read_events"},
		"no method header": {"MCP-Protocol-Version": mcpModern, "Mcp-Name": "search"},
		"no version":       {"Mcp-Method": "tools/call", "Mcp-Name": "search"},
	} {
		if w := postRPC(h, token, call, bad); w.Code != 400 || decodeRPC(t, w).Error.Code != mcpHeaderMismatch {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := postRPC(h, token, rpcBody(7, "tools/list", nil), map[string]string{"MCP-Protocol-Version": mcpModern, "Mcp-Method": "tools/list"}); w.Code != 400 || decodeRPC(t, w).Error.Code != mcpHeaderMismatch {
		t.Errorf("modern header, no _meta: %d %s", w.Code, w.Body.String())
	}
	future := map[string]any{"io.modelcontextprotocol/protocolVersion": "2099-01-01"}
	w := postRPC(h, token, rpcBody(8, "tools/list", map[string]any{"_meta": future}), map[string]string{"MCP-Protocol-Version": "2099-01-01", "Mcp-Method": "tools/list"})
	if reply := decodeRPC(t, w); w.Code != 400 || reply.Error.Code != mcpBadVersion || !strings.Contains(w.Body.String(), `"supported":["2026-07-28","2025-11-25"`) {
		t.Errorf("unsupported modern version: %d %s", w.Code, w.Body.String())
	}
	modern := map[string]string{"MCP-Protocol-Version": mcpModern, "Mcp-Method": "server/discover"}
	reply = decodeRPC(t, postRPC(h, token, rpcBody(9, "server/discover", map[string]any{"_meta": meta}), modern))
	if versions, _ := reply.Result["supportedVersions"].([]any); len(versions) != 4 || versions[0] != mcpModern || reply.Result["resultType"] != "complete" {
		t.Errorf("discover: %+v", reply)
	}
	modern["Mcp-Method"] = "resources/list"
	if w := postRPC(h, token, rpcBody(10, "resources/list", map[string]any{"_meta": meta}), modern); w.Code != 404 || decodeRPC(t, w).Error.Code != rpcNoMethod {
		t.Errorf("modern unknown method: %d %s", w.Code, w.Body.String())
	}

	// What neither era allows.
	for _, method := range []string{"GET", "DELETE"} {
		if w := bearer(h, method, mcpPath, token); w.Code != 405 || w.Header().Get("Allow") != "POST" {
			t.Errorf("%s: %d", method, w.Code)
		}
	}
	if w := postRPC(h, token, rpcBody(11, "ping", nil), map[string]string{"Origin": "https://evil.example"}); w.Code != 403 {
		t.Errorf("foreign origin: %d", w.Code)
	}
	if w := postRPC(h, token, rpcBody(12, "ping", nil), map[string]string{"Origin": "https://lake.example"}); w.Code != 200 {
		t.Errorf("own origin: %d", w.Code)
	}
	for name, body := range map[string]string{
		"batch":      "[" + rpcBody(13, "ping", nil) + "]",
		"not json":   "{",
		"null id":    `{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		"no version": `{"id":1,"method":"ping"}`,
		"too large":  `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", mcpBodyMax) + `"}}`,
	} {
		if w := postRPC(h, token, body, nil); w.Code < 400 || w.Code >= 500 {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/diagnose"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/recall"
)

// diagnosedSession publishes a Claude Code session whose result at
// position 3 is too large for context, and whose content is private.
func diagnosedSession(t *testing.T, lake *api.Server, native string) string {
	t.Helper()
	uid := seedHarness(t, lake, "claude", native)
	evs := toolEvents("claude", "Read secret-notes.md", "Bash ls")
	big := strings.Repeat("private pond water ", 4000)
	name, id := "Read", "toolu_1"
	evs[0].Tool.CallID = &id
	evs[1] = normalize.Event{SchemaVersion: 1, SessionID: "native", Harness: "claude", Actor: normalize.ActorTool, EventType: normalize.EventToolResult,
		RecordedAt: evs[0].RecordedAt, IngestedAt: evs[0].IngestedAt, Tool: normalize.Tool{Name: &name, CallID: &id}, ContentText: &big}
	evs[1], evs[2], evs[3] = evs[2], evs[3], evs[1]
	publishNormalized(t, lake, uid, evs)
	return uid
}

func decodeDiagnosis(t *testing.T, w *httptest.ResponseRecorder) diagnosisReport {
	t.Helper()
	var v diagnosisReport
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatalf("diagnosis %d: %s", w.Code, w.Body.String())
	}
	return v
}

// TKT-01M61MZNR: the read route, the browser route and the MCP tool
// answer one session's diagnosis alike, with a link per position and
// no content; the lake-wide routes and tool group it.
func TestDiagnosisRoutes(t *testing.T) {
	lake, idp, h, dir, _ := mcpLake(t)
	uid := diagnosedSession(t, lake, "sid-diag")
	other := diagnosedSession(t, lake, "sid-other")
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "diag", "", catalog.PermEventsRead)

	w := bearer(h, "GET", "/api/read/v1/sessions/"+uid+"/diagnosis", token)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("cache %q", w.Header().Get("Cache-Control"))
	}
	rep := decodeDiagnosis(t, w)
	if rep.SessionUID != uid || rep.Generation != 1 || rep.Harness != "claude" || len(rep.Findings) != 1 {
		t.Fatalf("%+v", rep)
	}
	f := rep.Findings[0]
	if f.Code != diagnose.LargeToolResult || len(f.Positions) != 1 || f.Positions[0] != 3 || f.Suggestion.Text == "" ||
		len(f.Links) != 1 || f.Links[0] != recall.EventLink(uid, 1, 3) {
		t.Errorf("%+v", f)
	}
	for _, private := range []string{"private pond", "secret-notes", token} {
		if strings.Contains(w.Body.String(), private) {
			t.Errorf("the answer holds %q", private)
		}
	}
	if web := decodeDiagnosis(t, get(h, "/api/web/v1/sessions/"+uid+"/diagnosis", admin)); !reflect.DeepEqual(web, rep) {
		t.Errorf("browser route differs:\n%+v\n%+v", web, rep)
	}
	text, isError := callTool(t, h, token, "diagnose_session", map[string]any{"session_uid": uid})
	var viaMCP diagnosisReport
	if isError || json.Unmarshal([]byte(text), &viaMCP) != nil || viaMCP.Findings[0].Links[0] != "https://lake.example"+f.Links[0] {
		t.Fatalf("mcp: %s", text)
	}
	viaMCP.Findings[0].Links[0] = f.Links[0]
	if !reflect.DeepEqual(viaMCP, rep) {
		t.Errorf("mcp differs:\n%+v\n%+v", viaMCP, rep)
	}

	// Lake-wide: one group of both sessions, alike on every surface.
	var groups recall.DiagnosisGroups
	w = bearer(h, "GET", readDiagnosisPath+"?limit=5", token)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &groups) != nil || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("lake %d %s", w.Code, w.Body.String())
	}
	if groups.Sessions != 2 || groups.Limit != 5 || groups.Truncated || len(groups.Groups) != 1 ||
		groups.Groups[0].Sessions != 2 || groups.Groups[0].Harness != "claude" || len(groups.Groups[0].Examples) != 2 {
		t.Errorf("lake: %+v", groups)
	}
	var web, mcp recall.DiagnosisGroups
	_ = json.Unmarshal(get(h, "/api/web/v1/diagnosis?limit=5", admin).Body.Bytes(), &web)
	text, isError = callTool(t, h, token, "diagnose_lake", map[string]any{"limit": 5})
	if isError || json.Unmarshal([]byte(text), &mcp) != nil {
		t.Fatalf("mcp lake: %s", text)
	}
	groups.AsOf, web.AsOf, mcp.AsOf = "", "", ""
	if !reflect.DeepEqual(web, groups) || !reflect.DeepEqual(mcp, groups) {
		t.Errorf("lake surfaces differ:\n%+v\n%+v\n%+v", groups, web, mcp)
	}
	if g := decodeLake(t, bearer(h, "GET", readDiagnosisPath+"?limit=1&harness=claude", token)); g.Sessions != 1 || g.TruncatedBy != recall.TruncatedLimit {
		t.Errorf("limit 1: %+v", g)
	}
	if g := decodeLake(t, bearer(h, "GET", readDiagnosisPath+"?harness=codex", token)); g.Sessions != 0 || len(g.Groups) != 0 {
		t.Errorf("codex: %+v", g)
	}

	// A session-scoped token reads its session only.
	onlyOther, _ := mintReadTokenWith(t, h, admin, "other only", other, catalog.PermEventsRead)
	if w := bearer(h, "GET", "/api/read/v1/sessions/"+uid+"/diagnosis", onlyOther); w.Code != 404 {
		t.Errorf("outside scope: %d %s", w.Code, w.Body.String())
	}
	if g := decodeLake(t, bearer(h, "GET", readDiagnosisPath, onlyOther)); g.Sessions != 1 || g.Groups[0].Examples[0] != other {
		t.Errorf("session scope: %+v", g)
	}

	log, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(log); !strings.Contains(s, `"diagnosis session=`+uid+` query="`) || !strings.Contains(s, `"diagnosis query=limit=5"`) || strings.Contains(s, token) {
		t.Errorf("audit:\n%s", s)
	}
}

// The transcript page shows the findings with their suggestion and
// links to their events, and still renders when diagnosis runs out of
// time.
func TestTranscriptFindingsPanel(t *testing.T) {
	lake, idp, h, _, _ := mcpLake(t)
	uid := diagnosedSession(t, lake, "sid-diag")
	clean := seedHarness(t, lake, "claude", "sid-clean")
	publishNormalized(t, lake, clean, toolEvents("claude", "Bash ls"))
	admin := signInAs(t, idp, h, "owners")

	body := get(h, "/sessions/"+uid+"/transcript?from=2&limit=1", admin).Body.String()
	link := strings.ReplaceAll(recall.EventLink(uid, 1, 3), "&", "&amp;")
	for _, want := range []string{"Diagnosis: 1 warn", `<span class="badge pending">warn</span> <code>large_tool_result</code>`,
		diagnose.Shipped.Codes[diagnose.LargeToolResult].Suggestion, `<a href="` + link + `">#3</a>`, "max bytes 74.2 KiB"} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q", want)
		}
	}
	if w := get(h, "/sessions/"+clean+"/transcript", admin); w.Code != 200 || !strings.Contains(w.Body.String(), "Diagnosis: no findings") {
		t.Errorf("clean session: %d", w.Code)
	}

	saved := transcriptDiagnosisBudget
	transcriptDiagnosisBudget = 0
	defer func() { transcriptDiagnosisBudget = saved }()
	if w := get(h, "/sessions/"+uid+"/transcript", admin); w.Code != 200 || !strings.Contains(w.Body.String(), "takes longer than the page allows") || !strings.Contains(w.Body.String(), "secret-notes") {
		t.Errorf("out of time: %d", w.Code)
	}
}

func decodeLake(t *testing.T, w *httptest.ResponseRecorder) recall.DiagnosisGroups {
	t.Helper()
	var v recall.DiagnosisGroups
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatalf("lake diagnosis %d: %s", w.Code, w.Body.String())
	}
	return v
}

// The read routes take only a read token with events:read and refuse
// what they do not know, naming the parameter and never its value.
func TestDiagnosisRefusals(t *testing.T) {
	lake, idp, h, _, _ := mcpLake(t)
	uid := diagnosedSession(t, lake, "sid-diag")
	pending := seedHarness(t, lake, "claude", "sid-pending")
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "diag", "", catalog.PermEventsRead)
	raw := mintReadToken(t, h, admin, "raw", "")
	one := "/api/read/v1/sessions/" + uid + "/diagnosis"
	for name, c := range map[string]struct {
		token, path string
		status      int
		body        string
	}{
		"no token":        {"", one, 401, "not_authenticated"},
		"raw-only token":  {raw, one, 404, "not_found"},
		"session query":   {token, one + "?gen=private", 400, `{"error":"invalid_request"}`},
		"unknown session": {token, "/api/read/v1/sessions/nope/diagnosis", 404, "not_found"},
		"not ready":       {token, "/api/read/v1/sessions/" + pending + "/diagnosis", 409, "transcript_unavailable"},
		"unknown key":     {token, readDiagnosisPath + "?text=private", 400, `{"error":"invalid_request"}`},
		"repeated key":    {token, readDiagnosisPath + "?limit=1&limit=2", 400, `"param":"limit"`},
		"limit too high":  {token, readDiagnosisPath + "?limit=1001", 400, `"param":"limit"`},
		"limit zero":      {token, readDiagnosisPath + "?limit=0", 400, `"param":"limit"`},
		"bad date":        {token, readDiagnosisPath + "?since=private", 400, `"param":"since"`},
	} {
		w := bearer(h, "GET", c.path, c.token)
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.body) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private") {
			t.Errorf("%s: the refusal echoes a value", name)
		}
	}
	for _, path := range []string{"/api/web/v1/diagnosis", "/api/web/v1/sessions/" + uid + "/diagnosis"} {
		if w := get(h, path, nil); w.Code != 401 {
			t.Errorf("%s unguarded: %d", path, w.Code)
		}
	}
}

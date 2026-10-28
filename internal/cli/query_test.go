package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// tokenFile writes a read token to a 0600 file and records its hash in
// the lake, as minting on the admin page does.
func tokenFile(t *testing.T, lake *api.Server, perms ...string) string {
	t.Helper()
	secret := web.ReadTokenPrefix + "synthetic-query-token-" + strings.Join(perms, "-")
	sum := sha256.Sum256([]byte(secret))
	if _, err := lake.Catalog.CreateReadToken(t.Context(), catalog.ReadToken{Label: "query test", Permissions: perms, CreatedBy: "test", Expires: time.Now().Add(time.Hour)}, hex.EncodeToString(sum[:]), time.Now()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "read-token")
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TKT-01M3V3KC: query events reads a lake's event stream and writes the
// rows export writes for the same flags.
func TestQueryEventsMatchesExport(t *testing.T) {
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lake.Close()
	text := func(s string) *string { return &s }
	bash, read := "Bash", "Read"
	day := func(d int) string { return time.Date(2026, 9, d, 12, 0, 0, 0, time.UTC).Format(time.RFC3339) }
	publishEvents(t, lake, "codex", "sid-a", "", []normalize.Event{
		{SchemaVersion: 1, Harness: "codex", Actor: "user", EventType: "message", RecordedAt: day(1), ContentText: text("push it")},
		{SchemaVersion: 1, Harness: "codex", Actor: "assistant", EventType: "tool_call", RecordedAt: day(2), Tool: normalize.Tool{Name: &bash}, ContentText: text("git push")},
		{SchemaVersion: 1, Harness: "codex", Actor: "assistant", EventType: "tool_call", RecordedAt: day(3), Tool: normalize.Tool{Name: &read}, ContentText: text("README.md")},
	})
	publishEvents(t, lake, "claude", "sid-b", "", []normalize.Event{
		{SchemaVersion: 1, Harness: "claude", Actor: "assistant", EventType: "tool_call", RecordedAt: day(4), Tool: normalize.Tool{Name: &bash}, ContentText: text("ls")},
	})
	idp := testidp.New()
	defer idp.Close()
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"owners": "admin"}}}
	reg := &web.Registrations{Lake: func() registrar.Lake { return registrar.Lake{Catalog: lake.Catalog, Dir: dir} }, Blobs: lake.CAS}
	if lake.Web, err = web.New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), nil, reg, nil, idp.Client()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(lake.Handler())
	defer srv.Close()
	token := tokenFile(t, lake, catalog.PermEventsRead)
	env := func(out, errw *bytes.Buffer) Env {
		return Env{Stdout: out, Stderr: errw, Getenv: func(k string) string {
			return map[string]string{"XDG_CONFIG_HOME": t.TempDir(), "XDG_STATE_HOME": t.TempDir()}[k]
		}}
	}
	sorted := func(s string) []string {
		lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
		slices.Sort(lines)
		return lines
	}
	for _, flags := range [][]string{
		{"--event-type", "tool_call", "--fields", "harness,tool.name,content_text"},
		{"--tool", "Bash"},
		{"--harness", "codex", "--until", "2026-09-02", "--fields", "content_text"},
		{"--event-type", "tool_call", "--count-by", "tool.name"},
		{"--harness", "codex", "--count-by", "event_type"},
	} {
		var exported bytes.Buffer
		if err := Run(append([]string{"export", "--data", dir}, flags...), Env{Stdout: &exported, Stderr: &bytes.Buffer{}}); err != nil {
			t.Fatal(err)
		}
		var out, errw bytes.Buffer
		if err := Run(append([]string{"query", "events", "--server", srv.URL, "--token-file", token}, flags...), env(&out, &errw)); err != nil {
			t.Fatal(flags, err)
		}
		if got, want := sorted(out.String()), sorted(exported.String()); len(got) == 0 || got[0] == "" || !slices.Equal(got, want) {
			t.Errorf("%q: query %q, export %q", flags, got, want)
		}
		if !strings.Contains(errw.String(), "events from") {
			t.Errorf("no summary on stderr: %q", errw.String())
		}
	}

	outPath := filepath.Join(t.TempDir(), "calls.jsonl")
	if err := Run([]string{"query", "events", "--server", srv.URL, "--token-file", token, "--tool", "Bash", "--out", outPath}, env(&bytes.Buffer{}, &bytes.Buffer{})); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(outPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("--out: %v %v", fi, err)
	}

	raw := tokenFile(t, lake, catalog.PermRawRead)
	var errw bytes.Buffer
	err = Run([]string{"query", "events", "--server", srv.URL, "--token-file", raw, "--tool", "Bash"}, env(&bytes.Buffer{}, &errw))
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "events:read") {
		t.Fatalf("raw-only token: %v", err)
	}
	secret, _ := os.ReadFile(raw)
	if strings.Contains(err.Error(), strings.TrimSpace(string(secret))) || strings.Contains(errw.String(), strings.TrimSpace(string(secret))) {
		t.Fatal("the token appears in the error")
	}
}

// A stream that ends without its end line, says it stopped partway, or
// sends fewer rows than it counted is an error, and --out is not left
// behind.
func TestQueryEventsRefusesAShortStream(t *testing.T) {
	body := map[string]string{
		"cut":     `{"a":1}` + "\n" + `{"a":2}` + "\n",
		"partway": `{"a":1}` + "\n" + `{"lampi:end":{"complete":false,"error":"read_failed","rows":1,"sessions":1,"skipped":0,"oversized":0}}` + "\n",
		"short":   `{"a":1}` + "\n" + `{"lampi:end":{"complete":true,"rows":2,"sessions":1,"skipped":0,"oversized":0}}` + "\n",
		"after":   `{"lampi:end":{"complete":true,"rows":0,"sessions":0,"skipped":0,"oversized":0}}` + "\n" + `{"a":1}` + "\n",
		"ok":      "\n" + `{"a":1}` + "\n\n" + `{"lampi:end":{"complete":true,"rows":1,"sessions":1,"skipped":0,"oversized":0}}` + "\n",
		"big":     `{"lampi:end":{"complete":true,"rows":0,"sessions":1,"skipped":0,"oversized":2}}` + "\n",
	}
	var seenAuth, seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth, seenQuery = r.Header.Get("Authorization"), r.URL.RawQuery
		if r.URL.Path != "/api/read/v1/events" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("tool") {
		case "redirect":
			// To plain http on a host that is not loopback: following
			// it would send the token in the clear.
			http.Redirect(w, r, "http://lake.example/api/read/v1/events", http.StatusFound)
			return
		case "stalled-error":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"err`))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		case "silent":
			w.Write([]byte(`{"a":1}` + "\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		w.Write([]byte(body[r.URL.Query().Get("tool")]))
	}))
	defer srv.Close()
	token := filepath.Join(t.TempDir(), "read-token")
	if err := os.WriteFile(token, []byte(web.ReadTokenPrefix+"synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(case_ string, extra ...string) (string, error) {
		var out bytes.Buffer
		err := Run(append([]string{"query", "events", "--server", srv.URL, "--tool", case_}, extra...), Env{Stdout: &out, Stderr: &bytes.Buffer{}, Getenv: func(k string) string {
			if k == "LAMPI_READ_TOKEN_FILE" {
				return token
			}
			return ""
		}})
		return out.String(), err
	}
	for c, says := range map[string]string{"cut": "ended early, after 2 events", "partway": "stopped partway (read_failed)", "short": "sent 1 events but counted 2", "after": "after the end"} {
		out := filepath.Join(t.TempDir(), "out.jsonl")
		if _, err := run(c, "--out", out); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%s: %v", c, err)
		}
		if entries, _ := os.ReadDir(filepath.Dir(out)); len(entries) != 0 {
			t.Errorf("%s left %v behind", c, entries)
		}
	}
	got, err := run("ok", "--until", "2026-09-02", "--fields", "harness")
	if err != nil || got != `{"a":1}`+"\n" {
		t.Fatalf("complete stream: %q %v", got, err)
	}
	if seenAuth != "Bearer "+web.ReadTokenPrefix+"synthetic" || !strings.Contains(seenQuery, "until=2026-09-02") || !strings.Contains(seenQuery, "fields=harness") {
		t.Fatalf("request %q %q", seenAuth, seenQuery)
	}

	if _, err := run("redirect"); err == nil || !strings.Contains(err.Error(), "302") || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("redirect: %v", err)
	}
	defer func(d time.Duration) { queryIdle = d }(queryIdle)
	queryIdle = 200 * time.Millisecond
	if _, err := run("stalled-error"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("stalled error body: %v", err)
	}
	if _, err := run("silent"); err == nil || !strings.Contains(err.Error(), "sent nothing for 200ms, after 1 events") {
		t.Errorf("silent lake: %v", err)
	}
	var warned bytes.Buffer
	if err := Run([]string{"query", "events", "--server", srv.URL, "--token-file", token, "--tool", "big"}, Env{Stdout: &bytes.Buffer{}, Stderr: &warned, Getenv: func(string) string { return "" }}); err != nil || !strings.Contains(warned.String(), "2 events were over 16 MiB") {
		t.Fatalf("oversized warning: %v %q", err, warned.String())
	}
	for _, c := range []struct {
		args []string
		says string
	}{
		{[]string{"query", "events", "--server", srv.URL, "--token-file", token}, "at least one event filter"},
		{[]string{"query", "events", "--server", srv.URL, "--token-file", token, "--fields", "harness"}, "at least one event filter"},
		{[]string{"query", "events", "--server", srv.URL, "--tool", "x"}, "no read token"},
		{[]string{"query", "events", "--server", "http://lake.example", "--token-file", token, "--tool", "x"}, "plain http"},
		{[]string{"query", "events", "--server", srv.URL, "--token-file", filepath.Join(t.TempDir(), "none"), "--tool", "x"}, "read token"},
		{[]string{"query", "events", "--lake", "a", "--server", srv.URL, "--tool", "x"}, "pass one"},
		{[]string{"query", "sessions"}, `unknown query "sessions"`},
		{[]string{"query", "events", "--server", srv.URL, "--token-file", token, "--tool", "x", "--count-by", "content_text"}, "free text"},
		{[]string{"query", "events", "--server", srv.URL, "--token-file", token, "--tool", "x", "--count-by", "tool.name", "--fields", "harness"}, "leave out --fields"},
	} {
		err := Run(c.args, Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getenv: func(string) string { return "" }})
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%q: %v", c.args, err)
		}
	}
	bad := filepath.Join(t.TempDir(), "device-token")
	if err := os.WriteFile(bad, []byte(strings.Repeat("d", 64)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"query", "events", "--server", srv.URL, "--token-file", bad, "--tool", "x"}, Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getenv: func(string) string { return "" }}); err == nil || strings.Contains(err.Error(), strings.Repeat("d", 64)) || !strings.Contains(err.Error(), "does not hold a read token") {
		t.Fatalf("device token file: %v", err)
	}
}

// TKT-01M61MZNC: query tools writes the lake's tool statistics as
// JSONL, one key a line, with the totals on stderr.
func TestQueryTools(t *testing.T) {
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lake.Close()
	text := func(s string) *string { return &s }
	day := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	bash, mcpRead, id1, id2 := "Bash", "mcp__pond__read", "c1", "c2"
	publishEvents(t, lake, "claude", "sid-a", "", []normalize.Event{
		{SchemaVersion: 1, Harness: "claude", Actor: "assistant", EventType: "tool_call", RecordedAt: day, Tool: normalize.Tool{Name: &bash, CallID: &id1}, ContentText: text("git push")},
		{SchemaVersion: 1, Harness: "claude", Actor: "tool", EventType: "tool_result", RecordedAt: day, Tool: normalize.Tool{Name: &bash, CallID: &id1}, ContentText: text("Everything up-to-date")},
		{SchemaVersion: 1, Harness: "claude", Actor: "assistant", EventType: "tool_call", RecordedAt: day, Tool: normalize.Tool{Name: &mcpRead, CallID: &id2}, ContentText: text("{}")},
		// Large enough for a diagnosis finding.
		{SchemaVersion: 1, Harness: "claude", Actor: "tool", EventType: "tool_result", RecordedAt: day, Tool: normalize.Tool{Name: &mcpRead, CallID: &id2}, ContentText: text(strings.Repeat("private pond water ", 4000))},
	})
	reader := recall.NewReader(lake.Catalog, lake.Normalized)
	index, err := recall.OpenIndex(filepath.Join(t.TempDir(), recall.IndexFile), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	idp := testidp.New()
	defer idp.Close()
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"owners": "admin"}}}
	reg := &web.Registrations{Lake: func() registrar.Lake { return registrar.Lake{Catalog: lake.Catalog, Dir: dir} }, Blobs: lake.CAS}
	if lake.Web, err = web.New(cfg, lake.Catalog, reader, index, reg, nil, idp.Client()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(lake.Handler())
	defer srv.Close()
	token := tokenFile(t, lake, catalog.PermEventsRead)
	env := func(out, errw *bytes.Buffer) Env {
		return Env{Stdout: out, Stderr: errw, Getenv: func(k string) string {
			return map[string]string{"XDG_CONFIG_HOME": t.TempDir(), "XDG_STATE_HOME": t.TempDir()}[k]
		}}
	}
	sub := "tools"
	run := func(args ...string) (map[string][]map[string]any, string, error) {
		var out, errw bytes.Buffer
		err := Run(append([]string{"query", sub, "--server", srv.URL, "--token-file", token}, args...), env(&out, &errw))
		lines := map[string][]map[string]any{}
		for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			var m map[string]map[string]any
			if l == "" {
				continue
			}
			if json.Unmarshal([]byte(l), &m) != nil || len(m) != 1 {
				t.Fatalf("not one key a line: %q", l)
			}
			for k, v := range m {
				lines[k] = append(lines[k], v)
			}
		}
		return lines, errw.String(), err
	}

	lines, stderr, err := run("--all", "--largest", "2", "--context")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines["summary"]) != 1 || len(lines["group"]) != 2 || len(lines["largest"]) != 2 || len(lines["context"]) != 1 {
		t.Fatalf("lines %v", lines)
	}
	if totals, _ := lines["summary"][0]["totals"].(map[string]any); totals["calls"] != 2.0 || totals["sessions"] != 1.0 {
		t.Errorf("summary %v", lines["summary"][0])
	}
	if c := lines["context"][0]; c["kind"] != recall.ToolContextKind || c["note"] == "" {
		t.Errorf("context %v", c)
	}
	if !strings.Contains(stderr, "2 tool calls and 2 results in 1 sessions, every recorded time; 2 groups") || !strings.Contains(stderr, "correlation hint") {
		t.Errorf("stderr %q", stderr)
	}
	lines, _, err = run("--by", "origin", "--origin", "mcp", "--since", "2026-09-01", "--until", "2026-09-03")
	if err != nil || len(lines["group"]) != 1 || lines["group"][0]["origin"].(map[string]any)["name"] != "pond" {
		t.Errorf("by origin: %v %v", lines, err)
	}
	if lines, _, err = run(); err != nil || lines["summary"][0]["since"] == nil || len(lines["group"]) != 0 {
		t.Errorf("default range: %v %v", lines, err)
	}

	outPath := filepath.Join(t.TempDir(), "tools.jsonl")
	if _, _, err := run("--all", "--out", outPath); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(outPath); err != nil || strings.Contains(string(b), "private pond water") || !strings.Contains(string(b), `{"group":`) {
		t.Errorf("--out: %s %v", b, err)
	}
	if fi, err := os.Stat(outPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("--out mode: %v %v", fi, err)
	}

	if _, _, err := run("--origin", "server"); err == nil || !strings.Contains(err.Error(), "refused the origin value") {
		t.Errorf("bad origin: %v", err)
	}
	if _, _, err := run("--all", "--since", "2026-09-01"); err == nil || !strings.Contains(err.Error(), "refused the all value") {
		t.Errorf("all with since: %v", err)
	}
	raw := tokenFile(t, lake, catalog.PermRawRead)
	var errw bytes.Buffer
	err = Run([]string{"query", "tools", "--server", srv.URL, "--token-file", raw}, env(&bytes.Buffer{}, &errw))
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "events:read") {
		t.Errorf("raw-only token: %v", err)
	}

	// TKT-01M61MZNR: query diagnose writes the lake-wide groups, and one
	// session's findings, as JSONL.
	sub = "diagnose"
	lines, stderr, err = run("--since", "2020-01-01", "--limit", "5")
	if err != nil || len(lines["summary"]) != 1 || len(lines["group"]) != 1 || lines["summary"][0]["sessions"] != 1.0 ||
		!strings.Contains(stderr, "1 groups from 1 sessions updated since 2020-01-01T00:00:00Z") {
		t.Fatalf("lake-wide: %v %q %v", lines, stderr, err)
	}
	g := lines["group"][0]
	uid, _ := g["example_sessions"].([]any)[0].(string)
	if g["code"] != "large_tool_result" || g["harness"] != "claude" || uid == "" {
		t.Errorf("group %v", g)
	}
	lines, stderr, err = run(uid)
	if err != nil || len(lines["summary"]) != 1 || len(lines["finding"]) != 1 || lines["summary"][0]["session_uid"] != uid ||
		!strings.Contains(stderr, "1 findings in 4 events of "+uid) {
		t.Fatalf("one session: %v %q %v", lines, stderr, err)
	}
	if f := lines["finding"][0]; f["code"] != "large_tool_result" || len(f["links"].([]any)) != 1 || !strings.HasPrefix(f["links"].([]any)[0].(string), "/sessions/"+uid+"/transcript?") {
		t.Errorf("finding %v", f)
	}
	var out bytes.Buffer
	if err := Run([]string{"query", "diagnose", uid, "--server", srv.URL, "--token-file", token}, env(&out, &bytes.Buffer{})); err != nil || strings.Contains(out.String(), "private pond") || !strings.Contains(out.String(), `{"finding":`) {
		t.Errorf("uid first: %v %s", err, out.String())
	}
	if _, _, err := run(uid, "--limit", "2"); err == nil || !strings.Contains(err.Error(), "lake-wide view") {
		t.Errorf("uid with --limit: %v", err)
	}
	if _, _, err := run("--limit", "1001"); err == nil || !strings.Contains(err.Error(), "refused the limit value") {
		t.Errorf("limit: %v", err)
	}
	if _, _, err := run("nope"); err == nil || !strings.Contains(err.Error(), "404 not_found") {
		t.Errorf("unknown session: %v", err)
	}
}

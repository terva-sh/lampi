package web

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/toolorigin"
)

func nc(harness, name string, n int64) recall.NameCount {
	return recall.NameCount{Harness: harness, Name: name, N: n}
}

func TestToolCategory(t *testing.T) {
	for _, c := range []struct{ harness, name, want string }{
		{"claude", "Bash", "shell"}, {"terva", "bash", "shell"}, {"codex", "exec_command", "shell"}, {"codex", "exec", "script"},
		{"claude", "Edit", "edit"}, {"codex", "apply_patch", "edit"}, {"claude", "Read", "read"}, {"opencode", "read_file", "read"},
		{"claude", "WebFetch", "web"}, {"claude", "Agent", "agents"}, {"codex", "spawn_agent", "agents"}, {"claude", "TodoWrite", "plan"},
		{"claude", "mcp__t3-code__link_pull_request", "mcp:t3-code"}, {"codex", "mcp__pond__read", "mcp:pond"},
		{"claude", "mcp__plugin_kit_docs__search", "plugin:kit"},
		// Cursor CLI's own tools in an older projection read by their bare name.
		{"cursor-cli", "mcp__cursor__TodoWrite", "plan"}, {"cursor-cli", "mcp__cursor__AwaitShell", "AwaitShell"},
		{"claude", "mcp__", "mcp__"}, {"terva", "terva_status", "terva_status"}, {"claude", "", "(unnamed)"},
	} {
		if got := toolCategory(c.harness, c.name); got != c.want {
			t.Errorf("toolCategory(%q, %q) = %q, want %q", c.harness, c.name, got, c.want)
		}
	}
}

// Names from different harnesses fall into one category, with the
// failed results of each, and the matrix keeps the harnesses apart.
func TestBuildToolsViewGroupsAcrossHarnesses(t *testing.T) {
	v := buildToolsView(
		[]recall.NameCount{nc("claude", "Bash", 80), nc("terva", "bash", 15), nc("codex", "exec", 30), nc("claude", "Read", 20), nc("codex", "custom_thing", 1)},
		[]recall.NameCount{nc("claude", "Bash", 8), nc("terva", "bash", 2)},
	)
	if len(v.Rows) != 4 || v.Rows[0].Category != "shell" || v.Rows[0].Calls != "95" || v.Rows[0].Failed != "10" || v.Rows[0].FailRate != "10.5%" {
		t.Fatalf("rows: %+v", v.Rows)
	}
	if strings.Join(v.Rows[0].Names, ",") != "claude Bash,terva bash" {
		t.Fatalf("shell names: %v", v.Rows[0].Names)
	}
	if v.Rows[3].Category != "custom_thing" || v.Calls != "146" || v.Distinct != "4" {
		t.Fatalf("totals: %+v", v)
	}
	if strings.Join(v.Harnesses, ",") != "claude,codex,terva" || strings.Join(v.Matrix[0].Cells, ",") != "80,—,15" {
		t.Fatalf("matrix: %v %+v", v.Harnesses, v.Matrix)
	}
}

// A category with more names than a row shows keeps the rest, busiest
// first, rather than counting calls it cannot name.
func TestBuildToolsViewKeepsEveryName(t *testing.T) {
	var calls []recall.NameCount
	for i, name := range []string{"Bash", "bash", "exec_command", "shell", "local_shell_call", "run_terminal_cmd", "terminal", "write_stdin"} {
		calls = append(calls, nc("h", name, int64(100-i)))
	}
	v := buildToolsView(calls, []recall.NameCount{nc("h", "write_stdin", 5)})
	r := v.Rows[0]
	if len(r.Names) != toolNamesShown || strings.Join(r.More, ",") != "h terminal,h write_stdin" {
		t.Fatalf("names %v, more %v", r.Names, r.More)
	}
}

func TestUsageToolsTab(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid, _ := storeSession(t, lake, "native-tools", []byte("{}\n"))
	at := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	ingested := time.Now().UTC().Format(time.RFC3339Nano)
	name, id, mcp, mcpID := "apply_patch", "call-1", "mcp__pond__read", "call-2"
	result := strings.Repeat("pond water ", 400)
	publishNormalized(t, lake, uid, []normalize.Event{
		{SchemaVersion: 1, EventID: "c", SessionID: "s", Harness: "codex", RecordedAt: at, IngestedAt: ingested,
			Actor: normalize.ActorAssistant, EventType: normalize.EventToolCall, Tool: normalize.Tool{Name: &name, CallID: &id}, Redaction: normalize.Redaction{Status: "none"}},
		{SchemaVersion: 1, EventID: "m", SessionID: "s", Harness: "codex", RecordedAt: at, IngestedAt: ingested,
			Actor: normalize.ActorAssistant, EventType: normalize.EventToolCall, Tool: normalize.Tool{Name: &mcp, CallID: &mcpID}, Redaction: normalize.Redaction{Status: "none"}},
		{SchemaVersion: 1, EventID: "r", SessionID: "s", Harness: "codex", RecordedAt: at, IngestedAt: ingested, ContentText: &result,
			Actor: normalize.ActorTool, EventType: normalize.EventToolResult, Tool: normalize.Tool{Name: &mcp, CallID: &mcpID}, Redaction: normalize.Redaction{Status: "none"}},
	})
	if err := indexes[lake].Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/usage/tools?range=24h", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"<h2>Tool calls</h2>", `<span class="mono">edit</span>`, `<span class="sub mono">codex apply_patch</span>`, `<span class="mono">mcp:pond</span>`,
		"Across harnesses", `href="/usage/tools?range=24h" aria-current="page"`,
		"<h2>By origin</h2>", "<th scope=\"row\">MCP server pond</th><td class=\"num\">1</td>", "50.0%", "100.0%",
		"<h2>Result sizes and latency</h2>", "4.3 KiB", "<h2>Largest blocks</h2>", `href="/sessions/` + uid, "Starting context by origin", "Correlation hint, not a finding"} {
		if !strings.Contains(body, want) {
			t.Fatalf("tools tab lacks %q", want)
		}
	}
	// Names, counts and sizes only: the result's text stays in the transcript.
	if strings.Contains(body, "pond water") {
		t.Fatal("tools tab shows a tool result's content")
	}
}

// The detail shares are taken within a class, so a Codex inner call
// does not dilute the context results, and a waiting tool's latency is
// labelled rather than shown.
func TestBuildToolDetailView(t *testing.T) {
	rate := 0.25
	st := recall.ToolStats{
		Totals: recall.ToolTotals{Sessions: 3},
		Groups: []recall.ToolGroup{
			{Tool: "Read", Origin: toolorigin.Origin{Kind: toolorigin.Builtin}, Calls: 30, Output: recall.Sizes{Count: 30, Total: 1000, P50: 20, P95: 90, Max: 100}, Latency: &recall.Latency{P50: 40, P95: 1500}},
			{Tool: "mcp__pond__read", Origin: toolorigin.Origin{Kind: toolorigin.MCP, Name: "pond"}, Calls: 10, Errors: 1, ErrorKnown: 4, ErrorRate: &rate, Output: recall.Sizes{Count: 10, Total: 3000, P50: 300, P95: 900, Max: 1024}},
			{Tool: "AskUserQuestion", Origin: toolorigin.Origin{Kind: toolorigin.Builtin}, Calls: 10, LatencyNote: recall.LatencyWaitsOnPerson},
			{Tool: "clock.sleep", Origin: toolorigin.Origin{Kind: toolorigin.Extension, Name: "clock"}, Inner: true, Calls: 5, Output: recall.Sizes{Count: 5, Total: 50}},
		},
	}
	d := buildToolDetailView(st)
	if len(d.Origins) != 3 || d.Origins[0].Label != "MCP server pond" || d.Origins[0].CallPercent != "20.0%" || d.Origins[0].BytePercent != "75.0%" {
		t.Fatalf("origins: %+v", d.Origins)
	}
	if o := d.Origins[1]; o.Label != "built in" || o.Calls != "40" || o.CallPercent != "80.0%" || o.BytePercent != "25.0%" {
		t.Fatalf("built in: %+v", o)
	}
	if o := d.Origins[2]; !o.Inner || o.CallPercent != "100.0%" || o.BytePercent != "100.0%" {
		t.Fatalf("inner: %+v", o)
	}
	if r := d.Sizes[0]; r.Name != "mcp__pond__read" || r.Errors != "25.0%" || r.ErrorTitle != "1 of 4 results with an error flag" || r.Sizes != "300 B · 900 B · 1.0 KiB" || r.Latency != "not recorded" {
		t.Fatalf("pond row: %+v", r)
	}
	if r := d.Sizes[1]; r.Name != "Read" || r.Latency != "40 ms · 1.5 s" {
		t.Fatalf("read row: %+v", r)
	}
	if r := d.Sizes[2]; r.Name != "clock.sleep" || !r.Inner {
		t.Fatalf("inner row sorts by its bytes: %+v", r)
	}
	if r := d.Sizes[3]; r.Name != "AskUserQuestion" || r.Latency != "waits on a person" || r.Sizes != "none" {
		t.Fatalf("ask row: %+v", r)
	}
}

// The size table's cutoff keeps the tools with the most result bytes,
// an inner tool among them, and hides only tools with fewer (review
// 2919).
func TestBuildToolDetailViewKeepsLargestInner(t *testing.T) {
	var st recall.ToolStats
	for i := range toolDetailRows + 1 {
		st.Groups = append(st.Groups, recall.ToolGroup{Tool: fmt.Sprintf("tool%02d", i), Origin: toolorigin.Origin{Kind: toolorigin.Builtin},
			Calls: 1, Output: recall.Sizes{Count: 1, Total: int64(100 + i)}})
	}
	st.Groups = append(st.Groups, recall.ToolGroup{Tool: "pond.query", Origin: toolorigin.Origin{Kind: toolorigin.Extension, Name: "pond"}, Inner: true,
		Calls: 1, Output: recall.Sizes{Count: 1, Total: 1 << 20}})
	d := buildToolDetailView(st)
	if len(d.Sizes) != toolDetailRows || d.Hidden != 2 || d.Sizes[0].Name != "pond.query" || !d.Sizes[0].Inner {
		t.Fatalf("hidden %d, first %+v", d.Hidden, d.Sizes[0])
	}
	if last := d.Sizes[len(d.Sizes)-1]; last.Name != "tool02" {
		t.Fatalf("last kept %q, want tool02", last.Name)
	}
}

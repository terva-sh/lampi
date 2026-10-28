package web

import (
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/recall"
)

func nc(harness, name string, n int64) recall.NameCount {
	return recall.NameCount{Harness: harness, Name: name, N: n}
}

func TestToolCategory(t *testing.T) {
	for name, want := range map[string]string{
		"Bash": "shell", "bash": "shell", "exec_command": "shell", "exec": "script",
		"Edit": "edit", "apply_patch": "edit", "Read": "read", "read_file": "read",
		"WebFetch": "web", "Agent": "agents", "spawn_agent": "agents", "TodoWrite": "plan",
		"mcp__t3-code__link_pull_request": "mcp:t3-code", "mcp__": "mcp__", "terva_status": "terva_status", "": "(unnamed)",
	} {
		if got := toolCategory(name); got != want {
			t.Errorf("toolCategory(%q) = %q, want %q", name, got, want)
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

func TestUsageToolsTab(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid, _ := storeSession(t, lake, "native-tools", []byte("{}\n"))
	at := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	name, id := "apply_patch", "call-1"
	publishNormalized(t, lake, uid, []normalize.Event{{SchemaVersion: 1, EventID: "c", SessionID: "s", Harness: "codex", RecordedAt: at, IngestedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Actor: normalize.ActorAssistant, EventType: normalize.EventToolCall, Tool: normalize.Tool{Name: &name, CallID: &id}, Redaction: normalize.Redaction{Status: "none"}}})
	if err := indexes[lake].Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/usage/tools?range=24h", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"<h2>Tool calls</h2>", `<span class="mono">edit</span>`, `<span class="sub mono">codex apply_patch</span>`, "Across harnesses", `href="/usage/tools?range=24h" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("tools tab lacks %q", want)
		}
	}
}

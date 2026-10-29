package web

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/toolorigin"
)

// toolCategories maps a tool name, lowercased, to the category it is
// shown under, so the same kind of tool reads the same across harnesses
// (TKT-01M61NTM2ZDBCKGA08BJKYM4Q1). It is applied when the page is read:
// changing it needs no reprojection, and a name it does not know shows
// as itself. The ticket records where the names came from.
var toolCategories = map[string]string{
	// Running commands.
	"bash": "shell", "shell": "shell", "exec_command": "shell", "local_shell_call": "shell", "run_terminal_cmd": "shell",
	"run_terminal_command": "shell", "terminal": "shell", "write_stdin": "shell", "bashoutput": "shell", "killshell": "shell",
	// Codex code mode: a script whose body calls other tools.
	"exec": "script",
	// Reading, writing and changing files.
	"read": "read", "read_file": "read", "view": "read", "notebookread": "read", "view_image": "read",
	"write": "write", "write_file": "write", "create_file": "write",
	"edit": "edit", "multiedit": "edit", "edit_file": "edit", "apply_patch": "edit", "str_replace": "edit",
	"str_replace_editor": "edit", "search_replace": "edit", "notebookedit": "edit", "patch": "edit",
	// Finding files and text.
	"grep": "search", "glob": "search", "ls": "search", "list": "search", "list_dir": "search", "find": "search",
	"codebase_search": "search", "file_search": "search", "grep_search": "search",
	// The web.
	"webfetch": "web", "websearch": "web", "web_search": "web", "web_search_call": "web", "web_fetch": "web", "fetch": "web",
	// Other agents.
	"agent": "agents", "task": "agents", "sendmessage": "agents", "listagents": "agents", "taskstop": "agents",
	"spawn_agent": "agents", "send_message": "agents", "wait_agent": "agents", "list_agents": "agents", "followup_task": "agents",
	// Plans and to-do lists.
	"todowrite": "plan", "todoread": "plan", "update_plan": "plan", "todo_write": "plan",
	// Questions to the user.
	"askuserquestion": "ask", "ask_user_question": "ask", "request_user_input": "ask", "request_user_input_async": "ask",
}

// toolCategory is the category name, called in harness, is shown
// under. toolorigin places it first, as the tool statistics do: an MCP
// server's tool is "mcp:SERVER" and a Claude Code plugin's
// "plugin:PLUGIN". A built-in tool in an MCP-shaped name, such as Cursor
// CLI's older mcp__cursor__AwaitShell, is read by its bare name. Then
// comes its entry in toolCategories, or the name itself.
func toolCategory(harness, name string) string {
	switch o := toolorigin.Of(harness, name); o.Kind {
	case toolorigin.MCP:
		return "mcp:" + o.Name
	case toolorigin.Plugin:
		return "plugin:" + o.Name
	case toolorigin.Builtin:
		if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
			_, name, _ = strings.Cut(rest, "__")
		}
	}
	if c, ok := toolCategories[strings.ToLower(name)]; ok {
		return c
	}
	if name == "" {
		return "(unnamed)"
	}
	return name
}

// toolsView is the Usage page's Tools tab.
type toolsView struct {
	Calls, Distinct, Failed, FailRate string
	Rows                              []toolRow
	// Harnesses heads the matrix's columns; each row's Cells follow
	// them.
	Harnesses []string
	Matrix    []toolMatrixRow
	// Detail is what the tool statistics engine adds.
	Detail toolDetailView
}

// toolRow is one category: its calls, failed results and the names that
// fell into it.
type toolRow struct {
	Category, Calls, Failed, FailRate string
	N, Max                            int64
	// Names are the busiest harness and name pairs counted. More is
	// the rest, which the page folds away but still lists, so every
	// call in the row has a name to it.
	Names, More []string
}

type toolMatrixRow struct {
	Category string
	Cells    []string
}

// toolTopRows bounds the categories listed, toolMatrixRows the rows
// of the matrix, and toolNamesShown the names a row shows unfolded.
const (
	toolTopRows    = 25
	toolMatrixRows = 12
	toolNamesShown = 6
)

type toolAgg struct {
	calls, failed int64
	names         map[string]int64
	byHarness     map[string]int64
}

func (s *Server) buildTools(ctx context.Context, v *usagePageView, scope catalog.Scope, series []recall.SeriesBucket, from, until time.Time, hourly bool) error {
	calls, err := s.index.Breakdown(ctx, scope, v.Harness, from, until, recall.StatToolCall)
	if err != nil {
		return err
	}
	failed, err := s.index.Breakdown(ctx, scope, v.Harness, from, until, recall.StatToolFailed)
	if err != nil {
		return err
	}
	tv := buildToolsView(calls, failed)
	starts := make([]time.Time, len(series))
	callsPer := make([]*int64, len(series))
	failedPer := make([]*int64, len(series))
	for i, b := range series {
		n, f := b.ToolCalls, b.ToolsFailed
		starts[i], callsPer[i], failedPer[i] = b.Start, &n, &f
	}
	v.Charts = []chartView{
		buildChart("tool-calls", "Tool calls", starts, callsPer, hourly, humanCount, niceCeil),
		buildChart("tool-failed", "Failed tool results", starts, failedPer, hourly, humanCount, niceCeil),
	}
	v.Tools = tv
	return s.buildToolDetail(ctx, v, scope, from, until)
}

// buildToolsView groups the counts by category, busiest first.
func buildToolsView(calls, failed []recall.NameCount) toolsView {
	agg := map[string]*toolAgg{}
	get := func(harness, name string) *toolAgg {
		c := toolCategory(harness, name)
		a := agg[c]
		if a == nil {
			a = &toolAgg{names: map[string]int64{}, byHarness: map[string]int64{}}
			agg[c] = a
		}
		return a
	}
	harnesses := map[string]bool{}
	var nCalls, nFailed int64
	for _, c := range calls {
		a := get(c.Harness, c.Name)
		a.calls += c.N
		a.names[c.Harness+" "+cmp.Or(c.Name, "(unnamed)")] += c.N
		a.byHarness[c.Harness] += c.N
		harnesses[c.Harness] = true
		nCalls += c.N
	}
	for _, f := range failed {
		get(f.Harness, f.Name).failed += f.N
		nFailed += f.N
	}
	cats := make([]string, 0, len(agg))
	for c := range agg {
		cats = append(cats, c)
	}
	slices.SortFunc(cats, func(a, b string) int {
		return cmp.Or(cmp.Compare(agg[b].calls, agg[a].calls), cmp.Compare(agg[b].failed, agg[a].failed), cmp.Compare(a, b))
	})
	tv := toolsView{Calls: humanCount(nCalls), Distinct: humanCount(int64(len(cats))), Failed: humanCount(nFailed), FailRate: "—"}
	if nCalls > 0 {
		tv.FailRate = percent(nFailed, nCalls)
	}
	var top int64
	if len(cats) > 0 {
		top = agg[cats[0]].calls
	}
	for i, c := range cats {
		if i == toolTopRows {
			break
		}
		a := agg[c]
		row := toolRow{Category: c, Calls: humanCount(a.calls), Failed: humanCount(a.failed), FailRate: "—", N: a.calls, Max: top}
		if a.calls > 0 {
			row.FailRate = percent(a.failed, a.calls)
		}
		names := make([]string, 0, len(a.names))
		for n := range a.names {
			names = append(names, n)
		}
		slices.SortFunc(names, func(x, y string) int { return cmp.Or(cmp.Compare(a.names[y], a.names[x]), cmp.Compare(x, y)) })
		row.Names, row.More = names[:min(len(names), toolNamesShown)], names[min(len(names), toolNamesShown):]
		tv.Rows = append(tv.Rows, row)
	}
	for h := range harnesses {
		tv.Harnesses = append(tv.Harnesses, h)
	}
	slices.Sort(tv.Harnesses)
	for i, c := range cats {
		if i == toolMatrixRows {
			break
		}
		row := toolMatrixRow{Category: c}
		for _, h := range tv.Harnesses {
			cell := "—"
			if n := agg[c].byHarness[h]; n > 0 {
				cell = humanCount(n)
			}
			row.Cells = append(row.Cells, cell)
		}
		tv.Matrix = append(tv.Matrix, row)
	}
	return tv
}

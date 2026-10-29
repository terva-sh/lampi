package recall

import (
	"fmt"
	"path/filepath"
	"testing"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/toolorigin"
)

// attachment is a Claude Code attachment line as the normalizer keeps
// it: an unknown event with the line in extra.attachment. The shapes
// are synthetic, after the key names of current transcripts.
func attachment(harness string, att map[string]any) normalize.Event {
	ev := toolSession(harness, tev{typ: normalize.EventUnknown, raw: "attachment", at: 0})[0]
	ev.EventID = fmt.Sprint("att-", att["type"])
	ev.Extra = map[string]any{"attachment": att}
	return ev
}

func promptSnapshot(names ...string) map[string]any {
	tools := []any{}
	for _, n := range names {
		tools = append(tools, map[string]any{"name": n, "description": "d", "schema": map[string]any{}})
	}
	return map[string]any{"type": "prompt_snapshot", "tools": tools}
}

// A session that recorded its tool list uses the origins it was
// offered, called or not; any other session the origins it called.
func TestToolStatsContextOffered(t *testing.T) {
	s := lake(t)
	add := func(harness, native string, start int, extra []normalize.Event, calls ...string) {
		t.Helper()
		uid := ingestAs(t, s, harness, native, "")
		evs := []tev{usage(0, 1000, start, 1000)}
		for i, c := range calls {
			evs = append(evs, call(c, fmt.Sprint("m", i), 4, 10))
		}
		publish(t, s, uid, append(toolSession(harness, evs...), extra...))
	}
	docs := []normalize.Event{attachment("claude", promptSnapshot()), attachment("claude", promptSnapshot("Read", "mcp__docs__search", "mcp__docs__fetch"))}
	for i := range 6 {
		var calls []string
		if i < 2 {
			calls = []string{"mcp__docs__search"}
		}
		add("claude", fmt.Sprint("listed", i), 20000+i, docs, calls...)
	}
	add("claude", "delta", 20006, []normalize.Event{attachment("claude", map[string]any{"type": "deferred_tools_delta",
		"addedNames": []any{"mcp__docs__search"}, "readdedNames": []any{"mcp__web__get"}, "removedNames": []any{"mcp__gone__x"}})})
	add("claude", "unlisted", 20007, nil, "mcp__docs__search")
	for i := range 5 {
		add("claude", fmt.Sprint("plain", i), 8000+i, []normalize.Event{attachment("claude", promptSnapshot("Read")),
			attachment("claude", map[string]any{"type": "some_future_type", "tools": []any{map[string]any{"name": "mcp__docs__search"}}})})
	}
	// Only Claude Code's lines are read as tool lists.
	add("codex", "codex", 5000, []normalize.Event{attachment("codex", promptSnapshot("mcp__docs__search"))})
	x := openIndex(t, s)
	pass(t, x)
	c := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Context: true}).Context
	if c == nil || len(c.Harnesses) != 2 {
		t.Fatalf("context %+v", c)
	}
	if h := c.Harnesses[0]; h.Harness != "claude" || h.All.Sessions != 13 || h.OfferedSessions != 12 {
		t.Fatalf("claude %+v", h)
	}
	if h := c.Harnesses[1]; h.Harness != "codex" || h.OfferedSessions != 0 {
		t.Fatalf("codex %+v", h)
	}
	rows := map[string]ContextRow{}
	for _, r := range c.Rows {
		rows[r.Harness+"/"+r.Origin.Name] = r
	}
	if len(rows) != 2 {
		t.Fatalf("rows %+v", c.Rows)
	}
	r := rows["claude/docs"]
	if r.Origin != (toolorigin.Origin{Kind: toolorigin.MCP, Name: "docs"}) || r.With.Sessions != 8 || r.Without.Sessions != 5 || r.Suppressed {
		t.Fatalf("docs %+v", r)
	}
	// Seven lists offered docs, five of them to a session that never
	// called it; nearest-rank median of six twos and a one.
	if r.Offered.Sessions != 7 || r.Offered.Uncalled != 5 || r.Offered.Tools == nil || *r.Offered.Tools != 2 {
		t.Fatalf("docs offered %+v", r.Offered)
	}
	if *r.Without.Median != 1000+8002+1000 {
		t.Fatalf("without %d", *r.Without.Median)
	}
	if w := rows["claude/web"]; w.With.Sessions != 1 || w.Offered.Sessions != 1 || w.Offered.Uncalled != 1 || !w.Suppressed {
		t.Fatalf("web %+v", w)
	}
	if x := rows["codex/docs"]; x.Offered.Sessions != 0 || x.Offered.Tools != nil {
		t.Fatalf("codex row %+v", x)
	}
}

// A version 7 index is upgraded in place: its rows are kept, and the
// next pass reads each session again and writes its offered tools.
func TestVersion7IndexUpgradesInPlace(t *testing.T) {
	s := lake(t)
	uid := ingestAs(t, s, "claude", "v7", "")
	publish(t, s, uid, append(toolSession("claude", usage(0, 1000, 2000, 0)), attachment("claude", promptSnapshot("mcp__docs__search"))))
	path := filepath.Join(t.TempDir(), IndexFile)
	x, err := OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	pass(t, x)
	before := rowIDs(t, x, uid)
	if _, err := x.db.Exec(`DELETE FROM stats WHERE kind='tool_offered'; PRAGMA user_version=7;`); err != nil {
		t.Fatal(err)
	}
	x.Close()
	x, err = OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close() })
	pass(t, x)
	var v, n int
	if err := x.db.QueryRow(`SELECT (SELECT user_version FROM pragma_user_version), (SELECT count(*) FROM stats WHERE kind='tool_offered')`).Scan(&v, &n); err != nil || v != indexVersion || n != 1 {
		t.Fatalf("version %d, offered rows %d (%v)", v, n, err)
	}
	after := rowIDs(t, x, uid)
	for pos, id := range before {
		if after[pos] != id {
			t.Fatalf("row %d was rewritten by the upgrade", pos)
		}
	}
}

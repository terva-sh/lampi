package recall

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

// rowIDs is uid's row id by position.
func rowIDs(t *testing.T, x *Index, uid string) map[int64]int64 {
	t.Helper()
	sigs, err := x.rowSigs(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]int64{}
	for pos, r := range sigs {
		out[pos] = r.id
	}
	return out
}

// A new generation rewrites only the rows that changed: a session that
// grew keeps its earlier rows and gains the new ones, a changed event
// is replaced, and a shorter generation drops the rows past its end
// (TKT-01M3KC2DD).
func TestNewGenerationWritesOnlyChangedRows(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "incremental")
	x := openIndex(t, s)
	text := func(i int) string { return fmt.Sprint("incremental event ", i) }
	publish(t, s, uid, events(100, text))
	pass(t, x)
	before := rowIDs(t, x, uid)

	// Grown by five events.
	grown := publish(t, s, uid, events(105, text))
	pass(t, x)
	after := rowIDs(t, x, uid)
	if len(after) != 105 {
		t.Fatalf("%d rows after growth", len(after))
	}
	for pos, id := range before {
		if after[pos] != id {
			t.Fatalf("row %d was rewritten although it did not change", pos)
		}
	}
	if x.deleted {
		t.Fatal("a pass that only added rows asked for a forced merge")
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "incremental event 104"}); len(p.Items) != 1 {
		t.Fatal("the new event is not searchable", len(p.Items))
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "incremental event 3"}); len(p.Items) == 0 || p.Items[0].Generation != grown {
		t.Fatal("a kept row does not report the current generation", p.Items)
	}

	// Event 7 changed, and the session is shorter.
	publish(t, s, uid, events(50, func(i int) string {
		if i == 7 {
			return "rewritten seven"
		}
		return text(i)
	}))
	pass(t, x)
	last := rowIDs(t, x, uid)
	if len(last) != 50 || last[7] == after[7] || last[8] != after[8] {
		t.Fatalf("rows %d, 7: %d->%d, 8: %d->%d", len(last), after[7], last[7], after[8], last[8])
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "rewritten seven"}); len(p.Items) != 1 {
		t.Fatal("the changed event is not searchable")
	}
	// Event 7's old text and events 70 to 79 are gone.
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "incremental event 7"}); len(p.Items) != 0 {
		t.Fatal("stale text still searchable", len(p.Items))
	}
}

// An event whose source had no time carries the projection time as
// recorded_at, the same as ingested_at, and a new one at every
// generation. Its row is kept across generations and reports no time
// (TKT-01M3NENNN8).
func TestUntimedEventsAreNotRewrittenEachGeneration(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "untimed")
	x := openIndex(t, s)
	gen := func(n int, stamp string) []normalize.Event {
		ev := events(n, func(i int) string { return fmt.Sprint("untimed event ", i) })
		for i := range ev {
			ev[i].IngestedAt = stamp
			if i%2 == 0 {
				ev[i].RecordedAt = stamp
			}
		}
		return ev
	}
	publish(t, s, uid, gen(20, "2026-09-29T01:00:00.123456789Z"))
	pass(t, x)
	before := rowIDs(t, x, uid)
	publish(t, s, uid, gen(22, "2026-09-29T02:00:00.987654321Z"))
	pass(t, x)
	after := rowIDs(t, x, uid)
	for pos, id := range before {
		if after[pos] != id {
			t.Fatalf("row %d was rewritten although only its projection time changed", pos)
		}
	}
	p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "untimed event 4"})
	if len(p.Items) != 1 || p.Items[0].RecordedAt != nil {
		t.Fatalf("an untimed event: %+v", p.Items)
	}
	p = search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "untimed event 5"})
	if len(p.Items) != 1 || p.Items[0].RecordedAt == nil {
		t.Fatalf("a timed event lost its time: %+v", p.Items)
	}
}

// toolEvents is a call and its result per pair, with i's model on the
// call and a result of size(i) bytes.
func toolEvents(pairs int, size func(i int) int) []normalize.Event {
	var out []normalize.Event
	for i := range pairs {
		name, call, model := "shell", fmt.Sprint("call-", i), fmt.Sprint("model-", i%2)
		in, res := fmt.Sprint(`{"cmd":"ls `, i, `"}`), strings.Repeat("r", size(i))
		failed := false
		at := fmt.Sprintf("2026-09-26T10:%02d:00Z", i)
		out = append(out,
			normalize.Event{SchemaVersion: 1, Harness: "codex", RecordedAt: at, IngestedAt: "2026-09-26T11:00:00Z", Actor: normalize.ActorAssistant, EventType: normalize.EventToolCall, Model: normalize.Model{ID: &model}, Tool: normalize.Tool{Name: &name, CallID: &call}, ContentText: &in},
			normalize.Event{SchemaVersion: 1, Harness: "codex", RecordedAt: at, IngestedAt: "2026-09-26T11:00:00Z", Actor: normalize.ActorTool, EventType: normalize.EventToolResult, Tool: normalize.Tool{CallID: &call, IsError: &failed}, ContentText: &res})
	}
	for i := range out {
		out[i].EventID, out[i].SessionID, out[i].Redaction = fmt.Sprint("ev-", i), "native", normalize.Redaction{Status: "none"}
	}
	return out
}

// session is three call and result pairs, a message with no content
// and a usage row. The second result is past IndexContentMax.
func session(output int) []normalize.Event {
	evs := append(toolEvents(3, func(i int) int {
		if i == 1 {
			return IndexContentMax + 1000
		}
		return 10 + i
	}), events(2, func(int) string { return "" })...)
	evs[6].EventID, evs[6].ContentText = "ev-6", nil
	in, cw := 1200, 300
	evs[7].EventID, evs[7].EventType, evs[7].ContentText = "ev-7", normalize.EventUsage, nil
	evs[7].Usage = normalize.Usage{Input: &in, Output: &output, CacheWrite: &cw}
	return evs
}

// Each row stores its content size before truncation, its call id, its
// model id and its token counts, and a change to any of them rewrites
// the row (TKT-01M61MZMT).
func TestRowsStoreSizeCallModelAndTokens(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "tools")
	x := openIndex(t, s)
	publish(t, s, uid, session(0))
	pass(t, x)
	type row struct {
		bytes, stored                        sql.NullInt64
		call, model                          sql.NullString
		input, output, cacheRead, cacheWrite sql.NullInt64
	}
	read := func() map[int64]row {
		rows, err := x.db.Query(`SELECT pos,content_bytes,length(CAST(content AS BLOB)),call_id,model_id,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens FROM docs WHERE session_uid=?`, uid)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[int64]row{}
		for rows.Next() {
			var pos int64
			var r row
			if err := rows.Scan(&pos, &r.bytes, &r.stored, &r.call, &r.model, &r.input, &r.output, &r.cacheRead, &r.cacheWrite); err != nil {
				t.Fatal(err)
			}
			out[pos] = r
		}
		return out
	}
	got := read()
	if r := got[3]; r.bytes.Int64 != IndexContentMax+1000 || r.stored.Int64 != IndexContentMax || r.call.String != "call-1" || r.model.Valid {
		t.Fatalf("a truncated result: %+v", r)
	}
	if r := got[2]; r.bytes.Int64 != int64(len(`{"cmd":"ls 1"}`)) || r.call.String != "call-1" || r.model.String != "model-1" || r.input.Valid {
		t.Fatalf("a call: %+v", r)
	}
	if r := got[6]; r.bytes.Valid || r.stored.Valid || r.call.Valid || r.model.Valid {
		t.Fatalf("a message with no content: %+v", r)
	}
	// A reported zero stays zero and a missing count stays null.
	if r := got[7]; r.input.Int64 != 1200 || !r.output.Valid || r.output.Int64 != 0 || r.cacheRead.Valid || r.cacheWrite.Int64 != 300 {
		t.Fatalf("a usage row: %+v", r)
	}

	before := rowIDs(t, x, uid)
	again := session(5)
	other, moved, longer := "model-9", "call-moved", strings.Repeat("r", IndexContentMax+2000)
	again[0].Model.ID = &other
	again[5].Tool.CallID = &moved
	again[3].ContentText = &longer
	publish(t, s, uid, again)
	pass(t, x)
	after := rowIDs(t, x, uid)
	for pos := range int64(8) {
		changed := pos == 0 || pos == 3 || pos == 5 || pos == 7
		if (after[pos] != before[pos]) != changed {
			t.Errorf("row %d rewritten=%v, want %v", pos, after[pos] != before[pos], changed)
		}
	}
	if r := read()[3]; r.bytes.Int64 != IndexContentMax+2000 {
		t.Fatalf("a result whose size past the indexed text changed: %+v", r)
	}
}

// The tool usage indexes serve pairing, time ranges and each session's
// first token count, and search, which never filters on their columns,
// does not use them.
func TestToolUsageIndexes(t *testing.T) {
	s := lake(t)
	x := openIndex(t, s)
	since := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	for name, req := range map[string]SearchRequest{
		"text":        {Scope: catalog.AllBays(), Query: "needle"},
		"tool":        {Scope: catalog.AllBays(), ToolName: "shell"},
		"tool since":  {Scope: catalog.AllBays(), ToolName: "shell", Since: &since},
		"type since":  {Scope: catalog.AllBays(), EventType: normalize.EventToolResult, Since: &since},
		"text type":   {Scope: catalog.AllBays(), Query: "needle", EventType: normalize.EventToolCall},
		"error since": {Scope: catalog.AllBays(), ToolError: new(true), Since: &since},
	} {
		if plan := queryPlan(t, x, req); strings.Contains(plan, "docs_call") || strings.Contains(plan, "docs_tokens") {
			t.Errorf("%s: search uses a tool usage index: %s", name, plan)
		}
	}
	explain := func(q string, args ...any) string {
		rows, err := x.db.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var plan string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan += detail + "; "
		}
		return plan
	}
	pairs := explain(`SELECT c.tool_name, r.content_bytes, r.recorded_ns-c.recorded_ns
		FROM docs r JOIN indexed i ON i.session_uid=r.session_uid
		JOIN docs c ON c.session_uid=r.session_uid AND c.call_id=r.call_id AND c.event_type='tool_call'
		WHERE r.event_type='tool_result' AND r.call_id IS NOT NULL AND r.recorded_ns>=? AND r.recorded_ns<?`, 0, 1)
	if !strings.Contains(pairs, "docs_call_time") || !strings.Contains(pairs, "INDEX docs_call ") {
		t.Fatal("pairing results with calls in a time range:", pairs)
	}
	first := explain(`SELECT session_uid, min(pos) FROM docs WHERE input_tokens IS NOT NULL GROUP BY session_uid`)
	if !strings.Contains(first, "docs_tokens") || strings.Contains(first, "TEMP B-TREE") {
		t.Fatal("each session's first token count:", first)
	}
}

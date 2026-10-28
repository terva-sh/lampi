package recall

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func ingestHarness(t *testing.T, s *api.Server, harness, native string) string {
	t.Helper()
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: harness, NativeSessionID: native,
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "s.jsonl", SHA256: strings.Repeat("c", 64), Size: 12}}}
	ack, err := s.Catalog.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}

func ip(n int) *int { return &n }

func sp(s string) *string { return &s }

func bp(b bool) *bool { return &b }

// ev is one event at minute m past 2026-09-26 10:00 UTC.
func ev(harness, typ string, m int, mod func(*normalize.Event)) normalize.Event {
	e := normalize.Event{SchemaVersion: 1, EventID: fmt.Sprint("ev-", typ, m), SessionID: "native", Harness: harness,
		RecordedAt: time.Date(2026, 9, 26, 10, m, 0, 0, time.UTC).Format(time.RFC3339), IngestedAt: "2026-09-27T00:00:00Z",
		Actor: normalize.ActorAssistant, EventType: typ, RawType: typ, Redaction: normalize.Redaction{Status: "none"}}
	if mod != nil {
		mod(&e)
	}
	return e
}

func totals(t *testing.T, x *Index, scope catalog.Scope) Totals {
	t.Helper()
	got, err := x.Totals(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func harnessTotals(t *testing.T, tot Totals, harness string) HarnessTotals {
	t.Helper()
	for _, h := range tot.Harnesses {
		if h.Harness == harness {
			return h
		}
	}
	t.Fatalf("no totals for %s in %+v", harness, tot.Harnesses)
	return HarnessTotals{}
}

func val(p *int64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprint(*p)
}

func checkTokens(t *testing.T, label string, got Tokens, input, output, cacheRead, cacheWrite string) {
	t.Helper()
	if val(got.Input) != input || val(got.Output) != output || val(got.CacheRead) != cacheRead || val(got.CacheWrite) != cacheWrite {
		t.Fatalf("%s: input %s output %s cache read %s cache write %s, want %s %s %s %s", label,
			val(got.Input), val(got.Output), val(got.CacheRead), val(got.CacheWrite), input, output, cacheRead, cacheWrite)
	}
}

// Claude Code repeats a response's usage on each content block, and a
// streamed response can repeat it with a larger output. Each message
// counts once, with its last usage.
func TestStatsCountEachClaudeMessageOnce(t *testing.T) {
	s := lake(t)
	uid := ingestHarness(t, s, "claude", "claude-1")
	usage := func(id string, in, out, cr, cw int) func(*normalize.Event) {
		return func(e *normalize.Event) {
			e.Usage = normalize.Usage{Input: ip(in), Output: ip(out), CacheRead: ip(cr), CacheWrite: ip(cw)}
			e.Extra = map[string]any{"id": id}
		}
	}
	publish(t, s, uid, []normalize.Event{
		ev("claude", normalize.EventUsage, 1, usage("msg_1", 10, 2, 100, 5)),
		ev("claude", normalize.EventUsage, 1, usage("msg_1", 10, 2, 100, 5)),
		ev("claude", normalize.EventUsage, 1, usage("msg_1", 10, 7, 100, 5)),
		ev("claude", normalize.EventUsage, 2, usage("msg_2", 3, 1, 200, 0)),
	})
	x := openIndex(t, s)
	pass(t, x)
	h := harnessTotals(t, totals(t, x, catalog.AllBays()), "claude")
	if h.ModelCalls != 2 {
		t.Fatalf("%d model calls, want 2", h.ModelCalls)
	}
	checkTokens(t, "claude", h.Tokens, "13", "8", "300", "5")
}

// Codex can report the same running total twice, and counts cached
// input inside input. Tokens come from differences of the total, with
// cached input taken out of input.
func TestStatsCodexUsesRunningTotals(t *testing.T) {
	s := lake(t)
	uid := ingestHarness(t, s, "codex", "codex-1")
	total := func(in, cached, out int) func(*normalize.Event) {
		return func(e *normalize.Event) {
			e.Usage = normalize.Usage{Input: ip(1), Output: ip(1)}
			e.Extra = map[string]any{"info": map[string]any{"total_token_usage": map[string]any{"input_tokens": in, "cached_input_tokens": cached, "output_tokens": out}}}
		}
	}
	publish(t, s, uid, []normalize.Event{
		ev("codex", normalize.EventUsage, 1, total(100, 40, 10)),
		ev("codex", normalize.EventUsage, 2, total(100, 40, 10)),
		ev("codex", normalize.EventUsage, 3, total(250, 140, 30)),
		// Without a total, last usage counts, cached taken out.
		ev("codex", normalize.EventUsage, 4, func(e *normalize.Event) {
			e.Usage = normalize.Usage{Input: ip(50), Output: ip(5), CacheRead: ip(20)}
		}),
	})
	x := openIndex(t, s)
	pass(t, x)
	h := harnessTotals(t, totals(t, x, catalog.AllBays()), "codex")
	if h.ModelCalls != 3 {
		t.Fatalf("%d model calls, want 3", h.ModelCalls)
	}
	// Plain input: (100-40) + (150-100) + (50-20).
	checkTokens(t, "codex", h.Tokens, "140", "35", "160", "nil")
}

// Other harnesses report one usage per model call with cache apart from
// input, and are summed as normalized. A field never reported stays nil.
func TestStatsSumOtherHarnessesAndKeepUnreportedNil(t *testing.T) {
	s := lake(t)
	uid := ingestHarness(t, s, "terva", "terva-1")
	publish(t, s, uid, []normalize.Event{
		ev("terva", normalize.EventUsage, 1, func(e *normalize.Event) {
			e.Usage = normalize.Usage{Input: ip(5), Output: ip(2), CacheRead: ip(1000)}
		}),
		ev("terva", normalize.EventCompaction, 2, func(e *normalize.Event) {
			e.Usage = normalize.Usage{Input: ip(7), Output: ip(3)}
		}),
		ev("terva", normalize.EventMessage, 3, nil),
	})
	x := openIndex(t, s)
	pass(t, x)
	h := harnessTotals(t, totals(t, x, catalog.AllBays()), "terva")
	if h.ModelCalls != 2 || h.Events != 3 {
		t.Fatalf("%d model calls and %d events, want 2 and 3", h.ModelCalls, h.Events)
	}
	checkTokens(t, "terva", h.Tokens, "12", "5", "1000", "nil")
}

// A failed tool result that carries only its call id is counted against
// the call's tool. Error events count by raw type, and unreadable lines
// apart from them.
func TestStatsNameFailedResultsAndSeparateUnreadable(t *testing.T) {
	s := lake(t)
	uid := ingestHarness(t, s, "claude", "claude-tools")
	publish(t, s, uid, []normalize.Event{
		ev("claude", normalize.EventToolCall, 1, func(e *normalize.Event) { e.Tool = normalize.Tool{Name: sp("Bash"), CallID: sp("c1")} }),
		ev("claude", normalize.EventToolResult, 2, func(e *normalize.Event) { e.Tool = normalize.Tool{CallID: sp("c1"), IsError: bp(true)} }),
		ev("claude", normalize.EventToolCall, 3, func(e *normalize.Event) { e.Tool = normalize.Tool{Name: sp("Read"), CallID: sp("c2")} }),
		ev("claude", normalize.EventToolResult, 4, func(e *normalize.Event) { e.Tool = normalize.Tool{CallID: sp("c2"), IsError: bp(false)} }),
		ev("claude", normalize.EventError, 5, func(e *normalize.Event) { e.RawType = "api_error" }),
		ev("claude", normalize.EventError, 6, func(e *normalize.Event) { e.Extra = map[string]any{"unreadable_line": 3} }),
	})
	x := openIndex(t, s)
	pass(t, x)
	h := harnessTotals(t, totals(t, x, catalog.AllBays()), "claude")
	if h.ToolCalls != 2 || h.ToolsFailed != 1 || h.Errors != 1 || h.Unreadable != 1 {
		t.Fatalf("got %+v", h)
	}
	var name string
	if err := x.db.QueryRow(`SELECT name FROM stats WHERE kind='tool_failed'`).Scan(&name); err != nil || name != "Bash" {
		t.Fatalf("failed tool named %q (%v), want Bash", name, err)
	}
	var hour int64
	if err := x.db.QueryRow(`SELECT hour FROM stats WHERE kind='error'`).Scan(&hour); err != nil || hour != time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("error in hour %d (%v)", hour, err)
	}
}

// A scope narrower than the lake sees only its sessions, a new
// generation replaces a session's stats, and removing a session drops
// them.
func TestStatsFollowScopeGenerationsAndRemoval(t *testing.T) {
	s := lake(t)
	a := ingestHarness(t, s, "terva", "a")
	b := ingestHarness(t, s, "terva", "b")
	msgs := func(n int) []normalize.Event {
		out := make([]normalize.Event, n)
		for i := range out {
			out[i] = ev("terva", normalize.EventMessage, i, nil)
		}
		return out
	}
	publish(t, s, a, msgs(2))
	publish(t, s, b, msgs(3))
	x := openIndex(t, s)
	pass(t, x)
	if got := totals(t, x, catalog.AllBays()).All; got.Events != 5 || got.Sessions != 2 {
		t.Fatalf("all: %+v", got)
	}
	if got := totals(t, x, catalog.AllBays().OnlySessions([]string{a})); got.All.Events != 2 || got.Coverage != (Coverage{}) {
		t.Fatalf("narrowed: %+v", got)
	}
	if got := totals(t, x, catalog.InBays(nil)).All; got.Events != 0 {
		t.Fatalf("empty scope: %+v", got)
	}
	publish(t, s, a, msgs(4))
	pass(t, x)
	if got := totals(t, x, catalog.AllBays()).All; got.Events != 7 {
		t.Fatalf("after a new generation: %+v", got)
	}
	if err := x.remove(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	if got := totals(t, x, catalog.AllBays()).All; got.Events != 4 || got.Sessions != 1 {
		t.Fatalf("after removal: %+v", got)
	}
}

// A version 4 index is upgraded in place: its rows and their ids are
// kept, so search still answers, and the next pass writes the stats.
func TestVersion4IndexUpgradesInPlace(t *testing.T) {
	s := lake(t)
	uid := ingestHarness(t, s, "terva", "old")
	publish(t, s, uid, events(10, func(i int) string { return fmt.Sprint("old event ", i) }))
	path := filepath.Join(t.TempDir(), IndexFile)
	x, err := OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	pass(t, x)
	before := rowIDs(t, x, uid)
	if _, err := x.db.Exec(`DROP TABLE stats; PRAGMA user_version=4;`); err != nil {
		t.Fatal(err)
	}
	x.Close()

	x, err = OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close() })
	var v int
	if err := x.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != indexVersion {
		t.Fatalf("version %d (%v)", v, err)
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "old event 7"}); len(p.Items) != 1 {
		t.Fatal("search lost its rows in the upgrade", len(p.Items))
	}
	pass(t, x)
	after := rowIDs(t, x, uid)
	for pos, id := range before {
		if after[pos] != id {
			t.Fatalf("row %d was rewritten by the upgrade", pos)
		}
	}
	if got := totals(t, x, catalog.AllBays()).All; got.Events != 10 {
		t.Fatalf("stats after the upgrade: %+v", got)
	}
	var head sql.NullString
	if err := x.db.QueryRow(`SELECT head FROM indexed WHERE session_uid=?`, uid).Scan(&head); err != nil || head.String == "" {
		t.Fatalf("head not restored: %q (%v)", head.String, err)
	}
}

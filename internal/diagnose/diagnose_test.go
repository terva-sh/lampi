package diagnose

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

// session builds a transcript one event at a time, as the normalizers
// would write it.
type session struct {
	harness string
	parent  *string
	at      time.Time
	lines   [][]byte
}

func newSession(harness string) *session {
	return &session{harness: harness, at: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
}

func (s *session) add(ev normalize.Event) int64 {
	ev.SchemaVersion = 1
	ev.Harness = s.harness
	ev.ParentSessionID = s.parent
	if ev.RecordedAt == "" {
		s.at = s.at.Add(time.Second)
		ev.RecordedAt = s.at.Format(time.RFC3339Nano)
	}
	b, err := json.Marshal(ev)
	if err != nil {
		panic(err)
	}
	s.lines = append(s.lines, b)
	return int64(len(s.lines) - 1)
}

func ptr[T any](v T) *T { return &v }

func (s *session) call(id, tool, input string) int64 {
	return s.add(normalize.Event{EventType: normalize.EventToolCall, Actor: normalize.ActorAssistant,
		ContentText: ptr(input), Tool: normalize.Tool{Name: ptr(tool), CallID: ptr(id)}})
}

func (s *session) result(id, tool, output string, isErr *bool) int64 {
	return s.add(normalize.Event{EventType: normalize.EventToolResult, Actor: normalize.ActorTool,
		ContentText: ptr(output), Tool: normalize.Tool{Name: ptr(tool), CallID: ptr(id), IsError: isErr}})
}

// pair writes a call and its result and returns the call's position.
func (s *session) pair(id, tool, input, output string, isErr *bool) int64 {
	p := s.call(id, tool, input)
	s.result(id, tool, output, isErr)
	return p
}

func (s *session) user(text string) int64 {
	return s.add(normalize.Event{EventType: normalize.EventMessage, Actor: normalize.ActorUser, ContentText: ptr(text)})
}

func (s *session) usage(in, cr, cw int, gap time.Duration) int64 {
	s.at = s.at.Add(gap)
	return s.add(normalize.Event{EventType: normalize.EventUsage, RecordedAt: s.at.Format(time.RFC3339Nano),
		Usage: normalize.Usage{Input: ptr(in), Output: ptr(10), CacheRead: ptr(cr), CacheWrite: ptr(cw)}})
}

func (s *session) run(t *testing.T) Report {
	t.Helper()
	a := New(nil)
	for _, l := range s.lines {
		a.Line(l)
	}
	rep := a.Report()
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatal("report is not JSON")
	}
	return rep
}

func find(rep Report, code Code, cause string) *Finding {
	for i := range rep.Findings {
		if rep.Findings[i].Code == code && rep.Findings[i].Cause == cause {
			return &rep.Findings[i]
		}
	}
	return nil
}

func codes(rep Report) []string {
	var out []string
	for _, f := range rep.Findings {
		out = append(out, string(f.Code)+"/"+f.Cause)
	}
	return out
}

func mustFind(t *testing.T, rep Report, code Code, cause string) *Finding {
	t.Helper()
	f := find(rep, code, cause)
	if f == nil {
		t.Fatalf("no %s/%s finding in %v", code, cause, codes(rep))
	}
	if f.Severity != Shipped.Codes[code].Severity || f.Suggestion.Code != code || f.Suggestion.Text != Shipped.Codes[code].Suggestion {
		t.Fatalf("%s: severity or suggestion %+v", code, f)
	}
	return f
}

func mustNotFind(t *testing.T, rep Report, code Code) {
	t.Helper()
	for _, f := range rep.Findings {
		if f.Code == code {
			t.Fatalf("unexpected %s finding %+v", code, f)
		}
	}
}

var (
	yes = ptr(true)
	no  = ptr(false)
)

func TestCleanSession(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	s.user("hello")
	for i := range 20 {
		s.pair(fmt.Sprint("c", i), "Bash", fmt.Sprint(`{"command":"ls `, i, `"}`), "ok", no)
	}
	rep := s.run(t)
	if len(rep.Findings) != 0 || rep.Findings == nil {
		t.Fatalf("findings %v", codes(rep))
	}
	if rep.Events != 41 || rep.ToolCalls != 20 || rep.ToolResults != 20 || rep.Harness != "claude" || rep.RulesVersion != Shipped.Version {
		t.Fatalf("%+v", rep)
	}
}

func TestLargeToolResult(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	big := strings.Repeat("x", 64<<10)
	p := s.pair("a", "Read", `{"file_path":"/a"}`, big, nil)
	f := mustFind(t, s.run(t), LargeToolResult, "")
	if !slices.Equal(f.Positions, []int64{p + 1}) || f.Values["max_bytes"] != 64<<10 || f.Values["over_result_bytes"] != 1 {
		t.Fatalf("%+v", f)
	}

	// Nine results at the count size do not fire; ten do.
	s = newSession(protocol.HarnessCodex)
	mid := strings.Repeat("y", 32<<10)
	for i := range 9 {
		s.pair(fmt.Sprint("c", i), "exec", "script", mid, nil)
	}
	mustNotFind(t, s.run(t), LargeToolResult)
	s.pair("c9", "exec", "script", mid, nil)
	f = mustFind(t, s.run(t), LargeToolResult, "")
	if f.Count != 10 || f.Values["over_count_bytes"] != 10 || f.Values["over_result_bytes"] != 0 {
		t.Fatalf("%+v", f)
	}

	// A Codex inner result is the script's, not the model's.
	s = newSession(protocol.HarnessCodex)
	s.add(normalize.Event{EventType: normalize.EventToolCall, RawType: InnerRawType, ContentText: ptr("cmd"), Tool: normalize.Tool{Name: ptr("exec_command"), CallID: ptr("i1")}})
	s.add(normalize.Event{EventType: normalize.EventToolResult, RawType: InnerRawType, ContentText: ptr(strings.Repeat("z", 1<<20)), Tool: normalize.Tool{Name: ptr("exec_command"), CallID: ptr("i1")}})
	mustNotFind(t, s.run(t), LargeToolResult)
}

func TestLargeToolInput(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	s.pair("a", "Write", `{"file_path":"/a","content":"`+strings.Repeat("x", 32<<10)+`"}`, "ok", nil)
	s.pair("b", "Write", `{"file_path":"/b","content":"small"}`, "ok", nil)
	f := mustFind(t, s.run(t), LargeToolInput, "")
	if f.Count != 1 || f.Positions[0] != 0 {
		t.Fatalf("%+v", f)
	}
}

func TestErrorStreak(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	for i := range 4 {
		s.pair(fmt.Sprint("a", i), "Bash", fmt.Sprint(i), "fail", yes)
	}
	s.pair("ok", "Bash", "fine", "ok", nil)
	mustNotFind(t, s.run(t), ErrorStreak)
	var start int64
	for i := range 6 {
		p := s.pair(fmt.Sprint("b", i), "Bash", fmt.Sprint("b", i), "fail", yes)
		if i == 0 {
			start = p + 1
		}
	}
	f := mustFind(t, s.run(t), ErrorStreak, "")
	if f.Values["longest"] != 6 || f.Values["streaks"] != 1 || !slices.Equal(f.Positions, []int64{start}) {
		t.Fatalf("%+v", f)
	}
}

func TestRetryLoop(t *testing.T) {
	// The same call with the same answer, four times: three retries.
	s := newSession(protocol.HarnessClaude)
	var calls []int64
	for i := range 4 {
		calls = append(calls, s.pair(fmt.Sprint("r", i), "Grep", `{"pattern":"x"}`, "no match", nil))
	}
	f := mustFind(t, s.run(t), RetryLoop, "same_output")
	if f.Tool != "Grep" || f.Values["retries"] != 3 || f.Values["loops"] != 1 || !slices.Equal(f.Positions, calls[3:]) {
		t.Fatalf("%+v", f)
	}

	// Pairs of repeats far apart, each with its own input, are not one
	// loop, however many there are (review 2903 on #222).
	s = newSession(protocol.HarnessClaude)
	for i := range 4 {
		in := fmt.Sprintf(`{"pattern":"p%d"}`, i)
		s.pair(fmt.Sprint("a", i), "Grep", in, "no match", nil)
		s.pair(fmt.Sprint("b", i), "Grep", in, "no match", nil)
		for j := range 6 {
			s.pair(fmt.Sprint("f", i, j), "Bash", fmt.Sprint(i, j), "ok", nil)
		}
	}
	mustNotFind(t, s.run(t), RetryLoop)

	// Results that arrive in reverse call order still chain (review
	// 2904 on #222).
	s = newSession(protocol.HarnessClaude)
	for i := range 4 {
		s.call(fmt.Sprint("o", i), "Grep", `{"pattern":"x"}`)
	}
	for i := 3; i >= 0; i-- {
		s.result(fmt.Sprint("o", i), "Grep", "no match", nil)
	}
	if f := mustFind(t, s.run(t), RetryLoop, "same_output"); f.Values["retries"] != 3 || f.Values["loops"] != 1 {
		t.Fatalf("%+v", f)
	}

	// Two separate loops of one tool are two loops.
	s = newSession(protocol.HarnessClaude)
	var ends []int64
	for i := range 2 {
		in := fmt.Sprintf(`{"pattern":"q%d"}`, i)
		for k := range 4 {
			p := s.pair(fmt.Sprint("l", i, k), "Grep", in, "no match", nil)
			if k == 3 {
				ends = append(ends, p)
			}
		}
		for j := range 6 {
			s.pair(fmt.Sprint("g", i, j), "Bash", fmt.Sprint(i, j), "ok", nil)
		}
	}
	f = mustFind(t, s.run(t), RetryLoop, "same_output")
	if f.Values["loops"] != 2 || f.Values["retries"] != 3 || !slices.Equal(f.Positions, ends) {
		t.Fatalf("%+v", f)
	}

	// A changing answer is polling for a change, not a loop.
	s = newSession(protocol.HarnessClaude)
	for i := range 6 {
		s.pair(fmt.Sprint("r", i), "Bash", `{"command":"status"}`, fmt.Sprint("state ", i), nil)
	}
	mustNotFind(t, s.run(t), RetryLoop)

	// Polling tools repeat by design.
	s = newSession(protocol.HarnessClaude)
	for i := range 6 {
		s.pair(fmt.Sprint("r", i), "Monitor", `{"id":"1"}`, "running", nil)
	}
	mustNotFind(t, s.run(t), RetryLoop)

	// Repeats outside the window are not retries.
	s = newSession(protocol.HarnessClaude)
	for i := range 4 {
		s.pair(fmt.Sprint("r", i), "Grep", `{"pattern":"x"}`, "no match", nil)
		for j := range 5 {
			s.pair(fmt.Sprint("f", i, j), "Bash", fmt.Sprint(i, j), "ok", nil)
		}
	}
	mustNotFind(t, s.run(t), RetryLoop)

	// Repeating a failed call unchanged is after_error, whatever it
	// returns the next time.
	s = newSession(protocol.HarnessClaude)
	for i := range 4 {
		s.pair(fmt.Sprint("e", i), "Bash", `{"command":"make"}`, fmt.Sprint("error ", i), yes)
	}
	f = mustFind(t, s.run(t), RetryLoop, "after_error")
	if f.Values["retries"] != 3 {
		t.Fatalf("%+v", f)
	}
}

func TestRepeatedRead(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	read := `{"file_path":"/repo/a.go","offset":10,"limit":20}`
	for i := range 4 {
		s.pair(fmt.Sprint("r", i), "Read", read, "text", nil)
	}
	// A write resets the count.
	s.pair("w", "Edit", `{"file_path":"/repo/a.go","old_string":"a","new_string":"b"}`, "ok", nil)
	for i := range 4 {
		s.pair(fmt.Sprint("s", i), "Read", read, "text", nil)
	}
	// Another range of the same file is paging, not a reread.
	s.pair("o", "Read", `{"file_path":"/repo/a.go","offset":30,"limit":20}`, "text", nil)
	mustNotFind(t, s.run(t), RepeatedRead)
	fifth := s.pair("s4", "Read", read, "text", nil)
	f := mustFind(t, s.run(t), RepeatedRead, "")
	if !slices.Equal(f.Positions, []int64{fifth}) || f.Values["paths"] != 1 || f.Values["max_reads"] != 5 {
		t.Fatalf("%+v", f)
	}
	b, _ := json.Marshal(f)
	if strings.Contains(string(b), "a.go") {
		t.Fatal("finding carries the path", string(b))
	}

	// Cursor CLI keeps the path under path.
	s = newSession(protocol.HarnessCursorCLI)
	for i := range 5 {
		s.pair(fmt.Sprint("r", i), "Read", `{"path":"/x"}`, "t", nil)
	}
	mustFind(t, s.run(t), RepeatedRead, "")

	// A Codex apply_patch header resets a read.
	if got := collectPatch("*** Begin Patch\n*** Update File: a/b.go\n@@\n*** Add File: c.go\n*** End Patch"); !slices.Equal(got, []string{"a/b.go", "c.go"}) {
		t.Fatal(got)
	}
	if got := collectPatch(`{"input":"*** Delete File: d.go\n"}`); !slices.Equal(got, []string{"d.go"}) {
		t.Fatal(got)
	}
}

func collectPatch(in string) []string {
	var out []string
	writePaths(protocol.HarnessCodex, "apply_patch", in, func(p string) { out = append(out, p) })
	return out
}

func TestContextPressureCompactions(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	var want []int64
	for i := range 3 {
		s.pair(fmt.Sprint("c", i), "Bash", fmt.Sprint(i), "ok", nil)
		// Boundary and summary are one compaction.
		want = append(want, s.add(normalize.Event{EventType: normalize.EventCompaction, RawType: "system"}))
		s.add(normalize.Event{EventType: normalize.EventCompaction, RawType: "user"})
		if i < 2 {
			mustNotFind(t, s.run(t), ContextPressure)
		}
	}
	f := mustFind(t, s.run(t), ContextPressure, "compactions")
	if f.Values["compactions"] != 3 || !slices.Equal(f.Positions, want) {
		t.Fatalf("%+v", f)
	}
}

// Compactions in a conversation with no tool activity are still
// separate when messages come between them, and one compaction's
// records stay one with usage between them (review 2903 on #222).
func TestContextPressureCompactionsWithoutTools(t *testing.T) {
	s := newSession(protocol.HarnessCodex)
	var want []int64
	for i := range 3 {
		s.user(fmt.Sprint("turn ", i))
		want = append(want, s.add(normalize.Event{EventType: normalize.EventCompaction, RawType: "compacted"}))
		s.usage(100, 0, 0, time.Second)
		s.add(normalize.Event{EventType: normalize.EventCompaction, RawType: "context_compacted"})
	}
	f := mustFind(t, s.run(t), ContextPressure, "compactions")
	if f.Values["compactions"] != 3 || !slices.Equal(f.Positions, want) {
		t.Fatalf("%+v", f)
	}
}

// A Cursor CLI event whose place among older branches is a guess
// neither joins a streak or a retry chain nor counts as a read.
func TestUnestablishedOrderIsSkipped(t *testing.T) {
	unsure := map[string]any{"order": "unestablished"}
	s := newSession(protocol.HarnessCursorCLI)
	for i := range 6 {
		id := fmt.Sprint("u", i)
		s.add(normalize.Event{EventType: normalize.EventToolCall, ContentText: ptr(`{"path":"a.go"}`), Tool: normalize.Tool{Name: ptr("Read"), CallID: ptr(id)}, Extra: unsure})
		s.add(normalize.Event{EventType: normalize.EventToolResult, ContentText: ptr("same"), Tool: normalize.Tool{Name: ptr("Read"), CallID: ptr(id), IsError: yes}, Extra: unsure})
	}
	rep := s.run(t)
	for _, code := range []Code{ErrorStreak, RetryLoop, RepeatedRead} {
		mustNotFind(t, rep, code)
	}
}

func TestContextPressureCacheCollapse(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	var want []int64
	s.usage(100, 50000, 1000, time.Second)
	// After an idle gap the cache expired: not counted.
	s.usage(100, 1000, 50000, 10*time.Minute)
	for range 3 {
		s.usage(100, 50000, 1000, time.Second)
		// The same usage repeated on a content-block line counts once.
		s.usage(100, 50000, 1000, 0)
		want = append(want, s.usage(100, 1000, 50000, 30*time.Second))
	}
	f := mustFind(t, s.run(t), ContextPressure, "cache_collapse")
	if f.Values["cache_collapses"] != 3 || !slices.Equal(f.Positions, want) || f.Values["peak_context_tokens"] != 51100 {
		t.Fatalf("%+v", f)
	}

	// The same usage seen again after an idle gap is the new baseline:
	// a collapse a second later is pressure (review 2906 on #222).
	s = newSession(protocol.HarnessClaude)
	want = nil
	for range 3 {
		s.usage(100, 50000, 1000, time.Second)
		s.usage(100, 50000, 1000, 10*time.Minute)
		want = append(want, s.usage(100, 1000, 50000, time.Second))
	}
	f = mustFind(t, s.run(t), ContextPressure, "cache_collapse")
	if f.Values["cache_collapses"] != 3 || !slices.Equal(f.Positions, want) {
		t.Fatalf("repeated baseline: %+v", f)
	}

	// A fall in context is not a collapse, nor is a Codex session.
	s = newSession(protocol.HarnessClaude)
	for range 4 {
		s.usage(100, 50000, 1000, time.Second)
		s.usage(100, 1000, 1000, time.Second)
	}
	mustNotFind(t, s.run(t), ContextPressure)
	s = newSession(protocol.HarnessCodex)
	for range 4 {
		s.usage(60000, 50000, 0, time.Second)
		s.usage(60000, 1000, 0, time.Second)
	}
	mustNotFind(t, s.run(t), ContextPressure)
}

func TestOrphanCall(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	var want []int64
	for i := range 3 {
		want = append(want, s.call(fmt.Sprint("lost", i), "Bash", fmt.Sprint(i)))
		s.pair(fmt.Sprint("ok", i), "Bash", fmt.Sprint("ok", i), "ok", nil)
	}
	// Trailing calls after the last result are an interrupted turn.
	s.call("t1", "Bash", "x")
	s.call("t2", "Bash", "y")
	f := mustFind(t, s.run(t), OrphanCall, "")
	if f.Values["calls"] != 3 || !slices.Equal(f.Positions, want) {
		t.Fatalf("%+v", f)
	}

	// Codex writes a call twice under one id, each with a result. The
	// second record is the same call, so four such calls in a row are
	// still a retry loop (review 2904 on #222).
	s = newSession(protocol.HarnessCodex)
	for i := range 4 {
		id := fmt.Sprint("d", i)
		s.call(id, "shell", "ls")
		s.call(id, "shell", "ls")
		s.result(id, "shell", "ok", nil)
		s.result(id, "shell", "ok", nil)
	}
	s.pair("end", "shell", "x", "ok", nil)
	rep := s.run(t)
	mustNotFind(t, rep, OrphanCall)
	mustNotFind(t, rep, OrphanResult)
	if f := mustFind(t, rep, RetryLoop, "same_output"); f.Values["retries"] != 3 {
		t.Fatalf("%+v", f)
	}

	// Off for Cursor CLI, whose store does not keep the order.
	s = newSession(protocol.HarnessCursorCLI)
	for i := range 3 {
		s.call(fmt.Sprint("lost", i), "Shell", fmt.Sprint(i))
	}
	s.pair("ok", "Shell", "x", "ok", nil)
	mustNotFind(t, s.run(t), OrphanCall)
}

func TestOrphanResult(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	s.pair("a", "Bash", "x", "ok", nil)
	p := s.result("ghost", "", "ok", nil)
	f := mustFind(t, s.run(t), OrphanResult, "")
	if !slices.Equal(f.Positions, []int64{p}) {
		t.Fatalf("%+v", f)
	}

	// A result before any call is from the file this one resumed.
	s = newSession(protocol.HarnessClaude)
	s.result("earlier", "", "ok", nil)
	s.pair("a", "Bash", "x", "ok", nil)
	s.result("ghost", "", "ok", nil)
	mustNotFind(t, s.run(t), OrphanResult)

	// So is one in a subagent session.
	s = newSession(protocol.HarnessClaude)
	s.parent = ptr("parent")
	s.pair("a", "Bash", "x", "ok", nil)
	s.result("ghost", "", "ok", nil)
	mustNotFind(t, s.run(t), OrphanResult)
}

func TestUnreadableLines(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	s.user("hi")
	p := s.add(normalize.Event{EventType: normalize.EventError, ContentText: ptr("Line 2 of the raw file is not a JSON record"),
		Extra: map[string]any{"unreadable_line": 2, "line_bytes": 40}})
	s.lines = append(s.lines, []byte("{not json"))
	s.lines = append(s.lines, nil)
	rep := s.run(t)
	f := mustFind(t, rep, UnreadableLines, "")
	if f.Values["lines"] != 2 || !slices.Equal(f.Positions, []int64{p, p + 1}) || rep.Oversized != 1 || rep.Events != 4 {
		t.Fatalf("%+v %+v", f, rep)
	}
	if rep.Findings[0].Code != UnreadableLines {
		t.Fatal("error severity is not first", codes(rep))
	}
}

func TestNoisyToolOrigin(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	chunk := strings.Repeat("m", 100<<10)
	for i := range 3 {
		s.pair(fmt.Sprint("m", i), "mcp__big-server__fetch", "{}", chunk, nil)
		s.pair(fmt.Sprint("b", i), "Bash", "ls", strings.Repeat("b", 200<<10), nil)
		s.pair(fmt.Sprint("q", i), "mcp__quiet__ping", "{}", "pong", nil)
	}
	rep := s.run(t)
	f := mustFind(t, rep, NoisyToolOrigin, "")
	if f.Origin != "big-server" || f.Values["bytes"] != 300<<10 || f.Values["share_percent"] != 33 || f.Count != 3 {
		t.Fatalf("%+v", f)
	}
	for _, g := range rep.Findings {
		if g.Code == NoisyToolOrigin && g.Origin != "big-server" {
			t.Fatalf("%+v", g)
		}
	}

	// Under the size floor a large share is not noise.
	s = newSession(protocol.HarnessClaude)
	s.pair("m", "mcp__big-server__fetch", "{}", strings.Repeat("m", 10<<10), nil)
	mustNotFind(t, s.run(t), NoisyToolOrigin)

	// Codex inner MCP results are judged among inner results.
	s = newSession(protocol.HarnessCodex)
	s.pair("cell", "exec", "script", strings.Repeat("c", 30<<10), nil)
	s.add(normalize.Event{EventType: normalize.EventToolCall, RawType: InnerRawType, ContentText: ptr("{}"), Tool: normalize.Tool{Name: ptr("mcp__docs__search"), CallID: ptr("i1")}})
	s.add(normalize.Event{EventType: normalize.EventToolResult, RawType: InnerRawType, ContentText: ptr(chunk + chunk + chunk), Tool: normalize.Tool{Name: ptr("mcp__docs__search"), CallID: ptr("i1")}})
	f = mustFind(t, s.run(t), NoisyToolOrigin, "inner_calls")
	if f.Origin != "docs" || f.Values["share_percent"] != 100 {
		t.Fatalf("%+v", f)
	}
}

func TestOrigin(t *testing.T) {
	for _, c := range []struct{ harness, name, want string }{
		{"claude", "mcp__my-server__do_thing", "my-server"},
		{"claude", "mcp__plugin_p_srv__tool", "p/srv"},
		{"claude", "Bash", ""},
		{"claude", "mcp__broken", ""},
		{"codex", "mcp__docs__search", "docs"},
		{"codex", "clock.sleep", ""},
		{"cursor-cli", "mcp__linear__list_issues", "linear"},
		{"cursor-cli", "mcp__cursor__AwaitShell", ""},
	} {
		if got := origin(c.harness, c.name); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.harness, c.name, got, c.want)
		}
	}
}

func TestUnknownTool(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	p := s.call("u", "mcp_wrong__tool", "{}")
	s.result("u", "", "<tool_use_error>Error: No such tool available: mcp_wrong__tool</tool_use_error>", yes)
	f := mustFind(t, s.run(t), UnknownTool, "")
	if f.Tool != "mcp_wrong__tool" || !slices.Equal(f.Positions, []int64{p + 1}) {
		t.Fatalf("%+v", f)
	}
	b, _ := json.Marshal(f)
	if strings.Contains(string(b), "tool_use_error") {
		t.Fatal("finding carries result text", string(b))
	}
}

func TestPositionsCapped(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	for i := range 3 * MaxPositions {
		s.pair(fmt.Sprint("c", i), "Write", fmt.Sprint(i, strings.Repeat("x", 32<<10)), "ok", nil)
	}
	f := mustFind(t, s.run(t), LargeToolInput, "")
	if len(f.Positions) != MaxPositions || f.Count != 3*MaxPositions {
		t.Fatalf("%d positions, count %d", len(f.Positions), f.Count)
	}
}

func TestReportJSONShape(t *testing.T) {
	s := newSession(protocol.HarnessClaude)
	s.pair("a", "Read", "{}", strings.Repeat("x", 64<<10), nil)
	b, err := json.Marshal(s.run(t))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"session_uid", "generation", "head_sha256", "harness", "rules_version", "events", "tool_calls", "tool_results", "oversized_events", "findings"} {
		if _, ok := m[k]; !ok {
			t.Errorf("report has no %s", k)
		}
	}
	f := m["findings"].([]any)[0].(map[string]any)
	for _, k := range []string{"code", "severity", "count", "positions", "values", "suggestion"} {
		if _, ok := f[k]; !ok {
			t.Errorf("finding has no %s", k)
		}
	}
}

// A Claude Code transcript through the real normalizer: the fields the
// rules read are where the normalizer puts them.
func TestClaudeTranscriptThroughNormalizer(t *testing.T) {
	var raw strings.Builder
	line := func(typ, content string) {
		fmt.Fprintf(&raw, `{"type":%q,"sessionId":"s","cwd":"/r","timestamp":"2026-10-17T00:00:00Z","message":{"role":%q,"content":%s}}`+"\n",
			typ, typ, content)
	}
	for i := range 5 {
		line("assistant", fmt.Sprintf(`[{"type":"tool_use","id":"r%d","name":"Read","input":{"file_path":"/r/a.go","offset":1,"limit":50}}]`, i))
		line("user", fmt.Sprintf(`[{"type":"tool_result","tool_use_id":"r%d","content":"package a"}]`, i))
	}
	line("assistant", `[{"type":"tool_use","id":"u","name":"Bassh","input":{"command":"ls"}}]`)
	line("user", `[{"type":"tool_result","tool_use_id":"u","is_error":true,"content":"<tool_use_error>Error: No such tool available: Bassh</tool_use_error>"}]`)
	a := New(nil)
	err := (normalize.Claude{Now: time.Now(), NativeID: "s"}).Stream(t.Context(), strings.NewReader(raw.String()), func(ev normalize.Event) error {
		b, err := json.Marshal(ev)
		a.Line(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	rep := a.Report()
	mustFind(t, rep, RepeatedRead, "")
	if f := mustFind(t, rep, UnknownTool, ""); f.Tool != "Bassh" {
		t.Fatalf("%+v", f)
	}
	mustNotFind(t, rep, OrphanCall)
	mustNotFind(t, rep, OrphanResult)
}

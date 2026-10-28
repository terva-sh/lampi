package recall

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/toolorigin"
)

// tev is one tool event for a fixture. at is seconds after base; a
// negative at records no time.
type tev struct {
	typ, name, call, raw, model string
	at                          int
	size                        int
	failed                      *bool
	usage                       []int
}

var toolBase = time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)

func call(name, id string, at, size int) tev {
	return tev{typ: normalize.EventToolCall, name: name, call: id, at: at, size: size}
}

func result(name, id string, at, size int, failed *bool) tev {
	return tev{typ: normalize.EventToolResult, name: name, call: id, at: at, size: size, failed: failed}
}

func toolSession(harness string, evs ...tev) []normalize.Event {
	out := make([]normalize.Event, len(evs))
	for i, e := range evs {
		ev := normalize.Event{SchemaVersion: 1, EventID: fmt.Sprint("ev-", i), SessionID: "native", Harness: harness,
			IngestedAt: "2026-09-27T00:00:00Z", EventType: e.typ, RawType: e.raw, Redaction: normalize.Redaction{Status: "none"}}
		if e.at >= 0 {
			ev.RecordedAt = toolBase.Add(time.Duration(e.at) * time.Second).Format(time.RFC3339Nano)
		}
		if e.name != "" {
			ev.Tool.Name = &e.name
		}
		if e.call != "" {
			ev.Tool.CallID = &e.call
		}
		if e.model != "" {
			ev.Model.ID = &e.model
		}
		ev.Tool.IsError = e.failed
		switch e.typ {
		case normalize.EventToolCall:
			ev.Actor = normalize.ActorAssistant
		case normalize.EventToolResult:
			ev.Actor = normalize.ActorTool
		case normalize.EventUsage:
			ev.Actor = normalize.ActorHarness
			if len(e.usage) == 3 {
				ev.Usage = normalize.Usage{Input: &e.usage[0], CacheRead: &e.usage[1], CacheWrite: &e.usage[2]}
			}
		default:
			ev.Actor = normalize.ActorUser
		}
		if e.typ != normalize.EventUsage {
			text := strings.Repeat("x", e.size)
			ev.ContentText = &text
		}
		out[i] = ev
	}
	return out
}

func toolStats(t *testing.T, x *Index, req ToolStatsRequest) ToolStats {
	t.Helper()
	if req.Now.IsZero() {
		req.Now = toolBase.Add(time.Hour)
	}
	st, err := x.ToolStats(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func findGroup(t *testing.T, st ToolStats, match func(ToolGroup) bool) ToolGroup {
	t.Helper()
	for _, g := range st.Groups {
		if match(g) {
			return g
		}
	}
	t.Fatalf("no such group in %+v", st.Groups)
	return ToolGroup{}
}

func byTool(name string) func(ToolGroup) bool {
	return func(g ToolGroup) bool { return g.Tool == name }
}

func TestToolStatsPerTool(t *testing.T) {
	s := lake(t)
	uid := ingestAs(t, s, "claude", "a", "")
	yes, no := true, false
	publish(t, s, uid, toolSession("claude",
		call("Bash", "b1", 0, 100), result("Bash", "b1", 1, 1000, &yes),
		call("Bash", "b2", 10, 200), result("Bash", "b2", 12, 2000, &no),
		call("Bash", "b3", 20, 300), result("", "b3", 24, 4000, nil),
		call("Bash", "b4", 30, 50),
		call("mcp__docs__search", "m1", 40, 60), result("mcp__docs__search", "m1", 41, 3000, nil),
		call("AskUserQuestion", "q1", 50, 70), result("AskUserQuestion", "q1", 140, 10, nil),
		tev{typ: normalize.EventMessage, at: 60, size: 5},
	))
	x := openIndex(t, s)
	pass(t, x)
	st := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Largest: 3})
	if st.Totals.Calls != 6 || st.Totals.Results != 5 || st.Totals.Sessions != 1 || st.Totals.Unanswered != 1 || st.Totals.ContextResultBytes != 10010 || st.Totals.InnerResultBytes != 0 {
		t.Fatalf("totals %+v", st.Totals)
	}
	bash := findGroup(t, st, byTool("Bash"))
	if bash.Calls != 4 || bash.Results != 3 || bash.Sessions != 1 || bash.Unanswered != 1 || bash.Origin.Kind != toolorigin.Builtin || bash.Inner {
		t.Fatalf("Bash %+v", bash)
	}
	// One result failed, one did not, one did not say.
	if bash.Errors != 1 || bash.ErrorKnown != 2 || bash.ErrorRate == nil || *bash.ErrorRate != 0.5 {
		t.Fatalf("Bash errors %+v", bash)
	}
	if in := bash.Input; in.Count != 4 || in.Total != 650 || in.Max != 300 || in.P50 < 100 || in.P50 > 119 || in.P95 != 300 {
		t.Fatalf("Bash input %+v", in)
	}
	if out := bash.Output; out.Count != 3 || out.Total != 7000 || out.Max != 4000 || out.P50 < 2000 || out.P50 > 2378 {
		t.Fatalf("Bash results %+v", out)
	}
	// The unnamed result took its call's name, and paired with it.
	if l := bash.Latency; l == nil || l.Count != 3 || l.Max != 4000 || l.P50 < 2000 || l.P50 > 2379 || l.P95 != 4000 {
		t.Fatalf("Bash latency %+v", bash.Latency)
	}
	if share := 7000.0 / 10010; bash.ResultShare != share || bash.SessionShare == nil || bash.SessionShare.Sessions != 1 || bash.SessionShare.Max != share {
		t.Fatalf("Bash share %v %+v", bash.ResultShare, bash.SessionShare)
	}
	mcp := findGroup(t, st, byTool("mcp__docs__search"))
	if mcp.Origin != (toolorigin.Origin{Kind: toolorigin.MCP, Name: "docs"}) || mcp.ErrorRate != nil || mcp.Latency == nil || mcp.Latency.Max != 1000 {
		t.Fatalf("MCP %+v", mcp)
	}
	// A person's answer time is not a tool's latency.
	ask := findGroup(t, st, byTool("AskUserQuestion"))
	if ask.Latency != nil || ask.LatencyNote != LatencyWaitsOnPerson {
		t.Fatalf("AskUserQuestion %+v", ask)
	}
	if len(st.Largest) != 3 || st.Largest[0].Bytes != 4000 || st.Largest[0].Tool != "Bash" || st.Largest[0].Position != 5 ||
		st.Largest[0].EventType != normalize.EventToolResult || st.Largest[1].Tool != "mcp__docs__search" || st.Largest[0].Link != EventLink(uid, st.Largest[0].Generation, 5) {
		t.Fatalf("largest %+v", st.Largest)
	}
	if st.Coverage.Indexed != 1 {
		t.Fatalf("coverage %+v", st.Coverage)
	}

	byOrigin := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), GroupBy: ToolGroupOrigin})
	if len(byOrigin.Groups) != 2 {
		t.Fatalf("origins %+v", byOrigin.Groups)
	}
	builtin := findGroup(t, byOrigin, func(g ToolGroup) bool { return g.Origin.Kind == toolorigin.Builtin })
	if builtin.Tool != "" || builtin.Calls != 5 || builtin.Results != 4 || builtin.Latency.Count != 3 || builtin.LatencyNote != "" {
		t.Fatalf("builtin %+v", builtin)
	}
	only := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), OriginKind: toolorigin.MCP, Largest: 5})
	if len(only.Groups) != 1 || only.Groups[0].Tool != "mcp__docs__search" || only.Totals.Calls != 1 || len(only.Largest) != 2 {
		t.Fatalf("origin filter %+v %+v", only.Groups, only.Largest)
	}
	if named := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Tool: "Bash"}); len(named.Groups) != 1 || named.Groups[0].Results != 3 {
		t.Fatalf("tool filter %+v", named.Groups)
	}
}

// Only sessions in scope, and only at the generation the catalog
// published, count.
func TestToolStatsScope(t *testing.T) {
	s := lake(t)
	ctx := t.Context()
	a := ingestAs(t, s, "claude", "a", "")
	b := ingestAs(t, s, "claude", "b", "")
	c := ingestAs(t, s, "claude", "c", "")
	for _, uid := range []string{a, b, c} {
		publish(t, s, uid, toolSession("claude", call("Read", "r1", 0, 10), result("Read", "r1", 1, 100, nil)))
	}
	bay, err := s.Catalog.CreateBay(ctx, "alpha", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: a, Bay: "alpha", Actor: "admin", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	x := openIndex(t, s)
	pass(t, x)
	calls := func(scope catalog.Scope) int64 {
		t.Helper()
		st := toolStats(t, x, ToolStatsRequest{Scope: scope, Largest: 10})
		for _, l := range st.Largest {
			if !scope.All() && l.SessionUID != a && l.SessionUID != b {
				t.Fatalf("a block outside scope: %+v", l)
			}
		}
		if !scope.All() && st.Coverage != (Coverage{}) {
			t.Fatalf("coverage for a limited scope: %+v", st.Coverage)
		}
		return st.Totals.Calls
	}
	if n := calls(catalog.AllBays()); n != 3 {
		t.Fatal("all bays", n)
	}
	if n := calls(catalog.InBays([]string{bay.ID})); n != 1 {
		t.Fatal("one bay", n)
	}
	if n := calls(catalog.AllBays().OnlySessions([]string{a, b})); n != 2 {
		t.Fatal("named sessions", n)
	}
	if n := calls(catalog.Scope{}); n != 0 {
		t.Fatal("zero scope", n)
	}
	// A new generation on its way hides the old rows, as search does.
	if _, err := s.Catalog.EnqueueNormalize(ctx, b, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := calls(catalog.AllBays()); n != 2 {
		t.Fatal("pending generation", n)
	}
	// A published generation the index has not caught up with is hidden
	// too.
	publish(t, s, c, toolSession("claude", call("Read", "r1", 0, 10), call("Read", "r2", 0, 10)))
	if n := calls(catalog.AllBays()); n != 1 {
		t.Fatal("stale generation", n)
	}
	pass(t, x)
	if n := calls(catalog.AllBays()); n != 3 {
		t.Fatal("after the pass", n)
	}
}

func TestToolStatsRangeAndBuckets(t *testing.T) {
	s := lake(t)
	uid := ingestAs(t, s, "claude", "a", "")
	publish(t, s, uid, toolSession("claude",
		call("Read", "r1", 0, 10), result("Read", "r1", 1, 10, nil),
		call("Read", "r2", 3600, 10), result("Read", "r2", 3601, 10, nil),
		call("Read", "r5", 7100, 10), result("Read", "r5", 7300, 10, nil),
		call("Read", "r3", 7300, 10),
		call("Read", "r4", -1, 10), result("Read", "r4", -1, 10, nil),
	))
	x := openIndex(t, s)
	pass(t, x)
	all := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays()})
	if r := all.Groups[0]; r.Calls != 5 || r.Undated != 2 || r.Latency.Count != 3 || r.Unanswered != 1 || all.Since != nil {
		t.Fatalf("no range %+v", r)
	}
	// A call in the range pairs with its result past the end, which is
	// not counted.
	since, until := toolBase, toolBase.Add(2*time.Hour)
	ranged := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Since: &since, Until: &until})
	if r := ranged.Groups[0]; r.Calls != 3 || r.Results != 2 || r.Undated != 0 || r.Unanswered != 0 || *ranged.Since != "2026-09-26T10:00:00Z" {
		t.Fatalf("range %+v", r)
	}
	// Buckets follow the activity rules: aligned, dense, clamped to the
	// current bucket.
	from := toolBase.Add(-30 * time.Minute)
	b := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Bucket: catalog.BucketHour, Since: &from, Now: toolBase.Add(2*time.Hour + time.Minute)})
	if strings.Join(b.BucketStarts, ",") != "2026-09-26T09:00:00Z,2026-09-26T10:00:00Z,2026-09-26T11:00:00Z,2026-09-26T12:00:00Z" {
		t.Fatal("bucket starts", b.BucketStarts)
	}
	if fmt.Sprint(b.Groups[0].Buckets) != "[0 1 2 1]" || fmt.Sprint(b.Totals.Buckets) != "[0 1 2 1]" || b.Groups[0].Calls != 4 {
		t.Fatalf("buckets %+v %v", b.Groups[0], b.Totals.Buckets)
	}
	day := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Bucket: catalog.BucketDay, Now: toolBase})
	if len(day.BucketStarts) != 7 || day.Totals.Buckets[6] != 4 {
		t.Fatalf("default day range %v %v", day.BucketStarts, day.Totals.Buckets)
	}
}

func ingestAt(t testing.TB, s *api.Server, harness, native string, at time.Time) string {
	t.Helper()
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: harness, NativeSessionID: native,
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "s.jsonl", SHA256: strings.Repeat("c", 64), Size: 12}}}
	ack, err := s.Catalog.Ingest(t.Context(), m, at, []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}

// A session the lake last accepted a day or more before Since holds no
// row recorded since, so a range does not read it.
func TestToolStatsSkipsSessionsAcceptedBeforeTheRange(t *testing.T) {
	s := lake(t)
	// Rows recorded after the lake accepted them are impossible, so they
	// show which sessions the walk read.
	old := ingestAt(t, s, "claude", "old", toolBase.Add(-25*time.Hour))
	skewed := ingestAt(t, s, "claude", "skewed", toolBase.Add(-23*time.Hour))
	for _, uid := range []string{old, skewed} {
		publish(t, s, uid, toolSession("claude", call("Read", "r1", 60, 10)))
	}
	x := openIndex(t, s)
	pass(t, x)
	since := toolBase
	if st := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Since: &since}); st.Totals.Sessions != 1 || st.Totals.Calls != 1 {
		t.Fatalf("since %+v", st.Totals)
	}
	if st := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays()}); st.Totals.Sessions != 2 {
		t.Fatalf("no range %+v", st.Totals)
	}
}

// A Codex call recorded both as a response_item and as an event_msg
// counts once, and code-mode inner calls are their own class.
func TestToolStatsCodex(t *testing.T) {
	s := lake(t)
	uid := ingestAs(t, s, "codex", "a", "")
	with := func(e tev, raw string) tev { e.raw = raw; return e }
	publish(t, s, uid, toolSession("codex",
		with(call("shell", "c1", 0, 10), "function_call"),
		with(call("exec_command", "c1", 0, 10), "exec_command_begin"),
		with(result("exec_command", "c1", 1, 100, nil), "exec_command_end"),
		with(result("shell", "c1", 1, 100, nil), "function_call_output"),
		// An event_msg pair with no response_item is the only record.
		with(call("web_search", "w1", 2, 10), "web_search_begin"),
		with(result("web_search", "w1", 3, 50, nil), "web_search_end"),
		with(call("exec", "x1", 4, 20), "custom_tool_call"),
		with(call("exec_command", "i1", 5, 30), codexInnerRawType),
		with(result("exec_command", "i1", 5, 9000, nil), codexInnerRawType),
		with(call("mcp__tracker__list", "i2", 5, 30), codexInnerRawType),
		with(result("mcp__tracker__list", "i2", 6, 1000, nil), codexInnerRawType),
		with(result("exec", "x1", 9, 850, nil), "custom_tool_call_output"),
	))
	x := openIndex(t, s)
	pass(t, x)
	st := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays()})
	if st.Totals.Calls != 5 || st.Totals.Results != 5 || st.Totals.ContextResultBytes != 1000 || st.Totals.InnerResultBytes != 10000 {
		t.Fatalf("totals %+v", st.Totals)
	}
	for _, g := range st.Groups {
		if g.Tool == "exec_command" && !g.Inner {
			t.Fatalf("a second record of a call counted: %+v", g)
		}
	}
	shell := findGroup(t, st, byTool("shell"))
	if shell.Calls != 1 || shell.Results != 1 || shell.Latency.Count != 1 {
		t.Fatalf("shell %+v", shell)
	}
	exec := findGroup(t, st, byTool("exec"))
	if exec.LatencyNote != LatencyCodeModeCell || exec.ResultShare != 0.85 || exec.Inner {
		t.Fatalf("exec %+v", exec)
	}
	inner := findGroup(t, st, byTool("exec_command"))
	// Recorded at one instant, so no latency.
	if !inner.Inner || inner.ResultShare != 0.9 || inner.Latency != nil {
		t.Fatalf("inner %+v", inner)
	}
	if mcp := findGroup(t, st, byTool("mcp__tracker__list")); !mcp.Inner || mcp.Origin.Name != "tracker" || mcp.Latency == nil || mcp.Latency.Max != 1000 {
		t.Fatalf("inner mcp %+v", mcp)
	}
	byOrigin := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), GroupBy: ToolGroupOrigin})
	if len(byOrigin.Groups) != 3 {
		t.Fatalf("origins: outer built-in, inner built-in, inner mcp: %+v", byOrigin.Groups)
	}
}

func TestToolStatsSplitAndFilters(t *testing.T) {
	s := lake(t)
	remote := "git@example.com:org/one.git"
	a := ingestAs(t, s, "claude", "a", remote)
	b := ingestAs(t, s, "cursor-cli", "b", "")
	withModel := func(e tev, m string) tev { e.model = m; return e }
	publish(t, s, a, toolSession("claude",
		withModel(call("Read", "r1", 0, 10), "m-1"), result("Read", "r1", 1, 10, nil),
		withModel(call("Read", "r2", 0, 10), "m-2"), result("Read", "r2", 1, 10, nil)))
	publish(t, s, b, toolSession("cursor-cli", withModel(call("Read", "r1", 0, 10), "m-1"), result("Read", "r1", 1, 10, nil)))
	x := openIndex(t, s)
	pass(t, x)
	st := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Split: ToolSplitHarness})
	if len(st.Groups) != 2 || st.Groups[0].Harness != "claude" || st.Groups[0].Calls != 2 || st.Groups[1].Harness != "cursor-cli" {
		t.Fatalf("split by harness %+v", st.Groups)
	}
	st = toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Split: ToolSplitModel})
	if len(st.Groups) != 2 || st.Groups[0].Model != "m-1" || st.Groups[0].Calls != 2 || st.Groups[0].Results != 2 || st.Groups[0].Sessions != 2 {
		t.Fatalf("split by model: a result takes its call's model: %+v", st.Groups)
	}
	// The work is bounded by rows read and by distinct keys.
	for _, limit := range []*int{&toolStatsMaxRows, &toolStatsMaxGroups} {
		was := *limit
		*limit = 1
		_, err := x.ToolStats(t.Context(), ToolStatsRequest{Scope: catalog.AllBays(), Split: ToolSplitModel})
		*limit = was
		if !errors.Is(err, ErrTooManyValues) {
			t.Errorf("past a limit of 1: %v", err)
		}
	}
	for name, c := range map[string]struct {
		req   ToolStatsRequest
		calls int64
	}{
		"harness":  {ToolStatsRequest{Harness: "cursor-cli"}, 1},
		"project":  {ToolStatsRequest{Project: protocol.ProjectLinkID(remote, root("x"))}, 2},
		"unlinked": {ToolStatsRequest{Unlinked: true}, 1},
		"model":    {ToolStatsRequest{Model: "m-2"}, 1},
		"tool":     {ToolStatsRequest{Tool: "Write"}, 0},
	} {
		c.req.Scope = catalog.AllBays()
		if got := toolStats(t, x, c.req).Totals; got.Calls != c.calls || name == "model" && got.Results != 1 {
			t.Errorf("%s: %+v, want %d calls", name, got, c.calls)
		}
	}
	since := toolBase
	for _, req := range []ToolStatsRequest{
		{Harness: "nope"}, {Project: "p", Unlinked: true}, {OriginKind: "server"}, {OriginName: "docs"},
		{GroupBy: "session"}, {Split: "project"}, {Largest: ToolStatsMaxLargest + 1}, {Largest: -1},
		{Since: &since, Until: &since}, {Bucket: "week"}, {Model: "\xff"},
		{Bucket: catalog.BucketHour, Since: &since, Until: new(since.Add(15 * 24 * time.Hour))},
	} {
		req.Scope = catalog.AllBays()
		if _, err := x.ToolStats(t.Context(), req); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %+v: %v", req, err)
		}
	}
}

func usage(at, input, cacheRead, cacheWrite int) tev {
	return tev{typ: normalize.EventUsage, at: at, usage: []int{input, cacheRead, cacheWrite}}
}

// Starting context compares sessions that called an origin with those
// that did not, per harness, and says it is a correlation.
func TestToolStatsContext(t *testing.T) {
	s := lake(t)
	for i := range 12 {
		uid := ingestAs(t, s, "claude", fmt.Sprint("c", i), "")
		evs := []tev{usage(0, 1000, 8000+i, 1000), call("Read", "r", 1, 10), result("Read", "r", 2, 10, nil), usage(3, 90000, 0, 0)}
		if i < 6 {
			evs = append(evs, call("mcp__docs__search", "m", 4, 10))
			evs[0] = usage(0, 1000, 18000+i, 1000)
		}
		publish(t, s, uid, toolSession("claude", evs...))
	}
	for i := range 3 {
		uid := ingestAs(t, s, "codex", fmt.Sprint("x", i), "")
		publish(t, s, uid, toolSession("codex", usage(0, 5000, 4000, 0), call("mcp__docs__search", "m", 4, 10)))
	}
	x := openIndex(t, s)
	pass(t, x)
	if st := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays()}); st.Context != nil {
		t.Fatal("context without asking", st.Context)
	}
	c := toolStats(t, x, ToolStatsRequest{Scope: catalog.AllBays(), Context: true, Tool: "Read"}).Context
	if c == nil || c.Kind != ToolContextKind || c.MinSessions != ToolContextMinSessions || len(c.Harnesses) != 2 || len(c.Rows) != 2 {
		t.Fatalf("context %+v", c)
	}
	if h := c.Harnesses[0]; h.Harness != "claude" || h.All.Sessions != 12 {
		t.Fatalf("claude sessions %+v", h)
	}
	r := c.Rows[0]
	if r.Harness != "claude" || r.Origin != (toolorigin.Origin{Kind: toolorigin.MCP, Name: "docs"}) || r.With.Sessions != 6 || r.Without.Sessions != 6 || r.Suppressed {
		t.Fatalf("claude row %+v", r)
	}
	// Medians are nearest rank: the third of six.
	if *r.With.Median != 1000+18002+1000 || *r.Without.Median != 1000+8008+1000 || *r.Difference != 9994 {
		t.Fatalf("claude medians %d %d %d", *r.With.Median, *r.Without.Median, *r.Difference)
	}
	// Codex input already holds its cached input, and three sessions
	// with none to compare against are too few.
	if r := c.Rows[1]; r.Harness != "codex" || *r.With.Median != 5000 || r.Without.Sessions != 0 || !r.Suppressed || r.Difference != nil {
		t.Fatalf("codex row %+v", r)
	}
	scoped := toolStats(t, x, ToolStatsRequest{Scope: catalog.Scope{}, Context: true}).Context
	if len(scoped.Harnesses) != 0 || len(scoped.Rows) != 0 {
		t.Fatalf("zero scope %+v", scoped)
	}
}

// The walk starts from the sessions in scope and reads each in position
// order through docs_session, with no sort.
func TestToolStatsPlans(t *testing.T) {
	s := lake(t)
	x := openIndex(t, s)
	conn, err := x.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := fillToolScope(t.Context(), conn, []scopeSession{{uid: "a", gen: 1}}); err != nil {
		t.Fatal(err)
	}
	explain := func(q string, args ...any) string {
		t.Helper()
		res, err := conn.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Close()
		plan := ""
		for res.Next() {
			var id, parent, notused int
			var detail string
			if err := res.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan += detail + "; "
		}
		return plan
	}
	for _, req := range []ToolStatsRequest{{}, {Context: true, Split: ToolSplitModel}} {
		plan := explain(toolRowsSQL(&req))
		if !strings.HasPrefix(plan, "SCAN s; ") || !strings.Contains(plan, "SEARCH d USING INDEX docs_session (session_uid=?)") || strings.Contains(plan, "TEMP B-TREE") {
			t.Errorf("%+v: %s", req, plan)
		}
	}
}

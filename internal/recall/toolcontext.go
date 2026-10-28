package recall

import (
	"cmp"
	"slices"
	"strings"

	"terva.sh/lampi/internal/toolorigin"
)

// Starting-context cost of MCP servers and extensions (TKT-01M61MZNW).
//
// A server's tool definitions take context at session start whether or
// not its tools are called, and the normalized events record no tool
// list: Codex rollouts hold none, and the lists Claude Code writes in
// attachment lines are not projected. So this compares the
// starting context of sessions that called a server with sessions that
// did not, within one harness. It is a correlation: a session that uses
// a server may differ in other ways, and a server configured but never
// called is invisible. Present it beside the statistics as a hint, not
// as a finding.

// ToolContextMinSessions is the fewest sessions each side of a
// comparison needs before a difference is reported.
const ToolContextMinSessions = 5

// ToolContextKind labels the block, so a surface cannot present it as
// a measurement.
const ToolContextKind = "correlation_hint"

const toolContextNote = "Starting context of sessions that called the origin against sessions in the same harness that did not. " +
	"A correlation, not a measurement: it cannot see a server that is configured but never called."

// ToolContext compares starting context per harness and origin.
// Starting context is the first usage event's input plus cache read and
// cache write tokens, or input alone for Codex, whose input already
// holds its cached input.
type ToolContext struct {
	Kind        string           `json:"kind"`
	Note        string           `json:"note"`
	MinSessions int              `json:"min_sessions"`
	Harnesses   []ContextHarness `json:"harnesses"`
	Rows        []ContextRow     `json:"rows"`
}

// ContextHarness is the sessions of one harness with a starting
// context.
type ContextHarness struct {
	Harness string      `json:"harness"`
	All     ContextSide `json:"all"`
}

// ContextRow is one origin in one harness. Difference is the median
// with minus the median without, null when Suppressed: either side has
// fewer than MinSessions sessions.
type ContextRow struct {
	Harness    string            `json:"harness"`
	Origin     toolorigin.Origin `json:"origin"`
	With       ContextSide       `json:"with"`
	Without    ContextSide       `json:"without"`
	Difference *int64            `json:"difference_tokens"`
	Suppressed bool              `json:"suppressed"`
}

// ContextSide is the median starting context of some sessions, in
// tokens, nearest-rank; null when there are none.
type ContextSide struct {
	Sessions int64  `json:"sessions"`
	Median   *int64 `json:"median_tokens"`
}

func sideOf(v []int64) ContextSide {
	s := ContextSide{Sessions: int64(len(v))}
	if len(v) > 0 {
		slices.Sort(v)
		m := v[(len(v)-1)/2]
		s.Median = &m
	}
	return s
}

// contextAgg collects each session's starting context and the origins
// its calls came from. The sessions are those the request walks: in
// scope, passing the harness and project filters and, with a range,
// active in it. The tool, origin and model filters do not apply, since
// the comparison is between sessions.
type contextAgg struct {
	start map[string][]int64
	used  map[contextUse][]int64
	// counts of sessions per harness, to tell without from with.
	seen map[contextUse]bool
}

type contextUse struct {
	harness string
	origin  toolorigin.Origin
}

func newContextAgg() *contextAgg {
	return &contextAgg{start: map[string][]int64{}, used: map[contextUse][]int64{}, seen: map[contextUse]bool{}}
}

// session adds one session. start is its first usage row's starting
// context, or -1 when it has none, which leaves the session out.
func (c *contextAgg) session(uid, harness string, start int64, rows []trow) {
	if start < 0 {
		return
	}
	c.start[harness] = append(c.start[harness], start)
	clear(c.seen)
	for i := range rows {
		r := &rows[i]
		if r.typ != rowCall || r.tool == "" {
			continue
		}
		o := toolorigin.Of(harness, r.tool)
		if o.Kind == toolorigin.Builtin || o.Kind == toolorigin.Unknown {
			continue
		}
		u := contextUse{harness, o}
		if !c.seen[u] {
			c.seen[u] = true
			c.used[u] = append(c.used[u], start)
		}
	}
}

func (c *contextAgg) result() ToolContext {
	out := ToolContext{Kind: ToolContextKind, Note: toolContextNote, MinSessions: ToolContextMinSessions, Harnesses: []ContextHarness{}, Rows: []ContextRow{}}
	for h, all := range c.start {
		out.Harnesses = append(out.Harnesses, ContextHarness{Harness: h, All: sideOf(slices.Clone(all))})
	}
	for u, with := range c.used {
		// The sessions without are all of the harness's less those with:
		// remove one occurrence of each value with holds.
		without := slices.Clone(c.start[u.harness])
		slices.Sort(without)
		slices.Sort(with)
		rest := without[:0]
		j := 0
		for _, v := range without {
			if j < len(with) && with[j] == v {
				j++
				continue
			}
			rest = append(rest, v)
		}
		row := ContextRow{Harness: u.harness, Origin: u.origin, With: sideOf(with), Without: sideOf(rest)}
		if len(with) < ToolContextMinSessions || len(rest) < ToolContextMinSessions {
			row.Suppressed = true
		} else {
			d := *row.With.Median - *row.Without.Median
			row.Difference = &d
		}
		out.Rows = append(out.Rows, row)
	}
	slices.SortFunc(out.Harnesses, func(a, b ContextHarness) int { return strings.Compare(a.Harness, b.Harness) })
	slices.SortFunc(out.Rows, func(a, b ContextRow) int {
		return cmp.Or(strings.Compare(a.Harness, b.Harness), cmp.Compare(b.With.Sessions, a.With.Sessions),
			strings.Compare(a.Origin.Kind, b.Origin.Kind), strings.Compare(a.Origin.Name, b.Origin.Name), strings.Compare(a.Origin.Server, b.Origin.Server))
	})
	return out
}

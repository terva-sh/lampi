package recall

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"strings"

	"terva.sh/lampi/internal/toolorigin"
)

// Starting-context cost of MCP servers and extensions (TKT-01M61MZNW).
//
// A server's tool definitions take context at session start whether or
// not its tools are called. Codex rollouts record no tool list. Claude
// Code records the tools it offers in attachment lines, which the index
// keeps as StatToolOffered rows (TKT-01M61SPVY). So this compares the
// starting context of sessions that used an origin with sessions that
// did not, within one harness. A session with a recorded list used the
// origins it was offered or called; any other session used the origins
// it called, and a server configured there but never called is
// invisible. A session that uses a server may differ in other ways, so
// present it beside the statistics as a hint, not as a finding.

// ToolContextMinSessions is the fewest sessions each side of a
// comparison needs before a difference is reported.
const ToolContextMinSessions = 5

// ToolContextKind labels the block, so a surface cannot present it as
// a measurement.
const ToolContextKind = "correlation_hint"

const toolContextNote = "Starting context of sessions that used the origin against sessions in the same harness that did not. " +
	"A session whose tool list was recorded (offered_sessions) used what it was offered or called; any other session used what it called, " +
	"so a server configured but never called is seen only where the list was recorded. A correlation, not a measurement."

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
// OfferedSessions counts those whose tool list was recorded.
type ContextHarness struct {
	Harness         string      `json:"harness"`
	All             ContextSide `json:"all"`
	OfferedSessions int64       `json:"offered_sessions"`
}

// ContextRow is one origin in one harness. Difference is the median
// with minus the median without, null when Suppressed: either side has
// fewer than MinSessions sessions. Offered is the part of With whose
// recorded tool list offered the origin.
type ContextRow struct {
	Harness    string            `json:"harness"`
	Origin     toolorigin.Origin `json:"origin"`
	With       ContextSide       `json:"with"`
	Without    ContextSide       `json:"without"`
	Difference *int64            `json:"difference_tokens"`
	Suppressed bool              `json:"suppressed"`
	Offered    ContextOffered    `json:"offered"`
}

// ContextOffered is the sessions whose recorded tool list offered an
// origin, how many of them never called it, and the median number of
// its tools they were offered, nearest-rank; null when there are none.
type ContextOffered struct {
	Sessions int64  `json:"sessions"`
	Uncalled int64  `json:"uncalled_sessions"`
	Tools    *int64 `json:"median_tools"`
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
// it used. The sessions are those the request walks: in scope, passing
// the harness and project filters and, with a range, active in it. The
// tool, origin and model filters do not apply, since the comparison is
// between sessions.
type contextAgg struct {
	start map[string][]int64
	used  map[contextUse][]int64
	// listed counts the sessions per harness with a recorded tool list,
	// and offered the tools each was offered per origin.
	listed  map[string]int64
	offered map[contextUse]*offeredAgg
	// lists is each walked session's recorded tool list by origin, read
	// before the walk. A session with a list of built-in tools only has
	// an empty one.
	lists map[string][]originTools
	// called is the session's called origins, reused.
	called map[contextUse]bool
}

type contextUse struct {
	harness string
	origin  toolorigin.Origin
}

type originTools struct {
	origin toolorigin.Origin
	tools  int64
}

type offeredAgg struct {
	uncalled int64
	tools    []int64
}

func newContextAgg() *contextAgg {
	return &contextAgg{start: map[string][]int64{}, used: map[contextUse][]int64{}, listed: map[string]int64{},
		offered: map[contextUse]*offeredAgg{}, lists: map[string][]originTools{}, called: map[contextUse]bool{}}
}

// offeredListsSQL reads the offered names of the sessions in the scope
// table through stats_session.
const offeredListsSQL = `SELECT s.uid, st.name FROM temp.tool_scope s
	CROSS JOIN indexed i ON i.session_uid=s.uid AND i.gen=s.gen
	CROSS JOIN stats st ON st.session_uid=s.uid WHERE st.kind=?`

// loadLists reads the recorded tool lists of the sessions in the scope
// table, counted per origin. Only names whose origin is not built in
// are kept, so the memory is per session and server.
func (c *contextAgg) loadLists(ctx context.Context, conn *sql.Conn, harness map[string]string) error {
	res, err := conn.QueryContext(ctx, offeredListsSQL, StatToolOffered)
	if err != nil {
		return err
	}
	defer res.Close()
	for res.Next() {
		var uid, name string
		if err := res.Scan(&uid, &name); err != nil {
			return err
		}
		l, ok := c.lists[uid]
		if !ok {
			l = []originTools{}
		}
		if o := toolorigin.Of(harness[uid], name); o.Kind != toolorigin.Builtin && o.Kind != toolorigin.Unknown {
			i := slices.IndexFunc(l, func(t originTools) bool { return t.origin == o })
			if i < 0 {
				i = len(l)
				l = append(l, originTools{origin: o})
			}
			l[i].tools++
		}
		c.lists[uid] = l
	}
	return res.Err()
}

// session adds one session. start is its first usage row's starting
// context, or -1 when it has none, which leaves the session out.
func (c *contextAgg) session(uid, harness string, start int64, rows []trow) {
	if start < 0 {
		return
	}
	c.start[harness] = append(c.start[harness], start)
	clear(c.called)
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
		if !c.called[u] {
			c.called[u] = true
			c.used[u] = append(c.used[u], start)
		}
	}
	list, ok := c.lists[uid]
	if !ok {
		return
	}
	c.listed[harness]++
	for _, t := range list {
		u := contextUse{harness, t.origin}
		if !c.called[u] {
			c.used[u] = append(c.used[u], start)
		}
		a := c.offered[u]
		if a == nil {
			a = &offeredAgg{}
			c.offered[u] = a
		}
		a.tools = append(a.tools, t.tools)
		if !c.called[u] {
			a.uncalled++
		}
	}
}

func (c *contextAgg) result() ToolContext {
	out := ToolContext{Kind: ToolContextKind, Note: toolContextNote, MinSessions: ToolContextMinSessions, Harnesses: []ContextHarness{}, Rows: []ContextRow{}}
	for h, all := range c.start {
		out.Harnesses = append(out.Harnesses, ContextHarness{Harness: h, All: sideOf(slices.Clone(all)), OfferedSessions: c.listed[h]})
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
		if a := c.offered[u]; a != nil {
			t := sideOf(a.tools)
			row.Offered = ContextOffered{Sessions: t.Sessions, Uncalled: a.uncalled, Tools: t.Median}
		}
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

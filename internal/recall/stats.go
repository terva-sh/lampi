package recall

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

// statsSchema is the rollup of each indexed session by UTC hour, written
// with the session's docs rows and replaced whole when it is indexed
// again (TKT-01M61NTE24T6CZN1P92AV7164F). hour is NULL for events that
// had no recorded time; they count in totals and in no chart.
const statsSchema = `
CREATE TABLE stats (
	session_uid TEXT NOT NULL,
	harness TEXT NOT NULL,
	hour INTEGER,
	kind TEXT NOT NULL,
	name TEXT NOT NULL,
	n INTEGER NOT NULL,
	input INTEGER,
	output INTEGER,
	cache_read INTEGER,
	cache_write INTEGER,
	cost_usd REAL
);
CREATE INDEX stats_session ON stats(session_uid);
CREATE INDEX stats_hour ON stats(hour);
`

// Kinds of stats row. Each counts n events in its hour; usage rows also
// carry token sums.
const (
	// StatEvent counts events by event type.
	StatEvent = "event"
	// StatError counts error events by raw type. Lines the normalizer
	// could not read are StatUnreadable instead: they are faults in the
	// lake's copy, not errors the session met.
	StatError      = "error"
	StatUnreadable = "unreadable"
	// StatToolCall counts tool calls by tool name, and StatToolFailed
	// the results that reported an error, named by their call.
	StatToolCall   = "tool_call"
	StatToolFailed = "tool_failed"
	// StatUsage counts model calls after deduplication, with their
	// tokens.
	StatUsage = "usage"
)

// Tokens is token usage split four ways that do not overlap: Input is
// input read neither from nor into the prompt cache. A nil field was
// reported by no event counted, which is not the same as zero.
type Tokens struct {
	Input      *int64   `json:"input"`
	Output     *int64   `json:"output"`
	CacheRead  *int64   `json:"cache_read"`
	CacheWrite *int64   `json:"cache_write"`
	CostUSD    *float64 `json:"cost_usd"`
}

// Add adds o to t. A kind stays nil only while neither reported it.
func (t *Tokens) Add(o Tokens) {
	addInt := func(dst **int64, v *int64) {
		if v == nil {
			return
		}
		if *dst == nil {
			*dst = new(int64)
		}
		**dst += *v
	}
	addInt(&t.Input, o.Input)
	addInt(&t.Output, o.Output)
	addInt(&t.CacheRead, o.CacheRead)
	addInt(&t.CacheWrite, o.CacheWrite)
	if o.CostUSD != nil {
		if t.CostUSD == nil {
			t.CostUSD = new(float64)
		}
		*t.CostUSD += *o.CostUSD
	}
}

func (t Tokens) empty() bool {
	return t.Input == nil && t.Output == nil && t.CacheRead == nil && t.CacheWrite == nil && t.CostUSD == nil
}

func intPtr(n int64) *int64 { return &n }

func fromUsage(u normalize.Usage) Tokens {
	var t Tokens
	conv := func(p *int) *int64 {
		if p == nil {
			return nil
		}
		return intPtr(int64(*p))
	}
	t.Input, t.Output, t.CacheRead, t.CacheWrite = conv(u.Input), conv(u.Output), conv(u.CacheRead), conv(u.CacheWrite)
	if u.CostUSD != nil {
		c := *u.CostUSD
		t.CostUSD = &c
	}
	return t
}

type statKey struct {
	hour       int64
	timed      bool
	kind, name string
}

type statVal struct {
	n      int64
	tokens Tokens
}

// sessionStats accumulates one session's stats rows while indexSession
// reads its events. The counting rules for usage differ by harness; see
// addUsage.
type sessionStats struct {
	harness string
	rows    map[statKey]*statVal
	// calls names each tool call by its id, so a result that carries
	// only the id is counted against its tool.
	calls map[string]string
	// claude holds the last usage of each Claude message id; Claude
	// Code repeats a response's usage on each of its content blocks.
	claude      map[string]claudeUsage
	claudeOrder []string
	// codexTotal is the baseline a Codex running total is measured
	// from: the last total, plus reports without one since. codexLast
	// is the last total reported.
	codexTotal, codexLast *codexTotals
}

type claudeUsage struct {
	key    statKey
	tokens Tokens
}

type codexTotals struct{ input, cached, output int64 }

func newSessionStats(harness string) *sessionStats {
	return &sessionStats{harness: harness, rows: map[statKey]*statVal{}, calls: map[string]string{}, claude: map[string]claudeUsage{}}
}

const hourNS = int64(3600) * 1e9

func (s *sessionStats) bump(recorded *int64, kind, name string, n int64, t Tokens) {
	k := statKey{kind: kind, name: name}
	if recorded != nil {
		k.timed, k.hour = true, floorHour(*recorded)
	}
	v := s.rows[k]
	if v == nil {
		v = &statVal{}
		s.rows[k] = v
	}
	v.n += n
	v.tokens.Add(t)
}

// add counts one line. ok is false for a line that is not an event.
func (s *sessionStats) add(row *docRow, ev *normalize.Event, ok bool) {
	if !ok {
		s.bump(nil, StatUnreadable, "", 1, Tokens{})
		return
	}
	s.bump(row.recorded, StatEvent, ev.EventType, 1, Tokens{})
	switch ev.EventType {
	case normalize.EventError:
		if _, unreadable := ev.Extra["unreadable_line"]; unreadable {
			s.bump(row.recorded, StatUnreadable, "", 1, Tokens{})
		} else {
			s.bump(row.recorded, StatError, ev.RawType, 1, Tokens{})
		}
	case normalize.EventToolCall:
		name := deref(ev.Tool.Name)
		if ev.Tool.CallID != nil && name != "" {
			s.calls[*ev.Tool.CallID] = name
		}
		s.bump(row.recorded, StatToolCall, name, 1, Tokens{})
	case normalize.EventToolResult:
		if ev.Tool.IsError != nil && *ev.Tool.IsError {
			name := deref(ev.Tool.Name)
			if name == "" && ev.Tool.CallID != nil {
				name = s.calls[*ev.Tool.CallID]
			}
			s.bump(row.recorded, StatToolFailed, name, 1, Tokens{})
		}
	}
	s.addUsage(row, ev)
}

// addUsage counts a model call's tokens once, by the rule for the
// session's harness. The ticket records the measurements behind each
// rule.
func (s *sessionStats) addUsage(row *docRow, ev *normalize.Event) {
	switch s.harness {
	case "claude":
		t := fromUsage(ev.Usage)
		if t.empty() {
			return
		}
		id, _ := ev.Extra["id"].(string)
		if id == "" {
			s.bump(row.recorded, StatUsage, "", 1, t)
			return
		}
		// The last block wins: a streamed response can repeat its
		// usage with a larger output count.
		if _, seen := s.claude[id]; !seen {
			s.claudeOrder = append(s.claudeOrder, id)
		}
		k := statKey{kind: StatUsage}
		if row.recorded != nil {
			k.timed, k.hour = true, floorHour(*row.recorded)
		}
		s.claude[id] = claudeUsage{key: k, tokens: t}
	case "codex":
		if ev.EventType != normalize.EventUsage {
			return
		}
		if cur, ok := codexTotal(ev.Extra); ok {
			// A running total never falls within one process, so a total
			// lower in any kind than the last one reported is a new
			// process counting from zero. The last reported total, not
			// the baseline, is the test: the baseline also holds reports
			// without a total, which a later total may not include.
			if last := s.codexLast; last != nil && (cur.input < last.input || cur.cached < last.cached || cur.output < last.output) {
				s.codexTotal = nil
			}
			s.codexLast = &cur
			prev := codexTotals{}
			if s.codexTotal != nil {
				prev = *s.codexTotal
			}
			d := codexTotals{max(0, cur.input-prev.input), max(0, cur.cached-prev.cached), max(0, cur.output-prev.output)}
			s.codexTotal = &codexTotals{prev.input + d.input, prev.cached + d.cached, prev.output + d.output}
			if d == (codexTotals{}) {
				// The same total again: no model call happened.
				return
			}
			s.bump(row.recorded, StatUsage, "", 1, Tokens{Input: intPtr(max(0, d.input-d.cached)), Output: intPtr(d.output), CacheRead: intPtr(d.cached)})
			return
		}
		t := fromUsage(ev.Usage)
		if t.empty() {
			return
		}
		// A report without a running total still moves the baseline,
		// so a total that follows and includes it does not count it
		// again.
		used := codexTotals{int64(deref0(ev.Usage.Input)), int64(deref0(ev.Usage.CacheRead)), int64(deref0(ev.Usage.Output))}
		if s.codexTotal == nil {
			s.codexTotal = &codexTotals{}
		}
		s.codexTotal.input += used.input
		s.codexTotal.cached += used.cached
		s.codexTotal.output += used.output
		// Codex counts cached input inside input.
		if t.Input != nil && t.CacheRead != nil {
			t.Input = intPtr(max(0, *t.Input-*t.CacheRead))
		}
		s.bump(row.recorded, StatUsage, "", 1, t)
	default:
		t := fromUsage(ev.Usage)
		if !t.empty() {
			s.bump(row.recorded, StatUsage, "", 1, t)
		}
	}
}

func wholeHour(t time.Time) bool { return t.Nanosecond() == 0 && t.Unix()%3600 == 0 }

func deref0(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func floorHour(ns int64) int64 {
	h := ns / hourNS * 3600
	if ns < 0 && ns%hourNS != 0 {
		h -= 3600
	}
	return h
}

// codexTotal reads extra.info.total_token_usage from a Codex
// token_count event.
func codexTotal(extra map[string]any) (codexTotals, bool) {
	info, _ := extra["info"].(map[string]any)
	total, _ := info["total_token_usage"].(map[string]any)
	if total == nil {
		return codexTotals{}, false
	}
	num := func(k string) int64 {
		f, _ := total[k].(float64)
		return int64(f)
	}
	return codexTotals{input: num("input_tokens"), cached: num("cached_input_tokens"), output: num("output_tokens")}, true
}

// write replaces uid's stats rows inside tx.
func (s *sessionStats) write(ctx context.Context, tx *sql.Tx, uid string) error {
	for _, id := range s.claudeOrder {
		u := s.claude[id]
		var rec *int64
		if u.key.timed {
			ns := u.key.hour * 1e9
			rec = &ns
		}
		s.bump(rec, StatUsage, "", 1, u.tokens)
	}
	s.claude, s.claudeOrder = map[string]claudeUsage{}, nil
	if _, err := tx.ExecContext(ctx, `DELETE FROM stats WHERE session_uid=?`, uid); err != nil {
		return err
	}
	const values = "(?,?,?,?,?,?,?,?,?,?,?)"
	var args []any
	rows := 0
	flush := func() error {
		if rows == 0 {
			return nil
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO stats(session_uid,harness,hour,kind,name,n,input,output,cache_read,cache_write,cost_usd) VALUES`+values+strings.Repeat(","+values, rows-1), args...)
		args, rows = args[:0], 0
		return err
	}
	for k, v := range s.rows {
		var hour any
		if k.timed {
			hour = k.hour
		}
		args = append(args, uid, s.harness, hour, k.kind, k.name, v.n, v.tokens.Input, v.tokens.Output, v.tokens.CacheRead, v.tokens.CacheWrite, v.tokens.CostUSD)
		if rows++; rows == 80 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// HarnessTotals is what the indexed sessions of one harness add up to.
type HarnessTotals struct {
	Harness  string `json:"harness"`
	Sessions int64  `json:"sessions"`
	Events   int64  `json:"events"`
	Errors   int64  `json:"errors"`
	// ModelCalls counts the usage reports after deduplication.
	ModelCalls  int64  `json:"model_calls"`
	Tokens      Tokens `json:"tokens"`
	ToolCalls   int64  `json:"tool_calls"`
	ToolsFailed int64  `json:"tool_results_failed"`
	Unreadable  int64  `json:"unreadable_lines"`
}

// Totals is the rollup of every indexed session a scope reads, overall
// and by harness, busiest first.
type Totals struct {
	All       HarnessTotals   `json:"all"`
	Harnesses []HarnessTotals `json:"harnesses"`
	// Coverage says how much the totals include: of the lake for a
	// scope that reads every bay, and otherwise of the sessions the
	// scope reads, so it tells nothing about the others.
	Coverage Coverage `json:"coverage"`
}

// scopeFilter is a condition on stats rows for the sessions scope
// reads. The index holds no bays, so a scope narrower than the lake is
// resolved to session uids in the catalog; uids is nil for a scope that
// reads every bay.
func (x *Index) scopeFilter(ctx context.Context, scope catalog.Scope) (cond string, args []any, uids []string, err error) {
	if scope.All() {
		return "1=1", nil, nil, nil
	}
	uids, err = x.reader.catalog.ScopeSessionUIDs(ctx, scope)
	if err != nil {
		return "", nil, nil, err
	}
	if uids == nil {
		uids = []string{}
	}
	b, err := json.Marshal(uids)
	if err != nil {
		return "", nil, nil, err
	}
	return "session_uid IN (SELECT value FROM json_each(?))", []any{string(b)}, uids, nil
}

// scopeCoverage is Coverage over the sessions in uids alone, the way a
// pass counts the lake, so a viewer who reads some bays learns how much
// of what it reads is counted and nothing about the rest.
func (x *Index) scopeCoverage(ctx context.Context, uids []string) (Coverage, error) {
	cov := Coverage{LastReconcile: x.Coverage().LastReconcile}
	if len(uids) == 0 {
		return cov, nil
	}
	b, err := json.Marshal(uids)
	if err != nil {
		return cov, err
	}
	have := map[string]indexedRow{}
	rows, err := x.db.QueryContext(ctx, `SELECT session_uid,gen,head FROM indexed WHERE session_uid IN (SELECT value FROM json_each(?))`, string(b))
	if err != nil {
		return cov, err
	}
	for rows.Next() {
		var uid string
		var r indexedRow
		if err := rows.Scan(&uid, &r.gen, &r.head); err != nil {
			rows.Close()
			return cov, err
		}
		have[uid] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return cov, err
	}
	sessions, err := x.reader.catalog.PublishedSessions(ctx)
	if err != nil {
		return cov, err
	}
	in := make(map[string]bool, len(uids))
	for _, uid := range uids {
		in[uid] = true
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, p := range sessions {
		if !in[p.UID] || p.State != "ready" {
			continue
		}
		cov.Ready++
		switch r, ok := have[p.UID]; {
		case ok && r.gen == p.Gen && r.head == p.Head:
			cov.Indexed++
		case x.failed[p.UID] == p.Gen && p.Gen != 0:
			cov.Failed++
		default:
			cov.Behind++
		}
	}
	return cov, nil
}

// Totals adds up the stats of every indexed session scope reads.
func (x *Index) Totals(ctx context.Context, scope catalog.Scope) (Totals, error) {
	out := Totals{Harnesses: []HarnessTotals{}}
	cond, args, uids, err := x.scopeFilter(ctx, scope)
	if err != nil {
		return out, err
	}
	if uids == nil {
		out.Coverage = x.Coverage()
	} else if out.Coverage, err = x.scopeCoverage(ctx, uids); err != nil {
		return out, err
	}
	rows, err := x.db.QueryContext(ctx, `SELECT harness,kind,COUNT(DISTINCT session_uid),SUM(n),SUM(input),SUM(output),SUM(cache_read),SUM(cache_write),SUM(cost_usd)
		FROM stats WHERE `+cond+` AND kind IN ('event','error','unreadable','tool_call','tool_failed','usage') GROUP BY harness,kind`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	by := map[string]*HarnessTotals{}
	for rows.Next() {
		var harness, kind string
		var sessions, n int64
		var t Tokens
		if err := rows.Scan(&harness, &kind, &sessions, &n, &t.Input, &t.Output, &t.CacheRead, &t.CacheWrite, &t.CostUSD); err != nil {
			return out, err
		}
		h := by[harness]
		if h == nil {
			h = &HarnessTotals{Harness: harness}
			by[harness] = h
		}
		for _, dst := range []*HarnessTotals{h, &out.All} {
			switch kind {
			case StatEvent:
				dst.Events += n
			case StatError:
				dst.Errors += n
			case StatUnreadable:
				dst.Unreadable += n
			case StatToolCall:
				dst.ToolCalls += n
			case StatToolFailed:
				dst.ToolsFailed += n
			case StatUsage:
				dst.ModelCalls += n
				dst.Tokens.Add(t)
			}
		}
		if kind == StatEvent {
			h.Sessions = sessions
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for _, h := range by {
		out.All.Sessions += h.Sessions
		out.Harnesses = append(out.Harnesses, *h)
	}
	slices.SortFunc(out.Harnesses, func(a, b HarnessTotals) int {
		return cmp.Or(cmp.Compare(b.Sessions, a.Sessions), cmp.Compare(a.Harness, b.Harness))
	})
	return out, nil
}

// SeriesBucket is what the indexed sessions add up to in one time
// bucket. Events with no recorded time are in no bucket.
type SeriesBucket struct {
	Start       time.Time `json:"start"`
	ModelCalls  int64     `json:"model_calls"`
	Tokens      Tokens    `json:"tokens"`
	Errors      int64     `json:"errors"`
	ToolCalls   int64     `json:"tool_calls"`
	ToolsFailed int64     `json:"tool_results_failed"`
}

// Series adds up the stats of the indexed sessions scope reads in
// buckets of width from from until until, which must be whole hours
// apart. An empty harness reads every harness. Every bucket in the
// range is returned, empty or not.
func (x *Index) Series(ctx context.Context, scope catalog.Scope, harness string, from, until time.Time, width time.Duration) ([]SeriesBucket, error) {
	if width < time.Hour || width%time.Hour != 0 || !from.Before(until) || from.Unix()%3600 != 0 || until.Sub(from)%width != 0 {
		return nil, ErrInvalid
	}
	n := int(until.Sub(from) / width)
	out := make([]SeriesBucket, n)
	for i := range out {
		out[i].Start = from.Add(time.Duration(i) * width).UTC()
	}
	cond, args, _, err := x.scopeFilter(ctx, scope)
	if err != nil {
		return nil, err
	}
	w := int64(width / time.Second)
	q := `SELECT (hour-?)/?,kind,SUM(n),SUM(input),SUM(output),SUM(cache_read),SUM(cache_write),SUM(cost_usd)
		FROM stats WHERE hour>=? AND hour<? AND kind IN ('usage','error','tool_call','tool_failed') AND ` + cond
	qargs := append([]any{from.Unix(), w, from.Unix(), until.Unix()}, args...)
	if harness != "" {
		q += ` AND harness=?`
		qargs = append(qargs, harness)
	}
	rows, err := x.db.QueryContext(ctx, q+` GROUP BY 1,2`, qargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var i int
		var kind string
		var sum int64
		var t Tokens
		if err := rows.Scan(&i, &kind, &sum, &t.Input, &t.Output, &t.CacheRead, &t.CacheWrite, &t.CostUSD); err != nil {
			return nil, err
		}
		if i < 0 || i >= n {
			continue
		}
		b := &out[i]
		switch kind {
		case StatUsage:
			b.ModelCalls += sum
			b.Tokens.Add(t)
		case StatError:
			b.Errors += sum
		case StatToolCall:
			b.ToolCalls += sum
		case StatToolFailed:
			b.ToolsFailed += sum
		}
	}
	return out, rows.Err()
}

// NameCount is the count of one name of a kind, such as a tool or an
// error's raw type, in one harness.
type NameCount struct {
	Harness string `json:"harness"`
	Name    string `json:"name"`
	N       int64  `json:"n"`
}

// rangeFilter is the condition shared by reads over a time range: the
// scope, the harness when one is given, and hours in [from, until).
// Both ends must be whole hours, as the rollup keeps no finer time: a
// range from 12:30 would leave out the 12:00 hour's events after 12:30.
func (x *Index) rangeFilter(ctx context.Context, scope catalog.Scope, harness string, from, until time.Time) (string, []any, error) {
	if !from.Before(until) || !wholeHour(from) || !wholeHour(until) {
		return "", nil, ErrInvalid
	}
	cond, args, _, err := x.scopeFilter(ctx, scope)
	if err != nil {
		return "", nil, err
	}
	cond = "hour>=? AND hour<? AND " + cond
	args = append([]any{from.Unix(), until.Unix()}, args...)
	if harness != "" {
		cond += " AND harness=?"
		args = append(args, harness)
	}
	return cond, args, nil
}

// Breakdown counts kind by harness and name over a range, largest
// first.
func (x *Index) Breakdown(ctx context.Context, scope catalog.Scope, harness string, from, until time.Time, kind string) ([]NameCount, error) {
	cond, args, err := x.rangeFilter(ctx, scope, harness, from, until)
	if err != nil {
		return nil, err
	}
	rows, err := x.db.QueryContext(ctx, `SELECT harness,name,SUM(n) FROM stats WHERE kind=? AND `+cond+` GROUP BY harness,name ORDER BY 3 DESC,1,2`, append([]any{kind}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NameCount{}
	for rows.Next() {
		var c NameCount
		if err := rows.Scan(&c.Harness, &c.Name, &c.N); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SessionErrors is one session's errors and failed tool results over a
// range. NativeID and ProjectLabel are the catalog's display labels.
type SessionErrors struct {
	SessionUID   string `json:"session_uid"`
	NativeID     string `json:"native_session_id"`
	ProjectLabel string `json:"project_label"`
	Harness      string `json:"harness"`
	Errors       int64  `json:"errors"`
	ToolsFailed  int64  `json:"tool_results_failed"`
}

// TopErrorSessions ranks the sessions scope reads by errors and failed
// tool results over a range, at most limit of them.
func (x *Index) TopErrorSessions(ctx context.Context, scope catalog.Scope, harness string, from, until time.Time, limit int) ([]SessionErrors, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	cond, args, err := x.rangeFilter(ctx, scope, harness, from, until)
	if err != nil {
		return nil, err
	}
	rows, err := x.db.QueryContext(ctx, `SELECT session_uid,harness,COALESCE(SUM(CASE kind WHEN 'error' THEN n END),0),COALESCE(SUM(CASE kind WHEN 'tool_failed' THEN n END),0)
		FROM stats WHERE kind IN ('error','tool_failed') AND `+cond+` GROUP BY session_uid,harness ORDER BY SUM(n) DESC,session_uid LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	out := []SessionErrors{}
	for rows.Next() {
		var e SessionErrors
		if err := rows.Scan(&e.SessionUID, &e.Harness, &e.Errors, &e.ToolsFailed); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// The labels come from the catalog under the same scope, so a
	// session that left it since the filter was read is dropped.
	kept := out[:0]
	for _, e := range out {
		summary, err := x.reader.catalog.DashboardSession(ctx, scope, e.SessionUID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		e.NativeID, e.ProjectLabel = summary.NativeID, summary.ProjectLabel
		kept = append(kept, e)
	}
	return kept, nil
}

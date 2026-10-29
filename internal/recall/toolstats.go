package recall

import (
	"cmp"
	"context"
	"database/sql"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/toolorigin"
)

// Tool usage statistics over the search index (TKT-01M61MZN8): calls,
// results, sessions, errors, sizes and latency per tool or per origin,
// the largest single blocks, calls over time, and the starting-context
// hint.
//
// Scope. search.db cannot see the catalog's bays, and an aggregate
// cannot drop rows after GROUP BY the way Search drops hits. So the
// catalog lists every ready session in scope first
// (catalog.ReadySessions), the harness and project filters narrow that
// list, and it goes into a temporary table on the request's connection.
// The one query joins it: a row counts only when its session is in
// scope and the index holds the generation the catalog published. A
// stale, pending, failed, purged or out-of-scope session adds nothing,
// not even to a total.
//
// One pass. The query walks each listed session's tool rows in position
// order, and Go aggregates them. Grouping in SQL sorted every row and
// paired each result with its call through a lookup per row, which took
// twice as long as reading the rows (docs/performance.md). Walking a
// session at a time lets Go pair calls and results by call id, drop
// duplicate records, and fill every part of the answer from one read.
// Its memory is the rows of the largest session plus the groups.
//
// Ranges. With Since, a session the lake last accepted a day or more
// before Since is not read: a harness records a row before the lake
// receives it, and the day allows for a clock running ahead. Every row
// of the sessions read is, so a call in the range pairs with a result
// past its end, and only rows in the range are counted. A query of
// docs_call_time for the sessions active in the range lost: its table
// lookups cost as much as the walk when the range covers most rows.
//
// Percentiles. Sizes and latencies are counted into quarter-octave
// buckets (four per power of two). A p50 or p95 is the upper bound of
// the bucket that holds that rank, nearest-rank, so it is at most 19%
// above the true value and never above the exact maximum. Maxima,
// totals and session shares are exact.
//
// Codex code-mode inner calls are the tool calls a cell's script made.
// Their results are data the script handled, not context the model
// read: the cell's own exec result is that. Each group says whether it
// is inner, inner and outer rows never share a group, and a share of
// result bytes is taken within the same class, so no byte is counted
// twice.

// Tool statistics limits.
const (
	// ToolStatsMaxGroups bounds the distinct (harness, tool, model,
	// inner) keys one request reads. Past it the request fails with
	// ErrTooManyValues, as a count does.
	ToolStatsMaxGroups = 10_000
	// ToolStatsMaxLargest bounds the largest blocks listed.
	ToolStatsMaxLargest = 100
	// ToolStatsMaxRows bounds the rows one request reads: the tool calls
	// and results, and for the context hint the usage rows, of every
	// session it walks. Past it the request fails with ErrTooManyValues,
	// and a range, harness or project narrows it. At the rate measured
	// in docs/performance.md it is about four seconds of reading, inside
	// the five-second read context of the routes.
	ToolStatsMaxRows = 2_500_000
)

// The limits in force, which tests lower.
var toolStatsMaxGroups, toolStatsMaxRows = ToolStatsMaxGroups, ToolStatsMaxRows

// Grouping and splitting.
const (
	ToolGroupTool    = "tool"
	ToolGroupOrigin  = "origin"
	ToolSplitHarness = "harness"
	ToolSplitModel   = "model"
)

// codexInnerRawType is the raw_type of a Codex code-mode inner call or
// result, a tool call made from inside an exec cell, when its
// event_type is tool_call or tool_result (TKT-01M61PSX6J,
// docs/harnesses.md). Other item_completed items are unknown events.
// It is the one place the engine names it.
const codexInnerRawType = "item_completed"

// Latency notes.
const (
	// LatencyWaitsOnPerson marks a tool whose call waits for a person to
	// answer, such as Claude Code's AskUserQuestion. Its latency is left
	// out of every group, so it does not swamp the tools it sits with.
	LatencyWaitsOnPerson = "waits_on_person"
	// LatencyCodeModeCell marks a Codex exec cell. A cell yields after
	// about five seconds, so its latency is the cell's, not one tool's.
	LatencyCodeModeCell = "code_mode_cell"
)

// waitsOnPerson names, per harness, the tools whose latency is a
// person's answer time.
var waitsOnPerson = map[string][]string{
	"claude": {"AskUserQuestion", "ExitPlanMode"},
	"codex":  {"request_user_input"},
}

// ToolStatsRequest selects tool rows and says how to group them. The
// zero Scope reads nothing.
type ToolStatsRequest struct {
	Scope catalog.Scope
	// Session filters, as search applies them.
	Harness  string
	Project  string
	Unlinked bool
	// Model matches the model of the call; a result takes its call's.
	Model string
	// Since is inclusive and Until exclusive, on the harness's recorded
	// time. With either set, a row with no recorded time is left out;
	// with neither, such rows count and are reported as undated.
	Since *time.Time
	Until *time.Time
	// Tool matches a tool name exactly. OriginKind and OriginName match
	// ToolOrigin; OriginName needs OriginKind.
	Tool       string
	OriginKind string
	OriginName string
	// GroupBy is ToolGroupTool (the default) or ToolGroupOrigin. Split
	// further splits each group by ToolSplitHarness or ToolSplitModel.
	GroupBy string
	Split   string
	// Bucket, catalog.BucketHour or catalog.BucketDay, adds calls per
	// bucket to each group. The range follows catalog.ActivityRequest:
	// defaults, alignment and the widest range a bucket accepts.
	Bucket string
	// Largest lists that many of the largest single blocks, up to
	// ToolStatsMaxLargest.
	Largest int
	// Context adds the starting-context comparison (ToolContext).
	Context bool
	// Now sets the default bucket range and as_of; zero is time.Now.
	Now time.Time
}

// ToolStats is the answer to a ToolStatsRequest.
type ToolStats struct {
	AsOf    string `json:"as_of"`
	GroupBy string `json:"group_by"`
	Split   string `json:"split,omitempty"`
	// Since and Until are the range applied, as RFC 3339, or null.
	Since *string `json:"since"`
	Until *string `json:"until"`
	// Bucket and BucketStarts describe the per-group Buckets arrays,
	// which hold calls per bucket in this order.
	Bucket       string      `json:"bucket,omitempty"`
	BucketStarts []string    `json:"bucket_starts,omitempty"`
	Totals       ToolTotals  `json:"totals"`
	Groups       []ToolGroup `json:"groups"`
	Largest      []ToolBlock `json:"largest"`
	// Context is present when the request asked for it.
	Context *ToolContext      `json:"context,omitempty"`
	Units   map[string]string `json:"units"`
	// Coverage is the index's, given only to a caller that reads every
	// bay, as for search.
	Coverage Coverage `json:"coverage"`
}

// ToolTotals sums every selected row.
type ToolTotals struct {
	Calls      int64 `json:"calls"`
	Results    int64 `json:"results"`
	Sessions   int64 `json:"sessions"`
	Unanswered int64 `json:"unanswered"`
	Undated    int64 `json:"undated"`
	// ContextResultBytes sums the results the model read, and
	// InnerResultBytes the Codex inner results it did not.
	ContextResultBytes int64   `json:"context_result_bytes"`
	InnerResultBytes   int64   `json:"inner_result_bytes"`
	Buckets            []int64 `json:"buckets,omitempty"`
}

// ToolGroup is one tool, or one origin, with its split.
type ToolGroup struct {
	// Tool is set when grouping by tool.
	Tool    string            `json:"tool,omitempty"`
	Origin  toolorigin.Origin `json:"origin"`
	Harness string            `json:"harness,omitempty"`
	Model   string            `json:"model,omitempty"`
	// Inner marks Codex code-mode inner calls.
	Inner      bool  `json:"inner"`
	Calls      int64 `json:"calls"`
	Results    int64 `json:"results"`
	Sessions   int64 `json:"sessions"`
	Unanswered int64 `json:"unanswered"`
	Undated    int64 `json:"undated"`
	// Errors counts results flagged as failed, out of ErrorKnown
	// results whose harness said either way. ErrorRate is their ratio,
	// or null when no result said.
	Errors     int64    `json:"errors"`
	ErrorKnown int64    `json:"results_with_error_flag"`
	ErrorRate  *float64 `json:"error_rate"`
	Input      Sizes    `json:"input_bytes"`
	Output     Sizes    `json:"result_bytes"`
	// ResultShare is this group's result bytes over all selected result
	// bytes of its class: context results, or inner results.
	ResultShare float64 `json:"result_share"`
	// SessionShare is, over the sessions with result bytes in this
	// group, this group's share of each session's result bytes of its
	// class.
	SessionShare *Shares `json:"session_share"`
	// Latency is call to result, where both carry a harness time, or
	// null when none does.
	Latency     *Latency `json:"latency_ms"`
	LatencyNote string   `json:"latency_note,omitempty"`
	Buckets     []int64  `json:"buckets,omitempty"`
}

// Sizes summarizes the content_bytes of the rows that had content.
type Sizes struct {
	Count int64 `json:"count"`
	Total int64 `json:"total"`
	P50   int64 `json:"p50"`
	P95   int64 `json:"p95"`
	Max   int64 `json:"max"`
}

// Latency is in milliseconds.
type Latency struct {
	Count int64 `json:"count"`
	P50   int64 `json:"p50"`
	P95   int64 `json:"p95"`
	Max   int64 `json:"max"`
}

// Shares are fractions from 0 to 1, over sessions.
type Shares struct {
	Sessions int64   `json:"sessions"`
	P50      float64 `json:"p50"`
	P95      float64 `json:"p95"`
	Max      float64 `json:"max"`
}

// ToolBlock is one large call input or result. It holds no content.
type ToolBlock struct {
	SessionUID string            `json:"session_uid"`
	Generation int64             `json:"generation"`
	Position   int64             `json:"position"`
	Link       string            `json:"link"`
	Harness    string            `json:"harness"`
	Tool       string            `json:"tool"`
	Origin     toolorigin.Origin `json:"origin"`
	EventType  string            `json:"event_type"`
	Inner      bool              `json:"inner"`
	Bytes      int64             `json:"bytes"`
	RecordedAt *string           `json:"recorded_at"`
}

var toolStatsUnits = map[string]string{
	"input_bytes":          "bytes of a call's content_text, its arguments, before index truncation",
	"result_bytes":         "bytes of a result's content_text before index truncation",
	"latency_ms":           "milliseconds from a call's recorded time to its result's",
	"percentiles":          "p50 and p95 by nearest rank, read from quarter-octave buckets: at most 19% above the true value, never above max",
	"result_share":         "fraction of the selected result bytes of the same class, context or inner",
	"session_share":        "fraction of one session's result bytes of the same class, exact, over sessions with result bytes in the group",
	"inner":                "Codex code-mode calls made inside an exec cell; their results are not context",
	"unanswered":           "calls with a call id and no result with that id in the session",
	"undated":              "rows with no recorded time, counted only when no range is given",
	"buckets":              "calls per bucket, by the call's recorded time",
	"context_result_bytes": "result bytes the model read",
	"inner_result_bytes":   "Codex inner result bytes, which the model did not read",
}

func (req *ToolStatsRequest) validate() error {
	switch {
	case !validHarness(req.Harness):
		return &FilterError{"harness"}
	case len(req.Project) > 4096 || req.Unlinked && req.Project != "":
		return &FilterError{"project"}
	case len(req.Model) > filterMaxBytes || !utf8.ValidString(req.Model):
		return &FilterError{"model"}
	case len(req.Tool) > filterMaxBytes || !utf8.ValidString(req.Tool):
		return &FilterError{"tool"}
	case req.OriginKind != "" && !oneOf(req.OriginKind, toolorigin.Kinds):
		return &FilterError{"origin"}
	case req.OriginName != "" && (req.OriginKind == "" || len(req.OriginName) > filterMaxBytes || !utf8.ValidString(req.OriginName)):
		return &FilterError{"origin_name"}
	case req.GroupBy != "" && req.GroupBy != ToolGroupTool && req.GroupBy != ToolGroupOrigin:
		return &FilterError{"group_by"}
	case req.Split != "" && req.Split != ToolSplitHarness && req.Split != ToolSplitModel:
		return &FilterError{"split"}
	case req.Largest < 0 || req.Largest > ToolStatsMaxLargest:
		return &FilterError{"largest"}
	case req.Since != nil && req.Until != nil && !req.Since.Before(*req.Until):
		return &FilterError{"until"}
	}
	if req.GroupBy == "" {
		req.GroupBy = ToolGroupTool
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	if req.Bucket != "" {
		var from, until time.Time
		if req.Since != nil {
			from = *req.Since
		}
		if req.Until != nil {
			until = *req.Until
		}
		r, err := catalog.ActivityRequest{Bucket: req.Bucket, From: from, Until: until, Harness: req.Harness}.Resolve(req.Now)
		if err != nil {
			return &FilterError{"bucket"}
		}
		req.Since, req.Until = &r.From, &r.Until
	}
	return nil
}

func bucketWidth(bucket string) time.Duration {
	if bucket == catalog.BucketHour {
		return time.Hour
	}
	return 24 * time.Hour
}

// toolKey is one row's tool, before Go folds keys into groups. model is
// empty unless the request splits or filters by model.
type toolKey struct {
	harness, tool, model string
	inner                bool
}

// groupKey is one output group.
type groupKey struct {
	tool    string
	origin  toolorigin.Origin
	harness string
	model   string
	inner   bool
}

func (req *ToolStatsRequest) group(k toolKey) groupKey {
	g := groupKey{origin: toolorigin.Of(k.harness, k.tool), inner: k.inner}
	if req.GroupBy == ToolGroupTool {
		g.tool = k.tool
	}
	switch req.Split {
	case ToolSplitHarness:
		g.harness = k.harness
	case ToolSplitModel:
		g.model = k.model
	}
	return g
}

// keep applies the origin filter.
func (req *ToolStatsRequest) keep(k toolKey) bool {
	if req.OriginKind == "" {
		return true
	}
	o := toolorigin.Of(k.harness, k.tool)
	return o.Kind == req.OriginKind && (req.OriginName == "" || o.Name == req.OriginName)
}

// hist counts values in quarter-octave buckets: bucket 0 holds 0 and
// below, and bucket b > 0 holds the integers n with (b-1)/4 <= log2(n) <
// b/4. 2^63 falls in bucket 253.
type hist [256]int64

func bucketOf(n int64) int {
	if n < 1 {
		return 0
	}
	return int(math.Log2(float64(n))*4) + 1
}

// bucketUpper is the largest integer bucket b can hold.
func bucketUpper(b int) int64 {
	if b <= 0 {
		return 0
	}
	return int64(math.Ceil(math.Exp2(float64(b)/4))) - 1
}

// quantile is the nearest-rank p quantile, as the upper bound of its
// bucket, capped at hi, the exact maximum.
func (h *hist) quantile(p float64, hi int64) int64 {
	var n int64
	for _, c := range h {
		n += c
	}
	if n == 0 {
		return 0
	}
	rank := int64(math.Ceil(p * float64(n)))
	var seen int64
	for b, c := range h {
		if seen += c; seen >= rank {
			return min(bucketUpper(b), hi)
		}
	}
	return hi
}

func (h *hist) add(o *hist) {
	for b, c := range o {
		h[b] += c
	}
}

// values sums size or latency values.
type values struct {
	h          hist
	n, sum, hi int64
}

func (v *values) add(x int64) {
	v.h[bucketOf(x)]++
	v.n++
	v.sum += x
	v.hi = max(v.hi, x)
}

func (v *values) merge(o *values) {
	v.h.add(&o.h)
	v.n += o.n
	v.sum += o.sum
	v.hi = max(v.hi, o.hi)
}

func (v *values) sizes() Sizes {
	return Sizes{Count: v.n, Total: v.sum, P50: v.h.quantile(0.5, v.hi), P95: v.h.quantile(0.95, v.hi), Max: v.hi}
}

// groupAgg accumulates one group.
type groupAgg struct {
	calls, results, unanswered, undated int64
	errors, errorKnown, sessions        int64
	in, out, lat                        values
	shares                              []float64
	note                                string
	buckets                             []int64
}

// keyInfo is what a toolKey resolves to, memoized per request so the
// origin rules run once per distinct name.
type keyInfo struct {
	keep  bool
	waits bool
	g     groupKey
	agg   *groupAgg
}

// trow is one row of a session. A null integer is -1 for bytes and at,
// which cannot be negative in the index; err is -1, 0 or 1.
type trow struct {
	pos               int64
	typ, kind         int8
	tool, call, model string
	bytes, at, tokens int64
	err               int8
}

const (
	rowSkip = iota - 1
	rowCall
	rowResult
	rowUsage
)

const (
	kindPlain = iota
	kindInner
	// kindEventMsg is a Codex event_msg *_begin or *_end, which repeats a
	// response_item with the same call id when there is one.
	kindEventMsg
)

// toolAgg is one request's aggregation.
type toolAgg struct {
	req     *ToolStatsRequest
	since   int64
	until   int64
	width   int64
	keys    map[toolKey]*keyInfo
	groups  map[groupKey]*groupAgg
	names   map[string]string
	totals  ToolTotals
	largest []ToolBlock
	ctx     *contextAgg
	// Per session, reused.
	calls    map[string]int
	answered map[string]bool
	plain    map[callRef]bool
	bytes    map[*groupAgg]*sessionBytes
	class    [2]int64
}

// callRef is a call id with the kind of row that carries it.
type callRef struct {
	typ  int8
	call string
}

// sessionBytes is one group's result bytes in one session, and the class
// they count in: 0 for context, 1 for inner.
type sessionBytes struct {
	n   int64
	cls int
}

func newToolAgg(req *ToolStatsRequest, nb int) *toolAgg {
	a := &toolAgg{req: req, since: math.MinInt64, until: math.MaxInt64, keys: map[toolKey]*keyInfo{}, groups: map[groupKey]*groupAgg{},
		names: map[string]string{}, calls: map[string]int{}, answered: map[string]bool{}, plain: map[callRef]bool{}, bytes: map[*groupAgg]*sessionBytes{}}
	if req.Since != nil {
		a.since = req.Since.UnixNano()
	}
	if req.Until != nil {
		a.until = req.Until.UnixNano()
	}
	if nb > 0 {
		a.width = int64(bucketWidth(req.Bucket))
		a.totals.Buckets = make([]int64, nb)
	}
	if req.Context {
		a.ctx = newContextAgg()
	}
	return a
}

// intern keeps one copy of each tool name and model.
func (a *toolAgg) intern(b sql.RawBytes) string {
	if s, ok := a.names[string(b)]; ok {
		return s
	}
	s := string(b)
	a.names[s] = s
	return s
}

func (a *toolAgg) info(k toolKey) (*keyInfo, error) {
	if i, ok := a.keys[k]; ok {
		return i, nil
	}
	if len(a.keys) >= toolStatsMaxGroups {
		return nil, ErrTooManyValues
	}
	i := &keyInfo{keep: a.req.keep(k), waits: slices.Contains(waitsOnPerson[k.harness], k.tool), g: a.req.group(k)}
	if i.keep {
		i.agg = a.groups[i.g]
		if i.agg == nil {
			i.agg = &groupAgg{}
			if a.width > 0 {
				i.agg.buckets = make([]int64, len(a.totals.Buckets))
			}
			a.groups[i.g] = i.agg
		}
		if a.req.GroupBy == ToolGroupTool {
			switch {
			case i.waits:
				i.agg.note = LatencyWaitsOnPerson
			case k.harness == "codex" && k.tool == "exec" && !k.inner:
				i.agg.note = LatencyCodeModeCell
			}
		}
	}
	a.keys[k] = i
	return i, nil
}

// session aggregates one session's rows, in position order.
func (a *toolAgg) session(uid string, gen int64, harness string, rows []trow) error {
	clear(a.calls)
	clear(a.answered)
	clear(a.plain)
	clear(a.bytes)
	a.class = [2]int64{}
	dupes := false
	for i := range rows {
		r := &rows[i]
		if r.call == "" || r.typ == rowUsage {
			continue
		}
		if r.kind == kindEventMsg {
			dupes = true
		} else {
			a.plain[callRef{r.typ, r.call}] = true
		}
	}
	// Pair calls with results over the whole session, so a call in the
	// range pairs with a result past it. A session's result bytes per
	// class count every result in the range, whatever the tool filters
	// keep, so a share is of the whole session.
	var start int64 = -1
	// active is whether any row was recorded in the range. The index's
	// bounds only say the session's span meets it.
	active := false
	for i := range rows {
		r := &rows[i]
		active = active || a.inRange(r.at)
		if r.typ == rowUsage {
			if start < 0 {
				start = r.tokens
			}
			continue
		}
		if dupes && r.kind == kindEventMsg && r.call != "" && a.plain[callRef{r.typ, r.call}] {
			r.typ = rowSkip
			continue
		}
		if r.typ == rowResult && r.bytes > 0 && a.inRange(r.at) {
			a.class[classOf(r.kind == kindInner)] += r.bytes
		}
		if r.call == "" {
			continue
		}
		if r.typ == rowCall {
			if _, ok := a.calls[r.call]; !ok {
				a.calls[r.call] = i
			}
		} else {
			a.answered[r.call] = true
		}
	}
	if a.ctx != nil && active {
		a.ctx.session(uid, harness, start, rows)
	}
	split := a.req.Split == ToolSplitModel || a.req.Model != ""
	counted := false
	for i := range rows {
		r := &rows[i]
		if r.typ == rowUsage || r.typ == rowSkip || !a.inRange(r.at) {
			continue
		}
		var c *trow
		if j, ok := a.calls[r.call]; ok && r.typ == rowResult {
			c = &rows[j]
		}
		// A paired result counts under its call's name. A harness can
		// write one operation's call and result under different names,
		// as Codex does with a shell call and its exec_command_end, and
		// the call's is the one its calls are counted under.
		k := toolKey{harness: harness, tool: r.tool, inner: r.kind == kindInner}
		if c != nil && c.tool != "" {
			k.tool = c.tool
		}
		if split {
			k.model = r.model
			if c != nil && c.model != "" {
				k.model = c.model
			}
		}
		if a.req.Tool != "" && k.tool != a.req.Tool || a.req.Model != "" && k.model != a.req.Model {
			continue
		}
		info, err := a.info(k)
		if err != nil {
			return err
		}
		if !info.keep {
			continue
		}
		g := info.agg
		counted = true
		if r.at < 0 {
			g.undated++
			a.totals.Undated++
		}
		if r.typ == rowCall {
			g.calls++
			a.totals.Calls++
			if r.call != "" && !a.answered[r.call] {
				g.unanswered++
				a.totals.Unanswered++
			}
			if r.bytes >= 0 {
				g.in.add(r.bytes)
			}
			if a.width > 0 && r.at >= 0 {
				b := (r.at - a.since) / a.width
				g.buckets[b]++
				a.totals.Buckets[b]++
			}
		} else {
			g.results++
			a.totals.Results++
			if r.err >= 0 {
				g.errorKnown++
				g.errors += int64(r.err)
			}
			if r.bytes >= 0 {
				g.out.add(r.bytes)
				if k.inner {
					a.totals.InnerResultBytes += r.bytes
				} else {
					a.totals.ContextResultBytes += r.bytes
				}
			}
			// An inner pair recorded at one instant came from one line and
			// has no latency.
			if c != nil && !info.waits && r.at >= 0 && c.at >= 0 && (r.at > c.at || r.at == c.at && !k.inner) {
				g.lat.add(r.at - c.at)
			}
		}
		sb := a.bytes[g]
		if sb == nil {
			sb = &sessionBytes{cls: classOf(k.inner)}
			a.bytes[g] = sb
		}
		if r.typ == rowResult && r.bytes > 0 {
			sb.n += r.bytes
		}
		if a.req.Largest > 0 && r.bytes >= 0 {
			a.offer(uid, gen, harness, k, r)
		}
	}
	if !counted {
		return nil
	}
	a.totals.Sessions++
	for g, sb := range a.bytes {
		g.sessions++
		if total := a.class[sb.cls]; sb.n > 0 && total > 0 {
			g.shares = append(g.shares, float64(sb.n)/float64(total))
		}
	}
	return nil
}

// inRange reports whether a row recorded at at (-1 for none) is in the
// request's range. With no range every row is.
func (a *toolAgg) inRange(at int64) bool {
	if at < 0 {
		return a.req.Since == nil && a.req.Until == nil
	}
	return at >= a.since && at < a.until
}

func classOf(inner bool) int {
	if inner {
		return 1
	}
	return 0
}

// offer keeps r if it is among the largest blocks so far, by bytes and
// then by session and position.
func (a *toolAgg) offer(uid string, gen int64, harness string, k toolKey, r *trow) {
	n := len(a.largest)
	if n == a.req.Largest {
		last := a.largest[n-1]
		if r.bytes < last.Bytes || r.bytes == last.Bytes && (uid > last.SessionUID || uid == last.SessionUID && r.pos > last.Position) {
			return
		}
	}
	b := ToolBlock{SessionUID: uid, Generation: gen, Position: r.pos, Harness: harness, Tool: k.tool, Inner: k.inner, Bytes: r.bytes, EventType: eventTypes[r.typ]}
	if r.at >= 0 {
		s := time.Unix(0, r.at).UTC().Format(time.RFC3339Nano)
		b.RecordedAt = &s
	}
	i, _ := slices.BinarySearchFunc(a.largest, b, func(x, y ToolBlock) int {
		return cmp.Or(cmp.Compare(y.Bytes, x.Bytes), strings.Compare(x.SessionUID, y.SessionUID), cmp.Compare(x.Position, y.Position))
	})
	a.largest = slices.Insert(a.largest, i, b)
	if len(a.largest) > a.req.Largest {
		a.largest = a.largest[:a.req.Largest]
	}
}

var eventTypes = [...]string{rowCall: "tool_call", rowResult: "tool_result", rowUsage: "usage"}

// ToolStats aggregates tool use. It reads on one connection, which holds
// the scope table; the caller bounds the time with ctx, as the read
// routes do.
func (x *Index) ToolStats(ctx context.Context, req ToolStatsRequest) (ToolStats, error) {
	out := ToolStats{Groups: []ToolGroup{}, Largest: []ToolBlock{}, Units: toolStatsUnits}
	if err := req.validate(); err != nil {
		return out, err
	}
	out.AsOf = req.Now.UTC().Format(time.RFC3339Nano)
	out.GroupBy, out.Split, out.Bucket = req.GroupBy, req.Split, req.Bucket
	if req.Since != nil {
		s := req.Since.UTC().Format(time.RFC3339)
		out.Since = &s
	}
	if req.Until != nil {
		s := req.Until.UTC().Format(time.RFC3339)
		out.Until = &s
	}
	if req.Scope.All() {
		out.Coverage = x.Coverage()
	}
	if req.Bucket != "" {
		w := bucketWidth(req.Bucket)
		for t := *req.Since; t.Before(*req.Until); t = t.Add(w) {
			out.BucketStarts = append(out.BucketStarts, t.UTC().Format(time.RFC3339))
		}
	}
	a := newToolAgg(&req, len(out.BucketStarts))
	if err := x.walkTools(ctx, &req, a); err != nil {
		return out, err
	}
	out.Totals = a.totals
	for g, ga := range a.groups {
		if ga.calls == 0 && ga.results == 0 {
			continue
		}
		tg := ToolGroup{Tool: g.tool, Origin: g.origin, Harness: g.harness, Model: g.model, Inner: g.inner,
			Calls: ga.calls, Results: ga.results, Sessions: ga.sessions, Unanswered: ga.unanswered, Undated: ga.undated,
			Errors: ga.errors, ErrorKnown: ga.errorKnown, Input: ga.in.sizes(), Output: ga.out.sizes(),
			LatencyNote: ga.note, Buckets: ga.buckets}
		if ga.errorKnown > 0 {
			r := float64(ga.errors) / float64(ga.errorKnown)
			tg.ErrorRate = &r
		}
		class := a.totals.ContextResultBytes
		if g.inner {
			class = a.totals.InnerResultBytes
		}
		if class > 0 {
			tg.ResultShare = float64(ga.out.sum) / float64(class)
		}
		if s := ga.shares; len(s) > 0 {
			slices.Sort(s)
			tg.SessionShare = &Shares{Sessions: int64(len(s)), P50: nearest(s, 0.5), P95: nearest(s, 0.95), Max: s[len(s)-1]}
		}
		if ga.lat.n > 0 {
			ms := func(ns int64) int64 { return (ns + 999_999) / 1_000_000 }
			tg.Latency = &Latency{Count: ga.lat.n, P50: ms(ga.lat.h.quantile(0.5, ga.lat.hi)), P95: ms(ga.lat.h.quantile(0.95, ga.lat.hi)), Max: ms(ga.lat.hi)}
		}
		out.Groups = append(out.Groups, tg)
	}
	slices.SortFunc(out.Groups, func(a, b ToolGroup) int {
		return cmp.Or(cmp.Compare(b.Calls, a.Calls), cmp.Compare(b.Results, a.Results), cmp.Compare(b.Output.Total, a.Output.Total),
			cmp.Compare(a.Tool, b.Tool), cmp.Compare(a.Origin.Kind, b.Origin.Kind), cmp.Compare(a.Origin.Name, b.Origin.Name),
			cmp.Compare(a.Origin.Server, b.Origin.Server), cmp.Compare(a.Harness, b.Harness), cmp.Compare(a.Model, b.Model),
			boolCmp(a.Inner, b.Inner))
	})
	for _, b := range a.largest {
		b.Origin = toolorigin.Of(b.Harness, b.Tool)
		b.Link = EventLink(b.SessionUID, b.Generation, b.Position)
		out.Largest = append(out.Largest, b)
	}
	if a.ctx != nil {
		c := a.ctx.result()
		out.Context = &c
	}
	return out, nil
}

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// nearest is the nearest-rank p quantile of sorted s.
func nearest(s []float64, p float64) float64 {
	i := int(math.Ceil(p*float64(len(s)))) - 1
	return s[max(0, min(i, len(s)-1))]
}

// scopeSession is one session to walk. Its number is its place in the
// scope table, which the walk reads instead of its uid.
type scopeSession struct {
	uid, harness string
	gen          int64
}

// walkTools lists the sessions to read, loads them into the scope table
// and feeds each session's rows to a.
func (x *Index) walkTools(ctx context.Context, req *ToolStatsRequest, a *toolAgg) error {
	ready, err := x.reader.catalog.ReadySessions(ctx, req.Scope)
	if err != nil {
		return err
	}
	conn, err := x.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var pick []scopeSession
	for _, s := range ready {
		if req.Harness != "" && s.Harness != req.Harness || req.Project != "" && s.ProjectID != req.Project || req.Unlinked && s.ProjectID != "" {
			continue
		}
		pick = append(pick, scopeSession{uid: s.UID, harness: s.Harness, gen: s.Gen})
	}
	if err := fillToolScope(ctx, conn, pick); err != nil {
		return err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `DELETE FROM temp.tool_scope`)
	if a.ctx != nil {
		harness := make(map[string]string, len(pick))
		for _, s := range pick {
			harness[s.uid] = s.harness
		}
		if err := a.ctx.loadLists(ctx, conn, harness); err != nil {
			return err
		}
	}
	res, err := conn.QueryContext(ctx, toolRowsSQL(req), toolRowsArgs(req, a)...)
	if err != nil {
		return err
	}
	defer res.Close()
	var rows []trow
	cur, read := int64(-1), 0
	var tool, call, model sql.RawBytes
	var n, pos, code int64
	var bytes, at, tokens sql.NullInt64
	flush := func() error {
		if cur < 0 {
			return nil
		}
		s := pick[cur]
		return a.session(s.uid, s.gen, s.harness, rows)
	}
	for res.Next() {
		if read++; read > toolStatsMaxRows {
			return ErrTooManyValues
		}
		if err := res.Scan(&n, &pos, &code, &tool, &call, &model, &bytes, &at, &tokens); err != nil {
			return err
		}
		if n != cur {
			if err := flush(); err != nil {
				return err
			}
			cur, rows = n, rows[:0]
		}
		r := trow{pos: pos, typ: int8(code >> 4), kind: int8(code >> 2 & 3), err: int8(code&3) - 1, tool: a.intern(tool), model: a.intern(model), bytes: -1, at: -1, tokens: tokens.Int64}
		if len(call) > 0 {
			r.call = string(call)
		}
		if bytes.Valid {
			r.bytes = bytes.Int64
		}
		if at.Valid {
			r.at = at.Int64
		}
		rows = append(rows, r)
	}
	if err := res.Err(); err != nil {
		return err
	}
	return flush()
}

// toolRowsSQL walks the tool rows of the sessions in the scope table, a
// session at a time in position order, through docs_session. With the
// context hint it reads each session's usage rows too. The row's type,
// kind and error flag share one column, as type*16 + kind*4 + err+1,
// since the driver's cost is per column.
func toolRowsSQL(req *ToolStatsRequest) string {
	model := "NULL"
	if req.Split == ToolSplitModel || req.Model != "" {
		model = "d.model_id"
	}
	types := "d.event_type IN ('tool_call','tool_result')"
	tokens := "NULL"
	if req.Context {
		types = "(" + types + " OR d.input_tokens IS NOT NULL)"
		tokens = "CASE WHEN d.harness='codex' THEN d.input_tokens ELSE d.input_tokens+ifnull(d.cache_read_tokens,0)+ifnull(d.cache_write_tokens,0) END"
	}
	// With a range, a session none of whose rows was recorded in it is
	// skipped by its own first and last recorded time. A session with no
	// recorded time holds only undated rows, which a range leaves out.
	// The bounds are the query's arguments, toolRowsArgs.
	span := ""
	if req.Since != nil || req.Until != nil {
		span = " AND i.last_recorded_ns >= ? AND i.first_recorded_ns < ?"
	}
	return `SELECT s.n, d.pos,
		(CASE d.event_type WHEN 'tool_call' THEN 0 WHEN 'tool_result' THEN 1 ELSE 2 END)*16
		+ (CASE WHEN d.harness='codex' AND d.raw_type='` + codexInnerRawType + `' THEN 1 WHEN d.harness='codex' AND (d.raw_type GLOB '*_begin' OR d.raw_type GLOB '*_end') THEN 2 ELSE 0 END)*4
		+ (CASE WHEN d.tool_error IS NULL THEN 0 WHEN d.tool_error THEN 2 ELSE 1 END),
		d.tool_name, d.call_id, ` + model + `, d.content_bytes, d.recorded_ns, ` + tokens + `
		FROM temp.tool_scope s CROSS JOIN indexed i ON i.session_uid=s.uid AND i.gen=s.gen` + span + `
		CROSS JOIN docs d ON d.session_uid=s.uid
		WHERE ` + types + ` ORDER BY s.n, d.pos`
}

// toolRowsArgs is the arguments toolRowsSQL takes for req: the range's
// bounds when it has one.
func toolRowsArgs(req *ToolStatsRequest, a *toolAgg) []any {
	if req.Since == nil && req.Until == nil {
		return nil
	}
	return []any{a.since, a.until}
}

// fillToolScope loads the sessions into the connection's temporary scope
// table, emptied first, numbered by their place in sessions. A
// temporary table lives in the connection's own temp database, so
// writing it takes no lock on search.db.
func fillToolScope(ctx context.Context, conn *sql.Conn, sessions []scopeSession) error {
	if _, err := conn.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS tool_scope(n INTEGER PRIMARY KEY, uid TEXT NOT NULL, gen INTEGER NOT NULL)`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM temp.tool_scope`); err != nil {
		return err
	}
	const batch = 2000
	args := make([]any, 0, 3*batch)
	flush := func() error {
		if len(args) == 0 {
			return nil
		}
		_, err := conn.ExecContext(ctx, `INSERT INTO temp.tool_scope(n,uid,gen) VALUES(?,?,?)`+strings.Repeat(",(?,?,?)", len(args)/3-1), args...)
		args = args[:0]
		return err
	}
	for i, s := range sessions {
		args = append(args, i, s.uid, s.gen)
		if len(args) == 3*batch {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// Package diagnose reads one session's normalized events once, in
// order, and reports what looks wrong with the session: findings with a
// stable code, a severity, the positions of the events involved, the
// measured values and a suggestion. The codes and their thresholds come
// from docs/session-diagnostics-calibration.md and ship in
// suggestions.json.
//
// A report never carries content. Inputs, results and file paths are
// hashed or measured inside the pass and only counts, sizes and event
// positions leave it; tool names and MCP server names do, as the event
// pages already show them. State is bounded whatever the session's
// length: each table below has a cap, and an event past a full table is
// left out of that one rule rather than grown into.
package diagnose

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

// MaxPositions is the most event positions one finding lists. Count
// says how many events it covers.
const MaxPositions = 16

// Caps on the pass's tables. A real session stays far below each.
const (
	// maxOpenCalls is the calls awaiting a result. Past it the oldest is
	// forgotten, so its result reads as an orphan only if it was not
	// recently closed either; maxClosed is that memory.
	maxOpenCalls = 4096
	maxClosed    = 4096
	// maxReads is the distinct path and range pairs counted.
	maxReads = 4096
	// maxNames is the distinct tool names and origins that get their
	// own finding, and maxNameBytes the longest one kept.
	maxNames     = 256
	maxNameBytes = 256
)

// Report is one session's diagnosis.
type Report struct {
	// SessionUID, Generation and Head name what was read. Analyzer
	// leaves them empty; recall.Reader.Diagnose fills them.
	SessionUID string `json:"session_uid"`
	Generation int64  `json:"generation"`
	Head       string `json:"head_sha256"`
	// Harness is the first event's harness.
	Harness string `json:"harness"`
	// RulesVersion is the version of the rules that made the report.
	RulesVersion int `json:"rules_version"`
	// Events is the lines read, ToolCalls and ToolResults the tool
	// events among them. Oversized counts lines too long to read; the
	// rules could not see them.
	Events      int64 `json:"events"`
	ToolCalls   int64 `json:"tool_calls"`
	ToolResults int64 `json:"tool_results"`
	Oversized   int64 `json:"oversized_events"`
	// Findings are ordered by severity, most severe first, then code.
	// It is empty, never null, for a clean session.
	Findings []Finding `json:"findings"`
}

// Finding is one thing the pass found.
type Finding struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	// Cause separates the rules one code has: for retry_loop
	// same_output or after_error, for context_pressure compactions or
	// cache_collapse, for noisy_tool_origin inner_calls when the bytes
	// are Codex code-mode inner results.
	Cause string `json:"cause,omitempty"`
	// Tool is the tool name for a finding about one tool, and Origin
	// the MCP server or extension for noisy_tool_origin.
	Tool   string `json:"tool,omitempty"`
	Origin string `json:"origin,omitempty"`
	// Count is the events the finding covers, and Positions at most
	// MaxPositions of them, ascending, counted from zero as on an
	// event page.
	Count     int64   `json:"count"`
	Positions []int64 `json:"positions"`
	// Values are the measurements, named per code; sizes are bytes.
	Values     map[string]int64 `json:"values"`
	Suggestion Suggestion       `json:"suggestion"`
}

// Suggestion is the static advice for a code.
type Suggestion struct {
	Code Code   `json:"code"`
	Text string `json:"text"`
	Link string `json:"link,omitempty"`
}

type hash [16]byte

func sum(parts ...string) hash {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	var out hash
	copy(out[:], h.Sum(nil))
	return out
}

// positions collects up to MaxPositions positions and counts them all.
type positions struct {
	list []int64
	n    int64
}

func (p *positions) add(pos int64) {
	p.n++
	if len(p.list) < MaxPositions {
		p.list = append(p.list, pos)
	}
}

// call is one tool call awaiting or holding its result.
type call struct {
	seq     int64
	pos     int64
	pending int
	tool    string
	input   hash
	origin  string
	inner   bool
	// retry is the earlier call in the window this one repeats.
	retry *call
	// sameRun and errorRun count the retries in the chain that ends at
	// this call: repeats, each within the window of the one before,
	// that got the same output or followed a failure. A loop is a chain
	// that reaches the retries threshold; repeats that do not chain are
	// not one.
	sameRun, errorRun int64
	// settled is set once the call's result arrived and its place in its
	// chain is known. A result can arrive before the result of the call
	// it repeats; next is then the repeat waiting on this call.
	settled bool
	next    *call
	// done, result and failed describe the result once it arrived.
	done   bool
	result hash
	failed bool
}

type idSeq struct {
	id  string
	seq int64
}

type usage struct {
	in, out, cr, cw int
	ctx             int64
	at              time.Time
}

type readCount struct {
	n     int64
	fired bool
}

type originPool struct {
	total int64
	bytes map[string]int64
	pos   map[string]*positions
}

// toolRetries holds one tool's loops: a position for each chain that
// reached the threshold, and the longest chain of each cause.
type toolRetries struct {
	sameOutput, afterError positions
	sameMax, errorMax      int64
}

// Analyzer is one streaming pass. Feed it every line of a session's
// events file in order with Line, then call Report. It is not safe for
// concurrent use.
type Analyzer struct {
	rules   *Rules
	polling map[string]bool

	pos     int64
	harness string
	parent  bool
	report  Report

	undecodable positions

	resultsSingle, resultsCount positions
	maxResult                   int64
	inputs                      positions
	maxInput                    int64

	streak, streakStart, longest int64
	streaks                      positions

	seq        int64
	open       map[string]*call
	openOrder  []idSeq
	openNext   int
	closed     map[string]bool
	closedRing []string
	closedNext int
	recent     []*call
	recentNext int
	retries    map[string]*toolRetries

	reads    map[hash]*readCount
	byPath   map[hash][]hash
	repeated positions
	maxReads int64

	compactions     positions
	sinceCompaction bool
	prev            *usage
	collapses       positions
	peak            int64

	sawCall       bool
	resumed       bool
	orphanResults positions
	lastResult    int64

	pools   [2]originPool
	unknown map[string]*positions
}

// New starts a pass under rules, or under Shipped when rules is nil.
func New(rules *Rules) *Analyzer {
	if rules == nil {
		rules = Shipped
	}
	a := &Analyzer{
		rules:           rules,
		polling:         map[string]bool{},
		open:            map[string]*call{},
		closed:          map[string]bool{},
		retries:         map[string]*toolRetries{},
		reads:           map[hash]*readCount{},
		byPath:          map[hash][]hash{},
		sinceCompaction: true,
		lastResult:      -1,
		unknown:         map[string]*positions{},
	}
	for _, t := range rules.PollingTools {
		a.polling[t] = true
	}
	for i := range a.pools {
		a.pools[i] = originPool{bytes: map[string]int64{}, pos: map[string]*positions{}}
	}
	a.recent = make([]*call, rules.t(RetryLoop, "window"))
	return a
}

// event is the part of a normalized event the rules read.
type event struct {
	ParentSessionID *string         `json:"parent_session_id"`
	Harness         string          `json:"harness"`
	RecordedAt      string          `json:"recorded_at"`
	EventType       string          `json:"event_type"`
	ContentText     *string         `json:"content_text"`
	Tool            normalize.Tool  `json:"tool"`
	Usage           normalize.Usage `json:"usage"`
	RawType         string          `json:"raw_type"`
	Extra           struct {
		UnreadableLine *int `json:"unreadable_line"`
		// Order is "unestablished" on a Cursor CLI event whose place
		// among older compaction branches no root gives
		// (docs/harnesses.md). The rules that read sequence skip it.
		Order string `json:"order"`
	} `json:"extra"`
}

// Line reads the next line of the events file, without its newline. A
// nil line is one too long to read, which keeps its position.
func (a *Analyzer) Line(line []byte) {
	pos := a.pos
	a.pos++
	if line == nil {
		a.report.Oversized++
		return
	}
	var ev event
	if json.Unmarshal(line, &ev) != nil {
		a.undecodable.add(pos)
		return
	}
	if a.harness == "" {
		a.harness = ev.Harness
	}
	if ev.ParentSessionID != nil && *ev.ParentSessionID != "" {
		a.parent = true
	}
	text := ""
	if ev.ContentText != nil {
		text = *ev.ContentText
	}
	switch ev.EventType {
	case normalize.EventToolCall:
		a.report.ToolCalls++
		a.toolCall(pos, &ev, text)
		a.sinceCompaction = true
	case normalize.EventToolResult:
		a.report.ToolResults++
		a.toolResult(pos, &ev, text)
		a.sinceCompaction = true
	case normalize.EventMessage:
		a.sinceCompaction = true
	case normalize.EventUsage:
		a.usage(pos, &ev)
	case normalize.EventCompaction:
		// A harness can write one compaction as several events, such as
		// Claude Code's boundary and its summary, or Codex's response
		// item and event message, with usage or meta events between.
		// Count one until a message or a tool event says the session
		// moved on.
		if a.sinceCompaction {
			a.compactions.add(pos)
			a.sinceCompaction = false
		}
	case normalize.EventError:
		if ev.Extra.UnreadableLine != nil {
			a.undecodable.add(pos)
		}
	}
}

func name(s *string) string {
	if s == nil || len(*s) > maxNameBytes {
		return ""
	}
	return *s
}

func (a *Analyzer) toolCall(pos int64, ev *event, text string) {
	in := inner(a.harness, ev.RawType)
	tool := name(ev.Tool.Name)
	unsure := ev.Extra.Order == "unestablished"
	if !in {
		a.sawCall = true
		if n := int64(len(text)); n >= a.rules.t(LargeToolInput, "input_bytes") {
			a.inputs.add(pos)
			a.maxInput = max(a.maxInput, n)
		}
		if path, rng, ok := readArgs(a.harness, tool, text); ok && !unsure {
			a.read(pos, path, rng)
		}
		writePaths(a.harness, tool, text, a.write)
	}
	id := ""
	if ev.Tool.CallID != nil {
		id = *ev.Tool.CallID
	}
	if id != "" && len(id) <= maxNameBytes {
		// Codex writes some calls twice under one id, as a response item
		// and as a begin event; each has its own result. The second
		// record is the same call, so it takes no place in the window.
		if prev, ok := a.open[id]; ok {
			prev.pending++
			return
		}
	}
	c := &call{pos: pos, tool: tool, input: sum(tool, text), inner: in}
	if org := origin(a.harness, tool); len(org) <= maxNameBytes {
		c.origin = org
	}
	if !in && tool != "" && !a.polling[tool] && !unsure {
		for i := range a.recent {
			// Newest first, so a chain of repeats links each call to the
			// one just before it.
			p := a.recent[(a.recentNext-1-i+2*len(a.recent))%len(a.recent)]
			if p != nil && p.tool == tool && p.input == c.input {
				c.retry = p
				break
			}
		}
		// A call leaving the window can no longer be matched, but keeps
		// its link until it settles: the repeat was made within the
		// window, and its result may still be on its way. A settled call
		// has already dropped its link, so a link only joins calls still
		// waiting on results, which the open-call bound limits.
		a.recent[a.recentNext] = c
		a.recentNext = (a.recentNext + 1) % len(a.recent)
	}
	if id == "" || len(id) > maxNameBytes {
		return
	}
	a.seq++
	c.seq, c.pending = a.seq, 1
	a.open[id] = c
	if len(a.openOrder) < maxOpenCalls {
		a.openOrder = append(a.openOrder, idSeq{id, c.seq})
		return
	}
	// Forget the oldest call, and remember it as closed so its late
	// result is not an orphan.
	old := a.openOrder[a.openNext]
	if o, ok := a.open[old.id]; ok && o.seq == old.seq {
		o.pending = 1
		a.close(old.id, o)
	}
	a.openOrder[a.openNext] = idSeq{id, c.seq}
	a.openNext = (a.openNext + 1) % maxOpenCalls
}

func (a *Analyzer) toolResult(pos int64, ev *event, text string) {
	in := inner(a.harness, ev.RawType)
	failed := ev.Tool.IsError != nil && *ev.Tool.IsError
	size := int64(len(text))
	id := ""
	if ev.Tool.CallID != nil {
		id = *ev.Tool.CallID
	}
	c := a.open[id]
	tool := name(ev.Tool.Name)
	if tool == "" && c != nil {
		tool = c.tool
	}

	switch {
	case ev.Extra.Order == "unestablished":
		// Its neighbours are a guess, so it neither extends a streak
		// nor is one: the streak starts again after it.
		a.streak = 0
	case failed:
		if a.streak == 0 {
			a.streakStart = pos
		}
		a.streak++
		a.longest = max(a.longest, a.streak)
		if a.streak == a.rules.t(ErrorStreak, "length") {
			a.streaks.add(a.streakStart)
		}
	default:
		a.streak = 0
	}

	org := ""
	if c != nil {
		org = c.origin
	}
	if org == "" {
		org = origin(a.harness, tool)
	}
	pool := &a.pools[0]
	if in {
		pool = &a.pools[1]
	}
	pool.total += size
	if org != "" && len(org) <= maxNameBytes {
		if _, ok := pool.bytes[org]; ok || len(pool.bytes) < maxNames {
			pool.bytes[org] += size
			if pool.pos[org] == nil {
				pool.pos[org] = &positions{}
			}
			pool.pos[org].add(pos)
		}
	}

	if failed && a.harness == protocol.HarnessClaude && strings.Contains(text, unknownToolMarker) {
		if p := a.unknown[tool]; p != nil {
			p.add(pos)
		} else if len(a.unknown) < maxNames {
			p = &positions{}
			p.add(pos)
			a.unknown[tool] = p
		}
	}

	if in {
		// The script read this result, not the model: no size, retry or
		// orphan rule applies. Close its call so it is not an orphan.
		if c != nil {
			a.close(id, c)
		}
		return
	}
	if size >= a.rules.t(LargeToolResult, "result_bytes") {
		a.resultsSingle.add(pos)
	}
	if size >= a.rules.t(LargeToolResult, "count_bytes") {
		a.resultsCount.add(pos)
	}
	a.maxResult = max(a.maxResult, size)
	a.lastResult = pos

	if c == nil {
		if id != "" && !a.closed[id] {
			if !a.sawCall {
				// A result before any call is the tail of calls made in
				// an earlier file: the session was resumed.
				a.resumed = true
			}
			a.orphanResults.add(pos)
		}
		return
	}
	if !c.done {
		c.done, c.result, c.failed = true, sum(text), failed
		a.settle(c)
	}
	a.close(id, c)
}

// settle places c in its retry chain once c and the call it repeats
// both have results, in whichever order they arrived, then settles the
// repeat that was waiting on c.
func (a *Analyzer) settle(c *call) {
	for c != nil && c.done && !c.settled {
		if p := c.retry; p != nil {
			if !p.settled {
				p.next = c
				return
			}
			a.retried(c, p)
		}
		c.settled, c.retry = true, nil
		n := c.next
		c.next = nil
		c = n
	}
}

// retried extends p's chain to c, its repeat, and records a loop when
// the chain reaches the threshold.
func (a *Analyzer) retried(c, p *call) {
	switch {
	case p.failed:
		c.errorRun = p.errorRun + 1
	case p.result == c.result:
		c.sameRun = p.sameRun + 1
	default:
		return
	}
	r := a.retries[c.tool]
	if r == nil {
		if len(a.retries) >= maxNames {
			return
		}
		r = &toolRetries{}
		a.retries[c.tool] = r
	}
	need := a.rules.t(RetryLoop, "retries")
	if c.errorRun == need {
		r.afterError.add(c.pos)
	}
	if c.sameRun == need {
		r.sameOutput.add(c.pos)
	}
	r.errorMax = max(r.errorMax, c.errorRun)
	r.sameMax = max(r.sameMax, c.sameRun)
}

func (a *Analyzer) close(id string, c *call) {
	if c.pending--; c.pending > 0 {
		return
	}
	delete(a.open, id)
	if a.closed[id] {
		return
	}
	if len(a.closedRing) < maxClosed {
		a.closedRing = append(a.closedRing, id)
	} else {
		delete(a.closed, a.closedRing[a.closedNext])
		a.closedRing[a.closedNext] = id
		a.closedNext = (a.closedNext + 1) % maxClosed
	}
	a.closed[id] = true
}

func (a *Analyzer) read(pos int64, path, rng string) {
	pk, rk := sum(path), sum(path, rng)
	r := a.reads[rk]
	if r == nil {
		if len(a.reads) >= maxReads {
			return
		}
		r = &readCount{}
		a.reads[rk] = r
		a.byPath[pk] = append(a.byPath[pk], rk)
	}
	r.n++
	if r.n >= a.rules.t(RepeatedRead, "reads") {
		a.maxReads = max(a.maxReads, r.n)
		if !r.fired {
			r.fired = true
			a.repeated.add(pos)
		}
	}
}

func (a *Analyzer) write(path string) {
	pk := sum(path)
	for _, rk := range a.byPath[pk] {
		delete(a.reads, rk)
	}
	delete(a.byPath, pk)
}

func (a *Analyzer) usage(pos int64, ev *event) {
	u := usage{in: deref(ev.Usage.Input), out: deref(ev.Usage.Output), cr: deref(ev.Usage.CacheRead), cw: deref(ev.Usage.CacheWrite)}
	if ev.Usage.Input == nil && ev.Usage.CacheRead == nil && ev.Usage.CacheWrite == nil {
		return
	}
	// Codex counts cached input inside input; the others beside it.
	u.ctx = int64(u.in)
	if a.harness != protocol.HarnessCodex {
		u.ctx += int64(u.cr) + int64(u.cw)
	}
	u.at, _ = time.Parse(time.RFC3339Nano, ev.RecordedAt)
	a.peak = max(a.peak, u.ctx)
	p := a.prev
	// Claude Code repeats one message's usage on each of its
	// content-block lines; count it once. A repeat seen later still
	// moves the time a gap is measured from, so a collapse right after
	// it is not taken for cache expiry.
	if p != nil && p.in == u.in && p.out == u.out && p.cr == u.cr && p.cw == u.cw {
		if u.at.After(p.at) {
			p.at = u.at
		}
		return
	}
	a.prev = &u
	// Codex's cached-input count falls often without a gap, so only
	// Claude Code's cache reads are a usable signal.
	if p == nil || a.harness != protocol.HarnessClaude {
		return
	}
	r := a.rules
	if int64(p.cr) <= r.t(ContextPressure, "cache_read_floor") ||
		int64(u.cr)*100 >= int64(p.cr)*r.t(ContextPressure, "cache_drop_percent") ||
		u.ctx*100 < p.ctx*r.t(ContextPressure, "context_keep_percent") {
		return
	}
	// After an idle gap the prompt cache expired: not pressure. A
	// missing time cannot rule a gap out, so it does not count.
	if p.at.IsZero() || u.at.IsZero() || u.at.Sub(p.at) >= time.Duration(r.t(ContextPressure, "idle_seconds"))*time.Second {
		return
	}
	a.collapses.add(pos)
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// Report finishes the pass. Call it once, after the last line.
func (a *Analyzer) Report() Report {
	rep := a.report
	rep.Harness = a.harness
	rep.RulesVersion = a.rules.Version
	rep.Events = a.pos
	rep.Findings = []Finding{}
	add := func(f Finding, p positions) {
		if a.rules.off(f.Code, a.harness) {
			return
		}
		rule := a.rules.Codes[f.Code]
		f.Severity = rule.Severity
		f.Suggestion = Suggestion{Code: f.Code, Text: rule.Suggestion, Link: rule.Link}
		f.Count = p.n
		f.Positions = slices.Clone(p.list)
		if f.Positions == nil {
			f.Positions = []int64{}
		}
		slices.Sort(f.Positions)
		if f.Values == nil {
			f.Values = map[string]int64{}
		}
		rep.Findings = append(rep.Findings, f)
	}
	r := a.rules

	if a.undecodable.n >= r.t(UnreadableLines, "lines") {
		add(Finding{Code: UnreadableLines, Values: map[string]int64{"lines": a.undecodable.n}}, a.undecodable)
	}
	single, count := a.resultsSingle.n > 0, a.resultsCount.n >= r.t(LargeToolResult, "count")
	if single || count {
		p := a.resultsCount
		if single {
			p = a.resultsSingle
		}
		add(Finding{Code: LargeToolResult, Values: map[string]int64{
			"max_bytes": a.maxResult, "over_result_bytes": a.resultsSingle.n, "over_count_bytes": a.resultsCount.n,
		}}, p)
	}
	if a.inputs.n > 0 {
		add(Finding{Code: LargeToolInput, Values: map[string]int64{"max_bytes": a.maxInput, "over_input_bytes": a.inputs.n}}, a.inputs)
	}
	if a.streaks.n > 0 {
		add(Finding{Code: ErrorStreak, Values: map[string]int64{"longest": a.longest, "streaks": a.streaks.n}}, a.streaks)
	}
	for _, tool := range sortedKeys(a.retries) {
		t := a.retries[tool]
		if t.afterError.n > 0 {
			add(Finding{Code: RetryLoop, Cause: "after_error", Tool: tool, Values: map[string]int64{"loops": t.afterError.n, "retries": t.errorMax}}, t.afterError)
		}
		if t.sameOutput.n > 0 {
			add(Finding{Code: RetryLoop, Cause: "same_output", Tool: tool, Values: map[string]int64{"loops": t.sameOutput.n, "retries": t.sameMax}}, t.sameOutput)
		}
	}
	if a.repeated.n > 0 {
		add(Finding{Code: RepeatedRead, Values: map[string]int64{"paths": a.repeated.n, "max_reads": a.maxReads}}, a.repeated)
	}
	if a.compactions.n >= r.t(ContextPressure, "compactions") {
		add(Finding{Code: ContextPressure, Cause: "compactions", Values: map[string]int64{"compactions": a.compactions.n, "peak_context_tokens": a.peak}}, a.compactions)
	}
	if a.collapses.n >= r.t(ContextPressure, "cache_collapses") {
		add(Finding{Code: ContextPressure, Cause: "cache_collapse", Values: map[string]int64{"cache_collapses": a.collapses.n, "peak_context_tokens": a.peak}}, a.collapses)
	}
	// A call after the last result is the session's trailing call: it
	// was interrupted, or the session is live.
	var orphans positions
	var open []int64
	for _, c := range a.open {
		if !c.inner && c.pos < a.lastResult {
			open = append(open, c.pos)
		}
	}
	slices.Sort(open)
	for _, p := range open {
		orphans.add(p)
	}
	if orphans.n >= r.t(OrphanCall, "calls") {
		add(Finding{Code: OrphanCall, Values: map[string]int64{"calls": orphans.n}}, orphans)
	}
	if !a.parent && !a.resumed && a.orphanResults.n >= r.t(OrphanResult, "results") {
		add(Finding{Code: OrphanResult, Values: map[string]int64{"results": a.orphanResults.n}}, a.orphanResults)
	}
	for i, pool := range a.pools {
		for _, org := range sortedKeys(pool.bytes) {
			b := pool.bytes[org]
			if b < r.t(NoisyToolOrigin, "min_bytes") || b*100 < pool.total*r.t(NoisyToolOrigin, "share_percent") {
				continue
			}
			f := Finding{Code: NoisyToolOrigin, Origin: org, Values: map[string]int64{
				"bytes": b, "total_bytes": pool.total, "share_percent": b * 100 / pool.total,
			}}
			if i == 1 {
				f.Cause = "inner_calls"
			}
			add(f, *pool.pos[org])
		}
	}
	for _, tool := range sortedKeys(a.unknown) {
		p := a.unknown[tool]
		if p.n >= r.t(UnknownTool, "calls") {
			add(Finding{Code: UnknownTool, Tool: tool, Values: map[string]int64{"calls": p.n}}, *p)
		}
	}

	slices.SortStableFunc(rep.Findings, func(x, y Finding) int {
		return cmp.Or(y.Severity.rank()-x.Severity.rank(), strings.Compare(string(x.Code), string(y.Code)))
	})
	return rep
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

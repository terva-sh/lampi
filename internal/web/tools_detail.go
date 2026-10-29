package web

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/toolorigin"
)

// The Tools tab's detail (TKT-01M61MZNFY69TNQJWH0PS2WPKS): what the
// stats rollup does not hold, read from the tool statistics engine
// (recall.ToolStats) for the tab's range and harness. Each origin's share
// of calls beside its share of result bytes, result sizes and latency per
// tool, the largest blocks linked to their events, and the
// starting-context hint. It holds names, counts, sizes and positions,
// never content.

const (
	// toolDetailRows bounds the size and latency table, largest result
	// bytes first; the API lists every tool.
	toolDetailRows = 25
	// toolDetailLargest is how many of the largest blocks are listed.
	toolDetailLargest = 20
)

// toolDetailView is the part of the Tools tab the engine fills.
// Problem, when set, says why it is missing.
type toolDetailView struct {
	Problem  string
	Sessions string
	Origins  []originRow
	Sizes    []toolSizeRow
	Hidden   int
	Largest  []blockRow
	Context  *contextView
}

// originRow sums the tools of one origin, so a server whose results
// outweigh its calls stands out. Inner rows are Codex code-mode calls,
// whose shares are taken among inner calls only.
type originRow struct {
	Label                    string
	Inner                    bool
	Calls                    string
	CallShare, ByteShare     float64
	CallPercent, BytePercent string
}

type toolSizeRow struct {
	Name, Origin                string
	Inner                       bool
	Calls, Bytes, Errors, Sizes string
	ErrorTitle, Latency, Tip    string
}

type blockRow struct {
	Tool, Origin, Harness, Kind, Size, Link string
	Inner                                   bool
	Position                                int64
	RecordedAt                              string
}

type contextView struct {
	Note, Harnesses string
	MinSessions     int
	Rows            []contextRow
}

type contextRow struct {
	Origin, Harness                     string
	With, MedianWith, Without, MedianNo string
	Difference                          string
}

// buildToolDetail reads the engine for the tab. A range with too many
// distinct values to count, or one that takes longer than the read
// allows, leaves the rest of the tab in place and says so.
func (s *Server) buildToolDetail(ctx context.Context, v *usagePageView, scope catalog.Scope, from, until time.Time) error {
	st, err := s.index.ToolStats(ctx, recall.ToolStatsRequest{Scope: scope, Harness: v.Harness, Since: &from, Until: &until,
		Largest: toolDetailLargest, Context: true, Now: s.now()})
	switch {
	case err == nil:
		v.Tools.Detail = buildToolDetailView(st)
	case errors.Is(err, recall.ErrTooManyValues):
		v.Tools.Detail.Problem = "There are too many tool calls or distinct tools in this range to measure. Choose a shorter range or a harness."
	case errors.Is(err, context.DeadlineExceeded):
		v.Tools.Detail.Problem = "Measuring the tools took longer than the lake allows. Choose a shorter range or a harness."
	default:
		return err
	}
	return nil
}

func buildToolDetailView(st recall.ToolStats) toolDetailView {
	d := toolDetailView{Sessions: humanCount(st.Totals.Sessions)}
	// Shares are taken within a class, context or Codex inner, as the
	// engine takes result_share, so no call or byte counts twice.
	class := func(inner bool) int {
		if inner {
			return 1
		}
		return 0
	}
	var classCalls, classBytes [2]int64
	for _, g := range st.Groups {
		classCalls[class(g.Inner)] += g.Calls
		classBytes[class(g.Inner)] += g.Output.Total
	}
	share := func(n, total int64) float64 {
		if total == 0 {
			return 0
		}
		return float64(n) / float64(total)
	}
	type okey struct {
		o     toolorigin.Origin
		inner bool
	}
	type osum struct{ calls, bytes int64 }
	sums := map[okey]*osum{}
	var order []okey
	for _, g := range st.Groups {
		k := okey{g.Origin, g.Inner}
		if sums[k] == nil {
			sums[k] = &osum{}
			order = append(order, k)
		}
		sums[k].calls += g.Calls
		sums[k].bytes += g.Output.Total
	}
	for _, k := range order {
		c := class(k.inner)
		o := originRow{Label: originLabel(k.o), Inner: k.inner, Calls: humanCount(sums[k].calls),
			CallShare: share(sums[k].calls, classCalls[c]), ByteShare: share(sums[k].bytes, classBytes[c])}
		o.CallPercent, o.BytePercent = sharePercent(o.CallShare), sharePercent(o.ByteShare)
		d.Origins = append(d.Origins, o)
	}
	slices.SortStableFunc(d.Origins, func(a, b originRow) int {
		return cmp.Or(boolOrder(a.Inner, b.Inner), cmp.Compare(b.ByteShare, a.ByteShare), cmp.Compare(b.CallShare, a.CallShare), cmp.Compare(a.Label, b.Label))
	})

	// One list by result bytes, inner tools among the rest, so the
	// cutoff keeps the biggest producers whatever their class; the inner
	// badge says which bytes the model did not read.
	groups := slices.Clone(st.Groups)
	slices.SortStableFunc(groups, func(a, b recall.ToolGroup) int {
		return cmp.Or(cmp.Compare(b.Output.Total, a.Output.Total), cmp.Compare(b.Calls, a.Calls))
	})
	for _, g := range groups {
		if len(d.Sizes) == toolDetailRows {
			d.Hidden++
			continue
		}
		row := toolSizeRow{Name: cmp.Or(g.Tool, "(unnamed)"), Origin: originLabel(g.Origin), Inner: g.Inner, Calls: humanCount(g.Calls),
			Bytes: bytesIEC(g.Output.Total), Errors: "not recorded", Sizes: "none", Latency: "not recorded"}
		if g.ErrorRate != nil {
			row.Errors = sharePercent(*g.ErrorRate)
			row.ErrorTitle = fmt.Sprintf("%d of %d results with an error flag", g.Errors, g.ErrorKnown)
		}
		if g.Output.Count > 0 {
			row.Sizes = bytesIEC(g.Output.P50) + " · " + bytesIEC(g.Output.P95) + " · " + bytesIEC(g.Output.Max)
		}
		if g.Latency != nil {
			row.Latency = millis(g.Latency.P50) + " · " + millis(g.Latency.P95)
		}
		switch g.LatencyNote {
		case recall.LatencyWaitsOnPerson:
			row.Latency, row.Tip = "waits on a person", "Its latency is a person's answer time, so it is left out."
		case recall.LatencyCodeModeCell:
			row.Tip = "A Codex exec cell: its latency is the whole cell's, which yields after about five seconds."
		}
		d.Sizes = append(d.Sizes, row)
	}

	for _, b := range st.Largest {
		row := blockRow{Tool: cmp.Or(b.Tool, "(unnamed)"), Origin: originLabel(b.Origin), Harness: b.Harness, Kind: "call input",
			Size: bytesIEC(b.Bytes), Link: b.Link, Inner: b.Inner, Position: b.Position}
		if b.EventType == "tool_result" {
			row.Kind = "result"
		}
		if b.RecordedAt != nil {
			row.RecordedAt = *b.RecordedAt
		}
		d.Largest = append(d.Largest, row)
	}

	if c := st.Context; c != nil {
		cv := &contextView{Note: c.Note, MinSessions: c.MinSessions}
		for i, h := range c.Harnesses {
			if i > 0 {
				cv.Harnesses += " "
			}
			cv.Harnesses += h.Harness + ": " + humanCount(h.All.Sessions) + " sessions"
			if h.All.Median != nil {
				cv.Harnesses += ", median " + humanCount(*h.All.Median) + " tokens"
			}
			cv.Harnesses += "."
		}
		median := func(p *int64) string {
			if p == nil {
				return "none"
			}
			return humanCount(*p)
		}
		for _, r := range c.Rows {
			row := contextRow{Origin: originLabel(r.Origin), Harness: r.Harness,
				With: humanCount(r.With.Sessions), MedianWith: median(r.With.Median),
				Without: humanCount(r.Without.Sessions), MedianNo: median(r.Without.Median)}
			switch {
			case r.Suppressed:
				row.Difference = "too few sessions"
			case r.Difference != nil:
				row.Difference = signedCount(*r.Difference) + " tokens"
			}
			cv.Rows = append(cv.Rows, row)
		}
		d.Context = cv
	}
	return d
}

// boolOrder puts false before true.
func boolOrder(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// originLabel names an origin for a person: "built in", "MCP server
// pond".
func originLabel(o toolorigin.Origin) string {
	switch o.Kind {
	case toolorigin.Builtin:
		return "built in"
	case toolorigin.MCP:
		return "MCP server " + o.Name
	case toolorigin.Plugin:
		if o.Server != "" {
			return "plugin " + o.Name + " (" + o.Server + ")"
		}
		return "plugin " + o.Name
	case toolorigin.Extension:
		return "extension " + o.Name
	}
	return "unknown origin"
}

// sharePercent is a fraction from 0 to 1 as a percentage to one
// decimal place.
func sharePercent(f float64) string {
	if f > 0 && f < 0.001 {
		return "<0.1%"
	}
	return strconv.FormatFloat(f*100, 'f', 1, 64) + "%"
}

// signedCount is n with its sign and thousands separators: "+1,204",
// "−87".
func signedCount(n int64) string {
	if n < 0 {
		return "−" + humanCount(-n)
	}
	return "+" + humanCount(n)
}

func millis(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%d ms", ms)
	case ms < 60_000:
		return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + " s"
	}
	return strconv.FormatFloat(float64(ms)/60_000, 'f', 1, 64) + " min"
}

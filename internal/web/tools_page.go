package web

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/toolorigin"
)

// The Tools page (TKT-01M61MZNF): the tool statistics of the browser
// route, for a range preset and the route's filters, with the largest
// blocks linked into their transcripts and the starting-context hint.

// toolsRanges are the Activity page's presets, which keep the calls
// chart within a bucket's widest range, and every recorded time, which
// has no chart.
var toolsRanges = append(slices.Clone(activityRanges), activityRange{Key: "all", Label: "All recorded time"})

const (
	// toolsPageRows is the most groups the table shows, most called
	// first; the API has the rest.
	toolsPageRows = 100
	// toolsPageLargest is how many of the largest blocks are listed.
	toolsPageLargest = 20
)

type toolsView struct {
	Ranges []activityRange
	Range  string
	// Form holds the filters as submitted, for the form and the links.
	Form    url.Values
	Enabled bool
	// Invalid says a filter was refused, and Problem why the read
	// failed when it was too large or too slow.
	Invalid bool
	Problem string
	Stats   recall.ToolStats
	// Chart is calls per bucket; HasChart is false for every recorded
	// time, which has no buckets.
	Chart    chartView
	HasChart bool
	Rows     []toolRow
	Hidden   int
	Origins  []originRow
	Largest  []blockRow
	Coverage *recall.Coverage
}

type toolRow struct {
	Name, Origin                  string
	Inner                         bool
	Calls, Sessions, Errors       string
	ErrorTitle, Sizes, Latency    string
	CallShare, ByteShare          float64
	CallPercent, BytePercent, Tip string
}

// originRow sums the groups of one origin, so the noisy-server case is
// visible whichever grouping the page shows.
type originRow struct {
	Label                    string
	Inner                    bool
	Calls                    int64
	CallShare, ByteShare     float64
	CallPercent, BytePercent string
}

type blockRow struct {
	recall.ToolBlock
	OriginLabel, Kind string
}

// toolsPage renders /tools. Unknown or repeated parameters are refused
// like the other pages; an empty value takes the default.
func (s *Server) toolsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	form := url.Values{}
	for k, v := range q {
		switch {
		case len(v) != 1:
			pageError(w, r, catalog.ErrPage)
			return
		case k == "range":
		case k == "harness" || k == "project" || k == "model" || k == "origin" || k == "group_by":
			form.Set(k, v[0])
		default:
			pageError(w, r, catalog.ErrPage)
			return
		}
	}
	key := cmp.Or(q.Get("range"), "7d")
	i := slices.IndexFunc(toolsRanges, func(p activityRange) bool { return p.Key == key })
	if i < 0 {
		pageError(w, r, catalog.ErrPage)
		return
	}
	v := toolsView{Ranges: toolsRanges, Range: key, Form: form, Enabled: s.index != nil}
	d := pageData{Title: "Tools", View: "tools"}
	if !v.Enabled {
		d.Tools = v
		renderStatus(w, r, d, http.StatusServiceUnavailable)
		return
	}
	api := url.Values{"largest": {strconv.Itoa(toolsPageLargest)}, "context": {"true"}}
	for k := range form {
		api.Set(k, form.Get(k))
	}
	if preset := toolsRanges[i]; preset.Bucket == "" {
		api.Set("all", "true")
	} else {
		resolved, err := catalog.ActivityRequest{Bucket: preset.Bucket}.Resolve(s.now())
		if err != nil {
			pageError(w, r, err)
			return
		}
		api.Set("bucket", preset.Bucket)
		api.Set("since", resolved.Until.Add(-preset.Span).Format(time.RFC3339))
		api.Set("until", resolved.Until.Format(time.RFC3339))
	}
	ctx, cancel := readContext(r)
	defer cancel()
	stats, err := s.toolStats(ctx, scopeOf(r), api)
	status := http.StatusOK
	switch {
	case err == nil:
		v.build(stats)
		d.AsOf = stats.AsOf
		if scopeOf(r).All() {
			v.Coverage = &stats.Coverage
		}
	case errors.Is(err, recall.ErrInvalid):
		v.Invalid, status = true, http.StatusBadRequest
	case errors.Is(err, recall.ErrTooManyValues):
		v.Problem, status = "There are too many tool calls or distinct tools in this range to count. Choose a shorter range, a harness or a project.", http.StatusBadRequest
	case errors.Is(err, context.DeadlineExceeded):
		v.Problem, status = "Counting took longer than the lake allows. Choose a shorter range, a harness or a project.", http.StatusServiceUnavailable
	default:
		pageError(w, r, err)
		return
	}
	d.Tools = v
	renderStatus(w, r, d, status)
}

func (v *toolsView) build(st recall.ToolStats) {
	v.Stats = st
	if st.Bucket != "" {
		starts := make([]time.Time, len(st.BucketStarts))
		calls := make([]*int64, len(st.BucketStarts))
		for i, b := range st.BucketStarts {
			starts[i], _ = time.Parse(time.RFC3339, b)
			if i < len(st.Totals.Buckets) {
				calls[i] = &st.Totals.Buckets[i]
			}
		}
		v.Chart = buildChart("calls", "Tool calls", starts, calls, st.Bucket == catalog.BucketHour, humanCount, niceCeil)
		v.HasChart = true
	}
	// Shares are taken within a class, context or Codex inner, as the
	// engine takes result_share, so no call or byte counts twice.
	var classCalls, classBytes [2]int64
	idx := func(inner bool) int {
		if inner {
			return 1
		}
		return 0
	}
	type okey struct {
		o     toolorigin.Origin
		inner bool
	}
	origins := map[okey]*originRow{}
	var order []okey
	for _, g := range st.Groups {
		classCalls[idx(g.Inner)] += g.Calls
		classBytes[idx(g.Inner)] += g.Output.Total
	}
	share := func(n int64, total int64) float64 {
		if total == 0 {
			return 0
		}
		return float64(n) / float64(total)
	}
	for _, g := range st.Groups {
		c := idx(g.Inner)
		k := okey{g.Origin, g.Inner}
		o := origins[k]
		if o == nil {
			o = &originRow{Label: originLabel(g.Origin), Inner: g.Inner}
			origins[k] = o
			order = append(order, k)
		}
		o.Calls += g.Calls
		o.ByteShare += share(g.Output.Total, classBytes[c])
		if len(v.Rows) == toolsPageRows {
			v.Hidden++
			continue
		}
		row := toolRow{Name: cmp.Or(g.Tool, originLabel(g.Origin)), Origin: originLabel(g.Origin), Inner: g.Inner,
			Calls: humanCount(g.Calls), Sessions: humanCount(g.Sessions), Errors: "not recorded", Sizes: "none", Latency: "not recorded",
			CallShare: share(g.Calls, classCalls[c]), ByteShare: share(g.Output.Total, classBytes[c])}
		row.CallPercent, row.BytePercent = percent(row.CallShare), percent(row.ByteShare)
		if g.ErrorRate != nil {
			row.Errors = percent(*g.ErrorRate)
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
		v.Rows = append(v.Rows, row)
	}
	for _, k := range order {
		o := origins[k]
		o.CallShare = share(o.Calls, classCalls[idx(k.inner)])
		o.CallPercent, o.BytePercent = percent(o.CallShare), percent(o.ByteShare)
		v.Origins = append(v.Origins, *o)
	}
	slices.SortStableFunc(v.Origins, func(a, b originRow) int {
		return cmp.Or(boolOrder(a.Inner, b.Inner), cmp.Compare(b.ByteShare, a.ByteShare), cmp.Compare(b.Calls, a.Calls))
	})
	for _, b := range st.Largest {
		kind := "call input"
		if b.EventType == "tool_result" {
			kind = "result"
		}
		v.Largest = append(v.Largest, blockRow{ToolBlock: b, OriginLabel: originLabel(b.Origin), Kind: kind})
	}
}

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

func percent(f float64) string {
	if f > 0 && f < 0.001 {
		return "<0.1%"
	}
	return strconv.FormatFloat(f*100, 'f', 1, 64) + "%"
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

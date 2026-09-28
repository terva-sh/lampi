package web

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
)

// activityRange is one preset on the Activity page. Every preset ends
// at the end of the current bucket, so its range is whole buckets and
// never trips the cap.
type activityRange struct {
	Key, Label, Bucket string
	Span               time.Duration
}

var activityRanges = []activityRange{
	{"24h", "Last 24 hours, hourly", catalog.BucketHour, 24 * time.Hour},
	{"7d-hourly", "Last 7 days, hourly", catalog.BucketHour, 7 * 24 * time.Hour},
	{"7d", "Last 7 days, daily", catalog.BucketDay, 7 * 24 * time.Hour},
	{"30d", "Last 30 days, daily", catalog.BucketDay, 30 * 24 * time.Hour},
	{"90d", "Last 90 days, daily", catalog.BucketDay, 90 * 24 * time.Hour},
}

// activityView is the Activity page: the form, the two charts and the
// table, all from one catalog read.
type activityView struct {
	Ranges  []activityRange
	Range   string
	Harness string
	// Since is the recording start for display, to the minute in UTC.
	Since    string
	Activity catalog.Activity
	Updates  chartView
	Net      chartView
	Rows     []activityRow
	// Measured is false when no bucket in the range was recorded.
	Measured bool
}

type activityRow struct {
	Start, Coverage, Updates, Net string
}

// activityPage renders /activity. Unknown or repeated parameters are
// refused like the other pages; an empty value takes the default.
func (s *Server) activityPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for k, v := range q {
		if len(v) != 1 || k != "range" && k != "harness" {
			pageError(w, r, catalog.ErrPage)
			return
		}
	}
	key := q.Get("range")
	if key == "" {
		key = "7d"
	}
	var preset *activityRange
	for i := range activityRanges {
		if activityRanges[i].Key == key {
			preset = &activityRanges[i]
		}
	}
	if preset == nil {
		pageError(w, r, catalog.ErrPage)
		return
	}
	now := time.Now()
	req := catalog.ActivityRequest{Bucket: preset.Bucket, Harness: q.Get("harness")}
	resolved, err := req.Resolve(now)
	if err != nil {
		pageError(w, r, err)
		return
	}
	req.From = resolved.Until.Add(-preset.Span)
	ctx, cancel := readContext(r)
	defer cancel()
	a, err := s.catalog.Activity(ctx, req, now)
	if err != nil {
		pageError(w, r, err)
		return
	}
	render(w, r, pageData{Title: "Activity", View: "activity", Poll: true, AsOf: a.AsOf, Activity: buildActivityView(a, key)})
}

func buildActivityView(a catalog.Activity, key string) activityView {
	v := activityView{Ranges: activityRanges, Range: key, Harness: a.Harness, Activity: a}
	if a.CoverageSince != nil {
		if t, err := time.Parse(time.RFC3339Nano, *a.CoverageSince); err == nil {
			v.Since = t.UTC().Format("Jan 2, 2006 15:04 UTC")
		}
	}
	hourly := a.Bucket == catalog.BucketHour
	updates := make([]*int64, len(a.Buckets))
	net := make([]*int64, len(a.Buckets))
	starts := make([]time.Time, len(a.Buckets))
	for i, b := range a.Buckets {
		starts[i], _ = time.Parse(time.RFC3339, b.Start)
		updates[i], net[i] = b.Updates, b.NetLogicalBytes
		row := activityRow{Start: bucketLabel(starts[i], hourly), Coverage: b.Coverage, Updates: "not measured", Net: "not measured"}
		if b.Updates != nil {
			v.Measured = true
			row.Updates = humanCount(*b.Updates)
			row.Net = signedBytes(*b.NetLogicalBytes)
		}
		v.Rows = append(v.Rows, row)
	}
	v.Updates = buildChart("updates", "Accepted head updates", starts, updates, hourly, humanCount, niceCeil)
	v.Net = buildChart("net", "Net logical head-size change", starts, net, hourly, signedBytes, niceBytes)
	return v
}

func bucketLabel(t time.Time, hourly bool) string {
	if hourly {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("Jan 2")
}

func humanCount(n int64) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "−" + s
	}
	return s
}

// signedBytes is a logical size change in binary units with its sign:
// "+1.5 MiB", "−340 B", "0 B".
func signedBytes(n int64) string {
	sign := "+"
	if n < 0 {
		sign = "−"
	}
	if n == 0 {
		sign = ""
	}
	m := math.Abs(float64(n))
	for _, u := range []string{"B", "KiB", "MiB", "GiB", "TiB"} {
		if m < 1024 || u == "TiB" {
			if u == "B" {
				return fmt.Sprintf("%s%d B", sign, int64(m))
			}
			return fmt.Sprintf("%s%.1f %s", sign, m, u)
		}
		m /= 1024
	}
	return ""
}

// Chart geometry, in SVG user units. The chart scales to its container
// through the viewBox.
const (
	chartW, chartH            = 720.0, 220.0
	padL, padR, padT, padB    = 72.0, 12.0, 12.0, 30.0
	plotW, plotH              = chartW - padL - padR, chartH - padT - padB
	maxBarWidth, barGap, barR = 24.0, 2.0, 4.0
	xLabelCount               = 5
)

type chartView struct {
	// ID keeps the hatch pattern of each chart on the page distinct.
	ID       string
	Title    string
	Summary  string
	Bars     []chartBar
	Gaps     []chartRect
	Hits     []chartHit
	Ticks    []chartTick
	XLabels  []chartLabel
	Baseline float64
	W, H     float64
	PlotL    float64
	PlotR    float64
	PlotT    float64
	PlotH    float64
	Negative bool
	Unknown  bool
}

type chartBar struct {
	Path     string
	Negative bool
}

type chartRect struct{ X, W float64 }

type chartHit struct {
	X, W  float64
	Title string
}

type chartTick struct {
	Y     float64
	Label string
}

type chartLabel struct {
	X      float64
	Label  string
	Anchor string
}

// niceCeil is the smallest 1, 2 or 5 times a power of ten at least v.
func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 5, 10} {
		if m*p >= v {
			return m * p
		}
	}
	return 10 * p
}

// niceBytes is niceCeil in the binary unit v falls in, so byte ticks
// read 256 KiB rather than 195.3 KiB.
func niceBytes(v float64) float64 {
	unit := 1.0
	for unit*1024 <= v {
		unit *= 1024
	}
	return niceCeil(v/unit) * unit
}

func buildChart(id, title string, starts []time.Time, values []*int64, hourly bool, format func(int64) string, nice func(float64) float64) chartView {
	c := chartView{ID: id, Title: title, W: chartW, H: chartH, PlotL: padL, PlotR: chartW - padR, PlotT: padT, PlotH: plotH}
	n := len(values)
	if n == 0 {
		return c
	}
	var lo, hi float64
	measured := 0
	for _, v := range values {
		if v == nil {
			c.Unknown = true
			continue
		}
		measured++
		lo, hi = math.Min(lo, float64(*v)), math.Max(hi, float64(*v))
	}
	top := nice(hi)
	bottom := 0.0
	if lo < 0 {
		bottom = -nice(-lo)
		c.Negative = true
	}
	y := func(v float64) float64 { return padT + (top-v)/(top-bottom)*plotH }
	c.Baseline = y(0)
	// A middle tick only where its label can say exactly what it marks:
	// half of 5 updates is not a count.
	ticks := []float64{0, top}
	if bottom < 0 {
		ticks = []float64{bottom, 0, top}
	} else if half := top / 2; half == math.Trunc(half) {
		ticks = []float64{0, half, top}
	}
	for _, t := range ticks {
		c.Ticks = append(c.Ticks, chartTick{Y: y(t), Label: format(int64(t))})
	}
	slot := plotW / float64(n)
	gap := math.Min(barGap, slot*0.25)
	bw := math.Min(maxBarWidth, slot-gap)
	var run *chartRect
	for i, v := range values {
		x := padL + float64(i)*slot
		label := bucketLabel(starts[i], hourly)
		if v == nil {
			if run != nil && math.Abs(run.X+run.W-x) < 1e-6 {
				run.W += slot
			} else {
				c.Gaps = append(c.Gaps, chartRect{X: x, W: slot})
				run = &c.Gaps[len(c.Gaps)-1]
			}
			c.Hits = append(c.Hits, chartHit{X: x, W: slot, Title: label + ": not measured"})
			continue
		}
		c.Hits = append(c.Hits, chartHit{X: x, W: slot, Title: label + ": " + format(*v)})
		if *v == 0 {
			continue
		}
		bx := x + (slot-bw)/2
		c.Bars = append(c.Bars, chartBar{Path: barPath(bx, bw, c.Baseline, y(float64(*v))), Negative: *v < 0})
	}
	seen := map[int]bool{}
	for k := 0; k < xLabelCount; k++ {
		i := k * (n - 1) / (xLabelCount - 1)
		if seen[i] {
			continue
		}
		seen[i] = true
		l := chartLabel{X: padL + float64(i)*slot + slot/2, Label: bucketLabel(starts[i], hourly), Anchor: "middle"}
		switch {
		case i == 0:
			l.X, l.Anchor = padL, "start"
		case i == n-1:
			l.X, l.Anchor = chartW-padR, "end"
		}
		c.XLabels = append(c.XLabels, l)
	}
	c.Summary = chartSummary(title, starts, values, hourly, format, measured)
	return c
}

// barPath is a bar from the baseline to y with its data end rounded and
// its baseline end square.
func barPath(x, w, base, y float64) string {
	h := math.Abs(base - y)
	r := math.Min(barR, math.Min(w/2, h))
	if y < base {
		return fmt.Sprintf("M%.2f %.2fV%.2fQ%.2f %.2f %.2f %.2fH%.2fQ%.2f %.2f %.2f %.2fV%.2fZ",
			x, base, y+r, x, y, x+r, y, x+w-r, x+w, y, x+w, y+r, base)
	}
	return fmt.Sprintf("M%.2f %.2fV%.2fQ%.2f %.2f %.2f %.2fH%.2fQ%.2f %.2f %.2f %.2fV%.2fZ",
		x, base, y-r, x, y, x+r, y, x+w-r, x+w, y, x+w, y-r, base)
}

// chartSummary is the chart's text alternative: range, measured total
// and the largest bucket.
func chartSummary(title string, starts []time.Time, values []*int64, hourly bool, format func(int64) string, measured int) string {
	if measured == 0 {
		return title + ": no bucket in this range was measured."
	}
	var total int64
	best := -1
	for i, v := range values {
		if v == nil {
			continue
		}
		total += *v
		if best < 0 || abs64(*v) > abs64(*values[best]) {
			best = i
		}
	}
	return fmt.Sprintf("%s from %s to %s: %s over %d measured buckets; largest %s on %s.",
		title, bucketLabel(starts[0], hourly), bucketLabel(starts[len(starts)-1], hourly), format(total), measured, format(*values[best]), bucketLabel(starts[best], hourly))
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

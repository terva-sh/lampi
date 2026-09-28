package web

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/storage"
)

// Operations is what the operations page reads from the running
// process rather than the catalog. A nil Operations, or a nil field,
// shows that figure as unknown.
type Operations struct {
	// Version is the build version of the running binary.
	Version string
	// Release is the lake's release, such as v0.1.3, that agents are
	// compared with; empty for a build that is not a release.
	Release string
	// Started is when this serve process started.
	Started time.Time
	// LakeID names the lake.
	LakeID func() string
	// Contacts is the time of each device's last authenticated
	// request, by device id, falling back to its newest report from
	// before Started.
	Contacts func() map[string]time.Time
}

// opsRange is one preset of the operations page's growth charts.
type opsRange struct {
	Key, Label string
	Span       time.Duration
	Hourly     bool
}

// The hourly preset fits inside the 14 days the catalog keeps every
// sample; the daily ones read the last sample of each day.
var opsRanges = []opsRange{
	{"7d", "Last 7 days, hourly", 7 * 24 * time.Hour, true},
	{"30d", "Last 30 days, daily", 30 * 24 * time.Hour, false},
	{"90d", "Last 90 days, daily", 90 * 24 * time.Hour, false},
}

// Freshness of a machine, from the later of its last contact and its
// last new data.
const (
	freshActive = "active" // within a day
	freshIdle   = "idle"   // within a week
	freshQuiet  = "quiet"  // longer ago
	freshNever  = "never"  // nothing recorded
)

// operations is the /api/web/v1/operations body, and what the page
// is built from.
type operations struct {
	AsOf     string       `json:"as_of"`
	Process  opsProcess   `json:"process"`
	Storage  opsStorage   `json:"storage"`
	Growth   opsGrowth    `json:"growth"`
	Queues   opsQueues    `json:"queues"`
	Machines []opsMachine `json:"machines"`
	range_   opsRange     `json:"-"`
	samples  []catalog.StorageSample
}

type opsProcess struct {
	Version       string `json:"version,omitempty"`
	Started       string `json:"started,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds,omitempty"`
	SchemaVersion int    `json:"schema_version"`
	LakeID        string `json:"lake_id,omitempty"`
}

// opsStorage is the newest sample. SampledAt is empty when serve has
// not taken one yet.
type opsStorage struct {
	SampledAt  string                        `json:"sampled_at,omitempty"`
	Components map[string]catalog.StorageUse `json:"components,omitempty"`
	// Total is the sum of the components: the lake directory.
	Total      *int64              `json:"total_bytes,omitempty"`
	Filesystem *storage.Filesystem `json:"filesystem,omitempty"`
	Referenced *catalog.StorageUse `json:"referenced,omitempty"`
	Unique     *catalog.StorageUse `json:"unique,omitempty"`
}

// opsGrowth is the lake directory total and the filesystem's free
// bytes at the end of each bucket in the range. A bucket with no
// sample is null.
type opsGrowth struct {
	Range  string     `json:"range"`
	Bucket string     `json:"bucket"`
	Points []opsPoint `json:"points"`
}

type opsPoint struct {
	Start string `json:"start"`
	Total *int64 `json:"total_bytes"`
	Free  *int64 `json:"free_bytes"`
}

type opsQueues struct {
	AuditPending     int              `json:"audit_pending"`
	NormalizePending int64            `json:"normalize_pending"`
	NormalizeFailed  int64            `json:"normalize_failed"`
	Search           *recall.Coverage `json:"search,omitempty"`
	UploadFiles      *int64           `json:"upload_files,omitempty"`
	UploadBytes      *int64           `json:"upload_bytes,omitempty"`
}

type opsMachine struct {
	Device        string `json:"device,omitempty"`
	State         string `json:"state,omitempty"`
	Source        string `json:"source,omitempty"`
	Profile       string `json:"profile,omitempty"`
	MachineID     string `json:"machine_id,omitempty"`
	LastContact   string `json:"last_contact,omitempty"`
	LastData      string `json:"last_data,omitempty"`
	Freshness     string `json:"freshness"`
	Sessions      int64  `json:"sessions"`
	RecentUpdates int64  `json:"updates_24h"`
	last          time.Time
}

func findOpsRange(key string) (opsRange, bool) {
	if key == "" {
		key = opsRanges[0].Key
	}
	for _, r := range opsRanges {
		if r.Key == key {
			return r, true
		}
	}
	return opsRange{}, false
}

// parseOpsRange accepts at most one range parameter and nothing else.
func parseOpsRange(r *http.Request) (opsRange, error) {
	q := r.URL.Query()
	for k, v := range q {
		if k != "range" || len(v) != 1 {
			return opsRange{}, catalog.ErrPage
		}
	}
	rg, ok := findOpsRange(q.Get("range"))
	if !ok {
		return opsRange{}, catalog.ErrPage
	}
	return rg, nil
}

func (s *Server) operations(w http.ResponseWriter, r *http.Request) {
	rg, err := parseOpsRange(r)
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readOperations(ctx, rg, s.now())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) operationsPage(w http.ResponseWriter, r *http.Request) {
	rg, err := parseOpsRange(r)
	if err != nil {
		pageError(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readOperations(ctx, rg, s.now())
	if err != nil {
		pageError(w, err)
		return
	}
	render(w, r, pageData{Title: "Operations", View: "operations", AsOf: v.AsOf, Ops: buildOpsView(v, s.now())})
}

func (s *Server) readOperations(ctx context.Context, rg opsRange, now time.Time) (operations, error) {
	now = now.UTC()
	v := operations{AsOf: now.Format(time.RFC3339Nano), range_: rg}
	if o := s.ops; o != nil {
		v.Process.Version = o.Version
		if !o.Started.IsZero() {
			v.Process.Started = o.Started.UTC().Format(time.RFC3339)
			v.Process.UptimeSeconds = int64(now.Sub(o.Started).Seconds())
		}
		if o.LakeID != nil {
			v.Process.LakeID = o.LakeID()
		}
	}
	var err error
	if v.Process.SchemaVersion, err = s.catalog.SchemaVersion(ctx); err != nil {
		return v, err
	}

	latest, ok, err := s.catalog.LatestStorage(ctx)
	if err != nil {
		return v, err
	}
	if ok {
		v.Storage = storageOf(latest)
		if u, ok := latest.Measures[storage.Uploads]; ok {
			v.Queues.UploadFiles, v.Queues.UploadBytes = &u.Files, &u.Bytes
		}
	}
	start, until, step := growthWindow(rg, now)
	v.samples, err = s.catalog.StorageSamples(ctx, start, until)
	if err != nil {
		return v, err
	}
	v.Growth = buildGrowth(rg, v.samples, start, until, step)

	if v.Queues.AuditPending, err = s.catalog.PendingAudit(ctx); err != nil {
		return v, err
	}
	overview, err := s.catalog.DashboardOverview(ctx)
	if err != nil {
		return v, err
	}
	v.Queues.NormalizePending = overview.Normalization["pending"]
	v.Queues.NormalizeFailed = overview.Normalization["failed"]
	if s.index != nil {
		c := s.index.Coverage()
		v.Queues.Search = &c
	}

	v.Machines, err = s.readMachines(ctx, now)
	return v, err
}

// storageOf splits a sample into the lake directory's components and
// the other measures.
func storageOf(sample catalog.StorageSample) opsStorage {
	st := opsStorage{SampledAt: sample.At.UTC().Format(time.RFC3339), Components: map[string]catalog.StorageUse{}}
	var total int64
	for _, c := range storage.Components {
		if u, ok := sample.Measures[c]; ok {
			st.Components[c] = u
			total += u.Bytes
		}
	}
	st.Total = &total
	ft, okT := sample.Measures[catalog.MeasureFSTotal]
	ff, okF := sample.Measures[catalog.MeasureFSFree]
	if okT && okF {
		st.Filesystem = &storage.Filesystem{Total: uint64(ft.Bytes), Free: uint64(ff.Bytes)}
	}
	if u, ok := sample.Measures[catalog.MeasureReferenced]; ok {
		st.Referenced = &u
	}
	if u, ok := sample.Measures[catalog.MeasureUnique]; ok {
		st.Unique = &u
	}
	return st
}

// growthWindow is the range's buckets: whole UTC hours or days ending
// with the one now falls in.
func growthWindow(rg opsRange, now time.Time) (start, until time.Time, step time.Duration) {
	step = 24 * time.Hour
	if rg.Hourly {
		step = time.Hour
	}
	until = now.Truncate(step).Add(step)
	return until.Add(-rg.Span), until, step
}

// buildGrowth takes the last sample in each bucket.
func buildGrowth(rg opsRange, samples []catalog.StorageSample, start, until time.Time, step time.Duration) opsGrowth {
	g := opsGrowth{Range: rg.Key, Bucket: "day"}
	if rg.Hourly {
		g.Bucket = "hour"
	}
	n := int(until.Sub(start) / step)
	g.Points = make([]opsPoint, n)
	for i := range g.Points {
		g.Points[i].Start = start.Add(time.Duration(i) * step).Format(time.RFC3339)
	}
	for _, s := range samples {
		// Division truncates toward zero, so a sample just before start
		// would land in the first bucket without this check.
		if s.At.Before(start) {
			continue
		}
		i := int(s.At.Sub(start) / step)
		if i >= n {
			continue
		}
		st := storageOf(s)
		g.Points[i].Total = st.Total
		if st.Filesystem != nil {
			free := int64(st.Filesystem.Free)
			g.Points[i].Free = &free
		} else {
			g.Points[i].Free = nil
		}
	}
	return g
}

// readMachines joins each device with its machine's activity, and adds
// the machines bound to no device: those that posted before devices
// were recorded, or under a token that is gone.
func (s *Server) readMachines(ctx context.Context, now time.Time) ([]opsMachine, error) {
	activity, err := s.catalog.MachinesActivity(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	devices, err := s.catalog.Devices(ctx)
	if err != nil {
		return nil, err
	}
	var contacts map[string]time.Time
	if s.ops != nil && s.ops.Contacts != nil {
		contacts = s.ops.Contacts()
	}
	byMachine := make(map[string]catalog.MachineActivity, len(activity))
	for _, a := range activity {
		byMachine[a.MachineID] = a
	}
	// Empty is [], not null: lists in this API are arrays.
	out := []opsMachine{}
	bound := map[string]bool{}
	for _, d := range devices {
		m := opsMachine{Device: d.Name, State: d.State(), Source: d.Source, Profile: d.Profile, MachineID: d.MachineID}
		if d.MachineID != "" {
			bound[d.MachineID] = true
			m.addActivity(byMachine[d.MachineID])
		}
		if t, ok := contacts[d.ID]; ok {
			m.LastContact = t.UTC().Format(time.RFC3339)
			if t.After(m.last) {
				m.last = t
			}
		}
		out = append(out, m)
	}
	for _, a := range activity {
		if bound[a.MachineID] {
			continue
		}
		m := opsMachine{MachineID: a.MachineID}
		m.addActivity(a)
		out = append(out, m)
	}
	for i := range out {
		out[i].Freshness = freshness(out[i].last, now)
	}
	// Active devices first, then by how recently each was heard from.
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := out[i].State == "revoked", out[j].State == "revoked"
		if ri != rj {
			return rj
		}
		return out[i].last.After(out[j].last)
	})
	return out, nil
}

func (m *opsMachine) addActivity(a catalog.MachineActivity) {
	m.Sessions, m.RecentUpdates = a.Sessions, a.RecentUpdates
	last := a.LastUpload
	if a.LastUpdate.After(last) {
		last = a.LastUpdate
	}
	if !last.IsZero() {
		m.LastData = last.UTC().Format(time.RFC3339)
		if last.After(m.last) {
			m.last = last
		}
	}
}

func freshness(last, now time.Time) string {
	switch {
	case last.IsZero():
		return freshNever
	case now.Sub(last) < 24*time.Hour:
		return freshActive
	case now.Sub(last) < 7*24*time.Hour:
		return freshIdle
	}
	return freshQuiet
}

// opsView is the operations page.
type opsView struct {
	Ranges    []opsRange
	Range     string
	Ops       operations
	Uptime    string
	SampleAge string
	// Rows are the components, largest first, with their share of the
	// lake directory and the change over the range.
	Rows       []storageRow
	Total      string
	FSUsed     string
	FSFree     string
	FSTotal    string
	FSUsedPct  int
	Dedup      string
	Disk       chartView
	Free       chartView
	HasFree    bool
	MachineCap int
}

type storageRow struct {
	Name, Label, Bytes, Files, Change, Hint string
	Share                                   int64
}

var componentLabels = map[string][2]string{
	storage.CAS:        {"Stored blobs", "Transcripts and exports as uploaded, one copy per digest"},
	storage.Uploads:    {"Uploads in progress", "Partial uploads and temp files; start-up sweeps those a day old"},
	storage.Catalog:    {"Catalog", "catalog.db with its write-ahead log"},
	storage.Normalized: {"Normalized events", "Derived JSONL, rebuilt by re-normalization"},
	storage.Parquet:    {"Parquet export", "Derived partitions for analysis"},
	storage.Search:     {"Search index", "Derived; rebuilt from the catalog when deleted"},
	storage.Audit:      {"Audit log", "audit.jsonl"},
	storage.Other:      {"Other", "Identity, tokens, profiles and locks"},
}

func buildOpsView(o operations, now time.Time) opsView {
	v := opsView{Ranges: opsRanges, Range: o.range_.Key, Ops: o}
	if o.Process.UptimeSeconds > 0 {
		v.Uptime = humanDuration(time.Duration(o.Process.UptimeSeconds) * time.Second)
	}
	st := o.Storage
	if st.SampledAt != "" {
		if t, err := time.Parse(time.RFC3339, st.SampledAt); err == nil {
			v.SampleAge = humanDuration(now.Sub(t))
		}
		first := map[string]catalog.StorageUse{}
		if len(o.samples) > 0 {
			first = o.samples[0].Measures
		}
		for _, c := range storage.Components {
			u := st.Components[c]
			row := storageRow{Name: c, Label: componentLabels[c][0], Hint: componentLabels[c][1], Bytes: bytesIEC(u.Bytes), Files: humanCount(u.Files)}
			if *st.Total > 0 {
				row.Share = int64(math.Round(float64(u.Bytes) / float64(*st.Total) * 1000))
			}
			if f, ok := first[c]; ok && len(o.samples) > 1 {
				row.Change = signedBytes(u.Bytes - f.Bytes)
			}
			v.Rows = append(v.Rows, row)
		}
		sort.SliceStable(v.Rows, func(i, j int) bool { return st.Components[v.Rows[i].Name].Bytes > st.Components[v.Rows[j].Name].Bytes })
		v.Total = bytesIEC(*st.Total)
		if fs := st.Filesystem; fs != nil && fs.Total > 0 {
			used := fs.Total - min(fs.Free, fs.Total)
			v.FSUsed, v.FSFree, v.FSTotal = bytesIEC(int64(used)), bytesIEC(int64(fs.Free)), bytesIEC(int64(fs.Total))
			v.FSUsedPct = int(math.Round(float64(used) / float64(fs.Total) * 100))
		}
		if st.Referenced != nil && st.Unique != nil && st.Unique.Bytes > 0 {
			v.Dedup = fmt.Sprintf("%.2f×", float64(st.Referenced.Bytes)/float64(st.Unique.Bytes))
		}
	}
	starts := make([]time.Time, len(o.Growth.Points))
	disk := make([]*int64, len(starts))
	free := make([]*int64, len(starts))
	for i, p := range o.Growth.Points {
		starts[i], _ = time.Parse(time.RFC3339, p.Start)
		disk[i], free[i] = p.Total, p.Free
		if p.Free != nil {
			v.HasFree = true
		}
	}
	hourly := o.range_.Hourly
	v.Disk = buildChart("disk", "Lake directory disk use", starts, disk, hourly, bytesIEC, niceBytes)
	v.Disk.Summary = levelSummary(v.Disk.Title, starts, disk, hourly)
	v.Free = buildChart("free", "Free space on the lake's filesystem", starts, free, hourly, bytesIEC, niceBytes)
	v.Free.Summary = levelSummary(v.Free.Title, starts, free, hourly)
	return v
}

// levelSummary describes a chart of levels, where a sum over buckets
// means nothing: the latest, lowest and highest measured value.
func levelSummary(title string, starts []time.Time, values []*int64, hourly bool) string {
	last, lo, hi := -1, -1, -1
	for i, v := range values {
		if v == nil {
			continue
		}
		last = i
		if lo < 0 || *v < *values[lo] {
			lo = i
		}
		if hi < 0 || *v > *values[hi] {
			hi = i
		}
	}
	if last < 0 {
		return title + ": no sample in this range."
	}
	return fmt.Sprintf("%s from %s to %s: %s at %s; lowest %s at %s, highest %s at %s.", title,
		bucketLabel(starts[0], hourly), bucketLabel(starts[len(starts)-1], hourly),
		bytesIEC(*values[last]), bucketLabel(starts[last], hourly),
		bytesIEC(*values[lo]), bucketLabel(starts[lo], hourly),
		bytesIEC(*values[hi]), bucketLabel(starts[hi], hourly))
}

// bytesIEC is a size in binary units without a sign.
func bytesIEC(n int64) string {
	return strings.TrimPrefix(signedBytes(n), "+")
}

// humanDuration is a coarse age: "45 s", "12 min", "5 h 3 min", "4 d 2 h".
func humanDuration(d time.Duration) string {
	switch {
	case d < 0:
		return "0 s"
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d d %d h", int(d.Hours())/24, int(d.Hours())%24)
}

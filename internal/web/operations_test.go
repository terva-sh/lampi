package web

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/storage"
	"terva.sh/lampi/internal/webconfig"
)

// TKT-01M3JV45Z: the operations API and page show the newest storage
// sample, the queues, the process, and each machine with its last
// contact and last new data.
func TestOperationsFromIngestToPage(t *testing.T) {
	lake, idp, _, _ := fixture(t)
	started := time.Now().Add(-90 * time.Minute)
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer"}}}
	var err error
	lake.Web, err = New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), indexes[lake], nil, &Operations{
		Version:  "v9.9.9 (abc)",
		Started:  started,
		LakeID:   func() string { return "lake_test" },
		Contacts: lake.Contacts,
	}, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	h := lake.Handler()
	cookie, _ := signIn(t, idp, h)

	// Before serve has sampled, the page says so rather than showing zeros.
	if w := get(h, "/operations", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "No storage sample yet") {
		t.Fatalf("unsampled page: %d", w.Code)
	}

	upload(t, h, "ops-session", []byte(`{"type":"user","text":"pond"}`+"\n"))
	if _, err := lake.SampleStorage(t.Context()); err != nil {
		t.Fatal(err)
	}

	w := get(h, "/api/web/v1/operations", cookie)
	if w.Code != 200 {
		t.Fatalf("api: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Process opsProcess `json:"process"`
		Storage struct {
			SampledAt  string                        `json:"sampled_at"`
			Components map[string]catalog.StorageUse `json:"components"`
			Total      int64                         `json:"total_bytes"`
			Referenced catalog.StorageUse            `json:"referenced"`
		} `json:"storage"`
		Growth   opsGrowth    `json:"growth"`
		Queues   opsQueues    `json:"queues"`
		Machines []opsMachine `json:"machines"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Process.Version != "v9.9.9 (abc)" || got.Process.LakeID != "lake_test" || got.Process.UptimeSeconds < 5000 || got.Process.SchemaVersion < 11 {
		t.Errorf("process %+v", got.Process)
	}
	if got.Storage.SampledAt == "" || got.Storage.Total <= 0 || len(got.Storage.Components) != len(storage.Components) || got.Storage.Components[storage.CAS].Files != 1 {
		t.Errorf("storage %+v", got.Storage)
	}
	if got.Storage.Referenced.Files != 1 {
		t.Errorf("referenced %+v", got.Storage.Referenced)
	}
	if got.Growth.Range != "7d" || got.Growth.Bucket != "hour" || len(got.Growth.Points) != 7*24 || got.Growth.Points[len(got.Growth.Points)-1].Total == nil {
		t.Errorf("growth %s/%s with %d points", got.Growth.Range, got.Growth.Bucket, len(got.Growth.Points))
	}
	if got.Queues.Search == nil || got.Queues.UploadFiles == nil {
		t.Errorf("queues %+v", got.Queues)
	}
	// The token Allow enrolled made the upload: its device has a
	// contact, and the machine it posted as is bound to no device.
	var device, machine *opsMachine
	for i := range got.Machines {
		m := &got.Machines[i]
		if m.Device != "" && m.LastContact != "" {
			device = m
		}
		if m.MachineID == "synthetic-machine" {
			machine = m
		}
	}
	if device == nil || device.Freshness != freshActive {
		t.Errorf("no device with a contact: %+v", got.Machines)
	}
	if machine == nil || machine.Device != "" || machine.LastData == "" || machine.Sessions != 1 || machine.RecentUpdates != 1 || machine.Freshness != freshActive {
		t.Errorf("machine %+v", machine)
	}

	w = get(h, "/operations?range=30d", cookie)
	body := w.Body.String()
	for _, want := range []string{"Where the space goes", "Stored blobs", `href="/operations" aria-current="page"`, "lake_test", "Unregistered machine", "fresh-active", "Last 30 days, daily"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Count(body, `<figure class="panel chart">`) != 2 {
		t.Errorf("page has %d charts", strings.Count(body, `<figure class="panel chart">`))
	}
	for _, bad := range []string{"/operations?range=1y", "/operations?range=7d&range=30d", "/operations?x=1", "/api/web/v1/operations?range=1y"} {
		if w := get(h, bad, cookie); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	if w := get(h, "/api/web/v1/operations", nil); w.Code != 401 {
		t.Errorf("signed out: %d", w.Code)
	}
}

func TestGrowthBucketsTakeTheLastSample(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 30, 0, 0, time.UTC)
	rg, _ := findOpsRange("30d")
	start, until, step := growthWindow(rg, now)
	if !until.Equal(time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)) || step != 24*time.Hour || until.Sub(start) != 30*24*time.Hour {
		t.Fatalf("window %s %s %s", start, until, step)
	}
	sample := func(at time.Time, cas, free int64) catalog.StorageSample {
		return catalog.StorageSample{At: at, Measures: map[string]catalog.StorageUse{
			storage.CAS: {Bytes: cas}, catalog.MeasureFSTotal: {Bytes: 1 << 40}, catalog.MeasureFSFree: {Bytes: free},
		}}
	}
	samples := []catalog.StorageSample{
		sample(now.Add(-48*time.Hour), 100, 900),
		sample(now.Add(-2*time.Hour), 200, 800),
		sample(now.Add(-time.Hour), 300, 700),
		sample(start.Add(-time.Hour), 1, 1),
	}
	g := buildGrowth(rg, samples, start, until, step)
	last := g.Points[len(g.Points)-1]
	if last.Total == nil || *last.Total != 300 || *last.Free != 700 {
		t.Fatalf("today %+v", last)
	}
	if p := g.Points[len(g.Points)-3]; p.Total == nil || *p.Total != 100 {
		t.Fatalf("two days ago %+v", p)
	}
	if p := g.Points[len(g.Points)-2]; p.Total != nil || p.Free != nil {
		t.Fatalf("yesterday was not sampled: %+v", p)
	}
	starts := make([]time.Time, len(g.Points))
	totals := make([]*int64, len(g.Points))
	for i, p := range g.Points {
		starts[i], _ = time.Parse(time.RFC3339, p.Start)
		totals[i] = p.Total
	}
	if s := levelSummary("Disk", starts, totals, false); !strings.Contains(s, "300 B at Sep 28; lowest 100 B at Sep 26, highest 300 B at Sep 28") {
		t.Fatalf("summary %q", s)
	}
}

func TestFreshnessAndDurations(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for last, want := range map[time.Time]string{
		{}:                            freshNever,
		now.Add(-time.Hour):           freshActive,
		now.Add(-3 * 24 * time.Hour):  freshIdle,
		now.Add(-10 * 24 * time.Hour): freshQuiet,
	} {
		if got := freshness(last, now); got != want {
			t.Errorf("freshness(%s) = %s, want %s", last, got, want)
		}
	}
	for d, want := range map[time.Duration]string{
		30 * time.Second:             "30 s",
		12 * time.Minute:             "12 min",
		5*time.Hour + 3*time.Minute:  "5 h 3 min",
		4*24*time.Hour + 2*time.Hour: "4 d 2 h",
	} {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%s) = %q, want %q", d, got, want)
		}
	}
}

// TKT-01M3JV45Z: a lake with no machines lists them as [], not null.
func TestNoMachinesIsAnEmptyArray(t *testing.T) {
	cat, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cat.Close() })
	s := &Server{catalog: cat}
	v, err := s.readOperations(t.Context(), opsRanges[0], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if !strings.Contains(string(raw), `"machines":[]`) {
		t.Fatalf("machines on an empty lake: %s", raw)
	}
}

// The storage card divides the current versions' bytes, the raw files
// the machines hold, by the stored blobs' disk use. Dividing every
// artifact row's bytes read 50x on a lake with no duplicate content,
// because each continuation of a session is a row (TKT-01M3N6Y5).
func TestCompressionIsAgainstRawTranscripts(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	cookie, _ := signIn(t, idp, h)
	sample := catalog.StorageSample{At: time.Now(), Measures: map[string]catalog.StorageUse{
		storage.CAS:                  {Bytes: 100 << 20, Files: 10},
		catalog.MeasureReferenced:    {Bytes: 5000 << 20, Files: 50},
		catalog.MeasureUnique:        {Bytes: 5000 << 20, Files: 50},
		catalog.MeasureCurrent:       {Bytes: 540 << 20, Files: 12},
		catalog.MeasureCurrentUnique: {Bytes: 536 << 20, Files: 10},
	}}
	if err := lake.Catalog.RecordStorage(t.Context(), sample); err != nil {
		t.Fatal(err)
	}
	body := get(h, "/operations", cookie).Body.String()
	for _, want := range []string{"Compression against raw", "5.40×", "540.0 MiB raw, 100.0 MiB on disk", "2 duplicate files, 4.0 MiB"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, gone := range []string{"50.00×", "Deduplication"} {
		if strings.Contains(body, gone) {
			t.Errorf("page still shows %q", gone)
		}
	}
}

// A sample taken before the current measures existed shows no ratio
// rather than one computed from every version.
func TestCompressionWaitsForCurrentMeasures(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	cookie, _ := signIn(t, idp, h)
	sample := catalog.StorageSample{At: time.Now(), Measures: map[string]catalog.StorageUse{
		storage.CAS:               {Bytes: 100 << 20, Files: 10},
		catalog.MeasureReferenced: {Bytes: 5000 << 20, Files: 50},
		catalog.MeasureUnique:     {Bytes: 5000 << 20, Files: 50},
	}}
	if err := lake.Catalog.RecordStorage(t.Context(), sample); err != nil {
		t.Fatal(err)
	}
	body := get(h, "/operations", cookie).Body.String()
	if !strings.Contains(body, "Not measured yet") || strings.Contains(body, "50.00×") {
		t.Errorf("an old sample should show no ratio")
	}
}

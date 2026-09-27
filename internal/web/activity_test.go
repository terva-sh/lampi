package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/testidp"
	"terva.sh/lampi/internal/webconfig"
)

func TestActivityAPIThroughLakeMux(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	seedSession(t, lake.Catalog, "first")
	seedSession(t, lake.Catalog, "second")
	// A repost of the same head is not another update.
	seedSession(t, lake.Catalog, "first")

	if w := get(h, "/api/web/v1/activity", nil); w.Code != 401 {
		t.Fatalf("unauthenticated activity: %d", w.Code)
	}
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/api/web/v1/activity", cookie)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("activity: %d %s", w.Code, w.Body.String())
	}
	var a catalog.Activity
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a.Bucket != catalog.BucketDay || len(a.Buckets) != 7 || a.CoverageSince == nil || a.Units["updates"] == "" {
		t.Fatalf("default activity: %+v", a)
	}
	today := a.Buckets[6]
	if today.Start != time.Now().UTC().Truncate(24*time.Hour).Format(time.RFC3339) || today.Updates == nil || *today.Updates != 2 || *today.NetLogicalBytes != 24 {
		t.Fatalf("today: %+v", today)
	}
	// The lake was created in this test, so every earlier day is unmeasured.
	if a.Buckets[0].Coverage != catalog.CoverageNone || a.Buckets[0].Updates != nil || a.Totals.Updates != 2 {
		t.Fatalf("earlier days: %+v totals %+v", a.Buckets[0], a.Totals)
	}

	q := url.Values{"bucket": {"hour"}, "harness": {"terva"}}
	w = get(h, "/api/web/v1/activity?"+q.Encode(), cookie)
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil || w.Code != 200 {
		t.Fatalf("hourly terva: %d %s", w.Code, w.Body.String())
	}
	if len(a.Buckets) != 24 || a.Harness != "terva" || a.Totals.Updates != 0 {
		t.Fatalf("hourly terva: %+v", a)
	}
	// Empty form fields take the defaults.
	if w := get(h, "/api/web/v1/activity?bucket=&from=&until=&harness=", cookie); w.Code != 200 {
		t.Fatalf("empty form fields: %d %s", w.Code, w.Body.String())
	}

	for _, bad := range []string{
		"bucket=week",
		"harness=vim",
		"harness=codex&harness=terva",
		"limit=5",
		"from=yesterday",
		"from=2026-09-01",
		"from=0001-01-01T00:00:00Z",
		"until=0001-01-01T00:00:00Z",
		"from=1969-12-31T23:00:00Z",
		"bucket=day&from=2026-01-01T00:00:00Z&until=2026-06-01T00:00:00Z",
		"bucket=hour&from=2026-09-01T00:00:00Z&until=2026-09-20T00:00:00Z",
		"from=2026-09-10T00:00:00Z&until=2026-09-01T00:00:00Z",
	} {
		w := get(h, "/api/web/v1/activity?"+bad, cookie)
		if w.Code != 400 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", bad, w.Code, w.Body.String())
		}
	}
}

// upload posts one transcript through the device API, as an agent does.
func upload(t *testing.T, h http.Handler, native string, body []byte) protocol.ManifestAck {
	t.Helper()
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	send := func(method, path string, payload io.Reader) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://lake.example"+path, payload)
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := send("PUT", "/v1/blobs/"+digest, bytes.NewReader(body)); w.Code != 200 {
		t.Fatalf("blob: %d %s", w.Code, w.Body.String())
	}
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "synthetic-machine", Harness: "terva", NativeSessionID: native,
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: native + ".jsonl", SHA256: digest, Size: int64(len(body))}}}
	encoded, _ := json.Marshal(m)
	w := send("POST", "/v1/manifests", bytes.NewReader(encoded))
	var ack protocol.ManifestAck
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ack) != nil {
		t.Fatalf("manifest: %d %s", w.Code, w.Body.String())
	}
	return ack
}

// activityTotals reads the API and checks the page states the same
// totals.
func activityTotals(t *testing.T, h http.Handler, cookie *http.Cookie) catalog.ActivityTotals {
	t.Helper()
	w := get(h, "/api/web/v1/activity?bucket=hour", cookie)
	var a catalog.Activity
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &a) != nil {
		t.Fatalf("activity: %d %s", w.Code, w.Body.String())
	}
	page := get(h, "/activity?range=24h", cookie)
	if page.Code != 200 {
		t.Fatalf("activity page: %d", page.Code)
	}
	want := humanCount(a.Totals.Updates) + " accepted head updates, " + signedBytes(a.Totals.NetLogicalBytes) + " net logical change."
	if a.Totals.Updates > 0 && !strings.Contains(html.UnescapeString(page.Body.String()), want) {
		t.Fatalf("page does not state %q", want)
	}
	return a.Totals
}

// From upload to page: a retry adds nothing, a grown transcript adds
// one update, purge removes a session's updates, and a restored backup
// shows the history it was taken with.
func TestActivityFromIngestToPage(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	cookie, _ := signIn(t, idp, h)
	first := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"one\",\"cwd\":\"/synthetic\"}}\n")
	grown := append(append([]byte{}, first...), "{\"type\":\"message\",\"text\":\"more\"}\n"...)
	other := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"two\",\"cwd\":\"/synthetic\"}}\n")

	upload(t, h, "one", first)
	upload(t, h, "one", first)
	if got := activityTotals(t, h, cookie); got != (catalog.ActivityTotals{Updates: 1, NetLogicalBytes: int64(len(first))}) {
		t.Fatalf("after retry: %+v", got)
	}
	if ack := upload(t, h, "one", grown); ack.Relation != protocol.RelationGrownFrom {
		t.Fatalf("grown: %+v", ack)
	}
	two := upload(t, h, "two", other)
	want := catalog.ActivityTotals{Updates: 3, NetLogicalBytes: int64(len(grown) + len(other))}
	if got := activityTotals(t, h, cookie); got != want {
		t.Fatalf("after uploads: %+v, want %+v", got, want)
	}
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}

	backup := filepath.Join(t.TempDir(), "restored")
	restoredDB := filepath.Join(backup, "catalog.db")
	if err := os.MkdirAll(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.VacuumInto(t.Context(), restoredDB); err != nil {
		t.Fatal(err)
	}

	plan, ok, err := lake.PlanPurge(t.Context(), two.SessionUID)
	if err != nil || !ok {
		t.Fatalf("plan purge: ok=%v err=%v", ok, err)
	}
	if err := lake.Purge(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if got := activityTotals(t, h, cookie); got != (catalog.ActivityTotals{Updates: 2, NetLogicalBytes: int64(len(grown))}) {
		t.Fatalf("after purge: %+v", got)
	}

	restored, err := api.Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restored.Close() })
	restored.Allow(strings.Repeat("a", 64))
	restoredWeb := restoredHandler(t, restored, idp)
	restoredCookie, _ := signIn(t, idp, restoredWeb)
	if got := activityTotals(t, restoredWeb, restoredCookie); got != want {
		t.Fatalf("restored backup: %+v, want %+v", got, want)
	}
}

func restoredHandler(t *testing.T, lake *api.Server, idp *testidp.Server) http.Handler {
	t.Helper()
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer"}}}
	var err error
	lake.Web, err = New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), nil, nil, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	return lake.Handler()
}

func TestChartEdges(t *testing.T) {
	day := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	n := func(v int64) *int64 { return &v }
	one := buildChart("x", "One", []time.Time{day}, []*int64{n(5)}, false, humanCount, niceCeil)
	if len(one.Bars) != 1 || len(one.XLabels) != 1 || one.XLabels[0].Anchor != "start" || !strings.Contains(one.Summary, "largest 5 on Sep 28") {
		t.Fatalf("one bucket: %+v", one)
	}
	none := buildChart("x", "None", []time.Time{day, day.Add(24 * time.Hour)}, []*int64{nil, nil}, false, humanCount, niceCeil)
	if len(none.Bars) != 0 || len(none.Gaps) != 1 || none.Gaps[0].W != plotW || !none.Unknown || !strings.Contains(none.Summary, "no bucket") {
		t.Fatalf("unmeasured: %+v", none)
	}
	zero := buildChart("x", "Zero", []time.Time{day}, []*int64{n(0)}, false, humanCount, niceCeil)
	if len(zero.Bars) != 0 || len(zero.Ticks) != 2 || zero.Ticks[1].Label != "1" {
		t.Fatalf("all zero: %+v", zero)
	}
	five := buildChart("x", "Five", []time.Time{day}, []*int64{n(4)}, false, humanCount, niceCeil)
	if len(five.Ticks) != 2 || five.Ticks[1].Label != "5" {
		t.Fatalf("top of 5 has a fractional middle tick: %+v", five.Ticks)
	}
	ten := buildChart("x", "Ten", []time.Time{day}, []*int64{n(7)}, false, humanCount, niceCeil)
	if len(ten.Ticks) != 3 || ten.Ticks[1].Label != "5" {
		t.Fatalf("top of 10: %+v", ten.Ticks)
	}
	neg := buildChart("x", "Net", []time.Time{day, day.Add(24 * time.Hour)}, []*int64{n(3000), n(-90000)}, false, signedBytes, niceBytes)
	if !neg.Negative || len(neg.Bars) != 2 || !neg.Bars[1].Negative || neg.Ticks[0].Label != "−100.0 KiB" || neg.Ticks[2].Label != "+5.0 KiB" {
		t.Fatalf("negative: %+v", neg.Ticks)
	}
	if neg.Baseline <= padT || neg.Baseline >= padT+plotH {
		t.Fatalf("baseline %v outside the plot", neg.Baseline)
	}
	for in, want := range map[int64]string{0: "0 B", 1023: "+1023 B", -1536: "−1.5 KiB", 3 << 20: "+3.0 MiB"} {
		if got := signedBytes(in); got != want {
			t.Fatalf("signedBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// A range with nothing in it still draws both charts, so measured zero
// and not measured stay visibly different, and the form sits outside
// the region a refresh replaces.
func TestActivityPageEmptyRangeKeepsCharts(t *testing.T) {
	_, idp, h, _ := fixture(t)
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/activity", cookie)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "No accepted updates in this range") || strings.Count(body, `<figure class="panel chart">`) != 2 {
		t.Fatalf("empty range page: %d", w.Code)
	}
	if form, live := strings.Index(body, `action="/activity"`), strings.Index(body, "data-live"); form < 0 || form > live {
		t.Fatal("activity form is inside the refreshed region")
	}
	if w := get(h, "/activity?range=week", cookie); w.Code != 400 {
		t.Fatalf("unknown range: %d", w.Code)
	}
}

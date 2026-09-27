package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func insertUpdate(t *testing.T, c *Catalog, harness string, at time.Time, oldSize, newSize int64) {
	t.Helper()
	if _, err := c.db.Exec(`INSERT INTO head_updates (session_uid, machine_id, harness, received_ns, old_sha256, new_sha256, old_size, new_size, relation)
		VALUES ('s', 'm', ?, ?, '', 'x', ?, ?, 'head')`, harness, at.UnixNano(), oldSize, newSize); err != nil {
		t.Fatal(err)
	}
}

func setCoverage(t *testing.T, c *Catalog, since time.Time) {
	t.Helper()
	if _, err := c.db.Exec(`UPDATE lake_meta SET value = ? WHERE key = 'head_updates_since'`, since.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
}

func TestActivityResolve(t *testing.T) {
	now := time.Date(2026, 9, 28, 13, 45, 10, 0, time.UTC)
	day := func(d, h int) time.Time { return time.Date(2026, 9, d, h, 0, 0, 0, time.UTC) }
	for _, tc := range []struct {
		name     string
		req      ActivityRequest
		from, to time.Time
		fail     bool
	}{
		{name: "default is 7 days", req: ActivityRequest{}, from: day(22, 0), to: day(29, 0)},
		{name: "hour default is 24 hours", req: ActivityRequest{Bucket: BucketHour}, from: day(27, 14), to: day(28, 14)},
		{name: "aligns outward", req: ActivityRequest{Bucket: BucketHour, From: day(28, 3).Add(59 * time.Minute), Until: day(28, 5).Add(time.Nanosecond)}, from: day(28, 3), to: day(28, 6)},
		{name: "until in the future is clamped", req: ActivityRequest{From: day(20, 0), Until: day(30, 0)}, from: day(20, 0), to: day(29, 0)},
		{name: "only until moves the default window", req: ActivityRequest{Until: day(10, 0)}, from: day(3, 0), to: day(10, 0)},
		{name: "a non-UTC offset is the same instant", req: ActivityRequest{Bucket: BucketHour, From: time.Date(2026, 9, 28, 1, 0, 0, 0, time.FixedZone("x", -5*3600)), Until: day(28, 7)}, from: day(28, 6), to: day(28, 7)},
		{name: "90 days is allowed", req: ActivityRequest{From: day(28, 0).Add(-90 * 24 * time.Hour), Until: day(28, 0)}, from: day(28, 0).Add(-90 * 24 * time.Hour), to: day(28, 0)},
		{name: "91 days is refused", req: ActivityRequest{From: day(28, 0).Add(-91 * 24 * time.Hour), Until: day(28, 0)}, fail: true},
		{name: "14 days of hours is allowed", req: ActivityRequest{Bucket: BucketHour, From: day(14, 0), Until: day(28, 0)}, from: day(14, 0), to: day(28, 0)},
		{name: "15 days of hours is refused", req: ActivityRequest{Bucket: BucketHour, From: day(13, 0), Until: day(28, 0)}, fail: true},
		{name: "empty range", req: ActivityRequest{From: day(20, 0), Until: day(20, 0)}, fail: true},
		{name: "reversed range", req: ActivityRequest{From: day(21, 0), Until: day(20, 0)}, fail: true},
		{name: "wholly in the future", req: ActivityRequest{From: day(30, 0), Until: day(30, 12)}, fail: true},
		{name: "reversed inside one bucket", req: ActivityRequest{Bucket: BucketHour, From: day(28, 12).Add(40 * time.Minute), Until: day(28, 12).Add(20 * time.Minute)}, fail: true},
		{name: "equal instants inside one bucket", req: ActivityRequest{Bucket: BucketHour, From: day(28, 12).Add(30 * time.Minute), Until: day(28, 12).Add(30 * time.Minute)}, fail: true},
		{name: "before the epoch", req: ActivityRequest{From: time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC), Until: time.Date(1600, 1, 5, 0, 0, 0, 0, time.UTC)}, fail: true},
		{name: "past what nanoseconds hold", req: ActivityRequest{From: time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)}, fail: true},
		{name: "unknown bucket", req: ActivityRequest{Bucket: "week"}, fail: true},
		{name: "unknown harness", req: ActivityRequest{Harness: "vim"}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.req.Resolve(now)
			if tc.fail {
				if !errors.Is(err, ErrPage) {
					t.Fatalf("resolve %+v = %+v, %v; want ErrPage", tc.req, got, err)
				}
				return
			}
			if err != nil || !got.From.Equal(tc.from) || !got.Until.Equal(tc.to) {
				t.Fatalf("resolve = %v..%v, %v; want %v..%v", got.From, got.Until, err, tc.from, tc.to)
			}
		})
	}
}

func TestActivityBuckets(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	h := func(hour int) time.Time { return time.Date(2026, 9, 28, hour, 0, 0, 0, time.UTC) }
	now := h(5).Add(30 * time.Minute)
	setCoverage(t, c, h(1).Add(20*time.Minute))
	insertUpdate(t, c, "terva", h(1).Add(30*time.Minute), 0, 100)
	insertUpdate(t, c, "terva", h(3), 100, 250)                     // first nanosecond of hour 3
	insertUpdate(t, c, "codex", h(3).Add(-1), 0, 40)                // last nanosecond of hour 2
	insertUpdate(t, c, "opencode", h(4).Add(time.Hour/2), 900, 300) // a rewrite that shrank

	a, err := c.Activity(ctx, ActivityRequest{Bucket: BucketHour, From: h(0), Until: h(6)}, now)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		coverage string
		n, net   int64
	}
	wants := []want{{CoverageNone, 0, 0}, {CoveragePartial, 1, 100}, {CoverageFull, 1, 40}, {CoverageFull, 1, 150}, {CoverageFull, 1, -600}, {CoverageFull, 0, 0}}
	if len(a.Buckets) != len(wants) {
		t.Fatalf("buckets %+v", a.Buckets)
	}
	for i, w := range wants {
		b := a.Buckets[i]
		if b.Start != h(i).Format(time.RFC3339) || b.Coverage != w.coverage {
			t.Fatalf("bucket %d = %+v, want %+v", i, b, w)
		}
		if w.coverage == CoverageNone {
			if b.Updates != nil || b.NetLogicalBytes != nil {
				t.Fatalf("unmeasured bucket %d has counts: %+v", i, b)
			}
			continue
		}
		if b.Updates == nil || *b.Updates != w.n || *b.NetLogicalBytes != w.net {
			t.Fatalf("bucket %d = %d/%d, want %+v", i, *b.Updates, *b.NetLogicalBytes, w)
		}
	}
	if a.Totals != (ActivityTotals{Updates: 4, NetLogicalBytes: -310}) {
		t.Fatalf("totals %+v", a.Totals)
	}
	if a.CoverageSince == nil || *a.CoverageSince != h(1).Add(20*time.Minute).Format(time.RFC3339Nano) || a.From != "2026-09-28T00:00:00Z" || a.Until != "2026-09-28T06:00:00Z" {
		t.Fatalf("header %+v", a)
	}
	// The unmeasured bucket encodes nulls, not zeros.
	b, _ := json.Marshal(a.Buckets[0])
	if !strings.Contains(string(b), `"updates":null`) {
		t.Fatalf("none bucket JSON %s", b)
	}

	terva, err := c.Activity(ctx, ActivityRequest{Bucket: BucketDay, Harness: "terva"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if terva.Totals != (ActivityTotals{Updates: 2, NetLogicalBytes: 250}) || len(terva.Buckets) != 7 {
		t.Fatalf("terva by day: %+v", terva)
	}
	last := terva.Buckets[6]
	if last.Coverage != CoveragePartial || *last.Updates != 2 || terva.Buckets[5].Coverage != CoverageNone {
		t.Fatalf("terva days: %+v", terva.Buckets)
	}
}

func TestActivityWithoutCoverageMarker(t *testing.T) {
	c, _ := openTemp(t)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	insertUpdate(t, c, "terva", now.Add(-time.Hour), 0, 10)
	if _, err := c.db.Exec(`DELETE FROM lake_meta WHERE key = 'head_updates_since'`); err != nil {
		t.Fatal(err)
	}
	a, err := c.Activity(context.Background(), ActivityRequest{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.CoverageSince != nil || a.Totals != (ActivityTotals{}) {
		t.Fatalf("no marker: %+v", a)
	}
	for _, b := range a.Buckets {
		if b.Coverage != CoverageNone || b.Updates != nil {
			t.Fatalf("no marker bucket %+v", b)
		}
	}
}

func TestActivityQueryUsesCoveringIndexes(t *testing.T) {
	c, _ := openTemp(t)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		req   ActivityRequest
		index string
	}{
		{ActivityRequest{Bucket: BucketHour}, "head_updates_time"},
		{ActivityRequest{Harness: "codex"}, "head_updates_harness_time"},
	} {
		r, err := tc.req.Resolve(now)
		if err != nil {
			t.Fatal(err)
		}
		q, args := activitySQL(r)
		rows, err := c.db.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var a, b, d int
			var detail string
			if err := rows.Scan(&a, &b, &d, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		if joined := strings.Join(plan, "; "); !strings.Contains(joined, "COVERING INDEX "+tc.index) {
			t.Fatalf("plan %q, want covering index %s", joined, tc.index)
		}
	}
}

// Ninety days of synthetic history, read the widest way the API allows,
// within the deadline every web read runs under. The time is logged,
// not asserted: it depends on the machine. now is a midnight, because
// the cap counts buckets after the range is aligned outward. Under
// -race the driver runs many times slower, so the race run checks the
// sums on fewer rows and holds no deadline.
func TestActivity90DaysWithinReadDeadline(t *testing.T) {
	c, _ := openTemp(t)
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	start := now.Add(-90 * 24 * time.Hour)
	setCoverage(t, c, start.Add(-time.Hour))
	n := int64(200000)
	deadline := 5 * time.Second
	if raceEnabled {
		n, deadline = 20000, time.Hour
	}
	step := int64(90*24*time.Hour) / n
	if _, err := c.db.Exec(`WITH RECURSIVE i(k) AS (SELECT 0 UNION ALL SELECT k + 1 FROM i WHERE k + 1 < ?)
		INSERT INTO head_updates (session_uid, machine_id, harness, received_ns, old_sha256, new_sha256, old_size, new_size, relation)
		SELECT 's' || (k % 5000), 'm', CASE k % 3 WHEN 0 THEN 'terva' WHEN 1 THEN 'codex' ELSE 'claude' END, ? + k * ?, 'a', 'b', k % 1000, k % 1000 + 64, 'grown_from' FROM i`,
		n, start.UnixNano(), step); err != nil {
		t.Fatal(err)
	}
	for _, req := range []ActivityRequest{
		{Bucket: BucketDay, From: start, Until: now},
		{Bucket: BucketDay, From: start, Until: now, Harness: "codex"},
		{Bucket: BucketHour, From: now.Add(-14 * 24 * time.Hour), Until: now},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), deadline)
		began := time.Now()
		a, err := c.Activity(ctx, req, now)
		cancel()
		if err != nil {
			t.Fatalf("%+v: %v", req, err)
		}
		b, _ := json.Marshal(a)
		t.Logf("%s harness=%q: %d buckets, %d updates, %d bytes of JSON in %s", req.Bucket, req.Harness, len(a.Buckets), a.Totals.Updates, len(b), time.Since(began))
		if req.Harness == "" && req.Bucket == BucketDay && a.Totals.Updates != n {
			t.Fatalf("90-day total %d, want %d", a.Totals.Updates, n)
		}
	}
}

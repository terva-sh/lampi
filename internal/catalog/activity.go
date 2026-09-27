package catalog

import (
	"context"
	"database/sql"
	"time"
)

// Activity buckets. Both are UTC: the Unix epoch is a UTC midnight, so
// a bucket is whole multiples of its width from the epoch.
const (
	BucketHour = "hour"
	BucketDay  = "day"
)

// Coverage of one bucket by head_updates recording. A none bucket ended
// before recording began and was not measured, which is not zero.
const (
	CoverageNone    = "none"
	CoveragePartial = "partial"
	CoverageFull    = "full"
)

// The widest range each bucket accepts. 14 days of hours is 336
// buckets; 90 days of hours would be 2,160, which reads as neither a
// chart nor a table.
const (
	MaxHourRange = 14 * 24 * time.Hour
	MaxDayRange  = 90 * 24 * time.Hour
)

// ActivityRequest asks for accepted head updates between From and
// Until. A zero From or Until takes the default for Bucket: the last 24
// hours, or the last 7 days, through the end of the current bucket.
type ActivityRequest struct {
	Bucket  string
	From    time.Time
	Until   time.Time
	Harness string
}

// ActivityBucket is one bucket. Updates and NetLogicalBytes are nil
// when Coverage is none.
type ActivityBucket struct {
	Start           string `json:"start"`
	Coverage        string `json:"coverage"`
	Updates         *int64 `json:"updates"`
	NetLogicalBytes *int64 `json:"net_logical_bytes"`
}

// ActivityTotals sums the measured buckets.
type ActivityTotals struct {
	Updates         int64 `json:"updates"`
	NetLogicalBytes int64 `json:"net_logical_bytes"`
}

// Activity is a dense series of buckets from From to Until. Every
// bucket in the range is present, so an empty one reads as zero and an
// unmeasured one as none.
type Activity struct {
	Bucket        string            `json:"bucket"`
	From          string            `json:"from"`
	Until         string            `json:"until"`
	Harness       string            `json:"harness"`
	CoverageSince *string           `json:"coverage_since"`
	AsOf          string            `json:"as_of"`
	Units         map[string]string `json:"units"`
	Totals        ActivityTotals    `json:"totals"`
	Buckets       []ActivityBucket  `json:"buckets"`
}

// activityLatest bounds a requested time well inside what Unix
// nanoseconds can hold.
var activityLatest = time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

var activityUnits = map[string]string{
	"updates":           "accepted session head updates",
	"net_logical_bytes": "sum of new minus old logical head size, in bytes; not network or disk bytes",
}

func bucketWidth(bucket string) (time.Duration, time.Duration, bool) {
	switch bucket {
	case BucketHour:
		return time.Hour, MaxHourRange, true
	case BucketDay:
		return 24 * time.Hour, MaxDayRange, true
	}
	return 0, 0, false
}

// alignDown and alignUp round t to a bucket boundary. Truncate rounds
// toward the zero time, not the epoch, so this works in nanoseconds.
func alignDown(t time.Time, w time.Duration) time.Time {
	ns := t.UnixNano()
	r := ns % int64(w)
	if r < 0 {
		r += int64(w)
	}
	return time.Unix(0, ns-r).UTC()
}

func alignUp(t time.Time, w time.Duration) time.Time {
	d := alignDown(t, w)
	if d.Before(t) {
		return d.Add(w)
	}
	return d
}

// Resolve fills defaults, aligns the range to buckets, and clamps an
// Until past the current bucket to its end. A range that is empty after
// that, or wider than the bucket allows, is ErrPage, as is an unknown
// bucket or harness.
func (r ActivityRequest) Resolve(now time.Time) (ActivityRequest, error) {
	if r.Bucket == "" {
		r.Bucket = BucketDay
	}
	w, max, ok := bucketWidth(r.Bucket)
	if !ok || !validHarness(r.Harness) {
		return r, ErrPage
	}
	// Rounding would turn a reversed or empty pair inside one bucket
	// into that bucket, so the instants are checked as given.
	if !r.From.IsZero() && !r.Until.IsZero() && !r.From.Before(r.Until) {
		return r, ErrPage
	}
	// Alignment works in Unix nanoseconds, which hold 1678 to 2262. The
	// lake has nothing before 1970, and a time past activityLatest is
	// refused rather than wrapped.
	for _, t := range []time.Time{r.From, r.Until} {
		if !t.IsZero() && (t.Before(time.Unix(0, 0)) || t.After(activityLatest)) {
			return r, ErrPage
		}
	}
	span := 7 * 24 * time.Hour
	if r.Bucket == BucketHour {
		span = 24 * time.Hour
	}
	end := alignUp(now.Add(1), w)
	if r.Until.IsZero() {
		r.Until = end
	}
	r.Until = alignUp(r.Until, w)
	if r.Until.After(end) {
		r.Until = end
	}
	if r.From.IsZero() {
		r.From = r.Until.Add(-span)
	}
	r.From = alignDown(r.From, w)
	if !r.From.Before(r.Until) || r.Until.Sub(r.From) > max {
		return r, ErrPage
	}
	return r, nil
}

// activitySQL sums head_updates per bucket over the resolved range. The
// bucket index is counted from From, which is aligned, and the time
// indexes carry both sizes, so this reads no table rows.
func activitySQL(r ActivityRequest) (string, []any) {
	w, _, _ := bucketWidth(r.Bucket)
	q := `SELECT (received_ns - ?) / ?, COUNT(*), SUM(new_size - old_size) FROM head_updates WHERE `
	args := []any{r.From.UnixNano(), int64(w)}
	if r.Harness != "" {
		q += `harness = ? AND `
		args = append(args, r.Harness)
	}
	q += `received_ns >= ? AND received_ns < ? GROUP BY 1`
	args = append(args, r.From.UnixNano(), r.Until.UnixNano())
	return q, args
}

// Activity reads accepted head updates in buckets. now sets the default
// range, the clamp, and as_of.
func (c *Catalog) Activity(ctx context.Context, req ActivityRequest, now time.Time) (Activity, error) {
	r, err := req.Resolve(now)
	if err != nil {
		return Activity{}, err
	}
	w, _, _ := bucketWidth(r.Bucket)
	out := Activity{
		Bucket:  r.Bucket,
		From:    r.From.Format(time.RFC3339),
		Until:   r.Until.Format(time.RFC3339),
		Harness: r.Harness,
		AsOf:    now.UTC().Format(time.RFC3339Nano),
		Units:   activityUnits,
		Buckets: make([]ActivityBucket, 0, int(r.Until.Sub(r.From)/w)),
	}
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Activity{}, err
	}
	defer tx.Rollback()
	var since time.Time
	var raw string
	switch err := tx.QueryRowContext(ctx, `SELECT value FROM lake_meta WHERE key = ?`, headUpdatesSinceKey).Scan(&raw); {
	case err == sql.ErrNoRows:
	case err != nil:
		return Activity{}, err
	default:
		if since, err = time.Parse(time.RFC3339Nano, raw); err != nil {
			return Activity{}, err
		}
		s := since.UTC().Format(time.RFC3339Nano)
		out.CoverageSince = &s
	}
	type sums struct{ n, net int64 }
	got := map[int64]sums{}
	q, args := activitySQL(r)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return Activity{}, err
	}
	for rows.Next() {
		var i int64
		var s sums
		if err := rows.Scan(&i, &s.n, &s.net); err != nil {
			rows.Close()
			return Activity{}, err
		}
		got[i] = s
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Activity{}, err
	}
	for i, start := int64(0), r.From; start.Before(r.Until); i, start = i+1, start.Add(w) {
		b := ActivityBucket{Start: start.Format(time.RFC3339), Coverage: CoverageNone}
		switch {
		case out.CoverageSince == nil || !start.Add(w).After(since):
		case start.Before(since):
			b.Coverage = CoveragePartial
		default:
			b.Coverage = CoverageFull
		}
		if b.Coverage != CoverageNone {
			s := got[i]
			b.Updates, b.NetLogicalBytes = &s.n, &s.net
			out.Totals.Updates += s.n
			out.Totals.NetLogicalBytes += s.net
		}
		out.Buckets = append(out.Buckets, b)
	}
	return out, nil
}

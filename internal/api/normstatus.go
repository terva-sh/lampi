package api

import (
	"context"
	"sync"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// How a normalize job ended in this process. A superseded job found a
// newer ingest had replaced it; the newer job does the work.
const (
	resultOK         = "ok"
	resultFailed     = "failed"
	resultRetried    = "retried"
	resultSuperseded = "superseded"
)

var normalizeResults = []string{resultOK, resultFailed, resultRetried, resultSuperseded}

// normalizeBuckets are the upper bounds, in seconds, of the normalize
// duration histogram. A session is usually well under a second; a
// long transcript takes tens.
var normalizeBuckets = []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

// drainBatch is how many jobs must finish between two idle moments of
// the queue for the drain to count as the end of a batch. Uploads
// normalize one session at a time and do not reach it; serve normalize
// --all does.
const drainBatch = 20

// normalizeStats counts what this process's workers did. It starts
// empty at each start, as Prometheus counters do.
type normalizeStats struct {
	mu          sync.Mutex
	results     map[string]int64
	buckets     []int64 // per bucket, not cumulative
	count       int64
	sum         float64
	lastSuccess time.Time
	lastFailure time.Time
	failedUID   string
}

func (st *normalizeStats) record(uid, result string, took time.Duration, at time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.results == nil {
		st.results = map[string]int64{}
		st.buckets = make([]int64, len(normalizeBuckets))
	}
	st.results[result]++
	secs := took.Seconds()
	st.count++
	st.sum += secs
	for i, le := range normalizeBuckets {
		if secs <= le {
			st.buckets[i]++
			break
		}
	}
	switch result {
	case resultOK:
		st.lastSuccess = at
	case resultFailed:
		st.lastFailure, st.failedUID = at, uid
	}
}

type normalizeSnapshot struct {
	results     map[string]int64
	cumulative  []int64
	count       int64
	sum         float64
	lastSuccess time.Time
	lastFailure time.Time
	failedUID   string
}

func (st *normalizeStats) snapshot() normalizeSnapshot {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := normalizeSnapshot{
		results:     map[string]int64{},
		cumulative:  make([]int64, len(normalizeBuckets)),
		count:       st.count,
		sum:         st.sum,
		lastSuccess: st.lastSuccess,
		lastFailure: st.lastFailure,
		failedUID:   st.failedUID,
	}
	for _, r := range normalizeResults {
		out.results[r] = st.results[r]
	}
	var run int64
	for i := range normalizeBuckets {
		if st.buckets != nil {
			run += st.buckets[i]
		}
		out.cumulative[i] = run
	}
	return out
}

// queueDepth is what the in-memory queue holds now: jobs waiting for a
// worker, jobs a worker is running, and jobs waiting on a retry timer.
func (q *normalizeQueue) depth() (queued, running, retrying int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items), q.inflight, q.delayed
}

// OnNormalizeDrained sets fn to be called when the normalize queue goes
// idle after a batch of at least drainBatch jobs. serve takes a storage
// sample then, so the derived stores a re-normalization rewrote are
// measured before the next hourly sample. fn must not block.
func (s *Server) OnNormalizeDrained(fn func()) {
	s.onDrained.Store(&fn)
}

func (s *Server) drained() {
	if fn := s.onDrained.Load(); fn != nil && *fn != nil {
		(*fn)()
	}
}

// CatalogNormalization is the part of the normalization status the
// catalog holds: sessions by state and the job table's backlog, aged
// at now. What a serve process holds and did is not in it, so serve
// normalize --status can read it with serve stopped.
func CatalogNormalization(ctx context.Context, cat *catalog.Catalog, now time.Time) (protocol.NormalizationStats, error) {
	counts, err := cat.NormalizationCounts(ctx)
	if err != nil {
		return protocol.NormalizationStats{}, err
	}
	backlog, err := cat.NormalizeBacklog(ctx)
	if err != nil {
		return protocol.NormalizationStats{}, err
	}
	out := protocol.NormalizationStats{Sessions: counts, Jobs: backlog.Jobs}
	if !backlog.Oldest.IsZero() {
		out.OldestPendingSeconds = max(0, now.Sub(backlog.Oldest).Seconds())
	}
	return out, nil
}

// NormalizationStatus is the lake's normalization at a glance, for
// GET /v1/stats: sessions by state, the job backlog, what this process
// is running, and when a job last succeeded or failed.
func (s *Server) NormalizationStatus(ctx context.Context) (protocol.NormalizationStats, error) {
	out, err := CatalogNormalization(ctx, s.Catalog, s.now())
	if err != nil {
		return protocol.NormalizationStats{}, err
	}
	if s.norm != nil {
		out.Queued, out.Running, out.Retrying = s.norm.depth()
	}
	snap := s.normStats.snapshot()
	if !snap.lastSuccess.IsZero() {
		out.LastSuccess = snap.lastSuccess.UTC().Format(time.RFC3339)
	}
	if !snap.lastFailure.IsZero() {
		out.LastFailure = &protocol.NormalizeFailure{
			SessionUID: snap.failedUID,
			At:         snap.lastFailure.UTC().Format(time.RFC3339),
		}
	}
	return out, nil
}

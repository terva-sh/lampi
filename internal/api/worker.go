package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"terva.sh/lampi/internal/catalog"
)

// normalizeWorkers is the in-process pool. Projection reads the CAS
// and writes derived files. SQLite stays at one connection, so the
// workers take turns on the catalog and overlap on the blobs.
const normalizeWorkers = 2

// normalizeAttempts bounds retries of a transient failure in this
// process: a catalog read, a CAS read, or storing the result. The
// waits double from retryBase. A job out of attempts stays in
// catalog.normalize_jobs, and the next start runs it again.
const normalizeAttempts = 5

const defaultRetryBase = 200 * time.Millisecond

// workerLog receives a worker panic and its stack. serve's stderr is
// the service journal.
var workerLog = log.New(os.Stderr, "terva-lampi: ", log.LstdFlags)

// normalizeQueue is the in-memory side of catalog.normalize_jobs.
// The table is what survives a restart. This queue is what keeps the
// manifest handler from waiting on projection.
type normalizeQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	items    []catalog.NormalizeJob
	inflight int
	// delayed counts retries waiting on a timer. waitIdle waits for
	// them too.
	delayed int
	// finished counts jobs done since the queue was last idle.
	finished int
	closed   bool
	// held counts each job, by session and generation, that is queued,
	// waiting on a retry timer, or running. pushNew reads it so a
	// reload of catalog.normalize_jobs does not queue a job twice.
	held map[jobKey]int
}

type jobKey struct {
	uid string
	gen int64
}

func newNormalizeQueue() *normalizeQueue {
	q := &normalizeQueue{held: map[jobKey]int{}}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push adds job. After shutdown it does nothing: no worker will pop
// again, and the job's row in catalog.normalize_jobs is what the next
// start loads.
func (q *normalizeQueue) push(job catalog.NormalizeJob) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.items = append(q.items, job)
	q.held[jobKey{job.SessionUID, job.Gen}]++
	q.cond.Broadcast()
	q.mu.Unlock()
}

// pushNew adds job unless the same session and generation is already
// queued, waiting on a retry, or running, and reports whether it did.
// A job whose attempts ran out is held by nothing, so a reload runs it
// again, as a restart would.
func (q *normalizeQueue) pushNew(job catalog.NormalizeJob) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.held[jobKey{job.SessionUID, job.Gen}] > 0 {
		return false
	}
	q.items = append(q.items, job)
	q.held[jobKey{job.SessionUID, job.Gen}]++
	q.cond.Broadcast()
	return true
}

// later pushes job after d.
func (q *normalizeQueue) later(job catalog.NormalizeJob, d time.Duration) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.delayed++
	q.held[jobKey{job.SessionUID, job.Gen}]++
	q.mu.Unlock()
	time.AfterFunc(d, func() {
		q.mu.Lock()
		q.delayed--
		if !q.closed {
			q.items = append(q.items, job)
		} else {
			q.release(job)
		}
		q.cond.Broadcast()
		q.mu.Unlock()
	})
}

func (q *normalizeQueue) pop() (catalog.NormalizeJob, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	// A closed queue hands out nothing more. What is left stays in
	// catalog.normalize_jobs for the next start.
	if q.closed {
		return catalog.NormalizeJob{}, false
	}
	job := q.items[0]
	q.items = q.items[1:]
	q.inflight++
	return job, true
}

// done ends a job pop handed out. A retry of it was counted again by
// later. batch is true when this leaves the queue idle after at least
// drainBatch jobs.
func (q *normalizeQueue) done(job catalog.NormalizeJob) (batch bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.inflight--
	q.release(job)
	q.finished++
	q.cond.Broadcast()
	if len(q.items) > 0 || q.inflight > 0 || q.delayed > 0 {
		return false
	}
	batch = q.finished >= drainBatch
	q.finished = 0
	return batch
}

// release drops one count of job from held. q.mu is held.
func (q *normalizeQueue) release(job catalog.NormalizeJob) {
	k := jobKey{job.SessionUID, job.Gen}
	if q.held[k] <= 1 {
		delete(q.held, k)
		return
	}
	q.held[k]--
}

func (q *normalizeQueue) waitIdle(ctx context.Context) error {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			q.mu.Lock()
			q.cond.Broadcast()
			q.mu.Unlock()
		case <-stop:
		}
	}()
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) > 0 || q.inflight > 0 || q.delayed > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		q.cond.Wait()
	}
	return nil
}

// shutdown stops pop and returns how many jobs were still queued or
// waiting to be retried.
func (q *normalizeQueue) shutdown() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.cond.Broadcast()
	return len(q.items) + q.delayed
}

// WaitNormalized blocks until queued and in-flight normalize jobs in
// this process have finished. The derived view lags the manifest ACK
// until this returns. A job left in the catalog by a store failure is
// not in this queue; the next Open loads it.
func (s *Server) WaitNormalized(ctx context.Context) error {
	if s == nil || s.norm == nil {
		return nil
	}
	return s.norm.waitIdle(ctx)
}

func (s *Server) loadNormalizeJobs(ctx context.Context) error {
	jobs, err := s.Catalog.ListNormalizeJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		s.norm.push(job)
	}
	return nil
}

// ReloadNormalizeJobs queues each row of catalog.normalize_jobs that
// this process does not already hold, and reports how many it queued.
// serve calls it on SIGHUP, so jobs serve normalize wrote, and jobs
// whose attempts ran out, start without a restart.
func (s *Server) ReloadNormalizeJobs(ctx context.Context) (int, error) {
	if s == nil || s.norm == nil {
		return 0, nil
	}
	jobs, err := s.Catalog.ListNormalizeJobs(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, job := range jobs {
		if s.norm.pushNew(job) {
			n++
		}
	}
	return n, nil
}

func (s *Server) startNormalizeWorkers() {
	for range normalizeWorkers {
		s.normalizeWG.Add(1)
		go s.normalizeLoop()
	}
}

func (s *Server) normalizeLoop() {
	defer s.normalizeWG.Done()
	for {
		job, ok := s.norm.pop()
		if !ok {
			return
		}
		start := time.Now()
		result := s.runNormalize(job)
		s.normStats.record(job.SessionUID, result, time.Since(start), s.now())
		if s.norm.done(job) {
			s.drained()
		}
	}
}

func (s *Server) enqueueNormalize(ctx context.Context, sessionUID string) error {
	gen, err := s.Catalog.EnqueueNormalize(ctx, sessionUID, s.now())
	if err != nil {
		return err
	}
	s.norm.push(catalog.NormalizeJob{SessionUID: sessionUID, Gen: gen})
	return nil
}

// runNormalize projects the catalog head and publishes when gen is
// still current. A newer ingest bumps gen and this publish is dropped.
// The CAS is not opened for write. beforeProject, when set, runs first
// so a test can show the ACK returning while projection waits.
//
// A catalog error, a CAS read error, or a failed store is transient:
// the job is tried again after a wait. A projection error from the
// bytes themselves is recorded on the session at once. A CAS read that
// still fails after the last attempt is recorded too, and its job row
// is kept, so the next start tries it again and a success clears it.
//
// A panic does not stop the process. It is logged, recorded as the
// session's normalize_error, and the job row is deleted, so a restart
// does not load the same panic again.
func (s *Server) runNormalize(job catalog.NormalizeJob) (result string) {
	defer func() {
		if r := recover(); r != nil {
			workerLog.Printf("normalize %s: panic: %v\n%s", job.SessionUID, r, debug.Stack())
			s.recordPanic(job, r)
			result = resultFailed
		}
	}()
	if s.beforeProject != nil {
		s.beforeProject()
	}
	ctx := context.Background()
	gen, head, ok, err := s.Catalog.NormalizeVersion(ctx, job.SessionUID)
	if err != nil {
		return retried(s.retryNormalize(job, "generation", err))
	}
	if !ok || gen != job.Gen {
		return resultSuperseded
	}
	info, ok, err := s.Catalog.Session(ctx, job.SessionUID)
	if err != nil {
		return retried(s.retryNormalize(job, "session", err))
	}
	if !ok {
		return resultSuperseded
	}
	events, nerr := s.Project(ctx, info.Manifest)
	keepJob := false
	if nerr != nil {
		if isTransient(nerr) {
			if s.retryNormalize(job, "project", nerr) {
				return resultRetried
			}
			keepJob = true
		}
		s.logger().Warn("normalize failed", "session_uid", job.SessionUID, "err", nerr.Error())
	}
	unlock := s.lockSession(job.SessionUID)
	defer unlock()
	gen, currentHead, ok, err := s.Catalog.NormalizeVersion(ctx, job.SessionUID)
	if err != nil {
		return retried(s.retryNormalize(job, "generation", err))
	}
	if !ok || gen != job.Gen || currentHead != head {
		return resultSuperseded
	}
	if err := s.storeGeneration(ctx, job.SessionUID, job.Gen, head, events, nerr); err != nil {
		return retried(s.retryNormalize(job, "store", err))
	}
	if keepJob {
		return resultFailed
	}
	if err := s.Catalog.DeleteNormalizeJob(ctx, job.SessionUID, job.Gen); err != nil {
		s.logger().Error("normalize job not cleared", "session_uid", job.SessionUID, "err", err.Error())
		return resultFailed
	}
	s.published(job.SessionUID)
	if nerr != nil {
		return resultFailed
	}
	return resultOK
}

// retried is the result of a job retryNormalize handled: queued again,
// or out of attempts.
func retried(again bool) string {
	if again {
		return resultRetried
	}
	return resultFailed
}

// OnPublished sets fn to be called with a session UID once a
// normalize outcome is final: published with its job cleared, or
// recorded as a failure. A derived view such as the search index uses
// it to catch up. fn must not block. It is safe to call while workers
// run.
func (s *Server) OnPublished(fn func(sessionUID string)) {
	s.onPublished.Store(&fn)
}

func (s *Server) published(sessionUID string) {
	if fn := s.onPublished.Load(); fn != nil && *fn != nil {
		(*fn)(sessionUID)
	}
}

// retryNormalize queues job again after a wait and reports true, or
// reports false when its attempts are spent. Its row stays in
// catalog.normalize_jobs either way.
func (s *Server) retryNormalize(job catalog.NormalizeJob, step string, err error) bool {
	if job.Attempt+1 >= normalizeAttempts {
		s.logger().Error("normalize left for the next start", "session_uid", job.SessionUID, "step", step, "attempts", job.Attempt+1, "err", err.Error())
		return false
	}
	s.logger().Warn("normalize retry", "session_uid", job.SessionUID, "step", step, "attempt", job.Attempt+1, "err", err.Error())
	base := s.retryBase
	if base <= 0 {
		base = defaultRetryBase
	}
	wait := base << job.Attempt
	job.Attempt++
	s.norm.later(job, wait)
	return true
}

// sessionLock serializes publishes for one session. refs counts the
// holders and waiters, so the entry is dropped when nobody needs it.
type sessionLock struct {
	mu   sync.Mutex
	refs int
}

// recordPanic stores a worker panic as the session's failure when job
// is still the current generation. A newer ingest has its own job and
// its own outcome. The derived files are removed, as for any failure.
func (s *Server) recordPanic(job catalog.NormalizeJob, r any) {
	ctx := context.Background()
	unlock := s.lockSession(job.SessionUID)
	defer unlock()
	gen, ok, err := s.Catalog.NormalizeGen(ctx, job.SessionUID)
	if err != nil || !ok || gen != job.Gen {
		return
	}
	_ = removeDerived(filepath.Join(s.Normalized, job.SessionUID+".jsonl"), s.Parquet, job.SessionUID)
	if err := s.Catalog.SetNormalizeError(ctx, job.SessionUID, fmt.Sprintf("normalize: panic: %v", r)); err != nil {
		workerLog.Printf("normalize %s: record panic: %v", job.SessionUID, err)
	}
	if err := s.Catalog.DeleteNormalizeJob(ctx, job.SessionUID, job.Gen); err != nil {
		workerLog.Printf("normalize %s: delete job: %v", job.SessionUID, err)
		return
	}
	s.published(job.SessionUID)
}

func (s *Server) lockSession(sessionUID string) func() {
	s.pubMu.Lock()
	if s.pubs == nil {
		s.pubs = map[string]*sessionLock{}
	}
	l := s.pubs[sessionUID]
	if l == nil {
		l = &sessionLock{}
		s.pubs[sessionUID] = l
	}
	l.refs++
	s.pubMu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		s.pubMu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(s.pubs, sessionUID)
		}
		s.pubMu.Unlock()
	}
}

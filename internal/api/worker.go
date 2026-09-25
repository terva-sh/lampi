package api

import (
	"context"
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
	closed  bool
}

func newNormalizeQueue() *normalizeQueue {
	q := &normalizeQueue{}
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
	q.cond.Broadcast()
	q.mu.Unlock()
}

// later pushes job after d.
func (q *normalizeQueue) later(job catalog.NormalizeJob, d time.Duration) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.delayed++
	q.mu.Unlock()
	time.AfterFunc(d, func() {
		q.mu.Lock()
		q.delayed--
		if !q.closed {
			q.items = append(q.items, job)
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

func (q *normalizeQueue) done() {
	q.mu.Lock()
	q.inflight--
	q.cond.Broadcast()
	q.mu.Unlock()
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
		s.runNormalize(job)
		s.norm.done()
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
func (s *Server) runNormalize(job catalog.NormalizeJob) {
	if s.beforeProject != nil {
		s.beforeProject()
	}
	ctx := context.Background()
	gen, ok, err := s.Catalog.NormalizeGen(ctx, job.SessionUID)
	if err != nil {
		s.retryNormalize(job, "generation", err)
		return
	}
	if !ok || gen != job.Gen {
		return
	}
	info, ok, err := s.Catalog.Session(ctx, job.SessionUID)
	if err != nil {
		s.retryNormalize(job, "session", err)
		return
	}
	if !ok {
		return
	}
	events, nerr := s.Project(ctx, info.Manifest)
	keepJob := false
	if nerr != nil {
		if isTransient(nerr) {
			if s.retryNormalize(job, "project", nerr) {
				return
			}
			keepJob = true
		}
		s.logger().Warn("normalize failed", "session_uid", job.SessionUID, "err", nerr.Error())
	}
	unlock := s.lockSession(job.SessionUID)
	defer unlock()
	gen, ok, err = s.Catalog.NormalizeGen(ctx, job.SessionUID)
	if err != nil {
		s.retryNormalize(job, "generation", err)
		return
	}
	if !ok || gen != job.Gen {
		return
	}
	if err := s.StoreEvents(ctx, job.SessionUID, events, nerr); err != nil {
		s.retryNormalize(job, "store", err)
		return
	}
	if keepJob {
		return
	}
	if err := s.Catalog.DeleteNormalizeJob(ctx, job.SessionUID, job.Gen); err != nil {
		s.logger().Error("normalize job not cleared", "session_uid", job.SessionUID, "err", err.Error())
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

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
	closed   bool
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
	for len(q.items) > 0 || q.inflight > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		q.cond.Wait()
	}
	return nil
}

// shutdown stops pop and returns how many jobs were still queued.
func (q *normalizeQueue) shutdown() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.cond.Broadcast()
	return len(q.items)
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
// A panic does not stop the process. It is logged, recorded as the
// session's normalize_error, and the job row is deleted, so a restart
// does not load the same panic again.
func (s *Server) runNormalize(job catalog.NormalizeJob) {
	defer func() {
		if r := recover(); r != nil {
			workerLog.Printf("normalize %s: panic: %v\n%s", job.SessionUID, r, debug.Stack())
			s.recordPanic(job, r)
		}
	}()
	if s.beforeProject != nil {
		s.beforeProject()
	}
	ctx := context.Background()
	gen, ok, err := s.Catalog.NormalizeGen(ctx, job.SessionUID)
	if err != nil || !ok || gen != job.Gen {
		return
	}
	info, ok, err := s.Catalog.Session(ctx, job.SessionUID)
	if err != nil || !ok {
		return
	}
	events, nerr := s.Project(ctx, info.Manifest)
	if nerr != nil {
		s.logger().Warn("normalize failed", "session_uid", job.SessionUID, "err", nerr.Error())
	}
	unlock := s.lockSession(job.SessionUID)
	defer unlock()
	gen, ok, err = s.Catalog.NormalizeGen(ctx, job.SessionUID)
	if err != nil || !ok || gen != job.Gen {
		return
	}
	if err := s.StoreEvents(ctx, job.SessionUID, events, nerr); err != nil {
		s.logger().Error("normalize store failed", "session_uid", job.SessionUID, "attempt", job.Attempt, "err", err.Error())
		if job.Attempt < 3 {
			job.Attempt++
			time.Sleep(50 * time.Millisecond)
			s.norm.push(job)
		}
		return
	}
	_ = s.Catalog.DeleteNormalizeJob(ctx, job.SessionUID, job.Gen)
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
	}
}

func (s *Server) lockSession(sessionUID string) func() {
	s.pubMu.Lock()
	if s.pubs == nil {
		s.pubs = map[string]*sync.Mutex{}
	}
	mu := s.pubs[sessionUID]
	if mu == nil {
		mu = &sync.Mutex{}
		s.pubs[sessionUID] = mu
	}
	s.pubMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

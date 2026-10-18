package api

import (
	"fmt"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
)

func nextJob(t *testing.T, q *normalizeQueue) <-chan catalog.NormalizeJob {
	t.Helper()
	ch := make(chan catalog.NormalizeJob, 1)
	go func() { job, _ := q.pop(); ch <- job }()
	return ch
}
func requireWaiting(t *testing.T, ch <-chan catalog.NormalizeJob) {
	t.Helper()
	select {
	case job := <-ch:
		t.Fatalf("admitted too early: %+v", job)
	case <-time.After(30 * time.Millisecond):
	}
}
func receiveJob(t *testing.T, ch <-chan catalog.NormalizeJob) catalog.NormalizeJob {
	t.Helper()
	select {
	case job := <-ch:
		return job
	case <-time.After(3 * time.Second):
		t.Fatal("admission did not resume")
		return catalog.NormalizeJob{}
	}
}

func TestQueueCoalescesAndSerializesSessions(t *testing.T) {
	q := newNormalizeQueue()
	q.budget = 10
	t.Cleanup(func() { q.shutdown() })
	q.push(catalog.NormalizeJob{SessionUID: "a", Gen: 1, Bytes: 6})
	first, _ := q.pop()
	q.push(catalog.NormalizeJob{SessionUID: "a", Gen: 2, Bytes: 6})
	q.push(catalog.NormalizeJob{SessionUID: "a", Gen: 3, Bytes: 7})
	q.push(catalog.NormalizeJob{SessionUID: "b", Gen: 1, Bytes: 3})
	other, _ := q.pop()
	if other.SessionUID != "b" {
		t.Fatalf("same-session work overlapped: %+v", other)
	}
	q.done(other)
	pending := nextJob(t, q)
	requireWaiting(t, pending)
	// A stale retry cannot replace the latest queued generation.
	q.later(first, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	q.done(first)
	last := receiveJob(t, pending)
	if last.SessionUID != "a" || last.Gen != 3 {
		t.Fatalf("latest generation lost: %+v", last)
	}
	q.done(last)
	if err := q.waitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	snap := q.admission()
	if snap.Coalesced != 1 || snap.Bytes != 0 || snap.WaitCount != 3 {
		t.Fatalf("admission=%+v", snap)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.held)+len(q.latest)+len(q.sessionHeld)+len(q.running) != 0 {
		t.Fatal("completed sessions retained")
	}
}

func TestQueueBudgetOversizeAndBoundedBypass(t *testing.T) {
	q := newNormalizeQueue()
	q.budget = 10
	t.Cleanup(func() { q.shutdown() })
	q.push(catalog.NormalizeJob{SessionUID: "anchor", Gen: 1, Bytes: 6})
	anchor, _ := q.pop()
	q.push(catalog.NormalizeJob{SessionUID: "large", Gen: 1, Bytes: 8})
	for i := 0; i < 8; i++ {
		q.push(catalog.NormalizeJob{SessionUID: fmt.Sprint("small", i), Gen: 1, Bytes: 1})
		job, _ := q.pop()
		if job.Bytes != 1 {
			t.Fatal("small session could not use spare capacity")
		}
		q.done(job)
	}
	q.push(catalog.NormalizeJob{SessionUID: "ninth", Gen: 1, Bytes: 1})
	waiting := nextJob(t, q)
	requireWaiting(t, waiting)
	q.done(anchor)
	large := receiveJob(t, waiting)
	if large.SessionUID != "large" {
		t.Fatal("large session starved")
	}
	small, _ := q.pop()
	if small.SessionUID != "ninth" {
		t.Fatal("small session starved")
	}
	q.done(small)
	q.done(large)
	q.push(catalog.NormalizeJob{SessionUID: "oversize", Gen: 1, Bytes: 30})
	big, _ := q.pop()
	q.push(catalog.NormalizeJob{SessionUID: "after", Gen: 1, Bytes: 1})
	waiting = nextJob(t, q)
	requireWaiting(t, waiting)
	if q.admission().Bytes != 30 {
		t.Fatal("oversized input not reported honestly")
	}
	q.done(big)
	job := receiveJob(t, waiting)
	q.done(job)
}

func TestNormalizeOptionsRefuseInvalidBeforeOpening(t *testing.T) {
	for _, o := range []NormalizeOptions{{Workers: -1}, {Workers: 65}, {InputBudgetBytes: -1}} {
		if s, err := OpenWithOptions("", o); err == nil {
			if s != nil {
				s.Close()
			}
			t.Fatal("invalid normalize options accepted")
		}
	}
}

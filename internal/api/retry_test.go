package api

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// retryLake has one terva session whose blob is moved away for the
// first `missing` projections, as a CAS read that fails for a while.
func retryLake(t *testing.T, missing int32) (s *Server, uid string, calls *atomic.Int32) {
	t.Helper()
	s = openServer(t)
	s.retryBase = time.Millisecond
	s.Allow("sekret")
	h := s.Handler()
	body := transcriptLines(
		`{"type":"meta","meta":{"id":"sid-retry","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"retried pond"}],"time":"2026-09-22T16:10:01Z"}}`,
	)
	sum := putBlob(t, h, "", body)
	path, err := s.CAS.Path(sum)
	if err != nil {
		t.Fatal(err)
	}
	calls = &atomic.Int32{}
	s.beforeProject = func() {
		n := calls.Add(1)
		switch {
		case n == 1:
			if err := os.Rename(path, path+".away"); err != nil {
				t.Error(err)
			}
		case n == missing+1:
			if err := os.Rename(path+".away", path); err != nil {
				t.Error(err)
			}
		}
	}
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-retry",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/sid-retry.jsonl",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s, ack.SessionUID, calls
}

// A CAS read that fails and then works is retried. It used to become
// a permanent normalize_error.
func TestNormalizeRetriesTransientCASRead(t *testing.T) {
	s, uid, calls := retryLake(t, 2)
	if n := calls.Load(); n != 3 {
		t.Fatalf("projected %d times, want 3", n)
	}
	msg, _, err := s.Catalog.NormalizeError(t.Context(), uid)
	if err != nil || msg != "" {
		t.Fatalf("normalize_error %q err %v", msg, err)
	}
	if _, err := os.Stat(s.Normalized + "/" + uid + ".jsonl"); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.Catalog.ListNormalizeJobs(t.Context())
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs %+v err %v", jobs, err)
	}
	s.pubMu.Lock()
	left := len(s.pubs)
	s.pubMu.Unlock()
	if left != 0 {
		t.Fatalf("%d session locks kept after publish", left)
	}
}

// Out of attempts, the failure is recorded so export names it, and the
// job row stays for the next start.
func TestNormalizeKeepsJobWhenRetriesRunOut(t *testing.T) {
	s, uid, calls := retryLake(t, normalizeAttempts+1)
	if n := calls.Load(); n != normalizeAttempts {
		t.Fatalf("projected %d times, want %d", n, normalizeAttempts)
	}
	msg, _, err := s.Catalog.NormalizeError(t.Context(), uid)
	if err != nil || msg == "" {
		t.Fatalf("normalize_error %q err %v", msg, err)
	}
	jobs, err := s.Catalog.ListNormalizeJobs(t.Context())
	if err != nil || len(jobs) != 1 || jobs[0].SessionUID != uid {
		t.Fatalf("jobs %+v err %v", jobs, err)
	}
}

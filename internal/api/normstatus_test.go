package api

import (
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// A session whose bytes do not normalize is counted as failed, and the
// status names it without its message.
func TestNormalizationStatusCountsAFailure(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	body := []byte("not json at all\n")
	sum := putRaw(t, h, body)
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version, MachineID: "m", Harness: protocol.HarnessClaude, HarnessVersion: "1", NativeSessionID: "sid-bad",
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "p/sid-bad.jsonl", Size: int64(len(body)), SHA256: sum}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	st, err := s.NormalizationStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.Sessions["failed"] != 1 || st.LastFailure == nil || st.LastFailure.SessionUID != ack.SessionUID || st.LastSuccess != "" {
		t.Fatalf("status %+v failure %+v", st, st.LastFailure)
	}
	if snap := s.normStats.snapshot(); snap.results[resultFailed] != 1 || snap.count != 1 {
		t.Fatalf("results %v count %d", snap.results, snap.count)
	}
}

// The queue reports a drain once a batch of drainBatch jobs finishes,
// and not for a single job.
func TestNormalizeQueueReportsTheEndOfABatch(t *testing.T) {
	q := newNormalizeQueue()
	one := catalog.NormalizeJob{SessionUID: "a", Gen: 1}
	q.push(one)
	job, _ := q.pop()
	if q.done(job) {
		t.Fatal("one job counted as a batch")
	}
	for i := 0; i < drainBatch; i++ {
		q.push(catalog.NormalizeJob{SessionUID: strings.Repeat("b", i+1), Gen: 1})
	}
	drains := 0
	for i := 0; i < drainBatch; i++ {
		job, _ := q.pop()
		if q.done(job) {
			drains++
		}
	}
	if drains != 1 {
		t.Fatalf("%d drains for one batch", drains)
	}
}

// The oldest pending age comes from the job table.
func TestNormalizationStatusAgesTheBacklog(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	body := transcriptLines(`{"type":"user","message":{"role":"user","content":"pond"}}`)
	sum := putRaw(t, h, body)
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version, MachineID: "m", Harness: protocol.HarnessClaude, HarnessVersion: "1", NativeSessionID: "sid-age",
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "p/sid-age.jsonl", Size: int64(len(body)), SHA256: sum}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	// A job queued an hour ago that no worker holds, as serve normalize
	// leaves one before a SIGHUP.
	if _, err := s.Catalog.EnqueueNormalize(t.Context(), ack.SessionUID, s.now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	st, err := s.NormalizationStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.Jobs != 1 || st.Sessions["pending"] != 1 || st.OldestPendingSeconds < 3500 || st.Queued != 0 {
		t.Fatalf("status %+v", st)
	}
}

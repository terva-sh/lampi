package api

import (
	"bytes"
	"testing"
	"time"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// ingestUnnormalized stores one terva session in a lake directory with
// no normalize job, the way sessions ingested before generation
// tracking look: the dashboard counts them as unknown.
func ingestUnnormalized(t *testing.T, dir string) string {
	t.Helper()
	store, err := cas.Open(dir + "/cas")
	if err != nil {
		t.Fatal(err)
	}
	body := transcriptLines(
		`{"type":"meta","meta":{"id":"sid-stale","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"stale pond"}],"time":"2026-09-22T16:10:01Z"}}`,
	)
	sum := shaOf(t, body)
	if _, err := store.Put(sum, bytes.NewReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Open(dir + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	defer cat.Close()
	ack, err := cat.Ingest(t.Context(), protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-stale",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/sid-stale.jsonl",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	}, time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC), []catalog.Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}

// TKT-01M3HKYFC: a job written by another process, as serve normalize
// does, runs in a live lake after ReloadNormalizeJobs, without a
// restart, and a second reload queues nothing.
func TestReloadNormalizeJobsRunsARequeuedSession(t *testing.T) {
	dir := t.TempDir()
	uid := ingestUnnormalized(t, dir)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	stale, err := s.Catalog.SessionsInNormalizationState(t.Context(), "unknown")
	if err != nil || len(stale) != 1 || stale[0] != uid {
		t.Fatalf("unknown sessions %v %v, want [%s]", stale, err, uid)
	}

	other, err := catalog.Open(dir + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.EnqueueNormalize(t.Context(), uid, time.Now()); err != nil {
		t.Fatal(err)
	}
	other.Close()

	n, err := s.ReloadNormalizeJobs(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("reload queued %d %v, want 1", n, err)
	}
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if state, err := s.Catalog.NormalizationState(t.Context(), uid); err != nil || state != "ready" {
		t.Fatalf("state %q %v, want ready", state, err)
	}
	if n, err := s.ReloadNormalizeJobs(t.Context()); err != nil || n != 0 {
		t.Fatalf("second reload queued %d %v, want 0", n, err)
	}
}

func TestQueuePushNewSkipsAHeldJob(t *testing.T) {
	q := newNormalizeQueue()
	job := catalog.NormalizeJob{SessionUID: "s", Gen: 3}
	if !q.pushNew(job) {
		t.Fatal("pushNew refused a new job")
	}
	if q.pushNew(job) {
		t.Fatal("pushNew queued a job that is already queued")
	}
	if !q.pushNew(catalog.NormalizeJob{SessionUID: "s", Gen: 4}) {
		t.Fatal("pushNew refused a newer generation")
	}
	got, _ := q.pop()
	if q.pushNew(got) {
		t.Fatal("pushNew queued a job that is running")
	}
	q.later(got, time.Hour)
	q.done(got)
	if q.pushNew(got) {
		t.Fatal("pushNew queued a job waiting on a retry")
	}
	q.shutdown()
	if q.pushNew(catalog.NormalizeJob{SessionUID: "x", Gen: 1}) {
		t.Fatal("pushNew queued after shutdown")
	}
}

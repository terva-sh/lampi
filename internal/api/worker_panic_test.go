package api

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

func TestNormalizePanicRecordsErrorAndKeepsServing(t *testing.T) {
	var logged bytes.Buffer
	workerLog.SetOutput(&logged)
	t.Cleanup(func() { workerLog.SetOutput(os.Stderr) })
	s := openServer(t)
	var boom atomic.Bool
	s.beforeProject = func() {
		if boom.Load() {
			panic("projector fell over")
		}
	}
	h := s.Handler()

	post := func(native string, body []byte) protocol.ManifestAck {
		sum := putBlob(t, h, "", body)
		return postManifest(t, h, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       "machine-a",
			Harness:         protocol.HarnessTerva,
			NativeSessionID: native,
			Artifacts: []protocol.Artifact{{
				Kind:    protocol.KindTranscriptJSONL,
				RelPath: "sessions/x/" + native + ".jsonl",
				Size:    int64(len(body)),
				SHA256:  sum,
			}},
		})
	}
	meta := func(native string) string {
		return `{"type":"meta","meta":{"id":"` + native + `","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`
	}
	msg := `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"panic pond"}],"time":"2026-09-22T16:10:01Z"}}`

	ack := post("sid-panic", transcriptLines(meta("sid-panic")))
	readDerived(t, s, ack.SessionUID)

	boom.Store(true)
	grown := post("sid-panic", transcriptLines(meta("sid-panic"), msg))
	if grown.SessionUID != ack.SessionUID || grown.Relation != protocol.RelationGrownFrom {
		t.Fatalf("grown: %+v", grown)
	}
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || !strings.Contains(got, "panic: projector fell over") {
		t.Fatalf("normalize_error %q ok=%v err=%v", got, ok, err)
	}
	if _, err := os.Stat(filepath.Join(s.Normalized, ack.SessionUID+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("derived jsonl kept after a panic: %v", err)
	}
	jobs, err := s.Catalog.ListNormalizeJobs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("panicked job left queued for the next start: %+v", jobs)
	}
	if !strings.Contains(logged.String(), "panic: projector fell over") || !strings.Contains(logged.String(), "runNormalize") {
		t.Fatalf("panic not logged with a stack:\n%s", logged.String())
	}

	// The workers are still running.
	boom.Store(false)
	for _, native := range []string{"sid-after-1", "sid-after-2", "sid-after-3"} {
		next := post(native, transcriptLines(meta(native), msg))
		if b := readDerived(t, s, next.SessionUID); !bytes.Contains(b, []byte("panic pond")) {
			t.Fatalf("%s after a panic: %s", native, b)
		}
	}
}

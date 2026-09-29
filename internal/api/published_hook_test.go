package api

import (
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// TestOnPublishedSeesReadyStateAndBeforeCloseSeesCatalog shows the hook
// fires only once the session reads as ready, and that BeforeClose runs
// while the catalog is still open.
func TestOnPublishedSeesReadyStateAndBeforeCloseSeesCatalog(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	states := make(chan string, 4)
	s.OnPublished(func(uid string) {
		state, err := s.Catalog.NormalizationState(t.Context(), uid)
		if err != nil {
			state = err.Error()
		}
		states <- state
	})
	s.Allow("sekret")
	h := s.Handler()
	body := transcriptLines(
		`{"type":"meta","meta":{"id":"sid-hook","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"hook"}],"time":"2026-09-22T16:10:01Z"}}`,
	)
	sum := putBlob(t, h, "", body)
	postManifest(t, h, protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: protocol.HarnessTerva, NativeSessionID: "sid-hook",
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/x/sid-hook.jsonl", Size: int64(len(body)), SHA256: sum}}})
	select {
	case state := <-states:
		if state != "ready" {
			t.Fatal("hook ran before ready:", state)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("hook not called")
	}
	closed := false
	s.BeforeClose = func() {
		if _, err := s.Catalog.Counts(t.Context(), catalog.AllBays()); err != nil {
			t.Error("catalog closed before BeforeClose:", err)
		}
		closed = true
	}
	if err := s.Close(); err != nil || !closed {
		t.Fatal(err, closed)
	}
}

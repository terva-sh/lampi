package api

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func TestSupersededStagingDoesNotPublish(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var calls atomic.Int32
	s.beforePublish = func() {
		if calls.Add(1) == 1 {
			close(ready)
			<-release
		}
	}
	body := transcriptLines(`{"type":"meta","meta":{"id":"stream-stale","cwd":"/synthetic"}}`, `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"old"}]}}`)
	sum := putBlob(t, h, "", body)
	ack := postManifest(t, h, manifest("machine-a", "stream-stale", body, sum, 0, sum))
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("staging did not finish")
	}
	if _, err := os.Stat(normalize.EventsPath(s.Normalized, ack.SessionUID)); !os.IsNotExist(err) {
		t.Fatal("private staging was published early")
	}
	next := append(append([]byte{}, body...), []byte("{\"type\":\"message\",\"message\":{\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":\"newest\"}]}}\n")...)
	digest := putBlob(t, h, "", next)
	m := manifest("machine-a", "stream-stale", next, digest, 0, digest)
	m.Harness = protocol.HarnessTerva
	postManifest(t, h, m)
	unblock()
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	raw, err := normalize.ReadEventsFile(s.Normalized, ack.SessionUID)
	if err != nil || !bytes.Contains(raw, []byte("newest")) {
		t.Fatalf("latest output missing: %v", err)
	}
	paths, _ := filepath.Glob(filepath.Join(s.dataDir, ".projection-*"))
	if len(paths) != 0 {
		t.Fatalf("superseded stage leaked: %v", paths)
	}
}

package api

import (
	"sync"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/storage"
)

// TKT-01M3JV45Y: a sample records every component, the bytes the
// catalog references, and the filesystem where the platform reports it.
func TestSampleStorageRecords(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	body := transcriptLines(`{"type":"user","message":{"role":"user","content":"pond"}}`)
	sha := putBlob(t, h, "", body)
	postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessClaude,
		HarnessVersion:  "1",
		NativeSessionID: "sid-storage",
		Artifacts: []protocol.Artifact{{
			Kind: protocol.KindTranscriptJSONL, RelPath: "p/sid-storage.jsonl", Size: int64(len(body)), SHA256: sha,
		}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := s.SampleStorage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range storage.Components {
		if _, ok := got.Measures[c]; !ok {
			t.Errorf("sample has no %s", c)
		}
	}
	if got.Measures[storage.CAS].Files < 1 || got.Measures[storage.Catalog].Bytes == 0 {
		t.Errorf("sample %+v", got.Measures)
	}
	want := catalog.StorageUse{Bytes: int64(len(body)), Files: 1}
	if got.Measures[catalog.MeasureReferenced] != want || got.Measures[catalog.MeasureUnique] != want {
		t.Errorf("referenced %+v unique %+v, want %+v", got.Measures[catalog.MeasureReferenced], got.Measures[catalog.MeasureUnique], want)
	}
	latest, ok, err := s.Catalog.LatestStorage(t.Context())
	if err != nil || !ok || !latest.At.Equal(got.At) || len(latest.Measures) != len(got.Measures) {
		t.Fatalf("latest %+v %v %v", latest, ok, err)
	}
}

// TKT-01M3JV45Z: an earlier contact arriving late does not replace a
// later one.
func TestNoteContactKeepsTheLatest(t *testing.T) {
	var s Server
	base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	s.noteContact("d", base.Add(time.Minute))
	s.noteContact("d", base)
	if got := s.Contacts()["d"]; !got.Equal(base.Add(time.Minute)) {
		t.Fatalf("contact moved back to %s", got)
	}
	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.noteContact("d", base.Add(time.Duration(i)*time.Second))
		}()
	}
	wg.Wait()
	if got := s.Contacts()["d"]; !got.Equal(base.Add(199 * time.Second)) {
		t.Fatalf("after concurrent contacts %s", got)
	}
}

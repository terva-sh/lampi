package api

import (
	"testing"

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

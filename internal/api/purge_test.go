package api

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

func TestPurgeRemovesSessionAndUnsharedBlobs(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	line := func(text string) string {
		return `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"` + text + `"}],"time":"2026-09-22T16:10:01Z"}}` + "\n"
	}
	v1 := []byte(`{"type":"meta","meta":{"id":"sid-a","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}` + "\n" + line("shared start"))
	v2 := append(append([]byte{}, v1...), line("sk-live-leaked")...)
	v3 := append(append([]byte{}, v2...), line("after the leak")...)
	tail1, tail2 := v2[len(v1):], v3[len(v2):]

	d1 := putBlob(t, h, "", v1)
	ackA := postManifest(t, h, manifest("machine-a", "sid-a", v1, d1, 0, d1))
	dt1 := putBlob(t, h, "", tail1)
	d2 := shaOf(t, v2)
	postManifest(t, h, manifest("machine-a", "sid-a", v2, d2, int64(len(v1)), dt1))

	// The third post adds an errors sidecar sent as a chunk list, and
	// the chunks were also bound as a logical file.
	c1, c2 := []byte("error one\n"), []byte("error two\n")
	dc1, dc2 := putBlob(t, h, "", c1), putBlob(t, h, "", c2)
	errs := append(append([]byte{}, c1...), c2...)
	de := shaOf(t, errs)
	if _, err := s.CAS.BindLogical(de, []string{dc1, dc2}, []int64{int64(len(c1)), int64(len(c2))}); err != nil {
		t.Fatal(err)
	}
	dt2 := putBlob(t, h, "", tail2)
	d3 := shaOf(t, v3)
	m3 := manifest("machine-a", "sid-a", v3, d3, int64(len(v2)), dt2)
	m3.Artifacts = append(m3.Artifacts, protocol.Artifact{
		Kind:         protocol.KindErrorsJSONL,
		RelPath:      "sessions/x/sid-a.errors.jsonl",
		Size:         int64(len(errs)),
		SHA256:       de,
		ChunkSHA256s: []string{dc1, dc2},
		ChunkLengths: []int64{int64(len(c1)), int64(len(c2))},
	})
	postManifest(t, h, m3)

	// Another session holds the same first bytes.
	ackB := postManifest(t, h, manifest("machine-b", "sid-b", v1, d1, 0, d1))
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}

	plan, ok, err := s.PlanPurge(t.Context(), ackA.SessionUID)
	if err != nil || !ok {
		t.Fatalf("plan ok=%v err=%v", ok, err)
	}
	want := []string{d2, d3, dt1, dt2, de, dc1, dc2}
	sort.Strings(want)
	if strings.Join(plan.Objects, ",") != strings.Join(want, ",") {
		t.Fatalf("objects\n%v\nwant\n%v", plan.Objects, want)
	}
	if len(plan.Logical) != 1 || plan.Logical[0] != de || plan.Kept != 1 {
		t.Fatalf("logical %v kept %d", plan.Logical, plan.Kept)
	}
	if ok, _ := s.CAS.Has(d2); !ok {
		t.Fatal("planning removed a blob")
	}

	if err := s.Purge(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	for _, d := range want {
		if obj, logical, _, _ := s.CAS.Stored(d); obj || logical {
			t.Fatalf("%s still stored", d)
		}
	}
	if got, err := s.CAS.Read(d1); err != nil || string(got) != string(v1) {
		t.Fatalf("shared blob: %v", err)
	}
	if _, ok, _ := s.Catalog.Session(t.Context(), ackA.SessionUID); ok {
		t.Fatal("session row kept")
	}
	if arts, _ := s.Catalog.Artifacts(t.Context(), ackA.SessionUID); len(arts) != 0 {
		t.Fatalf("artifact rows kept: %+v", arts)
	}
	if _, ok, _ := s.Catalog.Alias(t.Context(), protocol.HarnessTerva, "sid-a", "machine-a"); ok {
		t.Fatal("alias kept")
	}
	if _, err := os.Stat(filepath.Join(s.Normalized, ackA.SessionUID+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("derived file kept: %v", err)
	}
	if _, ok, _ := s.Catalog.Session(t.Context(), ackB.SessionUID); !ok {
		t.Fatal("the other session went too")
	}

	// Run again: the session is gone.
	if _, ok, err := s.PlanPurge(t.Context(), ackA.SessionUID); ok || err != nil {
		t.Fatalf("second plan ok=%v err=%v", ok, err)
	}
}

package api

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/protocol"
)

func TestHelloFields(t *testing.T) {
	s := openServer(t)
	s.Allow("sekret")
	now := time.Date(2026, 9, 22, 16, 10, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	h := s.Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/hello", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("hello without token: %d %s", rr.Code, rr.Body)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/hello", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("hello %d %s", rr.Code, rr.Body)
	}
	var hello protocol.HelloResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &hello); err != nil {
		t.Fatal(err)
	}
	if !hello.ServerTime.Equal(now) {
		t.Fatalf("server_time %s", hello.ServerTime)
	}
	if len(hello.ProtocolVersions) != 1 || hello.ProtocolVersions[0] != protocol.Version {
		t.Fatalf("versions %+v", hello.ProtocolVersions)
	}
	if hello.MaxBlobBytes != protocol.MaxBlobBytes {
		t.Fatalf("max_blob_bytes %d", hello.MaxBlobBytes)
	}
}

func TestAppendTailAndUnchangedResync(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	prefix := []byte("{\"type\":\"meta\"}\n")
	tail := []byte("{\"type\":\"message\"}\n")
	full := append(append([]byte{}, prefix...), tail...)
	prefixSHA := putRaw(t, h, prefix)
	fullSHA := sha256Hex(full)
	tailSHA := sha256Hex(tail)

	m := manifest("machine-a", "sid", prefix, prefixSHA, 0, prefixSHA)
	first := postManifest(t, h, m)
	if first.Relation != protocol.RelationHead || first.HeadSHA256 != prefixSHA || first.HeadSize != int64(len(prefix)) {
		t.Fatalf("first: %+v", first)
	}
	before := countBlobs(t, s)

	putRaw(t, h, tail)
	grown := manifest("machine-a", "sid", full, fullSHA, int64(len(prefix)), tailSHA)
	ack := postManifest(t, h, grown)
	if ack.SessionUID != first.SessionUID || ack.Relation != protocol.RelationGrownFrom || ack.HeadSHA256 != fullSHA || ack.HeadSize != int64(len(full)) {
		t.Fatalf("grown: %+v", ack)
	}
	if ack.ArtifactIDs[0] == first.ArtifactIDs[0] {
		t.Fatal("grown reused the prefix artifact id")
	}
	if countBlobs(t, s) != before+2 {
		t.Fatalf("blobs %d, want tail plus assembled", countBlobs(t, s))
	}
	stored, err := s.CAS.Read(fullSHA)
	if err != nil || !bytes.Equal(stored, full) {
		t.Fatalf("assembled %q err=%v", stored, err)
	}
	arts, err := s.Catalog.Artifacts(t.Context(), ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 || arts[1].Relation != protocol.RelationGrownFrom || arts[1].GrownFrom != prefixSHA || !arts[1].Current {
		t.Fatalf("artifacts: %+v", arts)
	}

	again := postManifest(t, h, manifest("machine-a", "sid", full, fullSHA, 0, fullSHA))
	if again.SessionUID != first.SessionUID || again.ArtifactIDs[0] != ack.ArtifactIDs[0] || again.Relation != protocol.RelationUnchanged {
		t.Fatalf("resync: %+v", again)
	}
	if countBlobs(t, s) != before+2 {
		t.Fatal("unchanged re-sync stored a blob")
	}
}

func TestDivergentCopyNotMerged(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	a := []byte("alpha-session\n")
	b := []byte("bravo-session\n")
	aSHA := putRaw(t, h, a)
	bSHA := putRaw(t, h, b)
	first := postManifest(t, h, manifest("machine-a", "sid", a, aSHA, 0, aSHA))
	div := postManifest(t, h, manifest("machine-a", "sid", b, bSHA, 0, bSHA))
	if div.SessionUID != first.SessionUID || div.HeadSHA256 != aSHA || div.Relation != protocol.RelationDivergentCopy {
		t.Fatalf("divergent ack: %+v", div)
	}
	if div.HeadSize != int64(len(a)) {
		t.Fatalf("head size %d", div.HeadSize)
	}
	arts, err := s.Catalog.Artifacts(t.Context(), first.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 || arts[0].SHA256 != aSHA || arts[1].SHA256 != bSHA || arts[1].Relation != protocol.RelationDivergentCopy || arts[1].Current || !arts[0].Current {
		t.Fatalf("artifacts: %+v", arts)
	}
}

func TestConflictsEndpoint(t *testing.T) {
	s := openServer(t)
	h := s.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/conflicts", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("without token: %d %s", rr.Code, rr.Body)
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/conflicts", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"conflicts":[]`)) {
		t.Fatalf("empty %d %s", rr.Code, rr.Body)
	}

	prefix := []byte("prefix\n")
	full := []byte("prefix\nmore\n")
	other := []byte("other-bytes\n")
	quiet := []byte("quiet-session\n")
	prefixSHA := putRaw(t, h, prefix)
	fullSHA := putRaw(t, h, full)
	otherSHA := putRaw(t, h, other)
	quietSHA := putRaw(t, h, quiet)
	postManifest(t, h, manifest("machine-a", "sid", prefix, prefixSHA, 0, prefixSHA))
	grown := postManifest(t, h, manifest("machine-a", "sid", full, fullSHA, 0, fullSHA))
	if grown.Relation != protocol.RelationGrownFrom {
		t.Fatalf("grown: %+v", grown)
	}
	div := postManifest(t, h, manifest("machine-b", "sid", other, otherSHA, 0, otherSHA))
	postManifest(t, h, manifest("machine-a", "sid-quiet", quiet, quietSHA, 0, quietSHA))

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/conflicts", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list %d %s", rr.Code, rr.Body)
	}
	var body protocol.ConflictsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Conflicts) != 1 {
		t.Fatalf("conflicts %+v", body.Conflicts)
	}
	got := body.Conflicts[0]
	if got.SessionUID != div.SessionUID || got.ArtifactID != div.ArtifactIDs[0] {
		t.Fatalf("identity %+v ack %+v", got, div)
	}
	if got.SHA256 != otherSHA || got.Size != int64(len(other)) || got.HeadSHA256 != fullSHA || got.HeadSize != int64(len(full)) {
		t.Fatalf("digests %+v", got)
	}
	if got.NativeSessionID != "sid" || got.Harness != protocol.HarnessTerva || got.Kind != protocol.KindTranscriptJSONL {
		t.Fatalf("session %+v", got)
	}
	if len(got.Machines) != 1 || got.Machines[0] != "machine-b" || len(got.HeadMachines) != 1 || got.HeadMachines[0] != "machine-a" {
		t.Fatalf("machines %+v head %+v", got.Machines, got.HeadMachines)
	}
}

func TestTailMismatchDoesNotMoveHead(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	prefix := []byte("prefix\n")
	prefixSHA := putRaw(t, h, prefix)
	first := postManifest(t, h, manifest("machine-a", "sid", prefix, prefixSHA, 0, prefixSHA))

	tail := []byte("nope\n")
	tailSHA := putRaw(t, h, tail)
	claimed := sha256Hex([]byte("not-the-assembly"))
	m := manifest("machine-a", "sid", nil, claimed, int64(len(prefix)), tailSHA)
	m.Artifacts[0].Size = int64(len(prefix) + len(tail))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict || !bytes.Contains(rr.Body.Bytes(), []byte("prefix mismatch")) {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	arts, err := s.Catalog.Artifacts(t.Context(), first.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 || arts[0].SHA256 != prefixSHA {
		t.Fatalf("head changed: %+v", arts)
	}
}

func TestStaleKeepsHead(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	full := []byte("prefix\nmessage\n")
	prefix := []byte("prefix\n")
	fullSHA := putRaw(t, h, full)
	prefixSHA := putRaw(t, h, prefix)
	first := postManifest(t, h, manifest("machine-a", "sid", full, fullSHA, 0, fullSHA))
	stale := postManifest(t, h, manifest("machine-b", "sid", prefix, prefixSHA, 0, prefixSHA))
	if stale.SessionUID != first.SessionUID || stale.Relation != protocol.RelationStale || stale.HeadSHA256 != fullSHA || stale.HeadSize != int64(len(full)) {
		t.Fatalf("stale: %+v", stale)
	}
	if stale.ArtifactIDs[0] != first.ArtifactIDs[0] {
		t.Fatalf("stale minted an artifact: %+v", stale)
	}
	arts, err := s.Catalog.Artifacts(t.Context(), first.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Fatalf("artifacts: %+v", arts)
	}
}

func TestSecondMachineProvenance(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	body := []byte("same-bytes\n")
	sum := putRaw(t, h, body)
	before := countBlobs(t, s)
	first := postManifest(t, h, manifest("machine-a", "sid", body, sum, 0, sum))
	second := postManifest(t, h, manifest("machine-b", "sid", body, sum, 0, sum))
	if second.SessionUID != first.SessionUID || second.ArtifactIDs[0] != first.ArtifactIDs[0] || second.Relation != protocol.RelationUnchanged {
		t.Fatalf("second: %+v", second)
	}
	if countBlobs(t, s) != before {
		t.Fatalf("second machine stored a blob: %d want %d", countBlobs(t, s), before)
	}
	ctx := t.Context()
	for _, machine := range []string{"machine-a", "machine-b"} {
		uid, ok, err := s.Catalog.Alias(ctx, protocol.HarnessTerva, "sid", machine)
		if err != nil || !ok || uid != first.SessionUID {
			t.Fatalf("alias %s: %q ok=%v err=%v", machine, uid, ok, err)
		}
	}
	prov, err := s.Catalog.Provenance(ctx, first.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(prov) != 2 || prov[0].MachineID != "machine-a" || prov[1].MachineID != "machine-b" || prov[0].SHA256 != sum || prov[1].SHA256 != sum {
		t.Fatalf("provenance: %+v", prov)
	}
}

func TestConcurrentGrownFromKeepsOneExtension(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	base := []byte("base\n")
	baseSHA := putRaw(t, h, base)
	first := postManifest(t, h, manifest("machine-a", "sid", base, baseSHA, 0, baseSHA))

	left := append(append([]byte{}, base...), []byte("left\n")...)
	right := append(append([]byte{}, base...), []byte("right\n")...)
	leftSHA := putRaw(t, h, left)
	rightSHA := putRaw(t, h, right)
	bodies := [][]byte{
		mustJSON(t, manifest("machine-a", "sid", left, leftSHA, 0, leftSHA)),
		mustJSON(t, manifest("machine-b", "sid", right, rightSHA, 0, rightSHA)),
	}
	type result struct {
		code int
		ack  protocol.ManifestAck
		body string
	}
	results := make([]result, len(bodies))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range bodies {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(bodies[i]))
			req.Header.Set("Authorization", "Bearer sekret")
			h.ServeHTTP(rr, req)
			var ack protocol.ManifestAck
			_ = json.Unmarshal(rr.Body.Bytes(), &ack)
			results[i] = result{code: rr.Code, ack: ack, body: rr.Body.String()}
		}(i)
	}
	close(start)
	wg.Wait()
	for i, r := range results {
		if r.code != http.StatusOK {
			t.Fatalf("manifest %d: %d %s", i, r.code, r.body)
		}
		if r.ack.SessionUID != first.SessionUID {
			t.Fatalf("manifest %d uid %s", i, r.ack.SessionUID)
		}
	}
	uid, current, ok, err := s.Catalog.Current(t.Context(), protocol.HarnessTerva, "sid")
	if err != nil || !ok || uid != first.SessionUID || len(current) != 1 {
		t.Fatalf("current uid=%s ok=%v err=%v rows=%+v", uid, ok, err, current)
	}
	head := current[0]
	if head.SHA256 != leftSHA && head.SHA256 != rightSHA {
		t.Fatalf("head %s is not one of the extensions", head.SHA256)
	}
	stored, err := s.CAS.Read(head.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(stored, base) {
		t.Fatalf("head is not an extension of the base: %q", stored)
	}
	arts, err := s.Catalog.Artifacts(t.Context(), first.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	var grown, divergent int
	for _, a := range arts {
		switch a.Relation {
		case protocol.RelationGrownFrom:
			if !a.Current || a.GrownFrom != baseSHA || a.SHA256 != head.SHA256 {
				t.Fatalf("grown: %+v", a)
			}
			grown++
		case protocol.RelationDivergentCopy:
			if a.Current {
				t.Fatalf("divergent is current: %+v", a)
			}
			divergent++
		}
	}
	if grown != 1 || divergent != 1 || len(arts) != 3 {
		t.Fatalf("artifacts: %+v", arts)
	}
}

func openServer(t *testing.T) *Server {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	return s
}

func putRaw(t *testing.T, h http.Handler, body []byte) string {
	t.Helper()
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+sum, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put %d %s", rr.Code, rr.Body)
	}
	return sum
}

func manifest(machine, native string, body []byte, sum string, prev int64, tail string) protocol.Manifest {
	size := int64(len(body))
	if body == nil {
		size = 0
	}
	return protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       machine,
		Harness:         protocol.HarnessTerva,
		NativeSessionID: native,
		Artifacts: []protocol.Artifact{{
			Kind:              protocol.KindTranscriptJSONL,
			RelPath:           "sessions/x/" + native + ".jsonl",
			Size:              size,
			SHA256:            sum,
			ByteWatermarkPrev: prev,
			TailSHA256:        tail,
		}},
	}
}

func countBlobs(t *testing.T, s *Server) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(s.CAS.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		n++
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return n
}

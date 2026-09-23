package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/protocol"
)

func TestCheckNamesMissingDigests(t *testing.T) {
	s := openResume(t)
	body := []byte("present\n")
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := putBytes(t, s, sum, body); code != http.StatusOK {
		t.Fatalf("put %d", code)
	}
	missing := strings.Repeat("b", 64)
	other := strings.Repeat("c", 64)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/blobs/check", bytes.NewReader(mustJSON(t, []string{sum, missing, sum, other})))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("check %d %s", rr.Code, rr.Body)
	}
	var got protocol.BlobCheckResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Missing) != 2 || got.Missing[0] != missing || got.Missing[1] != other {
		t.Fatalf("missing %+v", got.Missing)
	}
}

func TestContentRangeAssemblesAndExistingStoresNothing(t *testing.T) {
	s := openResume(t)
	body := []byte("0123456789abcdef")
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	// The tail arrives first. The object is not installed until the gap closes.
	code, put := putRange(t, s, sum, body[10:], 10, 15, int64(len(body)))
	if code != http.StatusOK || put.Complete || put.Exists {
		t.Fatalf("partial %d %+v", code, put)
	}
	ok, err := s.CAS.Has(sum)
	if err != nil || ok {
		t.Fatalf("partial installed: has %v %v", ok, err)
	}

	code, put = putRange(t, s, sum, body[:10], 0, 9, int64(len(body)))
	if code != http.StatusOK || !put.Complete || put.Exists {
		t.Fatalf("assemble %d %+v", code, put)
	}
	got, err := s.CAS.Read(sum)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("assembled %q", got)
	}

	path, err := s.CAS.Path(sum)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	code, put = putRange(t, s, sum, body[:4], 0, 3, int64(len(body)))
	if code != http.StatusOK || !put.Exists || !put.Complete {
		t.Fatalf("existing range %d %+v", code, put)
	}
	code, put = putBytes(t, s, sum, body)
	if code != http.StatusOK || !put.Exists {
		t.Fatalf("existing raw %d %+v", code, put)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(past) {
		t.Fatalf("existing digest was rewritten at %s", st.ModTime())
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, body) {
		t.Fatalf("rewritten %q", again)
	}
}

func TestChunkDigestsAssembleOnPutAndManifest(t *testing.T) {
	s := openResume(t)
	body := []byte("abcdefghijklmnopqrstuvwxyz")
	chunks := [][]byte{body[:10], body[10:20], body[20:]}
	var digests []string
	for _, c := range chunks {
		d, _, err := cas.Hash(bytes.NewReader(c))
		if err != nil {
			t.Fatal(err)
		}
		digests = append(digests, d)
		if code, put := putBytes(t, s, d, c); code != http.StatusOK || put.Exists {
			t.Fatalf("chunk put %d %+v", code, put)
		}
	}
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	// One chunk still absent: the assembly put names it and stores nothing.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+sum, bytes.NewReader(mustJSON(t, map[string]any{
		"chunk_sha256s": []string{digests[0], strings.Repeat("d", 64)},
	})))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict || !bytes.Contains(rr.Body.Bytes(), []byte(strings.Repeat("d", 64))) {
		t.Fatalf("missing chunk %d %s", rr.Code, rr.Body)
	}
	if ok, err := s.CAS.Has(sum); err != nil || ok {
		t.Fatalf("partial chunk install has %v %v", ok, err)
	}

	code, put := putChunks(t, s, sum, digests)
	if code != http.StatusOK || !put.Complete || put.Exists {
		t.Fatalf("chunk assemble %d %+v", code, put)
	}
	got, err := s.CAS.Read(sum)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("chunk bytes %q", got)
	}

	path, err := s.CAS.Path(sum)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	code, put = putChunks(t, s, sum, []string{strings.Repeat("e", 64)})
	if code != http.StatusOK || !put.Exists || !put.Complete {
		t.Fatalf("existing chunks %d %+v", code, put)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(past) {
		t.Fatalf("chunk put rewrote an existing digest at %s", st.ModTime())
	}

	// A second object is assembled from the manifest's chunk list, with
	// no separate assembly PUT. Repeating it does not rewrite the blob.
	body2 := []byte("manifest-chunks")
	c0, c1 := body2[:8], body2[8:]
	d0, _, err := cas.Hash(bytes.NewReader(c0))
	if err != nil {
		t.Fatal(err)
	}
	d1, _, err := cas.Hash(bytes.NewReader(c1))
	if err != nil {
		t.Fatal(err)
	}
	full, _, err := cas.Hash(bytes.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := putBytes(t, s, d0, c0); code != http.StatusOK {
		t.Fatalf("chunk0 %d", code)
	}
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "chunk-session",
		Artifacts: []protocol.Artifact{{
			Kind:         protocol.KindTranscriptJSONL,
			RelPath:      "sessions/x/chunk.jsonl",
			Size:         int64(len(body2)),
			SHA256:       full,
			ChunkSHA256s: []string{d0, d1},
		}},
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict || !bytes.Contains(rr.Body.Bytes(), []byte(d1)) {
		t.Fatalf("manifest missing chunk %d %s", rr.Code, rr.Body)
	}
	if code, _ := putBytes(t, s, d1, c1); code != http.StatusOK {
		t.Fatalf("chunk1 %d", code)
	}
	ack := postManifest(t, s.Handler(), m)
	if ack.HeadSHA256 != full {
		t.Fatalf("head %s", ack.HeadSHA256)
	}
	got, err = s.CAS.Read(full)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body2) {
		t.Fatalf("manifest assembled %q", got)
	}
	fullPath, err := s.CAS.Path(full)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fullPath, past, past); err != nil {
		t.Fatal(err)
	}
	postManifest(t, s.Handler(), m)
	st, err = os.Stat(fullPath)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(past) {
		t.Fatalf("manifest rewrote assembled digest at %s", st.ModTime())
	}
}

func TestContentRangeHashMismatchCanBeRetried(t *testing.T) {
	s := openResume(t)
	body := []byte("0123456789abcdef")
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	wrong := bytes.Repeat([]byte{'x'}, len(body))
	code, _ := putRange(t, s, sum, wrong, 0, int64(len(wrong)-1), int64(len(wrong)))
	if code != http.StatusBadRequest {
		t.Fatalf("mismatch status %d", code)
	}
	if ok, err := s.CAS.Has(sum); err != nil || ok {
		t.Fatalf("mismatch installed the blob: has %v %v", ok, err)
	}
	if _, err := os.Stat(partialPath(s, sum)); !os.IsNotExist(err) {
		t.Fatalf("covered partial left after hash mismatch: %v", err)
	}

	code, put := putRange(t, s, sum, body[:8], 0, 7, int64(len(body)))
	if code != http.StatusOK || put.Complete || put.Exists {
		t.Fatalf("retry partial %d %+v", code, put)
	}
	code, put = putRange(t, s, sum, body[8:], 8, 15, int64(len(body)))
	if code != http.StatusOK || !put.Complete || put.Exists {
		t.Fatalf("retry assemble %d %+v", code, put)
	}
	got, err := s.CAS.Read(sum)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("retried %q", got)
	}
}

func TestConcatRejectsOversizeAssembly(t *testing.T) {
	s := openResume(t)
	big := bytes.Repeat([]byte{'a'}, int(protocol.MaxBlobBytes))
	extra := []byte{'b'}
	d0, _, err := cas.Hash(bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	d1, _, err := cas.Hash(bytes.NewReader(extra))
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := putBytes(t, s, d0, big); code != http.StatusOK {
		t.Fatalf("chunk0 %d", code)
	}
	if code, _ := putBytes(t, s, d1, extra); code != http.StatusOK {
		t.Fatalf("chunk1 %d", code)
	}
	full, _, err := cas.Hash(io.MultiReader(bytes.NewReader(big), bytes.NewReader(extra)))
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+full, bytes.NewReader(mustJSON(t, map[string]any{
		"chunk_sha256s": []string{d0, d1},
	})))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || !bytes.Contains(rr.Body.Bytes(), []byte("exceeds")) {
		t.Fatalf("chunk put %d %s", rr.Code, rr.Body)
	}
	if ok, err := s.CAS.Has(full); err != nil || ok {
		t.Fatalf("oversize chunk put installed the blob: has %v %v", ok, err)
	}

	// The manifest is the path that records a file over the cap. A PUT
	// of the concatenation still refuses to install one object.
	post := func(m protocol.Manifest) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
		s.Handler().ServeHTTP(rr, req)
		return rr
	}
	base := func(sha string, lengths []int64) protocol.Manifest {
		return protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       "machine-a",
			Harness:         protocol.HarnessTerva,
			NativeSessionID: "oversize",
			Artifacts: []protocol.Artifact{{
				Kind:         protocol.KindTranscriptJSONL,
				RelPath:      "sessions/x/oversize.jsonl",
				Size:         protocol.MaxBlobBytes + 1,
				SHA256:       sha,
				ChunkSHA256s: []string{d0, d1},
				ChunkLengths: lengths,
			}},
		}
	}

	rr = post(base(full, nil))
	if rr.Code != http.StatusBadRequest || !bytes.Contains(rr.Body.Bytes(), []byte("chunk_lengths required")) {
		t.Fatalf("missing lengths %d %s", rr.Code, rr.Body)
	}
	rr = post(base(full, []int64{1, protocol.MaxBlobBytes}))
	if rr.Code != http.StatusBadRequest || !bytes.Contains(rr.Body.Bytes(), []byte("manifest says")) {
		t.Fatalf("length mismatch %d %s", rr.Code, rr.Body)
	}
	wrong := strings.Repeat("ab", 32)
	rr = post(base(wrong, []int64{protocol.MaxBlobBytes, 1}))
	if rr.Code != http.StatusBadRequest || !bytes.Contains(rr.Body.Bytes(), []byte("does not match")) {
		t.Fatalf("hash mismatch %d %s", rr.Code, rr.Body)
	}
	missingChunk := base(full, []int64{protocol.MaxBlobBytes, 1})
	missingChunk.Artifacts[0].ChunkSHA256s = []string{d0, strings.Repeat("cd", 32)}
	rr = post(missingChunk)
	if rr.Code != http.StatusConflict || !bytes.Contains(rr.Body.Bytes(), []byte(strings.Repeat("cd", 32))) {
		t.Fatalf("missing chunk %d %s", rr.Code, rr.Body)
	}
	tailed := base(full, []int64{protocol.MaxBlobBytes, 1})
	tailed.Artifacts[0].ByteWatermarkPrev = 1
	tailed.Artifacts[0].TailSHA256 = d1
	rr = post(tailed)
	if rr.Code != http.StatusBadRequest || !bytes.Contains(rr.Body.Bytes(), []byte("cannot be combined with a tail")) {
		t.Fatalf("tail plus chunks %d %s", rr.Code, rr.Body)
	}
	if ok, err := s.CAS.Has(full); err != nil || ok {
		t.Fatalf("failed manifest installed the blob: has %v %v", ok, err)
	}

	rr = post(base(full, []int64{protocol.MaxBlobBytes, 1}))
	if rr.Code != http.StatusOK {
		t.Fatalf("logical manifest %d %s", rr.Code, rr.Body)
	}
	if ok, err := s.CAS.Has(full); err != nil || ok {
		t.Fatalf("logical file installed a single object: has %v %v", ok, err)
	}
	got, err := s.CAS.Read(full)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(got)) != protocol.MaxBlobBytes+1 || got[0] != 'a' || got[len(got)-1] != 'b' {
		t.Fatalf("logical read len %d first %q last %q", len(got), got[0], got[len(got)-1])
	}
	rr = post(base(full, []int64{protocol.MaxBlobBytes, 1}))
	if rr.Code != http.StatusOK {
		t.Fatalf("second logical manifest %d %s", rr.Code, rr.Body)
	}
	if ok, err := s.CAS.Has(full); err != nil || ok {
		t.Fatalf("second manifest installed a single object: has %v %v", ok, err)
	}
}

func partialPath(s *Server, digest string) string {
	return filepath.Join(s.CAS.Root, "partial", digest[:2], digest[2:])
}

func openResume(t *testing.T) *Server {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func putBytes(t *testing.T, s *Server, digest string, body []byte) (int, protocol.PutResponse) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+digest, bytes.NewReader(body))
	s.Handler().ServeHTTP(rr, req)
	var put protocol.PutResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &put); err != nil {
			t.Fatal(err)
		}
	}
	return rr.Code, put
}

func putRange(t *testing.T, s *Server, digest string, body []byte, start, end, total int64) (int, protocol.PutResponse) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+digest, bytes.NewReader(body))
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	s.Handler().ServeHTTP(rr, req)
	var put protocol.PutResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &put); err != nil {
			t.Fatal(err)
		}
	}
	return rr.Code, put
}

func putChunks(t *testing.T, s *Server, digest string, chunks []string) (int, protocol.PutResponse) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+digest, bytes.NewReader(mustJSON(t, map[string]any{
		"chunk_sha256s": chunks,
	})))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rr, req)
	var put protocol.PutResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &put); err != nil {
			t.Fatal(err)
		}
	}
	return rr.Code, put
}

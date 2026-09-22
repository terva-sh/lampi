package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

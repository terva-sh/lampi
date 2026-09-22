package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/protocol"
)

func TestHealthAndIngest(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Token = "sekret"
	s.Now = func() time.Time { return time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC) }
	h := s.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"ok"`)) {
		t.Fatalf("health %d %s", rr.Code, rr.Body)
	}

	body := []byte("hello lake\n")
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	// Blob routes require the token. healthz does not.
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/blobs/check", bytes.NewReader(mustJSON(t, []string{sum})))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("check without token: %d %s", rr.Code, rr.Body)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/blobs/check", bytes.NewReader(mustJSON(t, []string{sum})))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("check %d %s", rr.Code, rr.Body)
	}
	var missing protocol.BlobCheckResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &missing); err != nil {
		t.Fatal(err)
	}
	if len(missing.Missing) != 1 || missing.Missing[0] != sum {
		t.Fatalf("missing: %+v", missing)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/v1/blobs/"+sum, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put %d %s", rr.Code, rr.Body)
	}
	var put protocol.PutResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &put); err != nil {
		t.Fatal(err)
	}
	if put.Exists || put.SHA256 != sum {
		t.Fatalf("put response: %+v", put)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/v1/blobs/"+sum, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if err := json.Unmarshal(rr.Body.Bytes(), &put); err != nil {
		t.Fatal(err)
	}
	if !put.Exists {
		t.Fatal("second put should report exists")
	}

	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-1",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/sid-1.jsonl",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	}
	ack := postManifest(t, h, m)
	again := postManifest(t, h, m)
	if again.SessionUID != ack.SessionUID || again.ArtifactIDs[0] != ack.ArtifactIDs[0] {
		t.Fatalf("ack changed: %+v %+v", ack, again)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/hello", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"protocol_versions"`)) {
		t.Fatalf("hello %d %s", rr.Code, rr.Body)
	}
}

func TestManifestMissingBlob(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "m",
		Harness:         "terva",
		NativeSessionID: "s",
		Artifacts: []protocol.Artifact{{
			Kind:   protocol.KindTranscriptJSONL,
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
}

func TestPutRejectsMismatchAndStorage(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := s.Handler()

	body := []byte("hello lake\n")
	other, _, err := cas.Hash(bytes.NewReader([]byte("other")))
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+other, bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("mismatch status %d %s", rr.Code, rr.Body)
	}

	broken := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(broken, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.CAS = &cas.Store{Root: broken}
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/v1/blobs/"+sum, bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("storage status %d %s", rr.Code, rr.Body)
	}
}

func postManifest(t *testing.T, h http.Handler, m protocol.Manifest) protocol.ManifestAck {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("manifest %d %s", rr.Code, rr.Body)
	}
	var ack protocol.ManifestAck
	if err := json.Unmarshal(rr.Body.Bytes(), &ack); err != nil {
		t.Fatal(err)
	}
	return ack
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

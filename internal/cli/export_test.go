package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/protocol"

	_ "modernc.org/sqlite"
)

const proofPrompt = "normalize-proof prompt: lampi-pond-7f3a"

// TestKnownPromptAfterIngest checks the export failure path: a session
// that did not normalize is named on stderr, and its raw blob stays as
// it was. The five-step MVP gate, which queries this same prompt after
// a real sync, is internal/accept.TestMVPAcceptance.
func TestKnownPromptAfterIngest(t *testing.T) {
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Allow("sekret")
	h := lake.Handler()

	bad := []byte("not-json sk-live-secret\n")
	badSum, _, err := cas.Hash(bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, badSum, bad)
	postManifest(t, h, manifest("sid-bad", "sessions/x/sid-bad.jsonl", badSum, int64(len(bad))))

	good := fixturePrompt()
	goodSum, _, err := cas.Hash(bytes.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, goodSum, good)
	ack := postManifest(t, h, manifest("sid-prompt", "sessions/x/sid-prompt.jsonl", goodSum, int64(len(good))))
	msg, ok, err := lake.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg != "" {
		t.Fatalf("normalize_error after a valid transcript: %q", msg)
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}

	// Drop the derived file so export projects from the raw blob again.
	if err := os.Remove(filepath.Join(dir, "normalized", ack.SessionUID+".jsonl")); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "events.jsonl")
	var stderr bytes.Buffer
	if err := Run([]string{"export", "--data", dir, "--out", out}, Env{Stdout: &bytes.Buffer{}, Stderr: &stderr}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr.String(), "sk-live-secret") || strings.Contains(stderr.String(), "not-json") {
		t.Fatalf("stderr includes the raw line:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "normalize_error") {
		t.Fatalf("stderr: %s", stderr.String())
	}
	if queryContent(t, out, "%"+proofPrompt+"%") != proofPrompt {
		t.Fatal("query missed the fixture prompt")
	}

	left, err := os.ReadFile(filepath.Join(dir, "cas", "sha256", badSum[:2], badSum[2:]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, bad) {
		t.Fatal("failed session's raw blob changed")
	}
}

func fixturePrompt() []byte {
	return []byte(strings.Join([]string{
		`{"type":"meta","meta":{"id":"sid-prompt","cwd":"/home/drew/src/foo","model":"gpt-5","provider":"openai","started":"2026-09-22T16:10:00Z","version":"0.137.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"` + proofPrompt + `"}],"time":"2026-09-22T16:10:01Z"}}`,
	}, "\n") + "\n")
}

func manifest(native, rel, sum string, size int64) protocol.Manifest {
	return protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: native,
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: rel,
			Size:    size,
			SHA256:  sum,
		}},
	}
}

func putBlob(t *testing.T, h http.Handler, sum string, body []byte) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+sum, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put %d %s", rr.Code, rr.Body)
	}
}

func postManifest(t *testing.T, h http.Handler, m protocol.Manifest) protocol.ManifestAck {
	t.Helper()
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("manifest %d %s", rr.Code, rr.Body)
	}
	var ack protocol.ManifestAck
	if err := json.Unmarshal(rr.Body.Bytes(), &ack); err != nil {
		t.Fatal(err)
	}
	if ack.SessionUID == "" {
		t.Fatal("empty session uid")
	}
	return ack
}

func queryContent(t *testing.T, path, like string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE export_lines (line TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if _, err := db.Exec(`INSERT INTO export_lines (line) VALUES (?)`, string(line)); err != nil {
			t.Fatal(err)
		}
	}
	var got string
	err = db.QueryRow(`
		SELECT json_extract(line, '$.content_text')
		FROM export_lines
		WHERE json_extract(line, '$.content_text') LIKE ?`, like).Scan(&got)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return got
}

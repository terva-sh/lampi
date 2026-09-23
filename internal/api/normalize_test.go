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

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func TestNormalizeFailureLeavesRaw(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	body := []byte("not-json sk-live-secret\n")
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, sum, body)
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-bad",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/sid-bad.jsonl",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}

	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !strings.Contains(msg, "not a JSON object") {
		t.Fatalf("normalize_error: %q ok=%v", msg, ok)
	}
	if strings.Contains(msg, "sk-live-secret") {
		t.Fatalf("normalize_error includes the raw line: %s", msg)
	}
	if got := readBlobBytes(t, s, sum); !bytes.Equal(got, body) {
		t.Fatal("raw blob changed after a normalize failure")
	}
	if _, err := os.Stat(filepath.Join(s.Normalized, ack.SessionUID+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("derived file after failure: %v", err)
	}
	parts, err := normalize.SessionParquet(s.Parquet, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 {
		t.Fatalf("parquet after failure: %v", parts)
	}
}

func TestProjectUsesAssembledHead(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	prefix := transcriptLines(
		`{"type":"meta","meta":{"id":"sid-grow","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"prefix-only pond"}],"time":"2026-09-22T16:10:01Z"}}`,
	)
	tail := transcriptLines(
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"assembled-only pond"}],"time":"2026-09-22T16:10:02Z"}}`,
	)
	full := append(append([]byte{}, prefix...), tail...)
	prefixSHA := putBlob(t, h, "", prefix)
	tailSHA := shaOf(t, tail)
	fullSHA := shaOf(t, full)
	putBlob(t, h, tailSHA, tail)

	first := postManifest(t, h, manifest("machine-a", "sid-grow", prefix, prefixSHA, 0, prefixSHA))
	grown := postManifest(t, h, manifest("machine-a", "sid-grow", full, fullSHA, int64(len(prefix)), tailSHA))
	if grown.Relation != protocol.RelationGrownFrom || grown.HeadSHA256 != fullSHA || grown.SessionUID != first.SessionUID {
		t.Fatalf("grown ack: %+v", grown)
	}
	derived := readDerived(t, s, grown.SessionUID)
	if !bytes.Contains(derived, []byte("prefix-only pond")) || !bytes.Contains(derived, []byte("assembled-only pond")) {
		t.Fatalf("projection missed the assembled head:\n%s", derived)
	}
	if got := readBlobBytes(t, s, prefixSHA); !bytes.Equal(got, prefix) {
		t.Fatal("prefix blob changed after growth")
	}

	left := transcriptLines(
		`{"type":"meta","meta":{"id":"chunk-a","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"chunk-left pond"}],"time":"2026-09-22T16:10:01Z"}}`,
	)
	right := transcriptLines(
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"chunk-right pond"}],"time":"2026-09-22T16:10:02Z"}}`,
	)
	whole := append(append([]byte{}, left...), right...)
	leftSHA := putBlob(t, h, "", left)
	rightSHA := putBlob(t, h, "", right)
	wholeSHA := shaOf(t, whole)
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-chunks",
		Artifacts: []protocol.Artifact{{
			Kind:         protocol.KindTranscriptJSONL,
			RelPath:      "sessions/x/sid-chunks.jsonl",
			Size:         int64(len(whole)),
			SHA256:       wholeSHA,
			ChunkSHA256s: []string{leftSHA, rightSHA},
		}},
	})
	if ack.HeadSHA256 != wholeSHA {
		t.Fatalf("concat head %s", ack.HeadSHA256)
	}
	derived = readDerived(t, s, ack.SessionUID)
	if !bytes.Contains(derived, []byte("chunk-left pond")) || !bytes.Contains(derived, []byte("chunk-right pond")) {
		t.Fatalf("projection missed the concat assembly:\n%s", derived)
	}

	other := transcriptLines(
		`{"type":"meta","meta":{"id":"sid-grow","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"divergent-only pond"}],"time":"2026-09-22T16:11:00Z"}}`,
	)
	otherSHA := putBlob(t, h, "", other)
	div := postManifest(t, h, manifest("machine-a", "sid-grow", other, otherSHA, 0, otherSHA))
	if div.Relation != protocol.RelationDivergentCopy || div.HeadSHA256 != fullSHA {
		t.Fatalf("divergent ack: %+v", div)
	}
	derived = readDerived(t, s, div.SessionUID)
	if bytes.Contains(derived, []byte("divergent-only pond")) || !bytes.Contains(derived, []byte("assembled-only pond")) {
		t.Fatalf("projection followed a non-current digest:\n%s", derived)
	}
}

func TestProjectionUsesProjectLinkNotCWDHash(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	root := strings.Repeat("ab", 20)
	remotes := []string{"git@github.com:Org/Foo.git", "https://github.com/org/foo"}
	cwds := []string{"/home/a/src/foo", "/Users/b/work/foo"}
	heads := []string{strings.Repeat("cd", 20), root}
	want := protocol.ProjectLinkID(remotes[0], root)
	var uids []string
	for i, cwd := range cwds {
		body := transcriptLines(
			fmt.Sprintf(`{"type":"meta","meta":{"id":"sid-%d","cwd":%q,"started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`, i, cwd),
			`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"link pond"}],"time":"2026-09-22T16:10:01Z"}}`,
		)
		sum := putBlob(t, h, "", body)
		ack := postManifest(t, h, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       fmt.Sprintf("machine-%d", i),
			Harness:         protocol.HarnessTerva,
			NativeSessionID: fmt.Sprintf("sid-%d", i),
			Project: protocol.Project{
				CWD:       cwd,
				CWDHash:   adapter.CWDHash(cwd),
				GitRemote: remotes[i],
				GitCommit: heads[i],
				GitRoot:   root,
				ProjectID: adapter.CWDHash(cwd),
			},
			Artifacts: []protocol.Artifact{{
				Kind:    protocol.KindTranscriptJSONL,
				RelPath: fmt.Sprintf("sessions/%d.jsonl", i),
				Size:    int64(len(body)),
				SHA256:  sum,
			}},
		})
		uids = append(uids, ack.SessionUID)
		var ev struct {
			CWDHash   string  `json:"cwd_hash"`
			ProjectID *string `json:"project_id"`
		}
		line := bytes.Split(bytes.TrimSpace(readDerived(t, s, ack.SessionUID)), []byte("\n"))[0]
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatal(err)
		}
		hash := adapter.CWDHash(cwd)
		if ev.ProjectID == nil || *ev.ProjectID != want || *ev.ProjectID == hash || ev.CWDHash != hash {
			t.Fatalf("event project %v cwd_hash %s want %s hash %s", ev.ProjectID, ev.CWDHash, want, hash)
		}
	}
	if uids[0] == uids[1] {
		t.Fatal("two sessions collapsed into one uid")
	}
	linked, err := s.Catalog.SessionsByProject(t.Context(), want)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 2 || linked[0].ProjectID != want || linked[1].ProjectID != want {
		t.Fatalf("catalog link: %+v", linked)
	}
	if linked[0].Manifest.Project.CWDHash == linked[1].Manifest.Project.CWDHash {
		t.Fatal("cwd hashes matched")
	}
}

func transcriptLines(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func shaOf(t *testing.T, body []byte) string {
	t.Helper()
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func readDerived(t *testing.T, s *Server, uid string) []byte {
	t.Helper()
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(s.Normalized, uid+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func putBlob(t *testing.T, h http.Handler, sum string, body []byte) string {
	t.Helper()
	if sum == "" {
		sum = shaOf(t, body)
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

func readBlobBytes(t *testing.T, s *Server, sum string) []byte {
	t.Helper()
	f, err := s.CAS.OpenBlob(sum)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

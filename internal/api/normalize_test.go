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

func TestClaudeWorkerProjectsTranscript(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	good := transcriptLines(
		`{"type":"user","sessionId":"sid-claude","cwd":"/work/app","version":"2.1.71","gitBranch":"main","timestamp":"2026-09-22T16:10:01.477Z","future_field":{"keep":true},"message":{"role":"user","content":"claude pond"}}`,
		`{"type":"assistant","sessionId":"sid-claude","cwd":"/work/app","version":"2.1.71","timestamp":"2026-09-22T16:10:02.100Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"I'll read the file."},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_01","content":[{"type":"web_search_result","title":"Pond","url":"https://example.com/pond","encrypted_content":"gAAAAABopaque==","page_age":"2d"}]}],"usage":{"input_tokens":10,"output_tokens":4,"cache_creation_input_tokens":2,"cache_read_input_tokens":1}}}`,
		`{"type":"summary","summary":"pond session","leafUuid":"leaf-1"}`,
	)
	sidecar := []byte("not-json sidecar-secret\n")
	goodSHA := putBlob(t, h, "", good)
	tasksSHA := putBlob(t, h, "", sidecar)
	raatiSHA := putBlob(t, h, "", sidecar)
	parent := "parent-session"
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessClaude,
		HarnessVersion:  "1",
		NativeSessionID: "sid-claude",
		Project:         protocol.Project{CWD: "/work/app", CWDHash: adapter.CWDHash("/work/app")},
		Lineage:         protocol.Lineage{ParentNativeID: &parent},
		Artifacts: []protocol.Artifact{
			{Kind: protocol.KindTranscriptJSONL, RelPath: "projects/-work-app/sid-claude.jsonl", Size: int64(len(good)), SHA256: goodSHA},
			{Kind: protocol.KindTasksJSON, RelPath: "tasks/board.json", Size: int64(len(sidecar)), SHA256: tasksSHA},
			{Kind: protocol.KindRaatiJSON, RelPath: "raati/note.json", Size: int64(len(sidecar)), SHA256: raatiSHA},
		},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	derived := readDerived(t, s, ack.SessionUID)
	if !bytes.Contains(derived, []byte(`"session_id":"claude:sid-claude"`)) || !bytes.Contains(derived, []byte("claude pond")) {
		t.Fatalf("derived:\n%s", derived)
	}
	if bytes.Contains(derived, []byte("sidecar-secret")) || bytes.Contains(derived, []byte(`"harness_version":"2.1.71"`)) {
		t.Fatalf("sidecar or cli version leaked:\n%s", derived)
	}
	if !bytes.Contains(derived, []byte(`"harness_version":"1"`)) || !bytes.Contains(derived, []byte("gAAAAABopaque==")) {
		t.Fatalf("projection dropped the reader version or ciphertext:\n%s", derived)
	}
	if bytes.Contains(derived, sidecar) {
		t.Fatal("sidecar bytes were projected")
	}
	var sawPromptDay, sawIngestDay bool
	for _, line := range bytes.Split(derived, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev struct {
			RecordedAt string  `json:"recorded_at"`
			IngestedAt string  `json:"ingested_at"`
			Content    *string `json:"content_text"`
			Harness    string  `json:"harness"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Harness != protocol.HarnessClaude {
			t.Fatalf("harness %s", ev.Harness)
		}
		day, err := normalize.PartitionDate(ev.RecordedAt, ev.IngestedAt)
		if err != nil {
			t.Fatal(err)
		}
		path, err := normalize.ParquetPath(s.Parquet, day, protocol.HarnessClaude, ack.SessionUID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("parquet %s: %v", path, err)
		}
		if ev.Content != nil && *ev.Content == "claude pond" && day != "2026-09-22" {
			t.Fatalf("prompt day %s", day)
		}
		if ev.Content != nil && *ev.Content == "claude pond" {
			sawPromptDay = true
		}
		if ev.Content != nil && *ev.Content == "pond session" {
			if ev.RecordedAt != ev.IngestedAt {
				t.Fatalf("summary recorded_at %s ingested %s", ev.RecordedAt, ev.IngestedAt)
			}
			sawIngestDay = day != ""
		}
		if ev.Content != nil && strings.Contains(*ev.Content, "gAAAAABopaque==") {
			t.Fatalf("ciphertext in content_text: %s", *ev.Content)
		}
	}
	if !sawPromptDay || !sawIngestDay {
		t.Fatal("missing prompt or summary event")
	}
	if got := readBlobBytes(t, s, goodSHA); !bytes.Equal(got, good) {
		t.Fatal("transcript blob changed")
	}
	if got := readBlobBytes(t, s, tasksSHA); !bytes.Equal(got, sidecar) {
		t.Fatal("tasks blob changed")
	}

	tail := []byte("not-json sk-live-secret\n")
	full := append(append([]byte{}, good...), tail...)
	tailSHA := putBlob(t, h, "", tail)
	fullSHA := shaOf(t, full)
	grown := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessClaude,
		HarnessVersion:  "1",
		NativeSessionID: "sid-claude",
		Artifacts: []protocol.Artifact{{
			Kind:              protocol.KindTranscriptJSONL,
			RelPath:           "projects/-work-app/sid-claude.jsonl",
			Size:              int64(len(full)),
			SHA256:            fullSHA,
			ByteWatermarkPrev: int64(len(good)),
			TailSHA256:        tailSHA,
		}},
	})
	if grown.SessionUID != ack.SessionUID || grown.Relation != protocol.RelationGrownFrom {
		t.Fatalf("grown ack %+v", grown)
	}
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err = s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || !strings.Contains(msg, "not a JSON object") {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	if strings.Contains(msg, "sk-live-secret") || strings.Contains(msg, "not-json") {
		t.Fatalf("normalize_error includes the raw line: %s", msg)
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
	if got := readBlobBytes(t, s, goodSHA); !bytes.Equal(got, good) {
		t.Fatal("raw prefix changed")
	}
	if got := readBlobBytes(t, s, fullSHA); !bytes.Equal(got, full) {
		t.Fatal("failed head changed")
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

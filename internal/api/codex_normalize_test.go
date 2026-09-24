package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func TestCodexWorkerProjectsTranscript(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	good := transcriptLines(
		`{"timestamp":"2026-09-22T16:10:00.100Z","type":"session_meta","payload":{"id":"sid-codex","cwd":"/work/app","cli_version":"0.121.0","model_provider":"openai","git":{"branch":"main","commit_hash":"deadbeef"}}}`,
		`{"timestamp":"2026-09-22T16:10:00.500Z","type":"turn_context","payload":{"cwd":"/work/app","model":"gpt-5.4","approval_policy":"on-request"}}`,
		`{"timestamp":"2026-09-22T16:10:01.477Z","type":"event_msg","payload":{"type":"user_message","message":"codex pond"},"future_field":{"keep":true}}`,
		`{"timestamp":"2026-09-22T16:10:02.000Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"look at the pond"}],"encrypted_content":"gAAAAABopaque=="}}`,
		`{"timestamp":"2026-09-22T16:10:02.300Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I'll read the file."}]}}`,
		`{"type":"future_event","future_queue":true}`,
	)
	history := []byte(`{"session_id":"not-a-rollout","ts":1710000000,"text":"history-only pond"}` + "\n")
	goodSHA := putBlob(t, h, "", good)
	histSHA := putBlob(t, h, "", history)
	parent := "parent-session"
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessCodex,
		HarnessVersion:  "1",
		NativeSessionID: "sid-codex",
		Project:         protocol.Project{CWD: "/work/app", CWDHash: adapter.CWDHash("/work/app")},
		Lineage:         protocol.Lineage{ParentNativeID: &parent},
		Artifacts: []protocol.Artifact{
			{
				Kind:    protocol.KindTranscriptJSONL,
				RelPath: "sessions/2026/09/22/rollout-2026-09-22T16-10-00-sid-codex.jsonl",
				Size:    int64(len(good)),
				SHA256:  goodSHA,
			},
			{
				Kind:    protocol.KindTranscriptJSONL,
				RelPath: "sessions/2026/09/22/history.jsonl",
				Size:    int64(len(history)),
				SHA256:  histSHA,
			},
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
	if !bytes.Contains(derived, []byte(`"session_id":"codex:sid-codex"`)) || !bytes.Contains(derived, []byte("codex pond")) {
		t.Fatalf("derived:\n%s", derived)
	}
	if bytes.Contains(derived, []byte("history-only pond")) || bytes.Contains(derived, []byte(`"harness_version":"0.121.0"`)) {
		t.Fatalf("history or cli version leaked:\n%s", derived)
	}
	if !bytes.Contains(derived, []byte(`"harness_version":"1"`)) || !bytes.Contains(derived, []byte("gAAAAABopaque==")) {
		t.Fatalf("projection dropped the reader version or ciphertext:\n%s", derived)
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
		if ev.Harness != protocol.HarnessCodex {
			t.Fatalf("harness %s", ev.Harness)
		}
		day, err := normalize.PartitionDate(ev.RecordedAt, ev.IngestedAt)
		if err != nil {
			t.Fatal(err)
		}
		path, err := normalize.ParquetPath(s.Parquet, day, protocol.HarnessCodex, ack.SessionUID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("parquet %s: %v", path, err)
		}
		if ev.Content != nil && *ev.Content == "codex pond" {
			if day != "2026-09-22" {
				t.Fatalf("prompt day %s", day)
			}
			sawPromptDay = true
		}
		if ev.Content == nil && ev.RecordedAt == ev.IngestedAt {
			sawIngestDay = day != ""
		}
		if ev.Content != nil && strings.Contains(*ev.Content, "gAAAAABopaque==") {
			t.Fatalf("ciphertext in content_text: %s", *ev.Content)
		}
	}
	if !sawPromptDay || !sawIngestDay {
		t.Fatal("missing prompt or timestamp-less event")
	}
	if got := readBlobBytes(t, s, goodSHA); !bytes.Equal(got, good) {
		t.Fatal("transcript blob changed")
	}
	if got := readBlobBytes(t, s, histSHA); !bytes.Equal(got, history) {
		t.Fatal("history blob changed")
	}

	tail := []byte("not-json sk-live-secret\n")
	full := append(append([]byte{}, good...), tail...)
	tailSHA := putBlob(t, h, "", tail)
	fullSHA := shaOf(t, full)
	grown := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessCodex,
		HarnessVersion:  "1",
		NativeSessionID: "sid-codex",
		Artifacts: []protocol.Artifact{{
			Kind:              protocol.KindTranscriptJSONL,
			RelPath:           "sessions/2026/09/22/rollout-2026-09-22T16-10-00-sid-codex.jsonl",
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
	if got := readBlobBytes(t, s, histSHA); !bytes.Equal(got, history) {
		t.Fatal("history blob changed after failure")
	}
}

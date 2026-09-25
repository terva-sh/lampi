package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/opencode"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func TestOpenCodeWorkerProjectsExport(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	userAt := time.Date(2026, 9, 22, 16, 10, 1, 477000000, time.UTC).UnixMilli()
	at := strconv.FormatInt(userAt, 10)
	good := []byte(`{"info":{"id":"from-info","directory":"/work/app","version":"1.2.3","title":"pond session","parentID":"parent-session","time":{"created":` + at + `},"future_field":{"keep":true}},"messages":[{"info":{"id":"msg_user","role":"user","time":{"created":` + at + `}},"parts":[{"type":"text","text":"opencode pond"}]},{"info":{"id":"msg_asst","role":"assistant","parentID":"msg_user","modelID":"claude-sonnet-4-6","providerID":"anthropic","time":{"created":` + strconv.FormatInt(userAt+1000, 10) + `}},"parts":[{"type":"reasoning","text":"look at the pond","metadata":{"encrypted_content":"gAAAAABopaque=="}},{"type":"text","text":"I'll read the file."},{"type":"tool","callID":"call_1","tool":"read","state":{"status":"completed","input":{"file_path":"main.go"},"output":"package main","title":"Read main.go"}}]}]}`)
	goodSHA := putBlob(t, h, "", good)
	parent := "parent-session"
	// An agent older than opencode_export_json labelled the export
	// transcript_jsonl. The worker still projects it.
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessOpenCode,
		HarnessVersion:  opencode.Version,
		NativeSessionID: "sid-opencode",
		Project:         protocol.Project{CWD: "/work/app", CWDHash: adapter.CWDHash("/work/app")},
		Lineage:         protocol.Lineage{ParentNativeID: &parent},
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "export/sid-opencode.json",
			Size:    int64(len(good)),
			SHA256:  goodSHA,
		}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	derived := readDerived(t, s, ack.SessionUID)
	if !bytes.Contains(derived, []byte(`"session_id":"opencode:sid-opencode"`)) || !bytes.Contains(derived, []byte("opencode pond")) {
		t.Fatalf("derived:\n%s", derived)
	}
	if bytes.Contains(derived, []byte(`"session_id":"opencode:from-info"`)) || bytes.Contains(derived, []byte(`"harness_version":"1.2.3"`)) {
		t.Fatalf("info id or cli version leaked:\n%s", derived)
	}
	if !bytes.Contains(derived, []byte(`"harness_version":"`+opencode.Version+`"`)) || !bytes.Contains(derived, []byte("gAAAAABopaque==")) {
		t.Fatalf("projection dropped the reader version or ciphertext:\n%s", derived)
	}
	if !bytes.Contains(derived, []byte(`"content_text":"package main"`)) || !bytes.Contains(derived, []byte(`"name":"read"`)) {
		t.Fatalf("tool result missing:\n%s", derived)
	}
	var sawPrompt bool
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
		if ev.Harness != protocol.HarnessOpenCode {
			t.Fatalf("harness %s", ev.Harness)
		}
		day, err := normalize.PartitionDate(ev.RecordedAt, ev.IngestedAt)
		if err != nil {
			t.Fatal(err)
		}
		path, err := normalize.ParquetPath(s.Parquet, day, protocol.HarnessOpenCode, ack.SessionUID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("parquet %s: %v", path, err)
		}
		if ev.Content != nil && *ev.Content == "opencode pond" {
			if day != "2026-09-22" {
				t.Fatalf("prompt day %s", day)
			}
			sawPrompt = true
		}
		if ev.Content != nil && strings.Contains(*ev.Content, "gAAAAABopaque==") {
			t.Fatalf("ciphertext in content_text: %s", *ev.Content)
		}
	}
	if !sawPrompt {
		t.Fatal("missing prompt event")
	}
	promptPath, err := normalize.ParquetPath(s.Parquet, "2026-09-22", protocol.HarnessOpenCode, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(promptPath); err != nil {
		t.Fatalf("parquet %s: %v", promptPath, err)
	}
	if got := readBlobBytes(t, s, goodSHA); !bytes.Equal(got, good) {
		t.Fatal("transcript blob changed")
	}

	tail := []byte("\nnot-json sk-live-secret\n")
	full := append(append([]byte{}, good...), tail...)
	tailSHA := putBlob(t, h, "", tail)
	fullSHA := shaOf(t, full)
	grown := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessOpenCode,
		HarnessVersion:  opencode.Version,
		NativeSessionID: "sid-opencode",
		Artifacts: []protocol.Artifact{{
			Kind:              protocol.KindTranscriptJSONL,
			RelPath:           "export/sid-opencode.json",
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

func TestOpenCodeWorkerSQLiteSetsError(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	body := append([]byte("SQLite format 3\x00"), []byte("sk-live-secret")...)
	sum := putBlob(t, h, "", body)
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessOpenCode,
		HarnessVersion:  opencode.Version,
		NativeSessionID: "opencode.db",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "opencode.db",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || !strings.Contains(msg, "not a JSON object") {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	if strings.Contains(msg, "sk-live-secret") || strings.Contains(msg, "SQLite format") {
		t.Fatalf("normalize_error includes the database: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(s.Normalized, ack.SessionUID+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("derived file after sqlite: %v", err)
	}
	parts, err := normalize.SessionParquet(s.Parquet, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 {
		t.Fatalf("parquet after sqlite: %v", parts)
	}
	if got := readBlobBytes(t, s, sum); !bytes.Equal(got, body) {
		t.Fatal("database blob changed")
	}
}

package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func TestCursorWorkerProjectsStateJSON(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	const cipher = "gAAAAABopaque=="
	body := cursorExport(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "User/workspaceStorage/ws1/state.vscdb",
		"scope":           "workspace",
		"item_table":      []any{},
		"cursor_disk_kv": []any{
			map[string]any{"key": "bubbleId:c1:user", "value": map[string]any{
				"type":         1,
				"rawText":      "cursor pond",
				"createdAt":    "2026-09-22T16:10:01Z",
				"future_field": map[string]any{"keep": true},
			}},
			map[string]any{"key": "bubbleId:c1:assistant", "value": map[string]any{
				"type":              2,
				"text":              "cursor reply",
				"encrypted_content": cipher,
				"tokenCount":        4,
			}},
			map[string]any{"key": "composerData:c1", "value": map[string]any{
				"fullConversationHeadersOnly": []any{
					map[string]any{"bubbleId": "user"},
					map[string]any{"bubbleId": "assistant"},
				},
			}},
		},
	})
	sum := putBlob(t, h, "", body)
	ack := postManifest(t, h, cursorManifest("workspace/ws1", protocol.KindCursorStateJSON, "User/workspaceStorage/ws1/state.json", body, sum))
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	derived := readDerived(t, s, ack.SessionUID)
	if !bytes.Contains(derived, []byte(`"session_id":"cursor:workspace/ws1"`)) || !bytes.Contains(derived, []byte(`"harness":"cursor"`)) || !bytes.Contains(derived, []byte(`"schema_version":1`)) {
		t.Fatalf("derived:\n%s", derived)
	}
	if !bytes.Contains(derived, []byte("cursor pond")) || !bytes.Contains(derived, []byte(cipher)) {
		t.Fatalf("projection dropped the turn or the ciphertext:\n%s", derived)
	}
	var sawPrompt, sawUsage bool
	for _, line := range bytes.Split(derived, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev struct {
			EventType  string         `json:"event_type"`
			RecordedAt string         `json:"recorded_at"`
			IngestedAt string         `json:"ingested_at"`
			Content    *string        `json:"content_text"`
			Harness    string         `json:"harness"`
			Schema     int            `json:"schema_version"`
			SessionID  string         `json:"session_id"`
			Extra      map[string]any `json:"extra"`
			Usage      struct {
				Input  *int `json:"input"`
				Output *int `json:"output"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Harness != protocol.HarnessCursor || ev.Schema != 1 || ev.SessionID != "cursor:workspace/ws1" {
			t.Fatalf("header %+v", ev)
		}
		day, err := normalize.PartitionDate(ev.RecordedAt, ev.IngestedAt)
		if err != nil {
			t.Fatal(err)
		}
		path, err := normalize.ParquetPath(s.Parquet, day, protocol.HarnessCursor, ack.SessionUID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("parquet %s: %v", path, err)
		}
		if !strings.Contains(path, "harness="+protocol.HarnessCursor) {
			t.Fatalf("parquet path %s", path)
		}
		if ev.Content != nil && strings.Contains(*ev.Content, cipher) {
			t.Fatalf("ciphertext in content_text: %s", *ev.Content)
		}
		if ev.Content != nil && *ev.Content == "cursor pond" {
			if day != "2026-09-22" {
				t.Fatalf("prompt day %s", day)
			}
			sawPrompt = true
		}
		if ev.Content != nil && *ev.Content == "cursor reply" {
			if _, stuffed := ev.Extra["tokenCount"]; stuffed {
				t.Fatalf("tokenCount stuffed into the message: %#v", ev.Extra)
			}
		}
		if ev.EventType == normalize.EventUsage {
			if ev.Content != nil || ev.Extra["tokenCount"] != float64(4) || ev.Usage.Input != nil || ev.Usage.Output != nil {
				t.Fatalf("usage event %+v", ev)
			}
			sawUsage = true
		}
		switch ev.EventType {
		case normalize.EventToolCall, normalize.EventToolResult:
			t.Fatalf("promoted %s", ev.EventType)
		}
	}
	if !sawPrompt {
		t.Fatal("missing prompt event")
	}
	if !sawUsage {
		t.Fatal("missing usage event")
	}
	if got := readBlobBytes(t, s, sum); !bytes.Equal(got, body) {
		t.Fatal("CAS object changed")
	}
}

func TestCursorKindSkipIsNormalizeError(t *testing.T) {
	const secret = "sk-cursor-kind-secret"
	cases := []struct {
		kind string
		rel  string
		body []byte
	}{
		{protocol.KindTranscriptJSONL, "export/sid.jsonl", []byte("{\"message\":\"" + secret + "\"}\n")},
		{protocol.KindCursorCLIStoreJSON, "chats/ab/store.json", []byte(`{"blobs":"` + secret + `"}`)},
		{protocol.KindErrorsJSONL, "errors.jsonl", []byte("{\"err\":\"" + secret + "\"}\n")},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			s.Allow("sekret")
			h := s.Handler()
			sum := putBlob(t, h, "", tc.body)
			ack := postManifest(t, h, cursorManifest("workspace/"+tc.kind, tc.kind, tc.rel, tc.body, sum))
			if err := s.WaitNormalized(t.Context()); err != nil {
				t.Fatal(err)
			}
			msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
			if err != nil || !ok || !strings.Contains(msg, "no cursor_state_json artifact") {
				t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
			}
			if strings.Contains(msg, secret) || strings.Contains(msg, string(tc.body)) {
				t.Fatalf("error includes the body: %s", msg)
			}
			if _, err := os.Stat(filepath.Join(s.Normalized, ack.SessionUID+".jsonl")); !os.IsNotExist(err) {
				t.Fatalf("derived file: %v", err)
			}
			parts, err := normalize.SessionParquet(s.Parquet, ack.SessionUID)
			if err != nil {
				t.Fatal(err)
			}
			if len(parts) != 0 {
				t.Fatalf("parquet: %v", parts)
			}
			if got := readBlobBytes(t, s, sum); !bytes.Equal(got, tc.body) {
				t.Fatal("CAS object changed")
			}
		})
	}
}

func TestCursorWorkerTranscriptJSONLSetsError(t *testing.T) {
	const secret = "sk-cursor-transcript-only"
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	body := []byte("{\"type\":\"user\",\"message\":\"" + secret + "\"}\n")
	sum := putBlob(t, h, "", body)
	ack := postManifest(t, h, cursorManifest("workspace/ws-jsonl", protocol.KindTranscriptJSONL, "projects/x/sid.jsonl", body, sum))
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || !strings.Contains(msg, "no cursor_state_json artifact") {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	if strings.Contains(msg, secret) {
		t.Fatalf("error includes the body: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(s.Normalized, ack.SessionUID+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("derived file: %v", err)
	}
	parts, err := normalize.SessionParquet(s.Parquet, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 {
		t.Fatalf("parquet: %v", parts)
	}
	if got := readBlobBytes(t, s, sum); !bytes.Equal(got, body) {
		t.Fatal("CAS object changed")
	}
}

func TestCursorWorkerCorruptHeadDropsDerived(t *testing.T) {
	const secret = "sk-cursor-corrupt-head"
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	good := cursorExport(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "state.vscdb",
		"scope":           "workspace",
		"item_table": []any{
			map[string]any{"key": "composer.composerData", "value": map[string]any{"note": "cursor pond"}},
		},
	})
	goodSum := putBlob(t, h, "", good)
	ack := postManifest(t, h, cursorManifest("workspace/ws-corrupt", protocol.KindCursorStateJSON, "User/workspaceStorage/ws-corrupt/state.json", good, goodSum))
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID); err != nil || !ok || msg != "" {
		t.Fatalf("index-only normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	assertCursorIndexOnly(t, readDerived(t, s, ack.SessionUID))

	bad := []byte(`{"item_table":[{"key":"x","value":"` + secret)
	badSum := putBlob(t, h, "", bad)
	again := postManifest(t, h, cursorManifest("workspace/ws-corrupt", protocol.KindCursorStateJSON, "User/workspaceStorage/ws-corrupt/state.json", bad, badSum))
	if again.SessionUID != ack.SessionUID {
		t.Fatalf("session changed %s %s", ack.SessionUID, again.SessionUID)
	}
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || msg == "" {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	if strings.Contains(msg, secret) || strings.Contains(msg, "item_table") {
		t.Fatalf("error includes the body: %s", msg)
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
	if got := readBlobBytes(t, s, goodSum); !bytes.Equal(got, good) {
		t.Fatal("previous CAS object changed")
	}
	if got := readBlobBytes(t, s, badSum); !bytes.Equal(got, bad) {
		t.Fatal("failed head changed")
	}
}

func assertCursorIndexOnly(t *testing.T, derived []byte) {
	t.Helper()
	var n int
	for _, line := range bytes.Split(derived, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		n++
		var ev struct {
			EventType string  `json:"event_type"`
			Content   *string `json:"content_text"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.EventType == normalize.EventMessage || (ev.Content != nil && *ev.Content != "") {
			t.Fatalf("invented message: %s", line)
		}
		if ev.EventType != normalize.EventMeta && ev.EventType != normalize.EventUnknown {
			t.Fatalf("index-only event %s", ev.EventType)
		}
	}
	if n == 0 {
		t.Fatal("index-only export produced no events")
	}
}

func cursorManifest(native, kind, rel string, body []byte, sum string) protocol.Manifest {
	return protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessCursor,
		HarnessVersion:  "1",
		NativeSessionID: native,
		Project:         protocol.Project{CWD: "/work/app"},
		Artifacts: []protocol.Artifact{{
			Kind:    kind,
			RelPath: rel,
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	}
}

func cursorExport(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

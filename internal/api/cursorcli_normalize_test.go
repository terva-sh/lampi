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

func TestCursorCLIWorkerProjectsStoreJSON(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	const cipher = "gAAAAABopaque=="
	body := cursorCLIExport(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/sid-1/store.db",
		"scope":           "session",
		"meta": []any{
			map[string]any{"key": "0", "value": map[string]any{
				"name":      "kept-title",
				"createdAt": "2026-09-22T16:00:00Z",
			}},
		},
		"blobs": []any{
			map[string]any{"id": "user", "data": map[string]any{
				"role":         "user",
				"content":      "cursor cli pond",
				"createdAt":    "2026-09-22T16:10:01Z",
				"future_field": map[string]any{"keep": true},
			}},
			map[string]any{"id": "assistant", "data": map[string]any{
				"role":              "assistant",
				"text":              "cursor cli reply",
				"encrypted_content": cipher,
				"usage":             map[string]any{"input": 4},
			}},
			map[string]any{"id": "mystery", "data": map[string]any{"keep": "unknown-marker"}},
		},
	})
	sum := putBlob(t, h, "", body)
	ack := postManifest(t, h, cursorCLIManifest("chats/ab12/sid-1", protocol.KindCursorCLIStoreJSON, "chats/ab12/sid-1/store.json", body, sum))
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	derived := readDerived(t, s, ack.SessionUID)
	if !bytes.Contains(derived, []byte(`"session_id":"cursor-cli:chats/ab12/sid-1"`)) || !bytes.Contains(derived, []byte(`"harness":"cursor-cli"`)) || !bytes.Contains(derived, []byte(`"schema_version":1`)) {
		t.Fatalf("derived:\n%s", derived)
	}
	if !bytes.Contains(derived, []byte("cursor cli pond")) || !bytes.Contains(derived, []byte(cipher)) {
		t.Fatalf("projection dropped the turn or the ciphertext:\n%s", derived)
	}
	var sawPrompt bool
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
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Harness != protocol.HarnessCursorCLI || ev.Schema != 1 || ev.SessionID != "cursor-cli:chats/ab12/sid-1" {
			t.Fatalf("header %+v", ev)
		}
		day, err := normalize.PartitionDate(ev.RecordedAt, ev.IngestedAt)
		if err != nil {
			t.Fatal(err)
		}
		path, err := normalize.ParquetPath(s.Parquet, day, protocol.HarnessCursorCLI, ack.SessionUID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("parquet %s: %v", path, err)
		}
		if !strings.Contains(path, "harness="+protocol.HarnessCursorCLI) {
			t.Fatalf("parquet path %s", path)
		}
		if ev.Content != nil && strings.Contains(*ev.Content, cipher) {
			t.Fatalf("ciphertext in content_text: %s", *ev.Content)
		}
		if ev.Content != nil && *ev.Content == "cursor cli pond" {
			if day != "2026-09-22" {
				t.Fatalf("prompt day %s", day)
			}
			sawPrompt = true
		}
		if ev.Content != nil && *ev.Content == "cursor cli reply" {
			usage, _ := ev.Extra["usage"].(map[string]any)
			if ev.Extra["encrypted_content"] != cipher || usage["input"] != float64(4) {
				t.Fatalf("reply extra %#v", ev.Extra)
			}
		}
		switch ev.EventType {
		case normalize.EventUsage, normalize.EventToolCall, normalize.EventToolResult, normalize.EventCompaction:
			t.Fatalf("promoted %s", ev.EventType)
		}
	}
	if !sawPrompt {
		t.Fatal("missing prompt event")
	}
	if got := readBlobBytes(t, s, sum); !bytes.Equal(got, body) {
		t.Fatal("CAS object changed")
	}
}

func TestCursorCLIKindSkipIsNormalizeError(t *testing.T) {
	const secret = "sk-cursor-cli-kind-secret"
	cases := []struct {
		kind string
		rel  string
		body []byte
	}{
		{protocol.KindTranscriptJSONL, "export/sid.jsonl", []byte("{\"message\":\"" + secret + "\"}\n")},
		{protocol.KindCursorStateJSON, "User/workspaceStorage/ws/state.json", []byte(`{"item_table":[{"key":"x","value":"` + secret + `"}]}`)},
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
			ack := postManifest(t, h, cursorCLIManifest("chats/ab12/"+tc.kind, tc.kind, tc.rel, tc.body, sum))
			if err := s.WaitNormalized(t.Context()); err != nil {
				t.Fatal(err)
			}
			msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
			if err != nil || !ok || !strings.Contains(msg, "no cursor_cli_store_json artifact") {
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

func TestCursorCLIEmptyTurnsClearError(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	body := cursorCLIExport(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/empty/store.db",
		"scope":           "session",
		"meta": []any{
			map[string]any{"key": "0", "value": map[string]any{"name": "quiet-title"}},
		},
		"blobs": []any{
			map[string]any{"id": "mystery", "data": map[string]any{"keep": "unknown-marker"}},
		},
	})
	sum := putBlob(t, h, "", body)
	ack := postManifest(t, h, cursorCLIManifest("chats/ab12/empty", protocol.KindCursorCLIStoreJSON, "chats/ab12/empty/store.json", body, sum))
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	derived := readDerived(t, s, ack.SessionUID)
	if !bytes.Contains(derived, []byte(`"raw_type":"cursor_cli_store_json"`)) {
		t.Fatalf("missing envelope:\n%s", derived)
	}
	for _, line := range bytes.Split(derived, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
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
	}
	parts, err := normalize.SessionParquet(s.Parquet, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) == 0 {
		t.Fatal("empty conversation wrote no parquet")
	}
	for _, path := range parts {
		if !strings.Contains(path, "harness="+protocol.HarnessCursorCLI) {
			t.Fatalf("parquet %s", path)
		}
	}
	if got := readBlobBytes(t, s, sum); !bytes.Equal(got, body) {
		t.Fatal("CAS object changed")
	}
}

func TestCursorCLIWrongShapeSetsError(t *testing.T) {
	const secret = "sk-cursor-cli-state-body"
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	body := []byte(`{"harness_version":"1","scope":"workspace","item_table":[{"key":"bubbleId:c:b","value":{"rawText":"` + secret + `"}}]}`)
	sum := putBlob(t, h, "", body)
	ack := postManifest(t, h, cursorCLIManifest("chats/ab12/wrong-shape", protocol.KindCursorCLIStoreJSON, "chats/ab12/wrong-shape/store.json", body, sum))
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

func TestCursorCLIWorkerCorruptHeadDropsDerived(t *testing.T) {
	const secret = "sk-cursor-cli-corrupt-head"
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	good := cursorCLIExport(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/sid-corrupt/store.db",
		"scope":           "session",
		"meta": []any{
			map[string]any{"key": "0", "value": map[string]any{"name": "cursor cli pond"}},
		},
		"blobs": []any{},
	})
	goodSum := putBlob(t, h, "", good)
	ack := postManifest(t, h, cursorCLIManifest("chats/ab12/sid-corrupt", protocol.KindCursorCLIStoreJSON, "chats/ab12/sid-corrupt/store.json", good, goodSum))
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID); err != nil || !ok || msg != "" {
		t.Fatalf("quiet normalize_error %q ok=%v err=%v", msg, ok, err)
	}

	bad := []byte(`{"meta":[{"key":"0","value":"` + secret)
	badSum := putBlob(t, h, "", bad)
	again := postManifest(t, h, cursorCLIManifest("chats/ab12/sid-corrupt", protocol.KindCursorCLIStoreJSON, "chats/ab12/sid-corrupt/store.json", bad, badSum))
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
	if strings.Contains(msg, secret) || strings.Contains(msg, "meta") {
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

func cursorCLIManifest(native, kind, rel string, body []byte, sum string) protocol.Manifest {
	return protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessCursorCLI,
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

func cursorCLIExport(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

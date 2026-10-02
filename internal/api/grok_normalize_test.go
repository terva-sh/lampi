package api

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter/grok"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func TestGrokProjectAndShareGPT(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	const native = "018f1a2b-3c4d-7e5f-8a9b-0c1d2e3f4a5b"
	dir := "sessions/%2Fwork%2Fdemo/" + native
	body := transcriptLines(
		`{"method":"session/update","params":{"sessionId":"`+native+`","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hello "},"_meta":{"promptIndex":1,"modelId":"grok-build"}},"_meta":{"agentTimestampMs":1784388059000}}}`,
		`{"method":"session/update","params":{"sessionId":"`+native+`","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"pond"},"_meta":{"promptIndex":1}}}}`,
		`{"method":"x.ai/fs_notify","params":{"path":"extension-only-path"}}`,
		`{"method":"session/update","params":{"sessionId":"`+native+`","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"I will read."}}}}`,
		`{"method":"session/update","params":{"sessionId":"`+native+`","update":{"sessionUpdate":"tool_call","toolCallId":"call-1","title":"Read","kind":"read","status":"pending","rawInput":{"path":"main.go"}}}}`,
		`{"method":"session/update","params":{"sessionId":"`+native+`","update":{"sessionUpdate":"tool_call_update","toolCallId":"call-1","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":"still-running"}}]}}}`,
		`{"method":"session/update","params":{"sessionId":"`+native+`","update":{"sessionUpdate":"tool_call_update","toolCallId":"call-1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"package main"}}]}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"turn_completed"}}}`,
	)
	summary := transcriptLines(
		`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"SUMMARY-NOT-THE-STREAM"}}},"generated_title":"SUMMARY-NOT-THE-STREAM","info":{"cwd":"/work/demo"}}`,
	)
	history := transcriptLines(
		`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"CHAT-HISTORY-NOT-TRANSCRIPT"}}}}`,
	)
	sum := putBlob(t, h, "", body)
	summarySum := putBlob(t, h, "", summary)
	historySum := putBlob(t, h, "", history)
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessGrok,
		HarnessVersion:  grok.Version,
		NativeSessionID: native,
		Project:         protocol.Project{CWD: "/work/demo"},
		Artifacts: []protocol.Artifact{
			{Kind: protocol.KindTranscriptJSONL, RelPath: dir + "/updates.jsonl", Size: int64(len(body)), SHA256: sum},
			{Kind: protocol.KindSummaryJSON, RelPath: dir + "/summary.json", Size: int64(len(summary)), SHA256: summarySum},
			{Kind: protocol.KindTranscriptJSONL, RelPath: dir + "/chat_history.jsonl", Size: int64(len(history)), SHA256: historySum},
		},
	}
	ack := postManifest(t, h, m)
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	if grok.Version != "1" {
		t.Fatalf("version pin %q", grok.Version)
	}

	events, err := s.Project(t.Context(), m)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("projected %d events", len(events))
	}
	for _, ev := range events {
		if ev.SchemaVersion != 1 || ev.Harness != protocol.HarnessGrok || ev.SessionID != "grok:"+native {
			t.Fatalf("event %+v", ev)
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != "1" {
			t.Fatalf("harness version %v", ev.HarnessVersion)
		}
		blob, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(blob, []byte(`"confidence"`)) || bytes.Contains(blob, []byte("SUMMARY-NOT-THE-STREAM")) || bytes.Contains(blob, []byte("CHAT-HISTORY-NOT-TRANSCRIPT")) || bytes.Contains(blob, []byte("extension-only-path")) || bytes.Contains(blob, []byte("still-running")) {
			t.Fatalf("event leaked a skipped source: %s", blob)
		}
	}
	if events[0].EventType != normalize.EventMessage || events[0].ContentText == nil || *events[0].ContentText != "hello pond" {
		t.Fatalf("user message %+v", events[0])
	}
	if events[2].EventType != normalize.EventToolCall || events[2].Tool.Name == nil || *events[2].Tool.Name != "Read" || events[2].Tool.CallID == nil || *events[2].Tool.CallID != "call-1" {
		t.Fatalf("tool call %+v", events[2])
	}
	if events[3].EventType != normalize.EventToolResult || events[3].ContentText == nil || *events[3].ContentText != "package main" || events[3].Tool.IsError == nil || *events[3].Tool.IsError {
		t.Fatalf("tool result %+v", events[3])
	}

	derived := readDerived(t, s, ack.SessionUID)
	if !bytes.Contains(derived, []byte(`"session_id":"grok:`+native+`"`)) || !bytes.Contains(derived, []byte(`"schema_version":1`)) || !bytes.Contains(derived, []byte(`"harness_version":"1"`)) {
		t.Fatalf("derived:\n%s", derived)
	}
	if bytes.Contains(derived, []byte("SUMMARY-NOT-THE-STREAM")) || bytes.Contains(derived, []byte("CHAT-HISTORY-NOT-TRANSCRIPT")) {
		t.Fatalf("derived read summary or chat history:\n%s", derived)
	}

	rec, ok := normalize.ShareGPT(ack.SessionUID, sum, events)
	if !ok {
		t.Fatal("sharegpt dropped the fixture")
	}
	if rec.SessionUID != ack.SessionUID || rec.SessionID != "grok:"+native || rec.RawSHA256 != sum {
		t.Fatalf("lineage %+v", rec)
	}
	var sawHuman, sawCall, sawResult bool
	for _, turn := range rec.Conversations {
		if strings.Contains(turn.Value, "SUMMARY-NOT-THE-STREAM") || strings.Contains(turn.Value, "CHAT-HISTORY-NOT-TRANSCRIPT") || strings.Contains(turn.Value, "still-running") || strings.Contains(turn.Value, "extension-only-path") {
			t.Fatalf("training turn kept a skipped source: %+v", turn)
		}
		if turn.From == "human" && turn.Value == "hello pond" {
			sawHuman = true
		}
		if turn.Name == "Read" && turn.CallID == "call-1" {
			sawCall = true
		}
		if turn.From == "tool" && turn.CallID == "call-1" && turn.Value == "package main" {
			sawResult = true
		}
	}
	if !sawHuman || !sawCall || !sawResult {
		t.Fatalf("turns %+v", rec.Conversations)
	}
}

package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/grok"
	"terva.sh/lampi/internal/protocol"
)

const grokNative = "018f1a2b-3c4d-7e5f-8a9b-0c1d2e3f4a5b"

func TestGrokNormalizerPinsVersionWithoutConfidence(t *testing.T) {
	if grok.Version != "1" {
		t.Fatalf("adapter version %q", grok.Version)
	}
	if _, ok := reflect.TypeOf(Grok{}).FieldByName("Confidence"); ok {
		t.Fatal("JSONL normalizer has a Confidence field")
	}
}

func TestGrokCoalescesChunksAndTools(t *testing.T) {
	if grok.Version != "1" {
		t.Fatalf("adapter version %q", grok.Version)
	}
	raw := grokACPFixture(t)
	before := append([]byte(nil), raw...)
	now := time.Date(2026, 10, 2, 19, 0, 0, 0, time.UTC)
	digest := strings.Repeat("ab", 32)
	events, err := (Grok{
		Now:            now,
		NativeID:       grokNative,
		ParentNativeID: "parent-session",
		HarnessVersion: grok.Version,
		ProjectID:      "github.com/org/app@abc",
		CWD:            "/work/demo",
		GitCommit:      "abc123",
		Digest:         digest,
	}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("normalize changed the raw bytes")
	}
	wantTypes := []string{
		EventMessage, EventMessage, EventMessage,
		EventToolCall, EventToolResult,
		EventToolCall, EventToolResult,
		EventMessage, EventError,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("events %d, want %d", len(events), len(wantTypes))
	}
	seen := map[string]bool{}
	for i, ev := range events {
		if ev.EventType != wantTypes[i] {
			t.Fatalf("event %d type %s, want %s", i, ev.EventType, wantTypes[i])
		}
		if ev.SchemaVersion != SchemaVersion || ev.Harness != protocol.HarnessGrok {
			t.Fatalf("header %+v", ev)
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != grok.Version {
			t.Fatalf("harness version %v", ev.HarnessVersion)
		}
		if ev.SessionID != "grok:"+grokNative {
			t.Fatalf("session id %s", ev.SessionID)
		}
		if ev.ParentSessionID == nil || *ev.ParentSessionID != "grok:parent-session" {
			t.Fatalf("parent %v", ev.ParentSessionID)
		}
		if ev.CWDHash != adapter.CWDHash("/work/demo") {
			t.Fatalf("cwd hash %s", ev.CWDHash)
		}
		if ev.ProjectID == nil || *ev.ProjectID != "github.com/org/app@abc" {
			t.Fatalf("project %v", ev.ProjectID)
		}
		if ev.EventType != EventError && (ev.Model.ID == nil || *ev.Model.ID != "grok-build") {
			t.Fatalf("model %v on %s", ev.Model.ID, ev.EventType)
		}
		if ev.EventID == "" || seen[ev.EventID] {
			t.Fatalf("event id %q", ev.EventID)
		}
		seen[ev.EventID] = true
		blob, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(blob, []byte(`"confidence"`)) {
			t.Fatalf("confidence on event %d: %s", i, blob)
		}
		if ev.EventType == EventUsage {
			t.Fatalf("usage was promoted: %+v", ev)
		}
	}

	user := events[0]
	if user.RawType != "user_message_chunk" || user.Actor != ActorUser || textOf(user) != "hello pond" {
		t.Fatalf("user message %+v text %q", user.RawType, textOf(user))
	}
	wantWhen := time.UnixMilli(1784388059000).UTC().Format(time.RFC3339Nano)
	if user.RecordedAt != wantWhen {
		t.Fatalf("recorded_at %s, want %s", user.RecordedAt, wantWhen)
	}
	if user.ContentRef == nil || *user.ContentRef != "sha256/"+digest+"#0" {
		t.Fatalf("content_ref %v", user.ContentRef)
	}

	thought := events[1]
	if thought.RawType != "agent_thought_chunk" || thought.Actor != ActorAssistant || textOf(thought) != "think hard" {
		t.Fatalf("thought %+v %q", thought.RawType, textOf(thought))
	}
	agent := events[2]
	if agent.RawType != "agent_message_chunk" || textOf(agent) != "I will read." {
		t.Fatalf("agent %q", textOf(agent))
	}

	call := events[3]
	if call.Tool.Name == nil || *call.Tool.Name != "Read" || call.Tool.CallID == nil || *call.Tool.CallID != "call-1" {
		t.Fatalf("tool call %+v", call.Tool)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(textOf(call)), &args); err != nil || args["path"] != "main.go" {
		t.Fatalf("tool arguments %q err %v", textOf(call), err)
	}
	result := events[4]
	if result.RawType != "tool_call_update" || result.Actor != ActorTool || textOf(result) != "package main" {
		t.Fatalf("tool result %q actor %s", textOf(result), result.Actor)
	}
	if result.Tool.CallID == nil || *result.Tool.CallID != "call-1" || result.Tool.IsError == nil || *result.Tool.IsError {
		t.Fatalf("completed result %+v", result.Tool)
	}

	failed := events[6]
	if textOf(failed) != "boom" || failed.Tool.IsError == nil || !*failed.Tool.IsError || failed.Tool.CallID == nil || *failed.Tool.CallID != "call-2" {
		t.Fatalf("failed result %+v %q", failed.Tool, textOf(failed))
	}
	if events[5].Tool.Name == nil || *events[5].Tool.Name != "Bash" {
		t.Fatalf("bash call %+v", events[5].Tool)
	}
	if textOf(events[7]) != "again" || events[7].RawType != "user_message_chunk" {
		t.Fatalf("second prompt %q", textOf(events[7]))
	}
	if events[8].Extra["unreadable_line"] == nil || strings.Contains(textOf(events[8]), "sk-live-not-json") {
		t.Fatalf("unreadable marker %+v %q", events[8].Extra, textOf(events[8]))
	}

	for _, ev := range events {
		for _, banned := range []string{"extension-only-path", "still-running", "do-not-project-plan", "usage-marker", "sk-live-not-json"} {
			if strings.Contains(textOf(ev), banned) {
				t.Fatalf("event kept %s: %q", banned, textOf(ev))
			}
		}
	}

	rec, ok := ShareGPT("uid-1", digest, events)
	if !ok {
		t.Fatal("sharegpt dropped the fixture")
	}
	if rec.SessionID != "grok:"+grokNative || rec.RawSHA256 != digest {
		t.Fatalf("lineage %+v", rec)
	}
	var sawHuman, sawReply, sawCall, sawResult bool
	for _, turn := range rec.Conversations {
		if strings.Contains(turn.Value, "extension-only-path") || strings.Contains(turn.Value, "still-running") || strings.Contains(turn.Value, "sk-live-not-json") || strings.Contains(turn.Value, "usage-marker") || strings.Contains(turn.Value, "do-not-project-plan") {
			t.Fatalf("training turn kept a skipped line: %+v", turn)
		}
		if turn.From == "human" && turn.Value == "hello pond" {
			sawHuman = true
		}
		if turn.From == "gpt" && turn.Value == "I will read." {
			sawReply = true
		}
		if turn.From == "gpt" && turn.Name == "Read" && turn.CallID == "call-1" {
			sawCall = true
		}
		if turn.From == "tool" && turn.CallID == "call-1" && turn.Value == "package main" {
			sawResult = true
		}
	}
	if !sawHuman || !sawReply || !sawCall || !sawResult {
		t.Fatalf("turns %+v", rec.Conversations)
	}
}

func TestGrokUnknownLinesDoNotAbort(t *testing.T) {
	raw := []byte(strings.Join([]string{
		`{"method":"x.ai/fs_notify","params":{"path":"extension-only-path"}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"turn_completed","stopReason":"end_turn"}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"usage_update","used":"usage-marker"}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":"still-running"}}]}}}`,
		`{"method":"_x.ai/session/update","params":{"sessionId":"` + grokNative + `","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"kept"}}}}`,
	}, "\n") + "\n")
	events, err := (Grok{Now: time.Unix(1, 0).UTC(), NativeID: grokNative, HarnessVersion: "1"}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || textOf(events[0]) != "kept" || events[0].SessionID != "grok:"+grokNative {
		t.Fatalf("events %+v", events)
	}
}

func textOf(ev Event) string {
	if ev.ContentText == nil {
		return ""
	}
	return *ev.ContentText
}

func grokACPFixture(t *testing.T) []byte {
	t.Helper()
	lines := []string{
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "user_message_chunk",
			"content":       map[string]any{"type": "text", "text": "hello "},
			"_meta":         map[string]any{"promptIndex": 1, "modelId": "grok-build"},
		}, map[string]any{"agentTimestampMs": 1784388059000}),
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "user_message_chunk",
			"content":       map[string]any{"type": "text", "text": "pond"},
			"_meta":         map[string]any{"promptIndex": 1, "modelId": "grok-build"},
		}, nil),
		`{"method":"x.ai/fs_notify","params":{"path":"extension-only-path"}}`,
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "agent_thought_chunk",
			"content":       map[string]any{"type": "text", "text": "think "},
		}, map[string]any{"promptId": "prompt-1"}),
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "agent_thought_chunk",
			"content":       map[string]any{"type": "text", "text": "hard"},
		}, map[string]any{"promptId": "prompt-1"}),
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "I will "},
		}, map[string]any{"promptId": "prompt-1"}),
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "read."},
		}, map[string]any{"promptId": "prompt-1"}),
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    "call-1",
			"title":         "Read",
			"kind":          "read",
			"status":        "pending",
			"rawInput":      map[string]any{"path": "main.go"},
		}, map[string]any{"promptId": "prompt-1"}),
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    "call-1",
			"status":        "in_progress",
			"content":       []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "still-running"}}},
		}, nil),
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    "call-1",
			"status":        "completed",
			"content":       []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "package main"}}},
		}, nil),
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    "call-2",
			"title":         "Bash",
			"kind":          "execute",
			"status":        "pending",
			"rawInput":      map[string]any{"cmd": "true"},
		}, nil),
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    "call-2",
			"status":        "failed",
			"content":       []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "boom"}}},
		}, nil),
		`{"method":"session/update","params":{"update":{"sessionUpdate":"turn_completed","stopReason":"end_turn"}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"usage_update","used":"usage-marker"}}}`,
		`{"method":"session/update","params":{"update":{"sessionUpdate":"plan","entries":[{"content":"do-not-project-plan"}]}}}`,
		grokLine(t, "session/update", grokNative, map[string]any{
			"sessionUpdate": "user_message_chunk",
			"content":       map[string]any{"type": "text", "text": "again"},
			"_meta":         map[string]any{"promptIndex": 2},
		}, nil),
		"not-json sk-live-not-json",
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func grokLine(t *testing.T, method, sessionID string, update, meta map[string]any) string {
	t.Helper()
	params := map[string]any{"update": update}
	if sessionID != "" {
		params["sessionId"] = sessionID
	}
	if meta != nil {
		params["_meta"] = meta
	}
	line := map[string]any{"method": method, "params": params}
	b, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

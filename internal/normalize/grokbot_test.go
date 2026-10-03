package normalize

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter/grokbot"
	"terva.sh/lampi/internal/protocol"
)

const grokbotNative = "018f1a2b-3c4d-7e5f-8a9b-0c1d2e3f4a5b"

func TestGrokBotMessagesSkipUnknown(t *testing.T) {
	if grokbot.Version != "1" {
		t.Fatalf("adapter version %q", grokbot.Version)
	}
	raw := []byte(strings.Join([]string{
		`{"role":"user","message":{"content":[{"type":"text","text":"hello "},{"type":"image","url":"not-text"},{"type":"text","text":"pond"}]}}`,
		`{"role":"tool","message":{"content":[{"type":"text","text":"TOOL-SKIPPED"}]}}`,
		`not-json-line`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"I can help."}]}}`,
		`{"role":"system","note":"skipped"}`,
		`{"role":"user","message":{"content":"not-an-array"}}`,
	}, "\n") + "\n")
	before := append([]byte(nil), raw...)
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	events, err := (GrokBot{
		Now:            now,
		NativeID:       grokbotNative,
		HarnessVersion: grokbot.Version,
		CWD:            "/home/box/agent-data/agent-transcripts/" + grokbotNative,
	}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("normalize changed the raw bytes")
	}
	var messages []Event
	for _, ev := range events {
		if ev.EventType == EventMessage {
			messages = append(messages, ev)
		}
		blob := ev.SessionID + " " + ev.EventType
		if ev.ContentText != nil {
			blob += " " + *ev.ContentText
		}
		if strings.Contains(blob, "TOOL-SKIPPED") || strings.Contains(blob, "not-text") || strings.Contains(blob, "not-json-line") {
			t.Fatalf("event kept a skipped part: %+v", ev)
		}
	}
	if len(messages) != 3 {
		t.Fatalf("messages %d, events %d", len(messages), len(events))
	}
	for _, ev := range messages {
		if ev.SchemaVersion != SchemaVersion || ev.Harness != protocol.HarnessGrokBot || ev.SessionID != "grokbot:"+grokbotNative {
			t.Fatalf("header %+v", ev)
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != "1" {
			t.Fatalf("harness version %v", ev.HarnessVersion)
		}
		if ev.Tool.Name != nil || ev.Tool.CallID != nil {
			t.Fatalf("tool promoted: %+v", ev.Tool)
		}
	}
	if messages[0].Role == nil || *messages[0].Role != ActorUser || messages[0].ContentText == nil || *messages[0].ContentText != "hello pond" {
		t.Fatalf("user %+v", messages[0])
	}
	if messages[1].Role == nil || *messages[1].Role != ActorAssistant || messages[1].ContentText == nil || *messages[1].ContentText != "I can help." {
		t.Fatalf("assistant %+v", messages[1])
	}
	if messages[2].Role == nil || *messages[2].Role != ActorUser || messages[2].ContentText != nil {
		t.Fatalf("non-array content should still be a message: %+v", messages[2])
	}
}

func TestGrokBotUnknownRoleDoesNotFail(t *testing.T) {
	events, err := (GrokBot{NativeID: grokbotNative, HarnessVersion: "1"}).Normalize(context.Background(), []byte(`{"role":"tool","message":{"content":[{"type":"text","text":"no"}]}}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events %+v", events)
	}
}

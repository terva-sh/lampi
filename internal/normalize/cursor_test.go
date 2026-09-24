package normalize

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/protocol"
)

func TestCursorSchemaOpaqueAndUnknownKeys(t *testing.T) {
	plain := "hello-decrypted"
	cipher := "gAAAAABopaque=="
	sealed := "sealed-bytes-v1"
	raw := cursorRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "User/workspaceStorage/ws1/state.vscdb",
		"scope":           "workspace",
		"cli_version":     "9.9.9",
		"item_table": []any{
			map[string]any{"key": "composer.composerHeaders", "value": map[string]any{
				"allComposers": []any{map[string]any{"composerId": "c1", "name": "do-not-invent"}},
			}},
		},
		"cursor_disk_kv": []any{
			map[string]any{"key": "bubbleId:c1:b1", "value": map[string]any{
				"type":              1,
				"rawText":           "from raw",
				"text":              "from text",
				"richText":          "from rich",
				"encrypted_content": cipher,
				"sealed_payload":    sealed,
				"future_field":      map[string]any{"n": 1},
				"version":           "9.9.9",
			}},
			map[string]any{"key": "mystery.key", "value": map[string]any{"keep": true}},
			map[string]any{"key": "composer.content.blob", "value": map[string]any{
				"base64": base64.StdEncoding.EncodeToString([]byte(plain)),
			}},
			map[string]any{"key": "checkpointId:cp1", "value": map[string]any{"files": []any{"a.go"}}},
			map[string]any{"key": "messageRequestContext:c1:b1", "value": map[string]any{"prompt": "fat context"}},
			map[string]any{"key": "agentKv:k", "value": "agent-blob"},
			map[string]any{"key": "codeBlockDiff:d", "value": map[string]any{"diff": "not-dialogue"}},
			map[string]any{"key": "ofsContent:o", "value": map[string]any{"body": "offset-blob"}},
		},
	})
	before := append([]byte(nil), raw...)
	events := projectCursor(t, "workspace/ws1", raw)
	if !bytes.Equal(raw, before) {
		t.Fatal("normalize changed the raw bytes")
	}
	var user, mystery, blob, checkpoint *Event
	var envelope int
	for i := range events {
		ev := &events[i]
		if ev.SchemaVersion != SchemaVersion {
			t.Fatalf("schema_version %d", ev.SchemaVersion)
		}
		if ev.Harness != protocol.HarnessCursor {
			t.Fatalf("harness %s", ev.Harness)
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != cursor.Version || *ev.HarnessVersion == "9.9.9" {
			t.Fatalf("harness version %v", ev.HarnessVersion)
		}
		if ev.Extra["scope"] != "workspace" {
			t.Fatalf("scope %#v", ev.Extra["scope"])
		}
		if ev.ContentText != nil && (strings.Contains(*ev.ContentText, cipher) || strings.Contains(*ev.ContentText, sealed) || strings.Contains(*ev.ContentText, plain)) {
			t.Fatalf("opaque value in content_text: %s", *ev.ContentText)
		}
		if ev.ContentText != nil && *ev.ContentText == "do-not-invent" {
			t.Fatal("allComposers became a message")
		}
		switch ev.RawType {
		case "cursor_state_json":
			envelope++
			if ev.EventType != EventMeta {
				t.Fatalf("envelope type %s", ev.EventType)
			}
			if ev.Extra["harness_version"] != "1" || ev.Extra["confidence"] != "low" || ev.Extra["source"] == "" {
				t.Fatalf("envelope extra %#v", ev.Extra)
			}
			if ev.Extra["cli_version"] != "9.9.9" {
				t.Fatalf("top-level unknown dropped: %#v", ev.Extra["cli_version"])
			}
		case "bubbleId:c1:b1":
			user = ev
		case "mystery.key":
			mystery = ev
		case "composer.content.blob":
			blob = ev
		case "checkpointId:cp1":
			checkpoint = ev
		}
		switch ev.RawType {
		case "checkpointId:cp1", "messageRequestContext:c1:b1", "agentKv:k", "codeBlockDiff:d", "ofsContent:o", "composer.content.blob", "mystery.key":
			if ev.EventType != EventUnknown || ev.ContentText != nil {
				t.Fatalf("classified %s as %+v", ev.RawType, ev)
			}
		}
	}
	if envelope != 1 {
		t.Fatalf("envelope events %d", envelope)
	}
	if user == nil || user.EventType != EventMessage || user.ContentText == nil || *user.ContentText != "from raw" {
		t.Fatalf("user bubble: %+v", user)
	}
	if user.Extra["future_field"] == nil || user.Extra["encrypted_content"] != cipher || user.Extra["sealed_payload"] != sealed {
		t.Fatalf("bubble extra %#v", user.Extra)
	}
	if user.Extra["richText"] != "from rich" || user.Extra["version"] != "9.9.9" {
		t.Fatalf("unknown bubble fields %#v", user.Extra)
	}
	if mystery == nil {
		t.Fatal("missing unknown key")
	}
	kept, _ := mystery.Extra["value"].(map[string]any)
	if kept["keep"] != true {
		t.Fatalf("unknown value %#v", mystery.Extra["value"])
	}
	if blob == nil {
		t.Fatal("missing content blob")
	}
	wrapped, _ := blob.Extra["value"].(map[string]any)
	if wrapped["base64"] == plain || wrapped["base64"] == "" {
		t.Fatalf("base64 wrapper %#v", blob.Extra["value"])
	}
	out, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(plain)) {
		t.Fatal("base64 wrapper was decoded")
	}
	if checkpoint == nil || checkpoint.EventType != EventUnknown {
		t.Fatalf("checkpoint %+v", checkpoint)
	}
}

func TestCursorBubbleMessages(t *testing.T) {
	raw := cursorRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "state.vscdb",
		"scope":           "workspace",
		"item_table":      []any{},
		"cursor_disk_kv": []any{
			map[string]any{"key": "bubbleId:c1:user", "value": map[string]any{
				"type":     1,
				"rawText":  "user raw",
				"text":     "user text",
				"richText": "user rich",
				"allThinkingBlocks": []any{
					map[string]any{"text": "hidden thought"},
				},
			}},
			map[string]any{"key": "bubbleId:c1:assistant", "value": map[string]any{
				"type":       2,
				"text":       "assistant text",
				"richText":   "assistant rich",
				"tokenCount": 12,
				"usageData":  map[string]any{"inputTokens": 3, "outputTokens": 4},
				"toolFormerData": map[string]any{
					"name": "Read",
					"id":   "call-1",
					"args": map[string]any{"path": "main.go"},
				},
				"toolResults": []any{
					map[string]any{"name": "Read", "id": "call-1", "result": "package main"},
				},
				"future_field": true,
			}},
			map[string]any{"key": "bubbleId:c1:other", "value": map[string]any{
				"type": 3,
				"text": "not a turn",
			}},
			map[string]any{"key": "composerData:c1", "value": map[string]any{
				"name":        "thread",
				"unifiedMode": "agent",
				"status":      "none",
				"modelConfig": map[string]any{"modelName": "gpt"},
				"createdAt":   "2026-09-22T16:00:00Z",
				"fullConversationHeadersOnly": []any{
					map[string]any{"bubbleId": "user", "type": 1},
					map[string]any{"bubbleId": "assistant", "type": 2},
					map[string]any{"bubbleId": "other", "type": 3},
				},
				"latestConversationSummary": map[string]any{"summary": "compacted the pond"},
				"conversationMap":           map[string]any{"user": map[string]any{"text": "legacy turn"}},
			}},
		},
	})
	events := projectCursor(t, "workspace/ws1", raw)
	assertNoToolPromotion(t, events)
	var user, assistant, usage, other, meta *Event
	var sawEnvelope bool
	for i := range events {
		ev := &events[i]
		if ev.RawType == "cursor_state_json" && ev.EventType == EventMeta {
			sawEnvelope = true
		}
		if ev.ContentText != nil && (*ev.ContentText == "compacted the pond" || *ev.ContentText == "legacy turn" || *ev.ContentText == "hidden thought" || *ev.ContentText == "not a turn" || *ev.ContentText == "12") {
			t.Fatalf("false turn %q", *ev.ContentText)
		}
		switch ev.RawType {
		case "bubbleId:c1:user":
			user = ev
		case "bubbleId:c1:assistant":
			if ev.EventType == EventUsage {
				usage = ev
				continue
			}
			assistant = ev
		case "bubbleId:c1:other":
			other = ev
		case "composerData:c1":
			meta = ev
		}
	}
	if !sawEnvelope {
		t.Fatal("missing envelope meta")
	}
	if user == nil || user.EventType != EventMessage || user.Actor != ActorUser || user.Role == nil || *user.Role != ActorUser {
		t.Fatalf("user: %+v", user)
	}
	if user.ContentText == nil || *user.ContentText != "user raw" || user.Extra["richText"] != "user rich" {
		t.Fatalf("user text %#v rich %#v", user.ContentText, user.Extra["richText"])
	}
	if user.Extra["allThinkingBlocks"] == nil {
		t.Fatal("thinking dropped")
	}
	if assistant == nil || assistant.EventType != EventMessage || assistant.Actor != ActorAssistant || assistant.Role == nil || *assistant.Role != ActorAssistant {
		t.Fatalf("assistant: %+v", assistant)
	}
	if assistant.ContentText == nil || *assistant.ContentText != "assistant text" {
		t.Fatalf("assistant text %#v", assistant.ContentText)
	}
	if _, stuffed := assistant.Extra["tokenCount"]; stuffed || assistant.Extra["usageData"] == nil {
		t.Fatalf("usage fields %#v", assistant.Extra)
	}
	if usage == nil || usage.EventType != EventUsage || usage.ContentText != nil {
		t.Fatalf("usage: %+v", usage)
	}
	if usage.Extra["tokenCount"] != float64(12) || usage.Extra["composer_id"] != "c1" || usage.Extra["bubble_id"] != "assistant" {
		t.Fatalf("usage extra %#v", usage.Extra)
	}
	if usage.Usage.Input != nil || usage.Usage.Output != nil || usage.SessionID != assistant.SessionID {
		t.Fatalf("usage split or session: %+v", usage)
	}
	tool, _ := assistant.Extra["toolFormerData"].(map[string]any)
	if tool["name"] != "Read" || tool["id"] != "call-1" || tool["args"] == nil {
		t.Fatalf("toolFormerData %#v", assistant.Extra["toolFormerData"])
	}
	if assistant.Extra["toolResults"] == nil || assistant.Extra["future_field"] != true {
		t.Fatalf("assistant extra %#v", assistant.Extra)
	}
	if other == nil || other.EventType != EventUnknown || other.ContentText != nil {
		t.Fatalf("type 3 bubble: %+v", other)
	}
	if meta == nil || meta.EventType != EventMeta || meta.ContentText != nil {
		t.Fatalf("composer meta: %+v", meta)
	}
	if meta.Extra["latestConversationSummary"] == nil || meta.Extra["fullConversationHeadersOnly"] == nil || meta.Extra["conversationMap"] == nil {
		t.Fatalf("composer extra %#v", meta.Extra)
	}
	if meta.Extra["name"] != "thread" || meta.Extra["unifiedMode"] != "agent" {
		t.Fatalf("composer fields %#v", meta.Extra)
	}
}

func TestCursorComposerDataHeadersOnly(t *testing.T) {
	raw := cursorRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "state.vscdb",
		"scope":           "workspace",
		"item_table": []any{
			map[string]any{"key": "composer.composerData", "value": map[string]any{
				"allComposers": []any{map[string]any{"composerId": "c1", "name": "index-only"}},
			}},
		},
		"cursor_disk_kv": []any{
			map[string]any{"key": "composerData:c1", "value": map[string]any{
				"name": "headers-only",
				"fullConversationHeadersOnly": []any{
					map[string]any{"bubbleId": "missing-user", "type": 1},
					map[string]any{"bubbleId": "missing-assistant", "type": 2},
				},
				"latestConversationSummary": map[string]any{"summary": "compacted the pond"},
			}},
		},
	})
	events := projectCursor(t, "workspace/ws1", raw)
	assertNoPromoted(t, events)
	var headers, index int
	for _, ev := range events {
		if ev.EventType == EventMessage || (ev.ContentText != nil && *ev.ContentText != "") {
			t.Fatalf("fabricated turn: %+v", ev)
		}
		switch ev.RawType {
		case "composerData:c1":
			headers++
			if ev.EventType != EventMeta || ev.Extra["fullConversationHeadersOnly"] == nil || ev.Extra["latestConversationSummary"] == nil {
				t.Fatalf("headers meta: %+v", ev)
			}
		case "composer.composerData":
			index++
			if ev.EventType != EventMeta {
				t.Fatalf("index: %+v", ev)
			}
		}
	}
	if headers != 1 || index != 1 {
		t.Fatalf("headers %d index %d", headers, index)
	}
}

func TestCursorBubbleOrderFollowsHeaders(t *testing.T) {
	t.Run("headers beat createdAt and scan order", func(t *testing.T) {
		raw := cursorRaw(t, map[string]any{
			"harness_version": "1",
			"confidence":      "low",
			"source":          "state.vscdb",
			"scope":           "workspace",
			"item_table":      []any{},
			"cursor_disk_kv": []any{
				map[string]any{"key": "bubbleId:c:early", "value": map[string]any{
					"type": 1, "rawText": "early-text", "createdAt": "2026-09-22T16:00:00Z",
				}},
				map[string]any{"key": "bubbleId:c:late", "value": map[string]any{
					"type": 2, "text": "late-text", "createdAt": "2026-09-22T18:00:00Z",
				}},
				map[string]any{"key": "composerData:c", "value": map[string]any{
					"fullConversationHeadersOnly": []any{
						map[string]any{"bubbleId": "late"},
						map[string]any{"bubbleId": "early"},
					},
				}},
			},
		})
		got := messageTexts(projectCursor(t, "workspace/ws1", raw))
		want := []string{"late-text", "early-text"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("order %v", got)
		}
	})
	t.Run("missing headers keep scan order", func(t *testing.T) {
		raw := cursorRaw(t, map[string]any{
			"harness_version": "1",
			"confidence":      "low",
			"source":          "state.vscdb",
			"scope":           "workspace",
			"item_table":      []any{},
			"cursor_disk_kv": []any{
				map[string]any{"key": "bubbleId:c:b", "value": map[string]any{"type": 1, "rawText": "scan-b"}},
				map[string]any{"key": "bubbleId:c:a", "value": map[string]any{"type": 2, "text": "scan-a"}},
				map[string]any{"key": "composerData:c", "value": map[string]any{"name": "no-headers"}},
			},
		})
		got := messageTexts(projectCursor(t, "workspace/ws1", raw))
		want := []string{"scan-b", "scan-a"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("order %v", got)
		}
	})
}

func TestCursorMultiComposerSameSession(t *testing.T) {
	const native = "workspace/ws-multi"
	raw := cursorRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "state.vscdb",
		"scope":           "workspace",
		"item_table":      []any{},
		"cursor_disk_kv": []any{
			map[string]any{"key": "bubbleId:comp-a:a2", "value": map[string]any{"type": 2, "text": "a-second"}},
			map[string]any{"key": "bubbleId:comp-b:b2", "value": map[string]any{"type": 2, "text": "b-second"}},
			map[string]any{"key": "bubbleId:comp-a:a1", "value": map[string]any{"type": 1, "rawText": "a-first"}},
			map[string]any{"key": "bubbleId:comp-b:b1", "value": map[string]any{"type": 1, "rawText": "b-first"}},
			map[string]any{"key": "composerData:comp-a", "value": map[string]any{
				"fullConversationHeadersOnly": []any{
					map[string]any{"bubbleId": "a1"},
					map[string]any{"bubbleId": "a2"},
				},
			}},
			map[string]any{"key": "composerData:comp-b", "value": map[string]any{
				"fullConversationHeadersOnly": []any{
					map[string]any{"bubbleId": "b1"},
					map[string]any{"bubbleId": "b2"},
				},
			}},
		},
	})
	events := projectCursor(t, native, raw)
	wantID := "cursor:" + native
	var texts []string
	for _, ev := range events {
		if ev.SessionID != wantID {
			t.Fatalf("session_id %s", ev.SessionID)
		}
		if ev.SessionID == "cursor:comp-a" || ev.SessionID == "cursor:comp-b" {
			t.Fatalf("composer used as session: %s", ev.SessionID)
		}
		if ev.EventType != EventMessage || ev.ContentText == nil {
			continue
		}
		texts = append(texts, *ev.ContentText)
		switch *ev.ContentText {
		case "a-first", "a-second":
			if ev.Extra["composer_id"] != "comp-a" {
				t.Fatalf("composer for %s: %#v", *ev.ContentText, ev.Extra["composer_id"])
			}
		case "b-first", "b-second":
			if ev.Extra["composer_id"] != "comp-b" {
				t.Fatalf("composer for %s: %#v", *ev.ContentText, ev.Extra["composer_id"])
			}
		}
	}
	want := []string{"a-first", "b-first", "a-second", "b-second"}
	if strings.Join(texts, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v", texts)
	}
}

func TestCursorItemTableOnlyWorkspaceIndex(t *testing.T) {
	raw := cursorRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "User/workspaceStorage/ws1/state.vscdb",
		"scope":           "workspace",
		"item_table": []any{
			map[string]any{"key": "composer.composerData", "value": map[string]any{
				"allComposers": []any{map[string]any{"name": "index-only"}},
			}},
			map[string]any{"key": "workbench.panel.aichat.view", "value": map[string]any{
				"messages": []any{map[string]any{"text": "panel-turn"}},
			}},
			map[string]any{"key": "aiService.generations", "value": map[string]any{"text": "service-turn"}},
			map[string]any{"key": "some.editor.setting", "value": 1},
		},
	})
	events := projectCursor(t, "workspace/ws1", raw)
	if len(events) == 0 {
		t.Fatal("empty success")
	}
	var envelope, composerMeta, uiMeta, service, setting bool
	for _, ev := range events {
		if ev.SchemaVersion != SchemaVersion || ev.Extra["scope"] != "workspace" {
			t.Fatalf("header %+v", ev)
		}
		if ev.ContentText != nil {
			t.Fatalf("content on index row: %s", *ev.ContentText)
		}
		switch ev.RawType {
		case "cursor_state_json":
			envelope = ev.EventType == EventMeta
		case "composer.composerData":
			composerMeta = ev.EventType == EventMeta
		case "workbench.panel.aichat.view":
			uiMeta = ev.EventType == EventMeta
		case "aiService.generations":
			service = ev.EventType == EventUnknown
		case "some.editor.setting":
			setting = ev.EventType == EventUnknown
		}
	}
	if !envelope || !composerMeta || !uiMeta || !service || !setting {
		t.Fatalf("classes envelope=%v composer=%v ui=%v service=%v setting=%v", envelope, composerMeta, uiMeta, service, setting)
	}

	empty := cursorRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "state.vscdb",
		"scope":           "global",
		"item_table":      []any{},
	})
	events, err := (Cursor{HarnessVersion: cursor.Version}).Normalize(context.Background(), empty)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != EventMeta || events[0].RawType != "cursor_state_json" {
		t.Fatalf("empty export: %+v", events)
	}
}

func TestCursorSessionIDPrefersNative(t *testing.T) {
	const native = "workspace/ws-native"
	raw := cursorRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "state.vscdb",
		"scope":           "workspace",
		"item_table":      []any{},
		"cursor_disk_kv": []any{
			map[string]any{"key": "bubbleId:other-session:b1", "value": map[string]any{
				"type": 1, "rawText": "hello",
			}},
		},
	})
	events := projectCursor(t, native, raw)
	want := "cursor:" + native
	if len(events) == 0 {
		t.Fatal("no events")
	}
	for _, ev := range events {
		if ev.SessionID != want {
			t.Fatalf("session_id %s", ev.SessionID)
		}
	}
	if events[0].SessionID == "cursor:other-session" {
		t.Fatal("composer id became the session")
	}
}

func TestCursorAuthAbsent(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		raw := cursorRaw(t, map[string]any{
			"harness_version": "1",
			"confidence":      "low",
			"source":          "state.vscdb",
			"scope":           "workspace",
			"item_table": []any{
				map[string]any{"key": "composer.composerData", "value": map[string]any{"note": "hello pond"}},
			},
		})
		out := marshalEvents(t, projectCursor(t, "workspace/ws1", raw))
		for _, forbidden := range []string{"cursorAuth", "accessToken", "invented-cursor-token"} {
			if bytes.Contains(out, []byte(forbidden)) {
				t.Fatalf("invented %s:\n%s", forbidden, out)
			}
		}
	})
	t.Run("hostile", func(t *testing.T) {
		const secret = "super-secret-token"
		raw := cursorRaw(t, map[string]any{
			"harness_version": "1",
			"confidence":      "low",
			"source":          "state.vscdb",
			"scope":           "workspace",
			"item_table": []any{
				map[string]any{"key": "cursorAuth/accessToken", "value": secret},
				map[string]any{"key": "composer.composerData", "value": map[string]any{"note": "hello pond"}},
			},
			"cursor_disk_kv": []any{
				map[string]any{"key": "bubbleId:c:b", "value": map[string]any{
					"type": 1, "rawText": "hello pond", "cursorAuth": secret,
				}},
			},
		})
		events := projectCursor(t, "workspace/ws1", raw)
		for _, ev := range events {
			if ev.ContentText != nil && strings.Contains(*ev.ContentText, secret) {
				t.Fatalf("secret in content_text: %s", *ev.ContentText)
			}
			if ev.RawType == "cursorAuth/accessToken" {
				t.Fatal("cursorAuth row was projected")
			}
		}
		out := marshalEvents(t, events)
		if bytes.Contains(out, []byte(secret)) || bytes.Contains(out, []byte("invented-cursor-token")) {
			t.Fatalf("secret surfaced:\n%s", out)
		}
	})
}

func TestCursorTranscriptJSONLRejected(t *testing.T) {
	const secret = "sk-cursor-transcript-secret"
	cases := []struct {
		name string
		body []byte
	}{
		{"jsonl", []byte("{\"type\":\"user\",\"message\":\"" + secret + "\"}\n{\"type\":\"assistant\"}\n")},
		{"sqlite", append([]byte("SQLite format 3\x00"), []byte(secret)...)},
		{"object", []byte(`{"type":"user","message":"` + secret + `"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events, err := (Cursor{HarnessVersion: cursor.Version}).Normalize(context.Background(), tc.body)
			if err == nil {
				t.Fatal("expected an error")
			}
			if events != nil {
				t.Fatalf("events: %+v", events)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "SQLite format") {
				t.Fatalf("error includes the body: %s", err)
			}
		})
	}
}

func TestCursorNormalizeErrorOmitsBody(t *testing.T) {
	const secret = "sk-cursor-truncated-secret"
	raw := []byte(`{"item_table":[{"key":"x","value":"` + secret + `"`)
	events, err := (Cursor{HarnessVersion: cursor.Version}).Normalize(context.Background(), raw)
	if err == nil {
		t.Fatal("expected an error")
	}
	if events != nil {
		t.Fatalf("events: %+v", events)
	}
	msg := err.Error()
	if strings.Contains(msg, secret) || strings.Contains(msg, "item_table") || strings.Contains(msg, `{"item_table"`) {
		t.Fatalf("error includes the body: %s", msg)
	}
}

func projectCursor(t *testing.T, native string, raw []byte) []Event {
	t.Helper()
	events, err := (Cursor{
		Now:            time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC),
		NativeID:       native,
		HarnessVersion: cursor.Version,
		CWD:            "/work/app",
		ProjectID:      "local",
	}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func cursorRaw(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func marshalEvents(t *testing.T, events []Event) []byte {
	t.Helper()
	b, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertNoPromoted(t *testing.T, events []Event) {
	t.Helper()
	for _, ev := range events {
		switch ev.EventType {
		case EventUsage, EventToolCall, EventToolResult, EventCompaction:
			t.Fatalf("promoted %s from %s", ev.EventType, ev.RawType)
		}
	}
}

func assertNoToolPromotion(t *testing.T, events []Event) {
	t.Helper()
	for _, ev := range events {
		switch ev.EventType {
		case EventToolCall, EventToolResult, EventCompaction:
			t.Fatalf("promoted %s from %s", ev.EventType, ev.RawType)
		}
	}
}

func messageTexts(events []Event) []string {
	var out []string
	for _, ev := range events {
		if ev.EventType == EventMessage && ev.ContentText != nil {
			out = append(out, *ev.ContentText)
		}
	}
	return out
}

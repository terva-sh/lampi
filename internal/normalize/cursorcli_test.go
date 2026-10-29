package normalize

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter/cursorcli"
	"terva.sh/lampi/internal/protocol"
)

func TestCursorCLISchemaOpaqueAndUnknownKeys(t *testing.T) {
	plain := "hello-decrypted"
	cipher := "gAAAAABopaque=="
	sealed := "sealed-bytes-v1"
	cipherField := "cipher-bytes-v1"
	raw := cursorCLIRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/sid-1/store.db",
		"scope":           "session",
		"cli_version":     "9.9.9",
		"meta": []any{
			map[string]any{"key": "0", "value": map[string]any{
				"name":      "kept-title",
				"title":     "session-title",
				"createdAt": 1767396459642,
			}},
			map[string]any{"key": "z-shell", "value": map[string]any{
				"name": "shell-name",
			}},
			map[string]any{"key": "a-unknown", "value": map[string]any{
				"name": "not-only-shell",
				"note": "mystery-meta",
			}},
		},
		"blobs": []any{
			map[string]any{"id": "m-user", "data": map[string]any{
				"role":              "user",
				"content":           "from content",
				"text":              "from text",
				"rawText":           "from raw",
				"encrypted_content": cipher,
				"cipher_blob":       cipherField,
				"sealed_payload":    sealed,
				"future_field":      map[string]any{"n": 1},
				"version":           "9.9.9",
			}},
			map[string]any{"id": "a-unknown", "data": map[string]any{"keep": true}},
			map[string]any{"id": "b64", "data": map[string]any{
				"base64": base64.StdEncoding.EncodeToString([]byte(plain)),
			}},
		},
	})
	before := append([]byte(nil), raw...)
	events := projectCursorCLI(t, "chats/ab12/sid-1", raw)
	if !bytes.Equal(raw, before) {
		t.Fatal("normalize changed the raw bytes")
	}
	wantOrder := []string{"cursor_cli_store_json", "0", "z-shell", "a-unknown", "m-user", "a-unknown", "b64"}
	if strings.Join(cursorCLIRawTypes(events), ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("order %v", cursorCLIRawTypes(events))
	}
	var user, mystery, blob, shell, session *Event
	var envelope int
	for i := range events {
		ev := &events[i]
		if ev.SchemaVersion != SchemaVersion {
			t.Fatalf("schema_version %d", ev.SchemaVersion)
		}
		if ev.Harness != protocol.HarnessCursorCLI {
			t.Fatalf("harness %s", ev.Harness)
		}
		if ev.SessionID != "cursor-cli:chats/ab12/sid-1" || ev.Harness == protocol.HarnessCursor {
			t.Fatalf("session %s harness %s", ev.SessionID, ev.Harness)
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != cursorcli.Version || *ev.HarnessVersion == "9.9.9" {
			t.Fatalf("harness version %v", ev.HarnessVersion)
		}
		if ev.ContentText != nil && (strings.Contains(*ev.ContentText, cipher) || strings.Contains(*ev.ContentText, sealed) || strings.Contains(*ev.ContentText, cipherField) || strings.Contains(*ev.ContentText, plain)) {
			t.Fatalf("opaque value in content_text: %s", *ev.ContentText)
		}
		switch {
		case ev.RawType == "cursor_cli_store_json":
			envelope++
			if ev.EventType != EventMeta || ev.ContentText != nil {
				t.Fatalf("envelope %+v", ev)
			}
			if ev.Extra["harness_version"] != "1" || ev.Extra["confidence"] != "low" || ev.Extra["source"] == "" || ev.Extra["scope"] != "session" {
				t.Fatalf("envelope extra %#v", ev.Extra)
			}
			if ev.Extra["cli_version"] != "9.9.9" {
				t.Fatalf("top-level unknown dropped: %#v", ev.Extra["cli_version"])
			}
			if _, ok := ev.Extra["meta"]; ok {
				t.Fatal("envelope kept the meta array")
			}
		case ev.RawType == "0":
			session = ev
		case ev.RawType == "z-shell":
			shell = ev
		case ev.RawType == "a-unknown" && ev.Extra["meta_key"] == "a-unknown":
			mystery = ev
		case ev.RawType == "m-user":
			user = ev
		case ev.RawType == "b64":
			blob = ev
		}
	}
	if envelope != 1 {
		t.Fatalf("envelope events %d", envelope)
	}
	if session == nil || session.EventType != EventMeta || session.ContentText != nil || session.Extra["meta_key"] != "0" {
		t.Fatalf("session record: %+v", session)
	}
	if session.Extra["name"] != "kept-title" || session.Extra["title"] != "session-title" || session.Extra["createdAt"] == nil {
		t.Fatalf("session extra %#v", session.Extra)
	}
	if shell == nil || shell.EventType != EventMeta || shell.ContentText != nil || shell.Extra["meta_key"] != "z-shell" || shell.Extra["name"] != "shell-name" {
		t.Fatalf("shell meta: %+v", shell)
	}
	if mystery == nil || mystery.EventType != EventUnknown || mystery.ContentText != nil {
		t.Fatalf("unknown meta: %+v", mystery)
	}
	kept, _ := mystery.Extra["value"].(map[string]any)
	if kept["note"] != "mystery-meta" {
		t.Fatalf("unknown meta value %#v", mystery.Extra["value"])
	}
	if user == nil || user.EventType != EventMessage || user.ContentText == nil || *user.ContentText != "from content" {
		t.Fatalf("user blob: %+v", user)
	}
	if user.Actor != ActorUser || user.Role == nil || *user.Role != ActorUser || user.Extra["blob_id"] != "m-user" {
		t.Fatalf("user identity %+v", user)
	}
	if user.Extra["future_field"] == nil || user.Extra["encrypted_content"] != cipher || user.Extra["sealed_payload"] != sealed || user.Extra["cipher_blob"] != cipherField {
		t.Fatalf("blob extra %#v", user.Extra)
	}
	if user.Extra["text"] != "from text" || user.Extra["rawText"] != "from raw" || user.Extra["version"] != "9.9.9" {
		t.Fatalf("unknown blob fields %#v", user.Extra)
	}
	if blob == nil || blob.EventType != EventUnknown || blob.ContentText != nil || blob.Extra["blob_id"] != "b64" {
		t.Fatalf("base64 blob: %+v", blob)
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
	assertNoPromoted(t, events)
}

func TestCursorCLIBlobPromotion(t *testing.T) {
	raw := cursorCLIRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/sid-1/store.db",
		"scope":           "session",
		"meta": []any{
			map[string]any{"key": "0", "value": map[string]any{
				"name":    "not-a-turn",
				"role":    "user",
				"content": "session text is not a message",
			}},
		},
		"blobs": []any{
			map[string]any{"id": "by-role", "data": map[string]any{
				"role": "assistant", "text": "by role", "createdAt": "2026-09-22T16:10:01Z",
			}},
			map[string]any{"id": "by-type", "data": map[string]any{
				"type": 1, "rawText": "by type",
			}},
			map[string]any{"id": "type-two", "data": map[string]any{
				"type": 2, "text": "type two", "content": "",
			}},
			map[string]any{"id": "toolish", "data": map[string]any{
				"type": "tool_call", "name": "Read", "content": "tool-args",
			}},
			map[string]any{"id": "usage", "data": map[string]any{
				"usage": map[string]any{"input": 3},
			}},
			map[string]any{"id": "summary", "data": map[string]any{
				"summary": "compacted the pond",
			}},
			map[string]any{"id": "no-text", "data": map[string]any{
				"role": "user", "encrypted_content": "only-cipher",
			}},
			map[string]any{"id": "system", "data": map[string]any{
				"role": "system", "content": "not a turn",
			}},
			map[string]any{"id": "string-blob", "data": "plain-string"},
		},
	})
	events := projectCursorCLI(t, "chats/ab12/sid-1", raw)
	assertNoPromoted(t, events)
	got := map[string]*Event{}
	for i := range events {
		got[events[i].RawType] = &events[i]
	}
	session := got["0"]
	if session == nil || session.EventType != EventMeta || session.ContentText != nil {
		t.Fatalf("session record became a turn: %+v", session)
	}
	if session.Extra["content"] != "session text is not a message" {
		t.Fatalf("session content dropped: %#v", session.Extra)
	}
	byRole := got["by-role"]
	if byRole == nil || byRole.EventType != EventMessage || byRole.Role == nil || *byRole.Role != ActorAssistant || byRole.ContentText == nil || *byRole.ContentText != "by role" {
		t.Fatalf("role message: %+v", byRole)
	}
	byType := got["by-type"]
	if byType == nil || byType.EventType != EventMessage || byType.Role == nil || *byType.Role != ActorUser || byType.ContentText == nil || *byType.ContentText != "by type" {
		t.Fatalf("type message: %+v", byType)
	}
	typeTwo := got["type-two"]
	if typeTwo == nil || typeTwo.EventType != EventMessage || typeTwo.ContentText == nil || *typeTwo.ContentText != "type two" {
		t.Fatalf("type 2 message: %+v", typeTwo)
	}
	for _, id := range []string{"toolish", "usage", "summary", "no-text", "system", "string-blob"} {
		ev := got[id]
		if ev == nil || ev.EventType != EventUnknown || ev.ContentText != nil {
			t.Fatalf("%s: %+v", id, ev)
		}
	}
	if got["no-text"].Extra["value"] == nil {
		t.Fatal("opaque blob dropped")
	}
	out := marshalEvents(t, events)
	if bytes.Contains(out, []byte(`"content_text":"only-cipher"`)) || bytes.Contains(out, []byte(`"content_text":"tool-args"`)) || bytes.Contains(out, []byte(`"content_text":"compacted the pond"`)) || bytes.Contains(out, []byte(`"content_text":"session text is not a message"`)) {
		t.Fatalf("false turn:\n%s", out)
	}
}

func TestCursorCLIContentPartsStayUnknown(t *testing.T) {
	const cipher = "gAAAAABparts-opaque=="
	raw := cursorCLIRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/sid-1/store.db",
		"scope":           "session",
		"meta":            []any{},
		"blobs": []any{
			map[string]any{"id": "by-content", "data": map[string]any{
				"role": "user", "content": "content string",
			}},
			map[string]any{"id": "by-text", "data": map[string]any{
				"role": "assistant", "text": "text string",
			}},
			map[string]any{"id": "by-raw", "data": map[string]any{
				"type": 1, "rawText": "rawText string",
			}},
			map[string]any{"id": "by-type", "data": map[string]any{
				"type": 2, "text": "type two string",
			}},
			map[string]any{"id": "parts", "data": map[string]any{
				"role":    "assistant",
				"text":    "string-fallback-not-used",
				"rawText": "raw-fallback-not-used",
				"content": []any{
					map[string]any{"type": "text", "text": "part text"},
					map[string]any{"type": "reasoning", "text": "hidden thought"},
					map[string]any{"type": "tool_use", "name": "Read", "input": map[string]any{"path": "main.go"}},
					map[string]any{"type": "tool_result", "content": "file bytes"},
				},
				"encrypted_content": cipher,
			}},
		},
	})
	events := projectCursorCLI(t, "chats/ab12/sid-1", raw)
	// Envelope, four string messages, one parts blob. A walk of the
	// parts would add events.
	if len(events) != 6 {
		t.Fatalf("events %d, raw types %v", len(events), cursorCLIRawTypes(events))
	}
	want := map[string]struct {
		text string
		role string
	}{
		"by-content": {text: "content string", role: ActorUser},
		"by-text":    {text: "text string", role: ActorAssistant},
		"by-raw":     {text: "rawText string", role: ActorUser},
		"by-type":    {text: "type two string", role: ActorAssistant},
	}
	var parts *Event
	var messages int
	for i := range events {
		ev := &events[i]
		if ev.RawType == "cursor_cli_store_json" {
			if ev.EventType != EventMeta {
				t.Fatalf("envelope %+v", ev)
			}
			continue
		}
		if ev.RawType == "parts" {
			parts = ev
			continue
		}
		spec, ok := want[ev.RawType]
		if !ok || ev.EventType != EventMessage || ev.Role == nil || *ev.Role != spec.role || ev.ContentText == nil || *ev.ContentText != spec.text {
			t.Fatalf("string cleartext %s: %+v", ev.RawType, ev)
		}
		messages++
	}
	if messages != len(want) {
		t.Fatalf("string messages %d", messages)
	}
	if parts == nil || parts.EventType != EventUnknown || parts.ContentText != nil || parts.Role != nil {
		t.Fatalf("parts blob: %+v", parts)
	}
	if parts.Extra["blob_id"] != "parts" || parts.Extra["encrypted_content"] != cipher {
		t.Fatalf("parts extra %#v", parts.Extra)
	}
	content, ok := parts.Extra["content"].([]any)
	if !ok || len(content) != 4 {
		t.Fatalf("content parts %#v", parts.Extra["content"])
	}
	if parts.Extra["value"] != nil {
		t.Fatalf("parts object was wrapped: %#v", parts.Extra["value"])
	}
	out := marshalEvents(t, events)
	for _, hidden := range []string{`"content_text":"part text"`, `"content_text":"hidden thought"`, `"content_text":"file bytes"`, `"content_text":"string-fallback-not-used"`, `"content_text":"raw-fallback-not-used"`, `"content_text":"` + cipher} {
		if bytes.Contains(out, []byte(hidden)) {
			t.Fatalf("promoted %s:\n%s", hidden, out)
		}
	}
}

// latestRootBlobId names a blob that is not a protobuf root, so no
// order is read from it: export order stays, and the root stays an
// opaque unknown event whose bytes are not decoded into any event.
func TestCursorCLIMetaDoesNotFollowBlobGraph(t *testing.T) {
	const (
		native    = "chats/ab12/sid-1"
		agent     = "agent-not-the-session"
		cipher    = "gAAAAABroot-opaque=="
		protoText = "proto-hello-pond"
	)
	raw := cursorCLIRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          native + "/store.db",
		"scope":           "session",
		"meta": []any{
			map[string]any{"key": "0", "value": map[string]any{
				"name":              "kept-title",
				"agentId":           agent,
				"latestRootBlobId":  "root-b64",
				"mode":              "agent",
				"lastUsedModel":     "composer-2",
				"encrypted_content": cipher,
			}},
		},
		"blobs": []any{
			map[string]any{"id": "user", "data": map[string]any{
				"role": "user", "content": "export-order user",
			}},
			map[string]any{"id": "root-b64", "data": map[string]any{
				"base64": base64.StdEncoding.EncodeToString([]byte(protoText)),
			}},
		},
	})
	events := projectCursorCLI(t, native, raw)
	wantOrder := []string{"cursor_cli_store_json", "0", "user", "root-b64"}
	if strings.Join(cursorCLIRawTypes(events), ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("followed the blob graph: %v", cursorCLIRawTypes(events))
	}
	var session, user, root *Event
	for i := range events {
		ev := &events[i]
		if ev.SessionID != "cursor-cli:"+native {
			t.Fatalf("session_id %s", ev.SessionID)
		}
		if ev.SessionID == "cursor-cli:"+agent || strings.Contains(ev.SessionID, agent) {
			t.Fatalf("agentId became the session: %s", ev.SessionID)
		}
		switch ev.RawType {
		case "0":
			session = ev
		case "user":
			user = ev
		case "root-b64":
			root = ev
		}
	}
	if session == nil || session.EventType != EventMeta || session.ContentText != nil {
		t.Fatalf("session record: %+v", session)
	}
	if session.Extra["agentId"] != agent || session.Extra["latestRootBlobId"] != "root-b64" || session.Extra["mode"] != "agent" || session.Extra["lastUsedModel"] != "composer-2" || session.Extra["encrypted_content"] != cipher || session.Extra["name"] != "kept-title" {
		t.Fatalf("session extra %#v", session.Extra)
	}
	if user == nil || user.EventType != EventMessage || user.ContentText == nil || *user.ContentText != "export-order user" {
		t.Fatalf("user: %+v", user)
	}
	if root == nil || root.EventType != EventUnknown || root.ContentText != nil || root.Extra["blob_id"] != "root-b64" {
		t.Fatalf("root blob: %+v", root)
	}
	wrapped, _ := root.Extra["value"].(map[string]any)
	if wrapped["base64"] == protoText || wrapped["base64"] == "" {
		t.Fatalf("protobuf root %#v", root.Extra["value"])
	}
	out := marshalEvents(t, events)
	if bytes.Contains(out, []byte(protoText)) {
		t.Fatal("protobuf root was decoded")
	}
	assertNoPromoted(t, events)
}

func TestCursorCLIEmptyTurns(t *testing.T) {
	t.Run("envelope only", func(t *testing.T) {
		raw := cursorCLIRaw(t, map[string]any{
			"harness_version": "1",
			"confidence":      "low",
			"source":          "chats/ab12/sid-1/store.db",
			"scope":           "session",
			"meta":            []any{},
			"blobs":           []any{},
		})
		events := projectCursorCLI(t, "chats/ab12/sid-1", raw)
		if len(events) != 1 || events[0].EventType != EventMeta || events[0].RawType != "cursor_cli_store_json" || events[0].ContentText != nil {
			t.Fatalf("envelope: %+v", events)
		}
	})
	t.Run("meta and unknown only", func(t *testing.T) {
		raw := cursorCLIRaw(t, map[string]any{
			"harness_version": "1",
			"confidence":      "low",
			"source":          "chats/ab12/empty/store.db",
			"scope":           "session",
			"meta": []any{
				map[string]any{"key": "0", "value": map[string]any{"name": "quiet"}},
			},
			"blobs": []any{
				map[string]any{"id": "mystery", "data": map[string]any{"keep": "unknown-marker"}},
			},
		})
		events := projectCursorCLI(t, "chats/ab12/empty", raw)
		if len(events) != 3 {
			t.Fatalf("events %d", len(events))
		}
		for _, ev := range events {
			if ev.EventType == EventMessage || (ev.ContentText != nil && *ev.ContentText != "") {
				t.Fatalf("invented message: %+v", ev)
			}
			if ev.EventType != EventMeta && ev.EventType != EventUnknown {
				t.Fatalf("event %s", ev.EventType)
			}
		}
	})
}

func TestCursorCLIWrongKind(t *testing.T) {
	const secret = "sk-cursor-cli-wrong-kind"
	cases := []struct {
		name string
		body []byte
	}{
		{"state", []byte(`{"harness_version":"1","item_table":[{"key":"bubbleId:c:b","value":{"type":1,"rawText":"` + secret + `"}}],"cursor_disk_kv":[]}`)},
		{"jsonl", []byte("{\"type\":\"user\",\"message\":\"" + secret + "\"}\n{\"type\":\"assistant\"}\n")},
		{"sqlite", append([]byte("SQLite format 3\x00"), []byte(secret)...)},
		{"truncated", []byte(`{"meta":[{"key":"0","value":"` + secret)},
		{"object", []byte(`{"type":"user","message":"` + secret + `"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events, err := (CursorCLI{HarnessVersion: cursorcli.Version}).Normalize(context.Background(), tc.body)
			if err == nil {
				t.Fatal("expected an error")
			}
			if events != nil {
				t.Fatalf("events: %+v", events)
			}
			msg := err.Error()
			if strings.Contains(msg, secret) || strings.Contains(msg, "item_table") || strings.Contains(msg, "SQLite format") || strings.Contains(msg, "bubbleId") {
				t.Fatalf("error includes the body: %s", msg)
			}
		})
	}
}

func TestCursorCLISessionIDPrefersNative(t *testing.T) {
	const native = "chats/ab12/sid-native"
	raw := cursorCLIRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/sid-native/store.db",
		"scope":           "session",
		"meta":            []any{},
		"blobs": []any{
			map[string]any{"id": "composer-id-not-session", "data": map[string]any{
				"role": "user", "content": "hello", "composerId": "other-session",
			}},
		},
	})
	events := projectCursorCLI(t, native, raw)
	parent := "chats/parent"
	withParent, err := (CursorCLI{
		Now:            time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC),
		NativeID:       native,
		ParentNativeID: parent,
		HarnessVersion: cursorcli.Version,
	}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "cursor-cli:" + native
	for _, ev := range events {
		if ev.SessionID != want {
			t.Fatalf("session_id %s", ev.SessionID)
		}
		if ev.SessionID == "cursor:other-session" || ev.SessionID == "cursor-cli:composer-id-not-session" || ev.SessionID == "cursor-cli:other-session" {
			t.Fatalf("invented session %s", ev.SessionID)
		}
	}
	if withParent[0].ParentSessionID == nil || *withParent[0].ParentSessionID != "cursor-cli:"+parent {
		t.Fatalf("parent %+v", withParent[0].ParentSessionID)
	}
}

func TestCursorCLIAuthAbsent(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		raw := cursorCLIRaw(t, map[string]any{
			"harness_version": "1",
			"confidence":      "low",
			"source":          "chats/ab12/sid-1/store.db",
			"scope":           "session",
			"meta": []any{
				map[string]any{"key": "0", "value": map[string]any{"name": "hello pond"}},
			},
			"blobs": []any{
				map[string]any{"id": "blob-1", "data": map[string]any{
					"role": "user", "content": "hello pond", "note": "the field accessToken is a name",
				}},
			},
		})
		out := marshalEvents(t, projectCursorCLI(t, "chats/ab12/sid-1", raw))
		for _, forbidden := range []string{"cursorAuth", "accessToken", "refreshToken", "invented-cursor-token", "workosCursorSessionToken"} {
			if bytes.Contains(out, []byte(forbidden)) && forbidden != "accessToken" {
				t.Fatalf("invented %s:\n%s", forbidden, out)
			}
		}
		if bytes.Contains(out, []byte(`"accessToken"`)) || bytes.Contains(out, []byte("invented-cursor-token")) {
			t.Fatalf("invented credential:\n%s", out)
		}
		if !bytes.Contains(out, []byte("the field accessToken is a name")) {
			t.Fatal("dropped a non-credential note")
		}
	})
	t.Run("hostile", func(t *testing.T) {
		const secret = "super-secret-token"
		raw := cursorCLIRaw(t, map[string]any{
			"harness_version": "1",
			"confidence":      "low",
			"source":          "chats/ab12/sid-1/store.db",
			"scope":           "session",
			"accessToken":     secret,
			"meta": []any{
				map[string]any{"key": "cursorAuth/accessToken", "value": secret},
				map[string]any{"key": "0", "value": map[string]any{
					"name": "hello pond", "refreshToken": secret, "blobEncryptionKey": secret,
				}},
			},
			"blobs": []any{
				map[string]any{"id": "cursorAuth/session", "data": secret},
				map[string]any{"id": "blob-1", "data": map[string]any{
					"role": "user", "content": "hello pond", "accessToken": secret, "cursorAuth": secret,
				}},
			},
		})
		events := projectCursorCLI(t, "chats/ab12/sid-1", raw)
		for _, ev := range events {
			if ev.ContentText != nil && strings.Contains(*ev.ContentText, secret) {
				t.Fatalf("secret in content_text: %s", *ev.ContentText)
			}
			if ev.RawType == "cursorAuth/accessToken" || ev.RawType == "cursorAuth/session" {
				t.Fatal("cursorAuth row was projected")
			}
		}
		out := marshalEvents(t, events)
		if bytes.Contains(out, []byte(secret)) || bytes.Contains(out, []byte("invented-cursor-token")) {
			t.Fatalf("secret surfaced:\n%s", out)
		}
	})
}

// cliBlobID is a 64-hex-digit blob id, the shape Cursor's content hash
// ids have, so that a root can list it.
func cliBlobID(n int) string {
	return strings.Repeat(fmt.Sprintf("%02x", n), 32)
}

// cliRoot is a base64 wrapper around a protobuf root: field 1 once per
// listed id, and a varint field 10 a reader must skip.
func cliRoot(ids ...string) map[string]any {
	var b []byte
	for _, id := range ids {
		raw, err := hex.DecodeString(id)
		if err != nil || len(raw) != 32 {
			panic("bad blob id " + id)
		}
		b = append(b, 0x0a, 0x20)
		b = append(b, raw...)
	}
	b = append(b, 0x50, 0x01)
	return map[string]any{"base64": base64.StdEncoding.EncodeToString(b)}
}

func TestCursorCLIToolPartsInConversationOrder(t *testing.T) {
	user, asst, tool, stray := cliBlobID(0x30), cliBlobID(0x10), cliBlobID(0x20), cliBlobID(0x05)
	early, latest := cliBlobID(0x40), cliBlobID(0x01)
	raw := cursorCLIRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/sid-1/store.db",
		"scope":           "session",
		"meta": []any{
			map[string]any{"key": "0", "value": map[string]any{"name": "pond", "latestRootBlobId": latest}},
		},
		// Export order is id order, which is not the conversation.
		"blobs": []any{
			map[string]any{"id": latest, "data": cliRoot(user, asst, tool)},
			map[string]any{"id": stray, "data": map[string]any{"role": "user", "content": "not in any root"}},
			map[string]any{"id": asst, "data": map[string]any{"role": "assistant", "id": "m2", "content": []any{
				map[string]any{"type": "text", "text": "reading"},
				map[string]any{"type": "tool-call", "toolCallId": "tc-1", "toolName": "Read", "args": map[string]any{"path": "main.go"}},
				map[string]any{"type": "tool-call", "toolCallId": "tc-2", "toolName": "CallDynamicTool", "args": `{"namespace":"pond-docs","toolName":"search","arguments":{"q":"pond"}}`},
				map[string]any{"type": "tool-call", "toolCallId": "tc-3", "toolName": "CallDynamicTool", "args": map[string]any{"namespace": "cursor", "toolName": "AwaitShell", "arguments": map[string]any{}}},
			}}},
			map[string]any{"id": tool, "data": map[string]any{"role": "tool", "id": "m3", "content": []any{
				map[string]any{"type": "tool-result", "toolCallId": "tc-1", "toolName": "Read", "result": "package main", "experimental_content": []any{}},
				map[string]any{"type": "tool-result", "toolCallId": "tc-2", "toolName": "CallDynamicTool", "result": map[string]any{"hits": 1}},
				map[string]any{"type": "tool-result", "toolCallId": "tc-3", "toolName": "CallDynamicTool", "result": "done"},
				map[string]any{"type": "tool-result", "toolCallId": "tc-9", "toolName": "Grep", "result": "orphan"},
			}}},
			map[string]any{"id": user, "data": map[string]any{"role": "user", "content": "read main.go"}},
			map[string]any{"id": early, "data": cliRoot(user, asst)},
		},
	})
	events := projectCursorCLI(t, "chats/ab12/sid-1", raw)
	var got []string
	for _, ev := range events {
		line := ev.EventType + " " + ev.RawType
		if len(ev.RawType) == 64 {
			line = ev.EventType + " " + ev.RawType[:2]
		}
		if ev.Tool.Name != nil {
			line += " " + *ev.Tool.Name + " " + *ev.Tool.CallID
		}
		if ev.Tool.IsError != nil {
			t.Fatalf("is_error set from a part with no error field: %+v", ev)
		}
		got = append(got, line)
	}
	want := []string{
		"meta cursor_cli_store_json",
		"meta 0",
		"message 30",
		"unknown 10",
		"tool_call tool-call Read tc-1",
		"tool_call tool-call mcp__pond-docs__search tc-2",
		"tool_call tool-call AwaitShell tc-3",
		"unknown 20",
		"tool_result tool-result Read tc-1",
		"tool_result tool-result mcp__pond-docs__search tc-2",
		"tool_result tool-result AwaitShell tc-3",
		"tool_result tool-result Grep tc-9",
		"unknown 01",
		"message 05",
		"unknown 40",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	asstEv := events[3]
	content, _ := asstEv.Extra["content"].([]any)
	if len(content) != 1 || asstEv.Extra["blob_id"] != asst || asstEv.Extra["id"] != "m2" {
		t.Fatalf("assistant blob keeps its other parts only: %#v", asstEv.Extra)
	}
	read, mcp, await := events[4], events[5], events[6]
	if *read.ContentText != `{"path":"main.go"}` || read.Extra["builtin"] != true || read.Extra["blob_id"] != asst || read.Actor != ActorAssistant {
		t.Fatalf("read call: %+v", read)
	}
	if !strings.Contains(*mcp.ContentText, `"namespace":"pond-docs"`) || mcp.Extra["mcp_server"] != "pond-docs" || mcp.Extra["cursor_tool_name"] != "CallDynamicTool" || mcp.Extra["builtin"] != nil {
		t.Fatalf("mcp call: %+v", mcp)
	}
	if await.Extra["builtin"] != true || await.Extra["cursor_tool_name"] != "CallDynamicTool" || await.Extra["mcp_server"] != nil {
		t.Fatalf("cursor-namespace call: %+v", await)
	}
	readResult, mcpResult := events[8], events[9]
	if *readResult.ContentText != "package main" || readResult.Actor != ActorTool || readResult.Extra["blob_id"] != tool || readResult.Extra["experimental_content"] == nil {
		t.Fatalf("read result: %+v", readResult)
	}
	if *mcpResult.ContentText != `{"hits":1}` || mcpResult.Extra["cursor_tool_name"] != "CallDynamicTool" {
		t.Fatalf("mcp result: %+v", mcpResult)
	}
	if content, _ := events[7].Extra["content"].([]any); content == nil || len(content) != 0 {
		t.Fatalf("tool blob content: %#v", events[7].Extra["content"])
	}
	if root := events[12]; root.Extra["value"] == nil || root.ContentText != nil {
		t.Fatalf("root blob stays an opaque unknown event: %+v", root)
	}
}

// After a compaction the latest root starts a new branch from the
// system message. The old branch comes first, then the summary, then
// what both kept, then what is new.
func TestCursorCLIOrderAfterCompaction(t *testing.T) {
	s, a1, a2, sum, b1 := cliBlobID(0x50), cliBlobID(0x40), cliBlobID(0x30), cliBlobID(0x20), cliBlobID(0x10)
	before, after := cliBlobID(0x02), cliBlobID(0x01)
	msg := func(id, role, text string) map[string]any {
		return map[string]any{"id": id, "data": map[string]any{"role": role, "content": text}}
	}
	raw := cursorCLIRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/sid-1/store.db",
		"scope":           "session",
		"meta":            []any{map[string]any{"key": "0", "value": map[string]any{"latestRootBlobId": after}}},
		"blobs": []any{
			map[string]any{"id": after, "data": cliRoot(s, sum, a2, b1)},
			map[string]any{"id": before, "data": cliRoot(s, a1, a2)},
			msg(b1, "user", "b1"),
			msg(sum, "user", "summary"),
			msg(a2, "assistant", "a2"),
			msg(a1, "user", "a1"),
			msg(s, "user", "s"),
		},
	})
	var got []string
	for _, ev := range projectCursorCLI(t, "chats/ab12/sid-1", raw) {
		if ev.EventType == EventMessage {
			got = append(got, *ev.ContentText)
		}
	}
	if want := "s a1 summary a2 b1"; strings.Join(got, " ") != want {
		t.Fatalf("order %v, want %s", got, want)
	}
	for _, ev := range projectCursorCLI(t, "chats/ab12/sid-1", raw) {
		if _, ok := ev.Extra["order"]; ok {
			t.Fatalf("one older branch is placed by the roots, got order on %+v", ev)
		}
	}
}

// After two compactions the two older branches share only the system
// message, so no root says which came first. Hash order here puts the
// middle branch before the oldest. Each branch must stay whole and in
// its own order, the latest root's branch last, and every event of the
// two older branches must say its place is not established.
func TestCursorCLIOrderAfterTwoCompactions(t *testing.T) {
	s := cliBlobID(0x90)
	a1, a2 := cliBlobID(0x81), cliBlobID(0x80) // oldest branch
	sum1, b1, b2 := cliBlobID(0x12), cliBlobID(0x11), cliBlobID(0x10)
	sum2, c1 := cliBlobID(0x51), cliBlobID(0x50)
	r1, r2, r3 := cliBlobID(0x03), cliBlobID(0x02), cliBlobID(0x01)
	msg := func(id, role, text string) map[string]any {
		return map[string]any{"id": id, "data": map[string]any{"role": role, "content": text}}
	}
	raw := cursorCLIRaw(t, map[string]any{
		"harness_version": "1",
		"confidence":      "low",
		"source":          "chats/ab12/sid-1/store.db",
		"scope":           "session",
		"meta":            []any{map[string]any{"key": "0", "value": map[string]any{"latestRootBlobId": r3}}},
		"blobs": []any{
			map[string]any{"id": r3, "data": cliRoot(s, sum2, c1)},
			map[string]any{"id": r2, "data": cliRoot(s, sum1, b1, b2)},
			map[string]any{"id": r1, "data": cliRoot(s, a1, a2)},
			msg(b2, "assistant", "b2"),
			msg(b1, "user", "b1"),
			msg(sum1, "user", "sum1"),
			msg(c1, "user", "c1"),
			msg(sum2, "user", "sum2"),
			msg(a2, "assistant", "a2"),
			msg(a1, "user", "a1"),
			msg(s, "user", "s"),
		},
	})
	var got []string
	unsure := map[string]bool{}
	for _, ev := range projectCursorCLI(t, "chats/ab12/sid-1", raw) {
		if ev.EventType != EventMessage {
			continue
		}
		got = append(got, *ev.ContentText)
		if ev.Extra["order"] == "unestablished" {
			unsure[*ev.ContentText] = true
		}
	}
	order := strings.Join(got, " ")
	if order != "s sum1 b1 b2 a1 a2 sum2 c1" && order != "s a1 a2 sum1 b1 b2 sum2 c1" {
		t.Fatalf("order %s: each older branch must stay whole, the latest last", order)
	}
	for _, text := range []string{"a1", "a2", "sum1", "b1", "b2"} {
		if !unsure[text] {
			t.Errorf("%s is in an older branch whose place no root gives, want order unestablished", text)
		}
	}
	for _, text := range []string{"s", "sum2", "c1"} {
		if unsure[text] {
			t.Errorf("%s is in the latest root, its place is known", text)
		}
	}
}

// A version 2 export carries each blob's rowid as seq. With a seq on
// every root, the older branches follow in the order their roots were
// written, whatever the hash order, and no event is unestablished. A
// root without a usable seq leaves the order as a version 1 export has
// it.
func TestCursorCLIOrderBySeq(t *testing.T) {
	s := cliBlobID(0x90)
	a1, a2 := cliBlobID(0x81), cliBlobID(0x80)
	sum1, b1, b2 := cliBlobID(0x12), cliBlobID(0x11), cliBlobID(0x10)
	sum2, c1 := cliBlobID(0x51), cliBlobID(0x50)
	r1, r1b, r2, r3 := cliBlobID(0x04), cliBlobID(0x03), cliBlobID(0x02), cliBlobID(0x01)
	for _, tc := range []struct {
		name   string
		seq    map[string]any // root id to seq; a missing root has none
		want   string
		unsure bool
	}{
		{"a first", map[string]any{r1: 10, r1b: 12, r2: 20, r3: 30}, "s a1 a2 sum1 b1 b2 sum2 c1", false},
		{"b first", map[string]any{r2: 10, r1: 20, r1b: 22, r3: 30}, "s sum1 b1 b2 a1 a2 sum2 c1", false},
		{"one root without seq", map[string]any{r1: 10, r1b: 12, r3: 30}, "", true},
		{"seq not an integer", map[string]any{r1: 10, r1b: 12, r2: "20", r3: 30}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blob := func(id string, data any) map[string]any {
				row := map[string]any{"id": id, "data": data}
				if seq, ok := tc.seq[id]; ok {
					row["seq"] = seq
				}
				return row
			}
			msg := func(id, role, text string) map[string]any {
				// Message seqs oppose the roots' order, so only the roots' count.
				return map[string]any{"id": id, "seq": 1000 - len(text), "data": map[string]any{"role": role, "content": text}}
			}
			raw := cursorCLIRaw(t, map[string]any{
				"harness_version": "2",
				"confidence":      "low",
				"source":          "chats/ab12/sid-1/store.db",
				"scope":           "session",
				"meta":            []any{map[string]any{"key": "0", "value": map[string]any{"latestRootBlobId": r3}}},
				"blobs": []any{
					blob(r3, cliRoot(s, sum2, c1)),
					blob(r2, cliRoot(s, sum1, b1, b2)),
					blob(r1b, cliRoot(s, a1, a2)),
					blob(r1, cliRoot(s, a1)),
					msg(b2, "assistant", "b2"),
					msg(b1, "user", "b1"),
					msg(sum1, "user", "sum1"),
					msg(c1, "user", "c1"),
					msg(sum2, "user", "sum2"),
					msg(a2, "assistant", "a2"),
					msg(a1, "user", "a1"),
					msg(s, "user", "s"),
				},
			})
			var got []string
			unsure := 0
			for _, ev := range projectCursorCLI(t, "chats/ab12/sid-1", raw) {
				if ev.Extra["order"] == "unestablished" {
					unsure++
				}
				if ev.EventType == EventMessage {
					got = append(got, *ev.ContentText)
				}
			}
			if tc.want != "" && strings.Join(got, " ") != tc.want {
				t.Fatalf("order %v, want %s", got, tc.want)
			}
			if tc.unsure != (unsure > 0) {
				t.Fatalf("%d events unestablished, want any: %v", unsure, tc.unsure)
			}
		})
	}
}

// BenchmarkCursorCLITools projects an export of 600 turns, each an
// assistant blob with one tool call and a tool blob with a 4 KiB result.
// Cursor writes a root per turn that lists every message so far, so the
// roots grow with the square of the turns, as they do in a real store.
func BenchmarkCursorCLITools(b *testing.B) {
	raw := cursorCLIToolExport(b, 600)
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := (CursorCLI{NativeID: "chats/ab12/sid-1"}).Normalize(context.Background(), raw); err != nil {
			b.Fatal(err)
		}
	}
}

func cursorCLIToolExport(b *testing.B, turns int) []byte {
	body := strings.Repeat("pond water ", 400)
	var blobs []any
	var chain []string
	n := 1
	next := func() string {
		n++
		return fmt.Sprintf("%064x", n)
	}
	latest := ""
	for i := 0; i < turns; i++ {
		asst, tool, root := next(), next(), next()
		call := fmt.Sprintf("tc-%d", i)
		blobs = append(blobs,
			map[string]any{"id": asst, "data": map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "text", "text": "reading"},
				map[string]any{"type": "tool-call", "toolCallId": call, "toolName": "Read", "args": map[string]any{"path": "main.go"}},
			}}},
			map[string]any{"id": tool, "data": map[string]any{"role": "tool", "content": []any{
				map[string]any{"type": "tool-result", "toolCallId": call, "toolName": "Read", "result": body},
			}}},
		)
		chain = append(chain, asst, tool)
		blobs = append(blobs, map[string]any{"id": root, "data": cliRoot(chain...)})
		latest = root
	}
	sort.Slice(blobs, func(i, j int) bool {
		return blobs[i].(map[string]any)["id"].(string) < blobs[j].(map[string]any)["id"].(string)
	})
	raw, err := json.Marshal(map[string]any{
		"harness_version": "1", "confidence": "low", "source": "store.db", "scope": "session",
		"meta":  []any{map[string]any{"key": "0", "value": map[string]any{"latestRootBlobId": latest}}},
		"blobs": blobs,
	})
	if err != nil {
		b.Fatal(err)
	}
	return raw
}

func projectCursorCLI(t *testing.T, native string, raw []byte) []Event {
	t.Helper()
	events, err := (CursorCLI{
		Now:            time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC),
		NativeID:       native,
		HarnessVersion: cursorcli.Version,
		CWD:            "/work/app",
		ProjectID:      "local",
	}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func cursorCLIRaw(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func cursorCLIRawTypes(events []Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.RawType
	}
	return out
}

// Each row's content_ref offset is where that row sits, found by a
// search that moves forward through the document. A blob id that an
// earlier blob names as a field value does not pull the later row's
// offset back to that field.
func TestCursorCLIOffsetsFollowTheRows(t *testing.T) {
	// The adapter's field order: meta before blobs, id before data.
	raw := []byte(`{"harness_version":"1","confidence":"low","source":"acp-sessions/s/store.db","scope":"session",` +
		`"meta":[{"key":"0","value":{"name":"pond"}}],` +
		`"blobs":[{"id":"aaa","data":{"role":"user","content":"first","next":"bbb"}},` +
		`{"id":"bbb","data":{"role":"assistant","content":"seen"}}]}`)
	at := cursorCLIOffsets(t, raw)
	want := map[string]int{
		"aaa": bytes.Index(raw, []byte(`"id":"aaa"`)) + len(`"id":`),
		"bbb": bytes.Index(raw, []byte(`"id":"bbb"`)) + len(`"id":`),
	}
	for id, w := range want {
		if at[id] != w {
			t.Fatalf("%s at %d, want %d (offsets %v)", id, at[id], w, at)
		}
	}
}

// Sections and fields in another order: blobs before meta, and the meta
// record names blobs aaa and zzz in nested ids. Each offset is the
// row's own top-level field. Blob zzz writes its id with a space,
// carries the same id, compact, in its data, and repeats id as null,
// which the parse ignores; its offset is still its own id. Blob yyy has
// no id field; its offset is its first byte. Blob xxx spells its id
// field ID, which the parse reads as id.
func TestCursorCLIOffsetsStayInTheirRow(t *testing.T) {
	raw := []byte(`{"harness_version":"1","confidence":"low","source":"s","scope":"session",` +
		`"blobs":[{"data":{"role":"user","content":"hi"},"id":"aaa"},{"id": "zzz","data":{"id":"zzz"},"id":null},{"data":{"id":"yyy"}},{"ID":"xxx"}],` +
		`"meta":[{"value":{"name":"pond","id":"aaa","x":{"id":"zzz"}},"key":"0"}]}`)
	at := cursorCLIOffsets(t, raw)
	want := map[string]int{
		"aaa": bytes.Index(raw, []byte(`"id":"aaa"},`)) + len(`"id":`),
		"zzz": bytes.Index(raw, []byte(`{"id": "zzz"`)) + len(`{"id": `),
		"0":   bytes.Index(raw, []byte(`"key":"0"`)) + len(`"key":`),
		"":    bytes.Index(raw, []byte(`{"data":{"id":"yyy"}}`)),
		"xxx": bytes.Index(raw, []byte(`"ID":"xxx"`)) + len(`"ID":`),
	}
	for k, w := range want {
		if at[k] != w {
			t.Fatalf("%q at %d, want %d (offsets %v)", k, at[k], w, at)
		}
	}
}

func cursorCLIOffsets(t *testing.T, raw []byte) map[string]int {
	t.Helper()
	c := CursorCLI{Now: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), NativeID: "acp-sessions/s", HarnessVersion: "1", CWD: "/work/app", ProjectID: "local", Digest: "d"}
	events, err := c.Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	at := map[string]int{}
	for _, ev := range events {
		if ev.ContentRef == nil {
			continue
		}
		_, off, _ := strings.Cut(*ev.ContentRef, "#")
		n, err := strconv.Atoi(off)
		if err != nil {
			t.Fatal(err)
		}
		at[ev.RawType] = n
	}
	return at
}

package normalize

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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
					"name": "hello pond", "refreshToken": secret,
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

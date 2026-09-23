package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestShareGPTKeepsLineageAndOpaqueContent(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	events, err := (Terva{
		Now:    time.Date(2026, 9, 22, 16, 30, 0, 0, time.UTC),
		Digest: digest,
	}).Normalize(context.Background(), fixtureTranscript())
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := ShareGPT("ses_1", digest, events)
	if !ok {
		t.Fatal("expected a trajectory")
	}
	if rec.RawSHA256 != digest || rec.SessionUID != "ses_1" {
		t.Fatalf("lineage: %+v", rec)
	}
	if rec.SessionID != "terva:20260922-161000-abcd1234" {
		t.Fatalf("session id %s", rec.SessionID)
	}

	want := []ShareGPTTurn{
		{From: "human", Value: proofPrompt},
		{From: "gpt", Value: "thinking", EncryptedContent: opaqueBlob},
		{From: "gpt", Value: "hello"},
		{From: "gpt", Value: `{"command":"ls"}`, Name: "bash", CallID: "call_1"},
		{From: "tool", Value: "main.go", CallID: "call_1"},
		{From: "system", Value: "summary of the pond", EncryptedContent: opaqueBlob},
	}
	if len(rec.Conversations) != len(want) {
		t.Fatalf("turns %d: %#v", len(rec.Conversations), rec.Conversations)
	}
	for i, turn := range rec.Conversations {
		if turn.From != want[i].From || turn.Value != want[i].Value || turn.Name != want[i].Name || turn.CallID != want[i].CallID {
			t.Fatalf("turn %d: %+v", i, turn)
		}
		if turn.EncryptedContent != want[i].EncryptedContent {
			t.Fatalf("turn %d ciphertext: %#v", i, turn.EncryptedContent)
		}
		if strings.Contains(turn.Value, opaqueBlob) {
			t.Fatalf("turn %d value holds ciphertext: %s", i, turn.Value)
		}
	}

	var buf bytes.Buffer
	if err := WriteShareGPT(&buf, []ShareGPTRecord{rec}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("aGVsbG8=")) {
		t.Fatal("image bytes were copied into the trajectory")
	}
	if !bytes.Contains(buf.Bytes(), []byte(opaqueBlob)) {
		t.Fatal("ciphertext was dropped")
	}
	var got ShareGPTRecord
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.RawSHA256 != digest {
		t.Fatalf("encoded digest %s", got.RawSHA256)
	}
	if got.Conversations[1].EncryptedContent != opaqueBlob {
		t.Fatalf("encoded ciphertext %#v", got.Conversations[1].EncryptedContent)
	}
}

func TestShareGPTCopiesNonStringCiphertext(t *testing.T) {
	wrapped := map[string]any{"wrapped": opaqueBlob}
	text := "visible"
	events := []Event{{
		SessionID:   "terva:sid",
		EventType:   EventMessage,
		Actor:       ActorAssistant,
		Role:        strPtr(ActorAssistant),
		ContentText: &text,
		Extra:       extraMap{"encrypted_content": wrapped},
	}}
	rec, ok := ShareGPT("ses_2", strings.Repeat("cd", 32), events)
	if !ok {
		t.Fatal("expected a trajectory")
	}
	got, ok := rec.Conversations[0].EncryptedContent.(map[string]any)
	if !ok || got["wrapped"] != opaqueBlob {
		t.Fatalf("ciphertext %#v", rec.Conversations[0].EncryptedContent)
	}
	if strings.Contains(rec.Conversations[0].Value, opaqueBlob) {
		t.Fatal("ciphertext was written into value")
	}
}

func TestShareGPTStripsPlaintextAndLeavesCiphertext(t *testing.T) {
	aws := "AKIAIOSFODNN7EXAMPLE"
	pat := "ghp_" + strings.Repeat("a", 36)
	slack := "xoxb-1234567890-abcdefghij"
	text := "key " + aws + " end"
	name := "tool-" + pat
	callID := slack
	opaque := "cipher-" + aws
	events := []Event{{
		SessionID:   "terva:sid",
		EventType:   EventToolCall,
		Actor:       ActorAssistant,
		Role:        strPtr(ActorAssistant),
		ContentText: &text,
		Tool:        Tool{Name: &name, CallID: &callID},
		Extra:       extraMap{"encrypted_content": opaque},
	}}
	digest := strings.Repeat("ab", 32)
	rec, ok := ShareGPT("ses_secret", digest, events)
	if !ok {
		t.Fatal("expected a trajectory")
	}
	turn := rec.Conversations[0]
	if turn.Value != "key [redacted:aws-access-key-id] end" {
		t.Fatalf("value %q", turn.Value)
	}
	if turn.Name != "tool-[redacted:github-pat]" {
		t.Fatalf("name %q", turn.Name)
	}
	if turn.CallID != "[redacted:slack-token]" {
		t.Fatalf("call id %q", turn.CallID)
	}
	if turn.EncryptedContent != opaque {
		t.Fatalf("ciphertext %#v", turn.EncryptedContent)
	}
	if rec.RawSHA256 != digest || rec.SessionID != "terva:sid" {
		t.Fatalf("lineage rewritten: %+v", rec)
	}
	if *events[0].ContentText != text || *events[0].Tool.Name != name || *events[0].Tool.CallID != callID {
		t.Fatal("normalized event was rewritten")
	}
	if events[0].Extra["encrypted_content"] != opaque {
		t.Fatal("ciphertext on the event was rewritten")
	}
}

func TestShareGPTOmitsUntraceableAndEmpty(t *testing.T) {
	text := "hello"
	events := []Event{{
		EventType:   EventMessage,
		Actor:       ActorUser,
		ContentText: &text,
	}}
	if _, ok := ShareGPT("ses", "", events); ok {
		t.Fatal("empty digest produced a row")
	}
	meta := []Event{{EventType: EventMeta, Actor: ActorHarness}}
	if _, ok := ShareGPT("ses", strings.Repeat("ab", 32), meta); ok {
		t.Fatal("meta-only session produced a row")
	}
}

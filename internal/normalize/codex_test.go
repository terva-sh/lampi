package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/codex"
	"terva.sh/lampi/internal/protocol"
)

const codexNative = "11111111-2222-4333-8444-555555555555"

func TestCodexSchemaOpaqueAndUnknownKeys(t *testing.T) {
	raw := codexFixture()
	before := append([]byte(nil), raw...)
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	digest := strings.Repeat("ab", 32)
	events, err := (Codex{
		Now:            now,
		NativeID:       codexNative,
		ParentNativeID: "parent-session",
		HarnessVersion: codex.Version,
		ProjectID:      "github.com/org/app@abc",
		GitCommit:      "abc123",
		Digest:         digest,
	}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("normalize changed the raw bytes")
	}
	if len(events) != 15 {
		t.Fatalf("events: %d", len(events))
	}

	var meta, turn, user, taskStart, reasoning, call, result, assistant, noted, usage, errEv, compaction, taskDone, image, future *Event
	for i := range events {
		ev := &events[i]
		if ev.SchemaVersion != SchemaVersion || ev.Harness != protocol.HarnessCodex {
			t.Fatalf("header: %+v", ev)
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != codex.Version || *ev.HarnessVersion == "0.121.0" {
			t.Fatalf("harness version: %v", ev.HarnessVersion)
		}
		if ev.SessionID != "codex:"+codexNative {
			t.Fatalf("session id %s", ev.SessionID)
		}
		if ev.ParentSessionID == nil || *ev.ParentSessionID != "codex:parent-session" {
			t.Fatalf("parent: %v", ev.ParentSessionID)
		}
		if strings.Contains(*ev.ParentSessionID, "parent-from-line") {
			t.Fatalf("parent_thread_id was used as the parent session: %s", *ev.ParentSessionID)
		}
		if ev.CWDHash != adapter.CWDHash("/work/app") {
			t.Fatalf("cwd hash %s", ev.CWDHash)
		}
		if ev.ProjectID == nil || *ev.ProjectID != "github.com/org/app@abc" {
			t.Fatalf("project id %v", ev.ProjectID)
		}
		if ev.IngestedAt != now.UTC().Format(time.RFC3339Nano) {
			t.Fatalf("ingested %s", ev.IngestedAt)
		}
		if ev.EventID == "" || ev.Redaction.Status != "none" || ev.Redaction.Ruleset != "v1" {
			t.Fatalf("id/redaction: %+v", ev)
		}
		if ev.Git.Commit == nil || *ev.Git.Commit != "abc123" || ev.Git.Branch == nil || *ev.Git.Branch != "main" {
			t.Fatalf("git: %+v", ev.Git)
		}
		if ev.ContentRef == nil || !strings.HasPrefix(*ev.ContentRef, "sha256/"+digest+"#") {
			t.Fatalf("content_ref %v", ev.ContentRef)
		}
		if ev.ContentText != nil && (strings.Contains(*ev.ContentText, opaqueBlob) || strings.Contains(*ev.ContentText, "aGVsbG8=") || strings.Contains(*ev.ContentText, "do-not-promote")) {
			t.Fatalf("opaque or image or lifecycle text leaked into content_text: %s", *ev.ContentText)
		}
		switch {
		case ev.RawType == "session_meta":
			meta = ev
		case ev.RawType == "turn_context":
			turn = ev
		case ev.EventType == EventMessage && ev.Role != nil && *ev.Role == ActorUser && ev.ContentText != nil && *ev.ContentText == proofPrompt:
			user = ev
		case ev.RawType == "task_started":
			taskStart = ev
		case ev.EventType == EventMessage && ev.ContentText != nil && *ev.ContentText == "look at the pond":
			reasoning = ev
		case ev.EventType == EventToolCall && ev.Tool.Name != nil && *ev.Tool.Name == "exec_command":
			call = ev
		case ev.EventType == EventToolResult && ev.ContentText != nil && *ev.ContentText == "package main":
			result = ev
		case ev.EventType == EventMessage && ev.ContentText != nil && *ev.ContentText == "I'll read the file.":
			assistant = ev
		case ev.EventType == EventMessage && ev.ContentText != nil && *ev.ContentText == "noted.":
			noted = ev
		case ev.EventType == EventUsage:
			usage = ev
		case ev.EventType == EventError:
			errEv = ev
		case ev.EventType == EventCompaction:
			compaction = ev
		case ev.RawType == "task_complete":
			taskDone = ev
		case ev.RawType == "image_generation_call":
			image = ev
		case ev.RawType == "future_event":
			future = ev
		}
	}

	if meta == nil || meta.EventType != EventMeta || meta.ContentText != nil {
		t.Fatalf("session meta: %+v", meta)
	}
	if meta.Extra["cli_version"] != "0.121.0" || meta.Extra["originator"] != "codex_cli_rs" {
		t.Fatalf("cli identity dropped or promoted: %#v", meta.Extra)
	}
	if meta.Extra["parent_thread_id"] != "parent-from-line" {
		t.Fatalf("parent_thread_id: %#v", meta.Extra["parent_thread_id"])
	}
	field, _ := meta.Extra["future_meta"].(map[string]any)
	if field["n"] != float64(1) {
		t.Fatalf("future_meta: %#v", meta.Extra["future_meta"])
	}
	gitInfo, _ := meta.Extra["git"].(map[string]any)
	if gitInfo["commit_hash"] != "deadbeef" || gitInfo["branch"] != "main" || gitInfo["repository_url"] != "https://github.com/org/app" {
		t.Fatalf("git payload: %#v", meta.Extra["git"])
	}
	instructions, _ := meta.Extra["base_instructions"].(map[string]any)
	if instructions["text"] != "You are Codex." {
		t.Fatalf("base instructions: %#v", meta.Extra["base_instructions"])
	}
	if meta.Model.Provider == nil || *meta.Model.Provider != "openai" || meta.Model.ID != nil {
		t.Fatalf("session model: %+v", meta.Model)
	}

	if turn == nil || turn.EventType != EventMeta || turn.Model.ID == nil || *turn.Model.ID != "gpt-5.4" {
		t.Fatalf("turn context: %+v", turn)
	}
	if turn.Extra["approval_policy"] != "on-request" || turn.Extra["future_turn"] != true {
		t.Fatalf("turn extra: %#v", turn.Extra)
	}
	policy, _ := turn.Extra["sandbox_policy"].(map[string]any)
	if policy["type"] != "workspace-write" {
		t.Fatalf("sandbox: %#v", turn.Extra["sandbox_policy"])
	}

	if user == nil || user.Actor != ActorUser {
		t.Fatalf("user event: %+v", user)
	}
	if user.Model.ID == nil || *user.Model.ID != "gpt-5.4" || user.Model.Provider == nil || *user.Model.Provider != "openai" {
		t.Fatalf("user model: %+v", user.Model)
	}
	kept, _ := user.Extra["future_field"].(map[string]any)
	if kept["n"] != float64(1) {
		t.Fatalf("future_field: %#v", user.Extra["future_field"])
	}
	if user.RecordedAt != "2026-09-22T16:10:01.477Z" {
		t.Fatalf("user recorded_at %s", user.RecordedAt)
	}
	if _, ok := user.Extra["images"].([]any); !ok {
		t.Fatalf("images dropped: %#v", user.Extra["images"])
	}

	if taskStart == nil || taskStart.EventType != EventUnknown || taskStart.ContentText != nil {
		t.Fatalf("task started: %+v", taskStart)
	}
	if taskStart.Extra["turn_id"] != "turn-1" {
		t.Fatalf("task started extra: %#v", taskStart.Extra)
	}

	if reasoning == nil || reasoning.Actor != ActorAssistant || reasoning.Role == nil || *reasoning.Role != ActorAssistant {
		t.Fatalf("reasoning: %+v", reasoning)
	}
	if reasoning.Extra["encrypted_content"] != opaqueBlob {
		t.Fatalf("ciphertext: %#v", reasoning.Extra["encrypted_content"])
	}
	if reasoning.Extra["id"] != "rs_01" {
		t.Fatalf("reasoning id: %#v", reasoning.Extra["id"])
	}

	if call == nil || call.Actor != ActorAssistant || call.Tool.CallID == nil || *call.Tool.CallID != "call_1" {
		t.Fatalf("call: %+v", call)
	}
	if call.ContentText == nil || !strings.Contains(*call.ContentText, "main.go") {
		t.Fatalf("call arguments: %v", call.ContentText)
	}
	if result == nil || result.Actor != ActorTool || result.Tool.CallID == nil || *result.Tool.CallID != "call_1" || result.Tool.IsError != nil {
		t.Fatalf("result: %+v", result)
	}
	if assistant == nil || assistant.Actor != ActorAssistant || assistant.Role == nil || *assistant.Role != ActorAssistant {
		t.Fatalf("assistant: %+v", assistant)
	}
	if noted == nil || noted.RawType != "agent_message" {
		t.Fatalf("agent message: %+v", noted)
	}

	if usage == nil || usage.Usage.Input == nil || *usage.Usage.Input != 10 || usage.Usage.Output == nil || *usage.Usage.Output != 4 {
		t.Fatalf("usage: %+v", usage)
	}
	if usage.Usage.CacheRead == nil || *usage.Usage.CacheRead != 1 || usage.Usage.CacheWrite != nil || usage.Usage.CostUSD != nil {
		t.Fatalf("cache: %+v", usage.Usage)
	}
	info, _ := usage.Extra["info"].(map[string]any)
	last, _ := info["last_token_usage"].(map[string]any)
	if last["reasoning_output_tokens"] != float64(2) {
		t.Fatalf("reasoning tokens: %#v", usage.Extra["info"])
	}
	total, _ := info["total_token_usage"].(map[string]any)
	if total["input_tokens"] != float64(100) {
		t.Fatalf("total usage was used as the turn or dropped: %#v", total)
	}
	limits, _ := usage.Extra["rate_limits"].(map[string]any)
	primary, _ := limits["primary"].(map[string]any)
	if primary["used_percent"] != 1.5 {
		t.Fatalf("rate limits: %#v", usage.Extra["rate_limits"])
	}

	if errEv == nil || errEv.ContentText == nil || *errEv.ContentText != "sandbox refused" || errEv.Actor != ActorHarness {
		t.Fatalf("error: %+v", errEv)
	}
	if compaction == nil || compaction.ContentText == nil || *compaction.ContentText != "summary of the pond" {
		t.Fatalf("compaction: %+v", compaction)
	}
	if taskDone == nil || taskDone.EventType != EventUnknown || taskDone.ContentText != nil || taskDone.Extra["last_agent_message"] != "do-not-promote" {
		t.Fatalf("task complete: %+v", taskDone)
	}
	if image == nil || image.EventType != EventUnknown || image.ContentText != nil || image.Extra["data_bytes"] != 5 {
		t.Fatalf("image: %+v", image)
	}
	if image.Extra["status"] != "completed" {
		t.Fatalf("image status: %#v", image.Extra)
	}
	if future == nil || future.EventType != EventUnknown || future.ContentText != nil || future.Extra["future_queue"] != true {
		t.Fatalf("future: %+v", future)
	}
	if future.RecordedAt != future.IngestedAt {
		t.Fatalf("future recorded_at %s ingested %s", future.RecordedAt, future.IngestedAt)
	}
	day, err := PartitionDate(future.RecordedAt, future.IngestedAt)
	if err != nil || day != "2026-09-23" {
		t.Fatalf("future partition %s err %v", day, err)
	}
	day, err = PartitionDate(user.RecordedAt, user.IngestedAt)
	if err != nil || day != "2026-09-22" {
		t.Fatalf("user partition %s err %v", day, err)
	}

	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(opaqueBlob)) {
		t.Fatal("opaque ciphertext was dropped from the projection")
	}
	if bytes.Contains(encoded, []byte("aGVsbG8=")) {
		t.Fatal("image bytes were copied into the projection")
	}
	if strings.Count(string(encoded), opaqueBlob) != 1 {
		t.Fatalf("ciphertext copies %d", strings.Count(string(encoded), opaqueBlob))
	}
	if bytes.Contains(encoded, []byte("AKIA")) || bytes.Contains(encoded, []byte("sk-")) {
		t.Fatal("projection invented a credential")
	}
}

func TestCodexSessionIDPrefersNative(t *testing.T) {
	raw := []byte(`{"timestamp":"2026-09-22T16:10:00Z","type":"session_meta","payload":{"id":"from-line","cwd":"/work/app","parent_thread_id":"other-thread"}}` + "\n" +
		`{"timestamp":"2026-09-22T16:10:01Z","type":"event_msg","payload":{"type":"user_message","message":"hi"}}` + "\n")
	events, err := (Codex{NativeID: "from-manifest", Now: time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].SessionID != "codex:from-manifest" || events[1].SessionID != "codex:from-manifest" {
		t.Fatalf("events %+v", events)
	}
	if events[1].ParentSessionID != nil {
		t.Fatalf("parent from parent_thread_id: %v", events[1].ParentSessionID)
	}
	events, err = (Codex{}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].SessionID != "codex:from-line" {
		t.Fatalf("session %s", events[0].SessionID)
	}
}

func TestCodexHistoryIsNotASession(t *testing.T) {
	raw := []byte(`{"session_id":"not-a-rollout","ts":1710000000,"text":"history prompt"}` + "\n")
	before := append([]byte(nil), raw...)
	events, err := (Codex{}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("normalize changed the history bytes")
	}
	if len(events) != 1 {
		t.Fatalf("events %+v", events)
	}
	ev := events[0]
	if ev.SessionID != "codex:unknown" || ev.EventType != EventUnknown || ev.ContentText != nil {
		t.Fatalf("history was projected as a session: %+v", ev)
	}
	if ev.Extra["session_id"] != "not-a-rollout" || ev.Extra["text"] != "history prompt" {
		t.Fatalf("history keys: %#v", ev.Extra)
	}
}

func TestCodexNormalizeErrorOmitsLine(t *testing.T) {
	raw := []byte("not-json sk-live-secret\n{\"type\":\"session_meta\"}\n")
	before := append([]byte(nil), raw...)
	_, err := (Codex{}).Normalize(context.Background(), raw)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "line 1 is not a JSON object") {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "sk-live-secret") || strings.Contains(err.Error(), "not-json") {
		t.Fatalf("error includes the raw line: %s", err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("failed normalize changed the raw bytes")
	}

	_, err = (Codex{}).Normalize(context.Background(), []byte("[]\n"))
	if err == nil || strings.Contains(err.Error(), "[]") {
		t.Fatalf("array line: %v", err)
	}
}

func codexFixture() []byte {
	lines := []string{
		`{"timestamp":"2026-09-22T16:10:00.100Z","type":"session_meta","payload":{"id":"` + codexNative + `","timestamp":"2026-09-22T16:10:00.100Z","cwd":"/work/app","originator":"codex_cli_rs","cli_version":"0.121.0","source":"cli","model_provider":"openai","base_instructions":{"text":"You are Codex."},"git":{"commit_hash":"deadbeef","branch":"main","repository_url":"https://github.com/org/app"},"parent_thread_id":"parent-from-line","future_meta":{"n":1}}}`,
		`{"timestamp":"2026-09-22T16:10:00.500Z","type":"turn_context","payload":{"turn_id":"turn-1","cwd":"/work/app","model":"gpt-5.4","approval_policy":"on-request","sandbox_policy":{"type":"workspace-write"},"future_turn":true}}`,
		`{"timestamp":"2026-09-22T16:10:01.477Z","type":"event_msg","payload":{"type":"user_message","message":"` + proofPrompt + `","images":[],"local_images":[],"text_elements":[]},"future_field":{"n":1}}`,
		`{"timestamp":"2026-09-22T16:10:01.500Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1","model_context_window":258400}}`,
		`{"timestamp":"2026-09-22T16:10:02.000Z","type":"response_item","payload":{"type":"reasoning","id":"rs_01","summary":[{"type":"summary_text","text":"look at the pond"}],"encrypted_content":"` + opaqueBlob + `"}}`,
		`{"timestamp":"2026-09-22T16:10:02.100Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"cat main.go\"}"}}`,
		`{"timestamp":"2026-09-22T16:10:02.200Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"package main"}}`,
		`{"timestamp":"2026-09-22T16:10:02.300Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I'll read the file."}]}}`,
		`{"timestamp":"2026-09-22T16:10:02.350Z","type":"event_msg","payload":{"type":"agent_message","message":"noted."}}`,
		`{"timestamp":"2026-09-22T16:10:02.400Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":30,"reasoning_output_tokens":5,"total_tokens":130},"last_token_usage":{"input_tokens":10,"cached_input_tokens":1,"output_tokens":4,"reasoning_output_tokens":2,"total_tokens":14},"model_context_window":258400},"rate_limits":{"primary":{"used_percent":1.5}}}}`,
		`{"timestamp":"2026-09-22T16:10:03.000Z","type":"event_msg","payload":{"type":"error","message":"sandbox refused"}}`,
		`{"timestamp":"2026-09-22T16:11:00.000Z","type":"compacted","payload":{"message":"summary of the pond"}}`,
		`{"timestamp":"2026-09-22T16:10:04.000Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","last_agent_message":"do-not-promote"}}`,
		`{"timestamp":"2026-09-22T16:10:06.000Z","type":"response_item","payload":{"type":"image_generation_call","id":"ig_1","status":"completed","result":"aGVsbG8="}}`,
		`{"type":"future_event","future_queue":true}`,
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

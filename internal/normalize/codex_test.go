package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	if result.Tool.Name == nil || *result.Tool.Name != "exec_command" {
		t.Fatalf("result was not named from its call: %+v", result.Tool)
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
	// A line that is not a JSON object, among lines that are, becomes an
	// error event naming the line and its length, never its bytes
	// (TKT-01M3NQ2R).
	raw := []byte("not-json sk-live-secret\n{\"type\":\"session_meta\"}\n")
	before := append([]byte(nil), raw...)
	events, err := (Codex{}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("normalize changed the raw bytes")
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("sk-live-secret")) || bytes.Contains(encoded, []byte("not-json")) {
		t.Fatalf("events include the unreadable line: %s", encoded)
	}
	marker := events[0]
	if marker.EventType != EventError || marker.ContentText == nil || !strings.Contains(*marker.ContentText, "Line 1 of the raw file is not a JSON record") || marker.Extra["unreadable_line"] != 1 || marker.Extra["line_bytes"] != 23 {
		t.Fatalf("marker %+v", marker)
	}

	// A file in which no line is a JSON object is not this format: it
	// fails, and the error does not quote the line either.
	for _, bad := range []string{"not-json sk-live-secret\n", "[]\n", "[]\n\"text\"\n"} {
		_, err = (Codex{}).Normalize(context.Background(), []byte(bad))
		if err == nil || !strings.Contains(err.Error(), "line 1 is not a JSON object") || strings.Contains(err.Error(), "sk-live-secret") || strings.Contains(err.Error(), "[]") {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}

func TestCodexResultNamesFollowTheirOwnCall(t *testing.T) {
	// One call id is both a response_item function_call (shell) and an
	// event_msg exec_command_begin. Each result takes the name of the
	// call event of its own kind.
	raw := strings.Join([]string{
		`{"type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"call_1","arguments":"{}"}}`,
		`{"type":"event_msg","payload":{"type":"exec_command_begin","call_id":"call_1","command":["ls"]}}`,
		`{"type":"event_msg","payload":{"type":"exec_command_end","call_id":"call_1","stdout":"ok","exit_code":2}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"ok"}}`,
		`{"type":"event_msg","payload":{"type":"mcp_tool_call_begin","call_id":"call_2","invocation":{"server":"pond","tool":"read"}}}`,
		`{"type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":"call_2","result":{"Ok":{}}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_9","output":"orphan"}}`,
	}, "\n") + "\n"
	events, err := (Codex{NativeID: codexNative}).Normalize(context.Background(), []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ev := range events {
		if ev.EventType != EventToolResult {
			continue
		}
		name := "<nil>"
		if ev.Tool.Name != nil {
			name = *ev.Tool.Name
		}
		got = append(got, ev.RawType+"="+name)
	}
	want := []string{"exec_command_end=exec_command", "function_call_output=shell", "mcp_tool_call_end=mcp__pond__read", "function_call_output=<nil>"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("result names %v, want %v", got, want)
	}
}

func TestCodexMCPOutsideCodeModeKeepsServer(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"event_msg","payload":{"type":"mcp_tool_call_begin","call_id":"call_1","invocation":{"server":"pond docs.v2","tool":"read-page","arguments":{"path":"a"}}}}`,
		`{"type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":"call_1","result":{"Err":"connection refused"}}}`,
		`{"type":"event_msg","payload":{"type":"mcp_tool_call_begin","call_id":"call_2","invocation":{"server":"pond-docs","tool":"read"}}}`,
		`{"type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":"call_2","result":{"Ok":{"content":[],"isError":false}}}}`,
		`{"type":"event_msg","payload":{"type":"mcp_tool_call_begin","call_id":"call_3","invocation":{"tool":"read"}}}`,
	}, "\n") + "\n"
	events, err := (Codex{NativeID: codexNative}).Normalize(context.Background(), []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("events: %d", len(events))
	}
	type want struct {
		kind, name, server string
		isErr              *bool
	}
	wants := []want{
		{EventToolCall, "mcp__pond_docs_v2__read-page", "pond docs.v2", nil},
		{EventToolResult, "mcp__pond_docs_v2__read-page", "", boolPtr(true)},
		{EventToolCall, "mcp__pond-docs__read", "pond-docs", nil},
		{EventToolResult, "mcp__pond-docs__read", "", boolPtr(false)},
		{EventToolCall, "read", "", nil},
	}
	for i, w := range wants {
		ev := events[i]
		if ev.EventType != w.kind || ev.Tool.Name == nil || *ev.Tool.Name != w.name {
			t.Fatalf("event %d: %s %v, want %s %s", i, ev.EventType, ev.Tool.Name, w.kind, w.name)
		}
		if got, _ := ev.Extra["mcp_server"].(string); got != w.server {
			t.Fatalf("event %d: mcp_server %q, want %q", i, got, w.server)
		}
		if (ev.Tool.IsError == nil) != (w.isErr == nil) || (w.isErr != nil && *ev.Tool.IsError != *w.isErr) {
			t.Fatalf("event %d: is_error %v, want %v", i, ev.Tool.IsError, w.isErr)
		}
	}
}

func TestCodexCodeModeInnerCalls(t *testing.T) {
	raw := strings.Join([]string{
		`{"timestamp":"2026-09-22T16:10:02.000Z","type":"response_item","payload":{"type":"custom_tool_call","status":"completed","call_id":"call_cell1","name":"exec","input":"await tools.exec_command({cmd:'ls'})"}}`,
		`{"timestamp":"2026-09-22T16:10:02.500Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t1","turn_id":"u1","started_at_ms":1790093402100,"completed_at_ms":1790093402500,"item":{"type":"CommandExecution","id":"exec-aaaa","command":"rg pond","cwd":"/work/app","process_id":"7","source":"unified_exec_startup","status":"failed","aggregated_output":"","stdout":"","exit_code":1,"duration":{"secs":1,"nanos":250000000},"parsed_cmd":[{"type":"search","cmd":"rg pond","query":"pond","path":null}]}}}`,
		`{"timestamp":"2026-09-22T16:10:03.000Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t1","turn_id":"u1","item":{"type":"McpToolCall","id":"exec-bbbb","server":"pond-docs","tool":"read","status":"completed","readOnlyHint":true,"arguments":{"path":"main.go"},"result":{"content":[{"type":"text","text":"package main"}],"isError":false,"structuredContent":{"n":1}},"duration":{"secs":0,"nanos":42000000}}}}`,
		`{"timestamp":"2026-09-22T16:10:03.100Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t1","turn_id":"u1","item":{"type":"McpToolCall","id":"exec-cccc","server":"pond-docs","tool":"write","status":"failed","arguments":{},"error":{"message":"denied"},"duration":{"secs":0,"nanos":1000000}}}}`,
		`{"timestamp":"2026-09-22T16:10:03.200Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t1","turn_id":"u1","item":{"type":"FileChange","id":"exec-dddd","status":"completed","changes":{"main.go":{"type":"update","unified_diff":"@@ -1 +1 @@"}},"stdout":"Success. Updated main.go","stderr":""}}}`,
		`{"timestamp":"2026-09-22T16:10:03.300Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call_cell1","output":"cell done"}}`,
		`{"timestamp":"2026-09-22T16:10:03.400Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t1","turn_id":"u1","item":{"type":"Reasoning","id":"rs_1","summary_text":[],"raw_content":[]}}}`,
		`{"timestamp":"2026-09-22T16:10:03.500Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t1","turn_id":"u1","item":{"type":"Extension","id":"call_sleep","kind":"clock.sleep","durationMs":10}}}`,
		`{"timestamp":"2026-09-22T16:10:04.000Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t1","turn_id":"u1","item":{"type":"CommandExecution","id":"exec-eeee","command":"sleep 5","status":"completed","aggregated_output":"late","exit_code":0}}}`,
	}, "\n") + "\n"
	events, err := (Codex{NativeID: codexNative}).Normalize(context.Background(), []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ev := range events {
		name, call, isErr := "-", "-", "-"
		if ev.Tool.Name != nil {
			name = *ev.Tool.Name
		}
		if ev.Tool.CallID != nil {
			call = *ev.Tool.CallID
		}
		if ev.Tool.IsError != nil {
			isErr = fmt.Sprint(*ev.Tool.IsError)
		}
		got = append(got, strings.Join([]string{ev.RawType, ev.EventType, name, call, isErr}, " "))
	}
	want := []string{
		"custom_tool_call tool_call exec call_cell1 -",
		"item_completed tool_call exec_command exec-aaaa -",
		"item_completed tool_result exec_command exec-aaaa true",
		"item_completed tool_call mcp__pond-docs__read exec-bbbb -",
		"item_completed tool_result mcp__pond-docs__read exec-bbbb false",
		"item_completed tool_call mcp__pond-docs__write exec-cccc -",
		"item_completed tool_result mcp__pond-docs__write exec-cccc true",
		"item_completed tool_call apply_patch exec-dddd -",
		"item_completed tool_result apply_patch exec-dddd false",
		"custom_tool_call_output tool_result exec call_cell1 -",
		"item_completed unknown - - -",
		"item_completed unknown - - -",
		"item_completed tool_call exec_command exec-eeee -",
		"item_completed tool_result exec_command exec-eeee false",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	cmdCall, cmdResult := events[1], events[2]
	if *cmdCall.ContentText != "rg pond" || cmdResult.ContentText != nil {
		t.Fatalf("command text: %q / %v", *cmdCall.ContentText, cmdResult.ContentText)
	}
	if cmdCall.RecordedAt != "2026-09-22T16:10:02.1Z" || cmdResult.RecordedAt != "2026-09-22T16:10:02.5Z" {
		t.Fatalf("recorded: %s / %s", cmdCall.RecordedAt, cmdResult.RecordedAt)
	}
	for _, ev := range events[1:9] {
		if ev.Extra["cell_call_id"] != "call_cell1" {
			t.Fatalf("%s %s: cell_call_id %v", ev.EventType, *ev.Tool.CallID, ev.Extra["cell_call_id"])
		}
	}
	if events[13].Extra["cell_call_id"] != "call_cell1" {
		t.Fatalf("a command that outlives its cell names the latest cell: %v", events[13].Extra["cell_call_id"])
	}
	if cmdCall.Extra["cwd"] != "/work/app" || cmdCall.Extra["parsed_cmd"] == nil || cmdCall.Extra["exit_code"] != nil {
		t.Fatalf("call extra: %v", cmdCall.Extra)
	}
	if cmdResult.Extra["duration_ms"] != int64(1250) || cmdResult.Extra["exit_code"] != float64(1) || cmdResult.Extra["cwd"] != nil || cmdResult.Extra["aggregated_output"] != nil || cmdResult.Extra["command"] != nil {
		t.Fatalf("result extra: %v", cmdResult.Extra)
	}
	if cmdResult.Extra["item_type"] != "CommandExecution" || cmdResult.Extra["turn_id"] != "u1" {
		t.Fatalf("result extra: %v", cmdResult.Extra)
	}

	mcpCall, mcpResult := events[3], events[4]
	if *mcpCall.ContentText != `{"path":"main.go"}` || *mcpResult.ContentText != "package main" {
		t.Fatalf("mcp text: %q / %q", *mcpCall.ContentText, *mcpResult.ContentText)
	}
	if mcpCall.Extra["mcp_server"] != "pond-docs" || mcpResult.Extra["mcp_server"] != "pond-docs" || mcpCall.Extra["readOnlyHint"] != true {
		t.Fatalf("mcp extra: %v / %v", mcpCall.Extra, mcpResult.Extra)
	}
	if res, _ := mcpResult.Extra["result"].(map[string]any); res == nil || res["structuredContent"] == nil || res["content"] != nil {
		t.Fatalf("mcp result extra: %v", mcpResult.Extra)
	}
	if mcpResult.Extra["duration_ms"] != int64(42) {
		t.Fatalf("mcp duration: %v", mcpResult.Extra["duration_ms"])
	}
	if *events[6].ContentText != "denied" {
		t.Fatalf("mcp error text: %q", *events[6].ContentText)
	}

	patchCall, patchResult := events[7], events[8]
	if !strings.Contains(*patchCall.ContentText, "unified_diff") || *patchResult.ContentText != "Success. Updated main.go" {
		t.Fatalf("patch text: %q / %q", *patchCall.ContentText, *patchResult.ContentText)
	}
	if patchCall.Extra["changes"] != nil || patchResult.Extra["changes"] != nil || patchResult.Extra["stdout"] != nil {
		t.Fatalf("patch extra: %v / %v", patchCall.Extra, patchResult.Extra)
	}
	if item, _ := events[11].Extra["item"].(map[string]any); item == nil || item["kind"] != "clock.sleep" {
		t.Fatalf("an Extension item stays unknown with its item: %v", events[11].Extra)
	}
}

// BenchmarkCodexStreamInner projects a 32 MiB code-mode transcript: each
// exec cell runs one command and one MCP call, so every cell writes two
// item_completed lines between the call and its output.
func BenchmarkCodexStreamInner(b *testing.B) {
	raw := codexInnerTranscript(32 << 20)
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := (Codex{NativeID: codexNative}).Stream(context.Background(), bytes.NewReader(raw), func(Event) error { return nil })
		if err != nil {
			b.Fatal(err)
		}
	}
}

func codexInnerTranscript(size int) []byte {
	body := strings.Repeat("pond water ", 400)
	var buf bytes.Buffer
	for n := 0; buf.Len() < size; n++ {
		cell := fmt.Sprintf("call_%08d", n)
		fmt.Fprintf(&buf, `{"timestamp":"2026-09-22T16:10:02.000Z","type":"response_item","payload":{"type":"custom_tool_call","status":"completed","call_id":"%s","name":"exec","input":"const r = await tools.exec_command({cmd: 'cat main.go'});"}}`+"\n", cell)
		fmt.Fprintf(&buf, `{"timestamp":"2026-09-22T16:10:02.500Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t","turn_id":"u","started_at_ms":1790000000000,"completed_at_ms":1790000000500,"item":{"type":"CommandExecution","id":"exec-%08d-a","command":"cat main.go","cwd":"/work/app","process_id":"1","source":"unified_exec_startup","status":"completed","aggregated_output":"%s","exit_code":0,"duration":{"secs":0,"nanos":500000000},"parsed_cmd":[{"type":"read","cmd":"cat main.go","name":"main.go","path":"main.go"}]}}}`+"\n", n, body)
		fmt.Fprintf(&buf, `{"timestamp":"2026-09-22T16:10:03.000Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"t","turn_id":"u","started_at_ms":1790000000500,"completed_at_ms":1790000001000,"item":{"type":"McpToolCall","id":"exec-%08d-b","server":"pond-docs","tool":"read","status":"completed","arguments":{"path":"main.go"},"result":{"content":[{"type":"text","text":"%s"}],"isError":false},"duration":{"secs":0,"nanos":500000000}}}}`+"\n", n, body)
		fmt.Fprintf(&buf, `{"timestamp":"2026-09-22T16:10:03.100Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"%s","output":"%s"}}`+"\n", cell, body)
	}
	return buf.Bytes()
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

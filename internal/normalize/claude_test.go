package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/claude"
	"terva.sh/lampi/internal/protocol"
)

const claudeNative = "11111111-2222-4333-8444-555555555555"

func TestClaudeSchemaOpaqueAndUnknownKeys(t *testing.T) {
	raw := claudeFixture()
	before := append([]byte(nil), raw...)
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	digest := strings.Repeat("ab", 32)
	events, err := (Claude{
		Now:            now,
		NativeID:       claudeNative,
		ParentNativeID: "parent-session",
		HarnessVersion: claude.Version,
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
	if len(events) != 13 {
		t.Fatalf("events: %d", len(events))
	}

	var user, thinking, text, searchCall, searchResult, readCall, redacted, usage, toolResult, image, compaction, summary, queue *Event
	for i := range events {
		ev := &events[i]
		if ev.SchemaVersion != SchemaVersion || ev.Harness != protocol.HarnessClaude {
			t.Fatalf("header: %+v", ev)
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != claude.Version || *ev.HarnessVersion == "2.1.71" {
			t.Fatalf("harness version: %v", ev.HarnessVersion)
		}
		if ev.SessionID != "claude:"+claudeNative {
			t.Fatalf("session id %s", ev.SessionID)
		}
		if ev.ParentSessionID == nil || *ev.ParentSessionID != "claude:parent-session" {
			t.Fatalf("parent: %v", ev.ParentSessionID)
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
		if ev.ContentText != nil && (strings.Contains(*ev.ContentText, opaqueBlob) || strings.Contains(*ev.ContentText, "EmwKredactedthinkingdata") || strings.Contains(*ev.ContentText, "EqQBCgsignatureopaque")) {
			t.Fatalf("opaque value leaked into content_text: %s", *ev.ContentText)
		}
		if parent, ok := ev.Extra["parentUuid"].(string); ok && parent != "" && ev.ParentSessionID != nil && strings.Contains(*ev.ParentSessionID, parent) {
			t.Fatalf("parentUuid was used as the parent session: %s", parent)
		}
		switch {
		case ev.EventType == EventMessage && ev.Role != nil && *ev.Role == ActorUser && ev.ContentText != nil && *ev.ContentText == proofPrompt:
			user = ev
		case ev.EventType == EventMessage && ev.ContentText != nil && *ev.ContentText == "look at the pond":
			thinking = ev
		case ev.EventType == EventMessage && ev.ContentText != nil && *ev.ContentText == "I'll read the file.":
			text = ev
		case ev.EventType == EventToolCall && ev.Tool.Name != nil && *ev.Tool.Name == "web_search":
			searchCall = ev
		case ev.EventType == EventToolResult && ev.Extra["encrypted_content"] != nil:
			searchResult = ev
		case ev.EventType == EventToolCall && ev.Tool.Name != nil && *ev.Tool.Name == "Read":
			readCall = ev
		case ev.Extra["block_type"] == "redacted_thinking":
			redacted = ev
		case ev.EventType == EventUsage:
			usage = ev
		case ev.EventType == EventToolResult && ev.ContentText != nil && *ev.ContentText == "package main":
			toolResult = ev
		case ev.Extra["block_type"] == "image":
			image = ev
		case ev.EventType == EventCompaction:
			compaction = ev
		case ev.RawType == "summary":
			summary = ev
		case ev.RawType == "queue-operation":
			queue = ev
		}
	}

	if user == nil || user.Actor != ActorUser {
		t.Fatalf("user event: %+v", user)
	}
	if user.Model.ID != nil || user.Model.Provider != nil {
		t.Fatalf("user model invented: %+v", user.Model)
	}
	if user.Extra["version"] != "2.1.71" {
		t.Fatalf("cli version was dropped or promoted: %#v", user.Extra["version"])
	}
	field, _ := user.Extra["future_field"].(map[string]any)
	if field["n"] != float64(1) {
		t.Fatalf("future_field: %#v", user.Extra["future_field"])
	}
	if user.RecordedAt != "2026-09-22T16:10:01.477Z" {
		t.Fatalf("user recorded_at %s", user.RecordedAt)
	}

	if thinking == nil || thinking.Actor != ActorAssistant || thinking.Extra["signature"] != "EqQBCgsignatureopaque" {
		t.Fatalf("thinking: %+v", thinking)
	}
	if _, ok := thinking.Extra["encrypted_content"]; ok {
		t.Fatalf("thinking invented encrypted_content: %#v", thinking.Extra)
	}
	if text == nil || text.Model.ID == nil || *text.Model.ID != "claude-sonnet-4-6" || text.Model.Provider != nil {
		t.Fatalf("assistant text: %+v", text)
	}
	if searchCall == nil || searchCall.Tool.CallID == nil || *searchCall.Tool.CallID != "srvtoolu_01" || searchCall.Actor != ActorAssistant {
		t.Fatalf("search call: %+v", searchCall)
	}
	if searchCall.ContentText == nil || !strings.Contains(*searchCall.ContentText, "pond") {
		t.Fatalf("search input: %v", searchCall.ContentText)
	}
	if searchResult == nil || searchResult.ContentText == nil || *searchResult.ContentText != "Pond" || searchResult.Actor != ActorTool {
		t.Fatalf("search result: %+v", searchResult)
	}
	if searchResult.Extra["encrypted_content"] != opaqueBlob {
		t.Fatalf("ciphertext: %#v", searchResult.Extra["encrypted_content"])
	}
	if searchResult.Tool.CallID == nil || *searchResult.Tool.CallID != "srvtoolu_01" {
		t.Fatalf("search call id: %+v", searchResult.Tool)
	}
	if readCall == nil || readCall.ContentText == nil || !strings.Contains(*readCall.ContentText, "main.go") {
		t.Fatalf("read call: %+v", readCall)
	}
	if redacted == nil || redacted.EventType != EventUnknown || redacted.ContentText != nil {
		t.Fatalf("redacted thinking: %+v", redacted)
	}
	if redacted.Extra["data"] != "EmwKredactedthinkingdata" {
		t.Fatalf("redacted data: %#v", redacted.Extra["data"])
	}
	if usage == nil || usage.Usage.Input == nil || *usage.Usage.Input != 10 || usage.Usage.Output == nil || *usage.Usage.Output != 4 {
		t.Fatalf("usage: %+v", usage)
	}
	if usage.Usage.CacheRead == nil || *usage.Usage.CacheRead != 1 || usage.Usage.CacheWrite == nil || *usage.Usage.CacheWrite != 2 {
		t.Fatalf("cache: %+v", usage.Usage)
	}
	if usage.Extra["service_tier"] != "standard" {
		t.Fatalf("service_tier: %#v", usage.Extra["service_tier"])
	}
	if toolResult == nil || toolResult.Actor != ActorTool || toolResult.Tool.IsError == nil || *toolResult.Tool.IsError {
		t.Fatalf("tool result: %+v", toolResult)
	}
	if toolResult.Extra["mystery"] != true {
		t.Fatalf("mystery: %#v", toolResult.Extra)
	}
	gotFile, _ := toolResult.Extra["toolUseResult"].(map[string]any)
	if gotFile["filePath"] != "main.go" {
		t.Fatalf("toolUseResult: %#v", toolResult.Extra["toolUseResult"])
	}
	if image == nil || image.EventType != EventUnknown || image.Extra["data_bytes"] != 5 {
		t.Fatalf("image: %+v", image)
	}
	if compaction == nil || compaction.ContentText == nil || *compaction.ContentText != "summary of the pond" {
		t.Fatalf("compaction: %+v", compaction)
	}
	meta, _ := compaction.Extra["compactMetadata"].(map[string]any)
	if meta["trigger"] != "auto" {
		t.Fatalf("compact metadata: %#v", compaction.Extra["compactMetadata"])
	}
	if summary == nil || summary.EventType != EventUnknown || summary.ContentText == nil || *summary.ContentText != "pond session" {
		t.Fatalf("summary: %+v", summary)
	}
	if summary.RecordedAt != summary.IngestedAt {
		t.Fatalf("summary recorded_at %s ingested %s", summary.RecordedAt, summary.IngestedAt)
	}
	if summary.Extra["leafUuid"] != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" {
		t.Fatalf("leafUuid: %#v", summary.Extra["leafUuid"])
	}
	day, err := PartitionDate(summary.RecordedAt, summary.IngestedAt)
	if err != nil || day != "2026-09-23" {
		t.Fatalf("summary partition %s err %v", day, err)
	}
	if queue == nil || queue.EventType != EventUnknown || queue.Extra["future_queue"] != true || queue.ContentText != nil {
		t.Fatalf("queue: %+v", queue)
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
}

func TestClaudeSessionIDPrefersNative(t *testing.T) {
	raw := []byte(`{"type":"user","sessionId":"from-line","cwd":"/work/app","timestamp":"2026-09-22T16:10:01Z","message":{"role":"user","content":"hi"}}` + "\n")
	events, err := (Claude{NativeID: "from-manifest", Now: time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].SessionID != "claude:from-manifest" {
		t.Fatalf("events %+v", events)
	}
	events, err = (Claude{}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].SessionID != "claude:from-line" {
		t.Fatalf("session %s", events[0].SessionID)
	}
	if events[0].ParentSessionID != nil {
		t.Fatalf("parent from parentUuid: %v", events[0].ParentSessionID)
	}
}

func TestClaudeNormalizeErrorOmitsLine(t *testing.T) {
	raw := []byte("not-json sk-live-secret\n{\"type\":\"user\"}\n")
	before := append([]byte(nil), raw...)
	_, err := (Claude{}).Normalize(context.Background(), raw)
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

	_, err = (Claude{}).Normalize(context.Background(), []byte("[]\n"))
	if err == nil || strings.Contains(err.Error(), "[]") {
		t.Fatalf("array line: %v", err)
	}
}

func claudeFixture() []byte {
	lines := []string{
		`{"parentUuid":null,"isSidechain":false,"userType":"external","cwd":"/work/app","sessionId":"` + claudeNative + `","version":"2.1.71","gitBranch":"main","type":"user","message":{"role":"user","content":"` + proofPrompt + `"},"uuid":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","timestamp":"2026-09-22T16:10:01.477Z","future_field":{"n":1}}`,
		`{"type":"assistant","parentUuid":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","uuid":"bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee","sessionId":"` + claudeNative + `","cwd":"/work/app","version":"2.1.71","gitBranch":"main","timestamp":"2026-09-22T16:10:02.100Z","requestId":"req_01","userType":"external","isSidechain":false,"message":{"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-6","stop_reason":"tool_use","content":[{"type":"thinking","thinking":"look at the pond","signature":"EqQBCgsignatureopaque"},{"type":"text","text":"I'll read the file."},{"type":"server_tool_use","id":"srvtoolu_01","name":"web_search","input":{"query":"pond"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_01","content":[{"type":"web_search_result","title":"Pond","url":"https://example.com/pond","encrypted_content":"` + opaqueBlob + `","page_age":"2d"}]},{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"main.go"}},{"type":"redacted_thinking","data":"EmwKredactedthinkingdata"}],"usage":{"input_tokens":10,"output_tokens":4,"cache_creation_input_tokens":2,"cache_read_input_tokens":1,"service_tier":"standard"}}}`,
		`{"type":"user","parentUuid":"bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee","uuid":"cccccccc-bbbb-4ccc-8ddd-eeeeeeeeeeee","sessionId":"` + claudeNative + `","cwd":"/work/app","version":"2.1.71","gitBranch":"main","timestamp":"2026-09-22T16:10:03.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","is_error":false,"mystery":true,"content":"package main"}]},"toolUseResult":{"filePath":"main.go"}}`,
		`{"type":"user","sessionId":"` + claudeNative + `","cwd":"/work/app","version":"2.1.71","gitBranch":"main","timestamp":"2026-09-22T16:10:04.000Z","message":{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}}`,
		`{"type":"system","subtype":"compact_boundary","content":"summary of the pond","sessionId":"` + claudeNative + `","cwd":"/work/app","version":"2.1.71","timestamp":"2026-09-22T16:11:00.000Z","compactMetadata":{"trigger":"auto","preTokens":1200}}`,
		`{"type":"summary","summary":"pond session","leafUuid":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"}`,
		`{"type":"queue-operation","sessionId":"` + claudeNative + `","cwd":"/work/app","timestamp":"2026-09-22T16:10:05.000Z","operation":"enqueue","future_queue":true}`,
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

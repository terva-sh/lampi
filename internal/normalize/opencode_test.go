package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/opencode"
	"terva.sh/lampi/internal/protocol"
)

const opencodeNative = "ses_01ARZ3NDEKTSV4RRFFQ69G5FAV"

func TestOpenCodeSchemaOpaqueAndUnknownKeys(t *testing.T) {
	raw := opencodeFixture()
	before := append([]byte(nil), raw...)
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	digest := strings.Repeat("ab", 32)
	events, err := (OpenCode{
		Now:            now,
		NativeID:       opencodeNative,
		ParentNativeID: "parent-from-manifest",
		HarnessVersion: opencode.Version,
		ProjectID:      "github.com/org/app@abc",
		GitCommit:      "abc123",
		GitBranch:      "main",
		Digest:         digest,
	}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("normalize changed the raw bytes")
	}
	if len(events) != 12 {
		t.Fatalf("events: %d", len(events))
	}

	var meta, user, reasoning, text, call, result, file, compaction, stepUsage, msgUsage, retry, future *Event
	for i := range events {
		ev := &events[i]
		if ev.SchemaVersion != SchemaVersion || ev.Harness != protocol.HarnessOpenCode {
			t.Fatalf("header: %+v", ev)
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != opencode.Version || *ev.HarnessVersion == "1.2.3" {
			t.Fatalf("harness version: %v", ev.HarnessVersion)
		}
		if ev.SessionID != "opencode:"+opencodeNative {
			t.Fatalf("session id %s", ev.SessionID)
		}
		if ev.ParentSessionID == nil || *ev.ParentSessionID != "opencode:parent-session" {
			t.Fatalf("parent: %v", ev.ParentSessionID)
		}
		if ev.ParentSessionID != nil && strings.Contains(*ev.ParentSessionID, "msg_user") {
			t.Fatalf("message parentID was used as the parent session: %s", *ev.ParentSessionID)
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
		if ev.ContentText != nil && (strings.Contains(*ev.ContentText, opaqueBlob) || strings.Contains(*ev.ContentText, "aGVsbG8=")) {
			t.Fatalf("opaque value leaked into content_text: %s", *ev.ContentText)
		}
		switch {
		case ev.RawType == "info":
			meta = ev
		case ev.EventType == EventMessage && ev.Role != nil && *ev.Role == ActorUser && ev.ContentText != nil && *ev.ContentText == proofPrompt:
			user = ev
		case ev.EventType == EventMessage && ev.ContentText != nil && *ev.ContentText == "look at the pond":
			reasoning = ev
		case ev.EventType == EventMessage && ev.ContentText != nil && *ev.ContentText == "I'll read the file.":
			text = ev
		case ev.EventType == EventToolCall:
			call = ev
		case ev.EventType == EventToolResult:
			result = ev
		case ev.RawType == "file":
			file = ev
		case ev.EventType == EventCompaction:
			compaction = ev
		case ev.EventType == EventUsage && ev.RawType == "step-finish":
			stepUsage = ev
		case ev.EventType == EventUsage && ev.RawType == "tokens":
			msgUsage = ev
		case ev.EventType == EventError:
			retry = ev
		case ev.RawType == "future_part":
			future = ev
		}
	}

	if meta == nil || meta.EventType != EventMeta || meta.ContentText != nil {
		t.Fatalf("meta: %+v", meta)
	}
	if meta.Extra["version"] != "1.2.3" || meta.Extra["title"] != "pond session" || meta.Extra["extra_top"] != float64(1) {
		t.Fatalf("info extra: %#v", meta.Extra)
	}
	field, _ := meta.Extra["future_field"].(map[string]any)
	if field["n"] != float64(1) {
		t.Fatalf("future_field: %#v", meta.Extra["future_field"])
	}
	if meta.Model.ID != nil || meta.Model.Provider != nil {
		t.Fatalf("meta model invented: %+v", meta.Model)
	}
	if meta.RecordedAt != "2026-09-22T16:10:00.1Z" {
		t.Fatalf("meta recorded_at %s", meta.RecordedAt)
	}

	if user == nil || user.Actor != ActorUser {
		t.Fatalf("user event: %+v", user)
	}
	if user.Model.ID != nil || user.Model.Provider != nil {
		t.Fatalf("user model invented: %+v", user.Model)
	}
	msgField, _ := user.Extra["future_field"].(map[string]any)
	if msgField["n"] != float64(1) || user.Extra["future_message"] != true {
		t.Fatalf("user extra: %#v", user.Extra)
	}
	if user.Extra["future_part"] != true {
		t.Fatalf("part key dropped: %#v", user.Extra)
	}
	if user.RecordedAt != "2026-09-22T16:10:01.477Z" {
		t.Fatalf("user recorded_at %s", user.RecordedAt)
	}

	if reasoning == nil || reasoning.Actor != ActorAssistant || reasoning.RawType != "reasoning" {
		t.Fatalf("reasoning: %+v", reasoning)
	}
	if reasoning.Extra["encrypted_content"] != opaqueBlob {
		t.Fatalf("ciphertext: %#v", reasoning.Extra["encrypted_content"])
	}
	if text == nil || text.Model.ID == nil || *text.Model.ID != "claude-sonnet-4-6" || text.Model.Provider == nil || *text.Model.Provider != "anthropic" {
		t.Fatalf("assistant text: %+v", text)
	}
	if text.Extra["parentID"] != "msg_user" {
		t.Fatalf("message parentID: %#v", text.Extra["parentID"])
	}
	if _, ok := text.Extra["encrypted_content"]; ok {
		t.Fatalf("text invented encrypted_content: %#v", text.Extra)
	}
	if call == nil || call.Tool.Name == nil || *call.Tool.Name != "read" || call.Tool.CallID == nil || *call.Tool.CallID != "call_1" {
		t.Fatalf("tool call: %+v", call)
	}
	if call.ContentText == nil || !strings.Contains(*call.ContentText, "main.go") || call.Actor != ActorAssistant {
		t.Fatalf("tool input: %+v", call)
	}
	if result == nil || result.Actor != ActorTool || result.ContentText == nil || *result.ContentText != "package main" {
		t.Fatalf("tool result: %+v", result)
	}
	if result.Tool.IsError == nil || *result.Tool.IsError {
		t.Fatalf("tool error flag: %+v", result.Tool)
	}
	gotMeta, _ := result.Extra["metadata"].(map[string]any)
	if gotMeta["mystery"] != true {
		t.Fatalf("tool metadata: %#v", result.Extra["metadata"])
	}
	atts, _ := result.Extra["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("attachments: %#v", result.Extra["attachments"])
	}
	att, _ := atts[0].(map[string]any)
	if att["data_bytes"] != 1 || att["mime"] != "image/png" {
		t.Fatalf("attachment: %#v", att)
	}
	if _, ok := att["url"]; ok {
		t.Fatalf("attachment url kept the data payload: %#v", att["url"])
	}
	if file == nil || file.EventType != EventUnknown || file.ContentText != nil || file.Extra["data_bytes"] != 5 {
		t.Fatalf("file: %+v extra %#v", file, fileExtra(file))
	}
	if _, ok := file.Extra["url"]; ok {
		t.Fatalf("file url kept the data payload: %#v", file.Extra["url"])
	}
	if compaction == nil || compaction.ContentText != nil || compaction.Extra["auto"] != true {
		t.Fatalf("compaction: %+v", compaction)
	}
	if stepUsage == nil || stepUsage.Usage.Input == nil || *stepUsage.Usage.Input != 10 || stepUsage.Usage.Output == nil || *stepUsage.Usage.Output != 4 {
		t.Fatalf("step usage: %+v", stepUsage)
	}
	if stepUsage.Usage.CacheRead == nil || *stepUsage.Usage.CacheRead != 1 || stepUsage.Usage.CacheWrite == nil || *stepUsage.Usage.CacheWrite != 2 {
		t.Fatalf("step cache: %+v", stepUsage.Usage)
	}
	if stepUsage.Usage.CostUSD == nil || *stepUsage.Usage.CostUSD != 1 {
		t.Fatalf("step cost: %+v", stepUsage.Usage)
	}
	if stepUsage.Extra["reasoning"] != 2 || stepUsage.Extra["reason"] != "tool-calls" {
		t.Fatalf("step extra: %#v", stepUsage.Extra)
	}
	if msgUsage == nil || msgUsage.Usage.Input == nil || *msgUsage.Usage.Input != 10 || msgUsage.Extra["reasoning"] != 2 {
		t.Fatalf("message usage: %+v extra %#v", msgUsage, usageExtra(msgUsage))
	}
	if retry == nil || retry.ContentText == nil || *retry.ContentText != "sandbox refused" || retry.Actor != ActorHarness {
		t.Fatalf("retry: %+v", retry)
	}
	if retry.Extra["attempt"] != float64(1) {
		t.Fatalf("retry attempt: %#v", retry.Extra)
	}
	if future == nil || future.EventType != EventUnknown || future.ContentText != nil || future.Extra["future_queue"] != true {
		t.Fatalf("future part: %+v", future)
	}
	if future.RecordedAt != future.IngestedAt {
		t.Fatalf("future recorded_at %s ingested %s", future.RecordedAt, future.IngestedAt)
	}
	day, err := PartitionDate(future.RecordedAt, future.IngestedAt)
	if err != nil || day != "2026-09-23" {
		t.Fatalf("future day %s err %v", day, err)
	}

	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(opaqueBlob)) {
		t.Fatal("ciphertext was dropped")
	}
	if bytes.Contains(encoded, []byte("aGVsbG8=")) || bytes.Contains(encoded, []byte("YQ==")) || bytes.Contains(encoded, []byte("data:image")) {
		t.Fatal("file bytes were copied into the projection")
	}
	if strings.Count(string(encoded), opaqueBlob) != 1 {
		t.Fatalf("ciphertext copies %d", strings.Count(string(encoded), opaqueBlob))
	}
	if bytes.Contains(encoded, []byte("AKIA")) || bytes.Contains(encoded, []byte("sk-")) {
		t.Fatal("projection invented a credential")
	}
}

func fileExtra(ev *Event) map[string]any {
	if ev == nil {
		return nil
	}
	return ev.Extra
}

func usageExtra(ev *Event) map[string]any {
	if ev == nil {
		return nil
	}
	return ev.Extra
}

func TestOpenCodeSessionIDPrefersNative(t *testing.T) {
	raw := []byte(`{"info":{"id":"from-info","directory":"/work/app","parentID":"parent-session"},"messages":[{"info":{"id":"msg_1","role":"user","parentID":"msg_parent","time":{"created":1758557401477}},"parts":[{"type":"text","text":"hi"}]}]}`)
	events, err := (OpenCode{NativeID: "from-manifest", Now: time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].SessionID != "opencode:from-manifest" || events[1].SessionID != "opencode:from-manifest" {
		t.Fatalf("events %+v", events)
	}
	if events[1].ParentSessionID == nil || *events[1].ParentSessionID != "opencode:parent-session" {
		t.Fatalf("parent: %v", events[1].ParentSessionID)
	}
	if events[1].Extra["parentID"] != "msg_parent" {
		t.Fatalf("message parent kept: %#v", events[1].Extra["parentID"])
	}
	events, err = (OpenCode{}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if events[1].SessionID != "opencode:from-info" {
		t.Fatalf("session %s", events[1].SessionID)
	}
}

func TestOpenCodeNormalizeErrorOmitsBody(t *testing.T) {
	cases := [][]byte{
		[]byte("not-json sk-live-secret\n"),
		[]byte("{\"info\":{\"id\":\"ses_1\",\"note\":\"sk-live-secret\""),
		[]byte("[]\n"),
		[]byte(`{"info":{"id":"ses_1","directory":"/work/app"},"messages":["sk-live-secret"]}`),
	}
	for _, raw := range cases {
		before := append([]byte(nil), raw...)
		events, err := (OpenCode{}).Normalize(context.Background(), raw)
		if err == nil {
			t.Fatalf("expected an error for %q", raw[:min(12, len(raw))])
		}
		if strings.Contains(err.Error(), "sk-live-secret") || strings.Contains(err.Error(), "not-json") || strings.Contains(err.Error(), "[]") {
			t.Fatalf("error includes the body: %s", err)
		}
		if events != nil {
			t.Fatalf("projected %d events", len(events))
		}
		if !bytes.Equal(raw, before) {
			t.Fatal("failed normalize changed the raw bytes")
		}
	}
}

func TestOpenCodeSQLiteRejected(t *testing.T) {
	cases := [][]byte{
		append([]byte("SQLite format 3\x00"), []byte("sk-live-secret")...),
		[]byte("not-json sk-live-secret"),
		[]byte("{\"info\":{\"id\":\"ses_1\",\"note\":\"sk-live-secret\""),
	}
	for _, raw := range cases {
		before := append([]byte(nil), raw...)
		events, err := (OpenCode{}).Normalize(context.Background(), raw)
		if err == nil || !strings.Contains(err.Error(), "not a JSON object") {
			t.Fatalf("reject: %v events %d", err, len(events))
		}
		if strings.Contains(err.Error(), "sk-live-secret") || strings.Contains(err.Error(), "SQLite format") || strings.Contains(err.Error(), "not-json") {
			t.Fatalf("error includes the body: %s", err)
		}
		if events != nil {
			t.Fatalf("projected: %+v", events)
		}
		if !bytes.Equal(raw, before) {
			t.Fatal("failed normalize changed the raw bytes")
		}
	}
}

func opencodeFixture() []byte {
	created := time.Date(2026, 9, 22, 16, 10, 0, 100000000, time.UTC).UnixMilli()
	userAt := time.Date(2026, 9, 22, 16, 10, 1, 477000000, time.UTC).UnixMilli()
	return []byte(`{"info":{"id":"` + opencodeNative + `","directory":"/work/app","version":"1.2.3","title":"pond session","parentID":"parent-session","time":{"created":` + strconv.FormatInt(created, 10) + `,"updated":` + strconv.FormatInt(created, 10) + `},"future_field":{"n":1}},"messages":[{"info":{"id":"msg_user","sessionID":"` + opencodeNative + `","role":"user","time":{"created":` + strconv.FormatInt(userAt, 10) + `},"agent":"build","future_field":{"n":1}},"parts":[{"id":"prt_user","sessionID":"` + opencodeNative + `","messageID":"msg_user","type":"text","text":"` + proofPrompt + `","future_part":true}],"future_message":true},{"info":{"id":"msg_asst","sessionID":"` + opencodeNative + `","role":"assistant","parentID":"msg_user","time":{"created":` + strconv.FormatInt(userAt+1000, 10) + `},"modelID":"claude-sonnet-4-6","providerID":"anthropic","mode":"build","agent":"build","path":{"cwd":"/work/app","root":"/work"},"cost":1,"tokens":{"input":10,"output":4,"reasoning":2,"cache":{"read":1,"write":2}},"finish":"tool-calls"},"parts":[{"id":"prt_reason","type":"reasoning","text":"look at the pond","time":{"start":` + strconv.FormatInt(userAt+1000, 10) + `},"metadata":{"encrypted_content":"` + opaqueBlob + `"}},{"id":"prt_text","type":"text","text":"I'll read the file."},{"id":"prt_tool","type":"tool","callID":"call_1","tool":"read","state":{"status":"completed","input":{"file_path":"main.go"},"output":"package main","title":"Read main.go","metadata":{"mystery":true},"attachments":[{"type":"file","mime":"image/png","url":"data:image/png;base64,YQ=="}],"time":{"start":` + strconv.FormatInt(userAt+1100, 10) + `,"end":` + strconv.FormatInt(userAt+1200, 10) + `}}},{"id":"prt_file","type":"file","mime":"image/png","filename":"pond.png","url":"data:image/png;base64,aGVsbG8="},{"id":"prt_compact","type":"compaction","auto":true},{"id":"prt_step","type":"step-finish","reason":"tool-calls","cost":1,"tokens":{"input":10,"output":4,"reasoning":2,"cache":{"read":1,"write":2}}},{"id":"prt_retry","type":"retry","attempt":1,"error":{"name":"APIError","data":{"message":"sandbox refused"}},"time":{"created":` + strconv.FormatInt(userAt+2000, 10) + `}}]},{"info":{"id":"msg_bare","role":"assistant"},"parts":[{"id":"prt_future","type":"future_part","future_queue":true}]}],"extra_top":1}`)
}

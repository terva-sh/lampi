package normalize

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter/terva"
	"terva.sh/lampi/internal/protocol"

	_ "modernc.org/sqlite"
)

const (
	proofPrompt = "normalize-proof prompt: lampi-pond-7f3a"
	opaqueBlob  = "gAAAAABopaque=="
)

func TestTervaSchemaAndOpaqueFields(t *testing.T) {
	raw := fixtureTranscript()
	before := append([]byte(nil), raw...)
	now := time.Date(2026, 9, 22, 16, 30, 0, 0, time.UTC)
	events, err := (Terva{
		Now:       now,
		GitCommit: "abc123",
		Digest:    strings.Repeat("ab", 32),
	}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("normalize changed the raw bytes")
	}
	if len(events) != 11 {
		t.Fatalf("events: %d", len(events))
	}

	var user, reasoning, toolCall, toolResult, usage, compaction, meta, rename, group *Event
	for i := range events {
		ev := &events[i]
		if ev.SchemaVersion != SchemaVersion || ev.Harness != protocol.HarnessTerva {
			t.Fatalf("header: %+v", ev)
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != "0.137.0" {
			t.Fatalf("harness version: %v", ev.HarnessVersion)
		}
		if ev.Model.Provider == nil || *ev.Model.Provider != "openai" || ev.Model.ID == nil || *ev.Model.ID != "gpt-5" {
			t.Fatalf("model: %+v", ev.Model)
		}
		if ev.SessionID != "terva:20260922-161000-abcd1234" {
			t.Fatalf("session id %s", ev.SessionID)
		}
		if ev.ParentSessionID == nil || *ev.ParentSessionID != "terva:parent-1" {
			t.Fatalf("parent: %v", ev.ParentSessionID)
		}
		if ev.CWDHash != terva.CWDHash("/home/drew/src/foo") {
			t.Fatalf("cwd hash %s", ev.CWDHash)
		}
		if ev.IngestedAt != now.UTC().Format(time.RFC3339Nano) {
			t.Fatalf("ingested %s", ev.IngestedAt)
		}
		if ev.EventID == "" || ev.Redaction.Status != "none" || ev.Redaction.Ruleset != "v1" {
			t.Fatalf("id/redaction: %+v", ev)
		}
		if ev.Git.Commit == nil || *ev.Git.Commit != "abc123" || ev.Git.Branch != nil || ev.Git.Dirty != nil {
			t.Fatalf("git: %+v", ev.Git)
		}
		if ev.ContentText != nil && strings.Contains(*ev.ContentText, opaqueBlob) {
			t.Fatalf("ciphertext leaked into content_text: %s", *ev.ContentText)
		}
		if ev.ContentRef == nil || !strings.HasPrefix(*ev.ContentRef, "sha256/"+strings.Repeat("ab", 32)+"#") {
			t.Fatalf("content_ref %v", ev.ContentRef)
		}
		switch {
		case ev.EventType == EventMeta:
			meta = ev
		case ev.EventType == EventMessage && ev.Role != nil && *ev.Role == ActorUser:
			user = ev
		case ev.EventType == EventMessage && ev.ContentText != nil && *ev.ContentText == "thinking":
			reasoning = ev
		case ev.EventType == EventToolCall:
			toolCall = ev
		case ev.EventType == EventToolResult:
			toolResult = ev
		case ev.EventType == EventUsage:
			usage = ev
		case ev.EventType == EventCompaction:
			compaction = ev
		case ev.RawType == "rename":
			rename = ev
		case ev.RawType == "tool_group":
			group = ev
		}
	}
	if user == nil || user.ContentText == nil || *user.ContentText != proofPrompt || user.Actor != ActorUser {
		t.Fatalf("user event: %+v", user)
	}
	if user.Extra["vendor_ext"] != "kept" {
		t.Fatalf("vendor_ext: %#v", user.Extra)
	}
	if meta == nil || meta.Extra["future_meta"] != "keep-me" {
		t.Fatalf("future_meta: %+v", meta)
	}
	line, _ := meta.Extra["future_line"].(map[string]any)
	if line["n"] != float64(1) {
		t.Fatalf("future_line: %#v", meta.Extra["future_line"])
	}
	if meta.Extra["persona"] != "kaiku" {
		t.Fatalf("persona dropped: %#v", meta.Extra)
	}
	if meta.RecordedAt != "2026-09-22T16:10:00.5Z" {
		t.Fatalf("meta recorded_at %s", meta.RecordedAt)
	}
	if reasoning == nil || reasoning.Extra["encrypted_content"] != opaqueBlob {
		t.Fatalf("reasoning: %+v", reasoning)
	}
	if reasoning.Extra["reasoning_id"] != "rs_1" || reasoning.Extra["shape"] != "openai" {
		t.Fatalf("reasoning fields: %#v", reasoning.Extra)
	}
	if toolCall == nil || toolCall.Tool.Name == nil || *toolCall.Tool.Name != "bash" || toolCall.Actor != ActorAssistant {
		t.Fatalf("tool call: %+v", toolCall)
	}
	if toolCall.ContentText == nil || !strings.Contains(*toolCall.ContentText, "ls") {
		t.Fatalf("tool call text: %+v", toolCall.ContentText)
	}
	if toolResult == nil || toolResult.ContentText == nil || *toolResult.ContentText != "main.go" || toolResult.Actor != ActorTool {
		t.Fatalf("tool result: %+v", toolResult)
	}
	if toolResult.Tool.IsError == nil || *toolResult.Tool.IsError {
		t.Fatalf("is_error: %+v", toolResult.Tool)
	}
	if toolResult.Extra["mystery"] != true {
		t.Fatalf("mystery: %#v", toolResult.Extra)
	}
	if usage == nil || usage.Usage.Input == nil || *usage.Usage.Input != 10 || usage.Usage.Output == nil || *usage.Usage.Output != 4 {
		t.Fatalf("usage: %+v", usage)
	}
	if usage.Usage.CacheRead == nil || *usage.Usage.CacheRead != 1 || usage.Usage.CacheWrite == nil || *usage.Usage.CacheWrite != 2 {
		t.Fatalf("cache: %+v", usage.Usage)
	}
	if usage.Usage.CostUSD == nil || math.Abs(*usage.Usage.CostUSD-0.01) > 1e-9 {
		t.Fatalf("cost: %+v", usage.Usage.CostUSD)
	}
	if usage.Extra["reasoning_tokens"] != float64(3) {
		t.Fatalf("reasoning tokens dropped: %#v", usage.Extra)
	}
	if _, ok := usage.Extra["cumulative"].(map[string]any); !ok {
		t.Fatalf("cumulative dropped: %#v", usage.Extra)
	}
	if compaction == nil || compaction.ContentText == nil || *compaction.ContentText != "summary of the pond" {
		t.Fatalf("compaction: %+v", compaction)
	}
	if compaction.Extra["encrypted_content"] != opaqueBlob {
		t.Fatalf("compaction ciphertext: %#v", compaction.Extra["encrypted_content"])
	}
	if compaction.Extra["strategy"] != "summary" {
		t.Fatalf("strategy: %#v", compaction.Extra)
	}
	if rename == nil || rename.EventType != EventUnknown || rename.ContentText == nil || *rename.ContentText != "pond session" {
		t.Fatalf("rename: %+v", rename)
	}
	if group == nil || group.EventType != EventUnknown || group.Extra["brand_new"] != "preserved" {
		t.Fatalf("tool group: %+v", group)
	}

	var image *Event
	for i := range events {
		if events[i].Extra["block_type"] == "image" {
			image = &events[i]
		}
	}
	if image == nil || image.Extra["data_bytes"] != 5 {
		t.Fatalf("image: %+v", image)
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
}

func TestNormalizeErrorOmitsLine(t *testing.T) {
	raw := []byte("not-json sk-live-secret\n{\"type\":\"meta\"}\n")
	before := append([]byte(nil), raw...)
	_, err := (Terva{}).Normalize(context.Background(), raw)
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
}

func TestErrorSidecar(t *testing.T) {
	raw := []byte("{\"time\":\"2026-09-22T16:12:00Z\",\"error\":\"overload\",\"provider\":\"openai\",\"model\":\"gpt-5\",\"trace\":\"keep\"}\n")
	events, err := (Terva{Kind: protocol.KindErrorsJSONL, Now: time.Date(2026, 9, 22, 16, 30, 0, 0, time.UTC)}).Normalize(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events %d", len(events))
	}
	ev := events[0]
	if ev.EventType != EventError || ev.Actor != ActorHarness || ev.ContentText == nil || *ev.ContentText != "overload" {
		t.Fatalf("event: %+v", ev)
	}
	if ev.Extra["trace"] != "keep" {
		t.Fatalf("trace: %#v", ev.Extra)
	}
	if ev.Model.Provider == nil || *ev.Model.Provider != "openai" || ev.Model.ID == nil || *ev.Model.ID != "gpt-5" {
		t.Fatalf("model: %+v", ev.Model)
	}
}

func TestExportJSONLQueriesContentText(t *testing.T) {
	now := time.Date(2026, 9, 22, 16, 30, 0, 0, time.UTC)
	events, err := (Terva{Now: now}).Normalize(context.Background(), fixtureTranscript())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	if err := WriteFile(path, events); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE export_lines (line TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	sc := bytes.Split(body, []byte("\n"))
	inserted := 0
	for _, line := range sc {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if _, err := db.Exec(`INSERT INTO export_lines (line) VALUES (?)`, string(line)); err != nil {
			t.Fatal(err)
		}
		inserted++
	}
	if inserted != len(events) {
		t.Fatalf("inserted %d events %d", inserted, len(events))
	}
	var got string
	err = db.QueryRow(`
		SELECT json_extract(line, '$.content_text')
		FROM export_lines
		WHERE json_extract(line, '$.content_text') LIKE ?`, "%"+proofPrompt+"%").Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	if got != proofPrompt {
		t.Fatalf("query returned %q", got)
	}
	var ver int
	if err := db.QueryRow(`SELECT json_extract(line, '$.schema_version') FROM export_lines LIMIT 1`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != SchemaVersion {
		t.Fatalf("schema_version %d", ver)
	}
}

func fixtureTranscript() []byte {
	lines := []string{
		`{"type":"meta","meta":{"id":"20260922-161000-abcd1234","cwd":"/home/drew/src/foo","parent":"parent-1","model":"gpt-5","provider":"openai","started":"2026-09-22T16:10:00Z","version":"0.137.0","format_version":2,"persona":"kaiku","future_meta":"keep-me"},"future_line":{"n":1},"at":"2026-09-22T16:10:00.5Z"}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"` + proofPrompt + `"}],"time":"2026-09-22T16:10:01Z"},"vendor_ext":"kept"}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"reasoning","reasoning_id":"rs_1","summary":"thinking","encrypted_content":"` + opaqueBlob + `","shape":"openai"},{"type":"text","text":"hello"},{"type":"tool_call","id":"call_1","name":"bash","arguments":{"command":"ls"}},{"type":"image","mime_type":"image/png","data":"aGVsbG8="}],"time":"2026-09-22T16:10:02Z"}}`,
		`{"type":"message","message":{"role":"tool","content":[{"type":"tool_result","call_id":"call_1","is_error":false,"mystery":true,"content":[{"type":"text","text":"main.go"}]}],"time":"2026-09-22T16:10:03Z"}}`,
		`{"type":"usage","usage":{"input_tokens":10,"output_tokens":4,"cache_read_tokens":1,"cache_write_tokens":2,"cost_usd":0.01,"reasoning_tokens":3},"cumulative":{"input_tokens":10,"output_tokens":4,"cache_read_tokens":0,"cache_write_tokens":0,"cost_usd":0.01},"at":"2026-09-22T16:10:04Z"}`,
		`{"type":"compaction","messages":[{"role":"assistant","content":[{"type":"compaction_summary","id":"cmp_1","encrypted_content":"` + opaqueBlob + `","provider":"openai"},{"type":"text","text":"summary of the pond"}],"time":"2026-09-22T16:11:00Z"}],"usage":{"input_tokens":1,"output_tokens":1,"cache_read_tokens":0,"cache_write_tokens":0,"cost_usd":0},"strategy":"summary"}`,
		`{"type":"rename","title":"pond session","source":"generated"}`,
		`{"type":"tool_group","tool_group":{"group":"web"},"brand_new":"preserved"}`,
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

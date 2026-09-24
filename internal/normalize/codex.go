package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/id"
	"terva.sh/lampi/internal/protocol"
)

// Codex projects one Codex CLI rollout transcript_jsonl blob. The bytes
// are the file the adapter uploaded. The struct does not open a file.
// history.jsonl is not a rollout; the caller does not pass that file.
//
// session_id is "codex:" plus the native id (NativeID, else the id on
// session_meta). parent_thread_id is a sub-agent pointer, not the
// manifest lineage, so it stays in extra. Unknown keys are copied into
// extra. encrypted_content is copied as stored and is not written into
// content_text. Image bytes stay in the raw blob. HarnessVersion is
// the caller's pinned reader version, not the CLI version on a line.
type Codex struct {
	Now            time.Time
	NativeID       string
	ParentNativeID string
	HarnessVersion string
	ProjectID      string
	CWD            string
	GitCommit      string
	GitBranch      string
	GitDirty       *bool
	Digest         string
}

var _ Normalizer = Codex{}

type codexState struct {
	session  string
	cwd      string
	model    string
	provider string
	branch   string
	commit   string
}

// Normalize projects raw. raw is not modified. A line that is not a
// JSON object fails the whole blob; the error text does not include
// the line, which may hold a secret.
func (c Codex) Normalize(ctx context.Context, raw []byte) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
	c.Now = c.Now.UTC()

	events := []Event{}
	var st codexState
	rest := raw
	lineNo := 0
	for len(rest) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineNo++
		rel := bytes.IndexByte(rest, '\n')
		var line []byte
		var advance int
		if rel < 0 {
			line = rest
			advance = len(rest)
		} else {
			line = rest[:rel]
			advance = rel + 1
		}
		offset := len(raw) - len(rest)
		rest = rest[advance:]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		evs, err := c.line(lineNo, offset, line, &st)
		if err != nil {
			return nil, err
		}
		events = append(events, evs...)
	}
	return events, nil
}

func (c Codex) line(lineNo, offset int, line []byte, st *codexState) ([]Event, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("normalize: line %d is not a JSON object", lineNo)
	}
	rawType, when, payload, lineExtra, err := splitCodexLine(obj)
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	switch rawType {
	case "session_meta":
		return c.sessionMeta(lineNo, offset, when, payload, lineExtra, st)
	case "turn_context":
		return c.turnContext(lineNo, offset, when, payload, lineExtra, st)
	case "event_msg":
		return c.eventMsg(lineNo, offset, when, payload, lineExtra, st)
	case "response_item":
		return c.responseItem(lineNo, offset, when, payload, lineExtra, st)
	case "compacted":
		return c.compacted(lineNo, offset, when, "compacted", payload, lineExtra, st)
	default:
		return c.unknownLine(lineNo, offset, when, rawType, payload, lineExtra, st)
	}
}

func splitCodexLine(obj map[string]json.RawMessage) (rawType string, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, err error) {
	rawType, _ = jsonString(obj["type"])
	when, _ = jsonTime(obj["timestamp"])
	lineExtra, err = extraFrom(obj, "timestamp", "type", "payload")
	if err != nil {
		return "", time.Time{}, nil, nil, err
	}
	raw, ok := obj["payload"]
	if !ok || isNull(raw) {
		return rawType, when, nil, lineExtra, nil
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		lineExtra["payload"] = opaque(raw)
		return rawType, when, nil, lineExtra, nil
	}
	return rawType, when, payload, lineExtra, nil
}

func (c Codex) sessionMeta(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	if s, ok := jsonString(payload["id"]); ok && s != "" {
		st.session = s
	}
	if s, ok := jsonString(payload["cwd"]); ok && s != "" {
		st.cwd = s
	}
	if s, ok := jsonString(payload["model_provider"]); ok && s != "" {
		st.provider = s
	}
	applyCodexGit(st, payload["git"])
	extra, err := codexPayloadExtra(payload, "id", "cwd", "model_provider")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	ev, err := c.emit(st, "session_meta", EventMeta, "", when, offset, "", Tool{}, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func applyCodexGit(st *codexState, raw json.RawMessage) {
	if isNull(raw) {
		return
	}
	var git map[string]json.RawMessage
	if err := json.Unmarshal(raw, &git); err != nil || git == nil {
		return
	}
	if s, ok := jsonString(git["branch"]); ok && s != "" {
		st.branch = s
	}
	if s, ok := jsonString(git["commit_hash"]); ok && s != "" {
		st.commit = s
	}
}

func (c Codex) turnContext(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	if s, ok := jsonString(payload["model"]); ok && s != "" {
		st.model = s
	}
	if s, ok := jsonString(payload["cwd"]); ok && s != "" {
		st.cwd = s
	}
	extra, err := codexPayloadExtra(payload, "model", "cwd")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	ev, err := c.emit(st, "turn_context", EventMeta, "", when, offset, "", Tool{}, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) eventMsg(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	kind, _ := jsonString(payload["type"])
	if kind == "" {
		kind = "event_msg"
	}
	switch kind {
	case "user_message":
		return c.textMessage(lineNo, offset, when, kind, ActorUser, payload, lineExtra, st)
	case "agent_message":
		return c.textMessage(lineNo, offset, when, kind, ActorAssistant, payload, lineExtra, st)
	case "agent_reasoning", "reasoning":
		return c.reasoning(lineNo, offset, when, kind, payload, lineExtra, st)
	case "token_count":
		return c.tokenCount(lineNo, offset, when, payload, lineExtra, st)
	case "error":
		return c.errorMsg(lineNo, offset, when, payload, lineExtra, st)
	case "context_compacted":
		return c.compacted(lineNo, offset, when, "context_compacted", payload, lineExtra, st)
	case "exec_command_begin", "mcp_tool_call_begin", "web_search_begin", "patch_apply_begin":
		return c.beginTool(lineNo, offset, when, kind, payload, lineExtra, st)
	case "exec_command_end", "mcp_tool_call_end", "web_search_end", "patch_apply_end":
		return c.endTool(lineNo, offset, when, kind, payload, lineExtra, st)
	default:
		return c.unknownLine(lineNo, offset, when, kind, payload, lineExtra, st)
	}
}

func (c Codex) textMessage(lineNo, offset int, when time.Time, rawType, role string, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	text, _ := jsonString(payload["message"])
	extra, err := codexPayloadExtra(payload, "type", "message")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	extra = stripCodexImages(extra)
	ev, err := c.emit(st, rawType, EventMessage, role, when, offset, text, Tool{}, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) reasoning(lineNo, offset int, when time.Time, rawType string, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	text := codexPlainText(payload["summary"])
	if text == "" {
		text = codexPlainText(payload["text"])
	}
	if text == "" {
		text = codexPlainText(payload["content"])
	}
	extra, err := codexPayloadExtra(payload, "type")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	ev, err := c.emit(st, rawType, EventMessage, ActorAssistant, when, offset, text, Tool{}, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) tokenCount(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	usage, err := codexUsage(payload["info"])
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	extra, err := codexPayloadExtra(payload, "type")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	ev, err := c.emit(st, "token_count", EventUsage, "", when, offset, "", Tool{}, usage, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func codexUsage(raw json.RawMessage) (Usage, error) {
	if isNull(raw) {
		return Usage{}, nil
	}
	var info map[string]json.RawMessage
	if err := json.Unmarshal(raw, &info); err != nil || info == nil {
		return Usage{}, fmt.Errorf("token info is not an object")
	}
	src := info["last_token_usage"]
	if isNull(src) {
		src = info["total_token_usage"]
	}
	if isNull(src) {
		return Usage{}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(src, &obj); err != nil || obj == nil {
		return Usage{}, fmt.Errorf("token usage is not an object")
	}
	var u Usage
	if n, ok := jsonInt(obj["input_tokens"]); ok {
		u.Input = &n
	}
	if n, ok := jsonInt(obj["output_tokens"]); ok {
		u.Output = &n
	}
	if n, ok := jsonInt(obj["cached_input_tokens"]); ok {
		u.CacheRead = &n
	}
	return u, nil
}

func (c Codex) errorMsg(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	text, _ := jsonString(payload["message"])
	drop := []string{"type"}
	if text != "" {
		drop = append(drop, "message")
	}
	if text == "" {
		if s, ok := jsonString(payload["error"]); ok {
			text = s
			drop = append(drop, "error")
		}
	}
	extra, err := codexPayloadExtra(payload, drop...)
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	ev, err := c.emit(st, "error", EventError, "", when, offset, text, Tool{}, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) beginTool(lineNo, offset int, when time.Time, kind string, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	callID, _ := jsonString(payload["call_id"])
	name := strings.TrimSuffix(kind, "_begin")
	if s, ok := jsonString(payload["name"]); ok && s != "" {
		name = s
	}
	if inv := payload["invocation"]; !isNull(inv) {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(inv, &obj); err == nil && obj != nil {
			if s, ok := jsonString(obj["tool"]); ok && s != "" {
				name = s
			}
		}
	}
	text := codexCommandText(payload["command"])
	if text == "" {
		text = argumentsText(payload["arguments"])
	}
	if text == "" {
		text = argumentsText(payload["input"])
	}
	extra, err := codexPayloadExtra(payload, "type", "call_id", "name", "command", "arguments", "input")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	tool := Tool{Name: strPtr(name), CallID: strPtr(callID)}
	ev, err := c.emit(st, kind, EventToolCall, ActorAssistant, when, offset, text, tool, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) endTool(lineNo, offset int, when time.Time, kind string, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	callID, _ := jsonString(payload["call_id"])
	text, _ := jsonString(payload["stdout"])
	if text == "" {
		text = codexPlainText(payload["output"])
	}
	if text == "" {
		text = argumentsText(payload["result"])
	}
	extra, err := codexPayloadExtra(payload, "type", "call_id", "stdout", "output")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	tool := Tool{CallID: strPtr(callID), IsError: codexExitError(payload["exit_code"])}
	ev, err := c.emit(st, kind, EventToolResult, ActorTool, when, offset, text, tool, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func codexCommandText(raw json.RawMessage) string {
	if isNull(raw) {
		return ""
	}
	if s, ok := jsonString(raw); ok {
		return s
	}
	var parts []string
	if err := json.Unmarshal(raw, &parts); err == nil {
		return strings.Join(parts, " ")
	}
	return argumentsText(raw)
}

func codexExitError(raw json.RawMessage) *bool {
	n, ok := jsonInt(raw)
	if !ok {
		return nil
	}
	err := n != 0
	return &err
}

func (c Codex) responseItem(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	kind, _ := jsonString(payload["type"])
	if kind == "" {
		kind = "response_item"
	}
	switch kind {
	case "message":
		return c.modelMessage(lineNo, offset, when, payload, lineExtra, st)
	case "function_call", "custom_tool_call", "tool_search_call":
		return c.functionCall(lineNo, offset, when, kind, payload, lineExtra, st)
	case "function_call_output", "custom_tool_call_output", "tool_search_output":
		return c.functionOutput(lineNo, offset, when, kind, payload, lineExtra, st)
	case "web_search_call":
		return c.webSearchCall(lineNo, offset, when, payload, lineExtra, st)
	case "reasoning":
		return c.reasoning(lineNo, offset, when, kind, payload, lineExtra, st)
	case "compaction":
		return c.compacted(lineNo, offset, when, "compaction", payload, lineExtra, st)
	case "image_generation_call":
		return c.imageCall(lineNo, offset, when, payload, lineExtra, st)
	default:
		return c.unknownLine(lineNo, offset, when, kind, payload, lineExtra, st)
	}
}

func (c Codex) modelMessage(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	role, _ := jsonString(payload["role"])
	text := codexPlainText(payload["content"])
	extra, err := codexPayloadExtra(payload, "type", "role", "content")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	extra = mergeEncrypted(extra, nestedEncrypted(payload["content"]))
	ev, err := c.emit(st, "message", EventMessage, role, when, offset, text, Tool{}, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) functionCall(lineNo, offset int, when time.Time, kind string, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	name, _ := jsonString(payload["name"])
	callID, _ := jsonString(payload["call_id"])
	if callID == "" {
		callID, _ = jsonString(payload["id"])
	}
	text := argumentsText(payload["arguments"])
	if text == "" {
		text = argumentsText(payload["input"])
	}
	extra, err := codexPayloadExtra(payload, "type", "name", "call_id", "id", "arguments", "input")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	tool := Tool{Name: strPtr(name), CallID: strPtr(callID)}
	ev, err := c.emit(st, kind, EventToolCall, ActorAssistant, when, offset, text, tool, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) functionOutput(lineNo, offset int, when time.Time, kind string, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	callID, _ := jsonString(payload["call_id"])
	text := codexPlainText(payload["output"])
	extra, err := codexPayloadExtra(payload, "type", "call_id", "output")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	extra = mergeEncrypted(extra, nestedEncrypted(payload["output"]))
	tool := Tool{CallID: strPtr(callID), IsError: jsonBoolPtr(payload["is_error"])}
	ev, err := c.emit(st, kind, EventToolResult, ActorTool, when, offset, text, tool, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) webSearchCall(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	callID, _ := jsonString(payload["id"])
	text := ""
	if action := payload["action"]; !isNull(action) {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(action, &obj); err == nil && obj != nil {
			text, _ = jsonString(obj["query"])
		}
	}
	extra, err := codexPayloadExtra(payload, "type", "id")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	tool := Tool{Name: strPtr("web_search"), CallID: strPtr(callID)}
	ev, err := c.emit(st, "web_search_call", EventToolCall, ActorAssistant, when, offset, text, tool, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) compacted(lineNo, offset int, when time.Time, rawType string, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	text, _ := jsonString(payload["message"])
	if text == "" {
		text = codexPlainText(payload["summary"])
	}
	drop := []string{"type"}
	if _, ok := jsonString(payload["message"]); ok {
		drop = append(drop, "message")
	}
	extra, err := codexPayloadExtra(payload, drop...)
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	ev, err := c.emit(st, rawType, EventCompaction, "", when, offset, text, Tool{}, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) imageCall(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	extra, err := codexPayloadExtra(payload, "type", "result")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	if n, ok := decodedLen(payload["result"]); ok {
		extra["data_bytes"] = n
	}
	ev, err := c.emit(st, "image_generation_call", EventUnknown, "", when, offset, "", Tool{}, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) unknownLine(lineNo, offset int, when time.Time, rawType string, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	extra, err := codexPayloadExtra(payload, "type")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	if rawType == "" {
		rawType = "unknown"
	}
	ev, err := c.emit(st, rawType, EventUnknown, "", when, offset, "", Tool{}, Usage{}, mergeExtra(lineExtra, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Codex) emit(st *codexState, rawType, eventType, role string, recorded time.Time, offset int, text string, tool Tool, usage Usage, extra map[string]any) (Event, error) {
	eid, err := id.New(c.Now)
	if err != nil {
		return Event{}, err
	}
	if extra == nil {
		extra = map[string]any{}
	}
	if role != "" && knownRole(role) == nil {
		extra["role"] = role
	}
	cwd := st.cwd
	if cwd == "" {
		cwd = c.CWD
	}
	branch := st.branch
	if branch == "" {
		branch = c.GitBranch
	}
	commit := c.GitCommit
	if commit == "" {
		commit = st.commit
	}
	return Event{
		SchemaVersion:   SchemaVersion,
		EventID:         eid,
		SessionID:       codexSessionID(c.NativeID, st.session),
		ParentSessionID: codexParentID(c.ParentNativeID),
		Harness:         protocol.HarnessCodex,
		HarnessVersion:  strPtr(c.HarnessVersion),
		RecordedAt:      recordedAt(recorded, c.Now),
		IngestedAt:      c.Now.Format(time.RFC3339Nano),
		CWDHash:         adapter.CWDHash(cwd),
		ProjectID:       strPtr(c.ProjectID),
		Git: Git{
			Branch: strPtr(branch),
			Commit: strPtr(commit),
			Dirty:  c.GitDirty,
		},
		Actor:       actorFor(eventType, role),
		EventType:   eventType,
		Role:        knownRole(role),
		Model:       Model{Provider: strPtr(st.provider), ID: strPtr(st.model)},
		ContentText: strPtr(text),
		ContentRef:  contentRef(c.Digest, offset),
		Tool:        tool,
		Usage:       usage,
		RawType:     rawType,
		Redaction:   Redaction{Status: "none", Ruleset: "v1"},
		Extra:       extraMap(extra),
	}, nil
}

func codexSessionID(native, fromMeta string) string {
	id := native
	if id == "" {
		id = fromMeta
	}
	if id == "" {
		id = "unknown"
	}
	return protocol.HarnessCodex + ":" + id
}

func codexParentID(fromManifest string) *string {
	if fromManifest == "" {
		return nil
	}
	s := protocol.HarnessCodex + ":" + fromManifest
	return &s
}

func codexPayloadExtra(payload map[string]json.RawMessage, drop ...string) (map[string]any, error) {
	if payload == nil {
		return map[string]any{}, nil
	}
	drop = append(append([]string{}, drop...), "encrypted_content")
	extra, err := extraFrom(payload, drop...)
	if err != nil {
		return nil, err
	}
	cleaned, nested := collectEncrypted(extra)
	extra, _ = cleaned.(map[string]any)
	if extra == nil {
		extra = map[string]any{}
	}
	var vals []any
	if raw, ok := payload["encrypted_content"]; ok && !isNull(raw) {
		vals = append(vals, opaque(raw))
	}
	vals = append(vals, nested...)
	return mergeEncrypted(extra, vals), nil
}

func codexPlainText(raw json.RawMessage) string {
	if isNull(raw) {
		return ""
	}
	if s, ok := jsonString(raw); ok {
		return s
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		var parts []string
		for _, item := range arr {
			if s, ok := jsonString(item); ok {
				if s != "" {
					parts = append(parts, s)
				}
				continue
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(item, &obj); err != nil || obj == nil {
				continue
			}
			if s, ok := jsonString(obj["text"]); ok && s != "" {
				parts = append(parts, s)
				continue
			}
			if s, ok := jsonString(obj["message"]); ok && s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return argumentsText(raw)
	}
	if s, ok := jsonString(obj["text"]); ok && s != "" {
		return s
	}
	if s, ok := jsonString(obj["message"]); ok && s != "" {
		return s
	}
	return ""
}

func nestedEncrypted(raw json.RawMessage) []any {
	if isNull(raw) {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	_, enc := collectEncrypted(v)
	return enc
}

func collectEncrypted(v any) (any, []any) {
	switch t := v.(type) {
	case map[string]any:
		var enc []any
		out := make(map[string]any, len(t))
		for k, val := range t {
			if k == "encrypted_content" {
				if val != nil && val != "" {
					enc = append(enc, val)
				}
				continue
			}
			cleaned, more := collectEncrypted(val)
			out[k] = cleaned
			enc = append(enc, more...)
		}
		return out, enc
	case []any:
		var enc []any
		out := make([]any, len(t))
		for i, val := range t {
			cleaned, more := collectEncrypted(val)
			out[i] = cleaned
			enc = append(enc, more...)
		}
		return out, enc
	default:
		return v, nil
	}
}

func mergeEncrypted(extra map[string]any, vals []any) map[string]any {
	var strs []string
	var other []any
	for _, v := range vals {
		if v == nil || v == "" {
			continue
		}
		if s, ok := v.(string); ok {
			strs = append(strs, s)
			continue
		}
		other = append(other, v)
	}
	extra = putEncrypted(extra, strs)
	if len(other) == 0 {
		return extra
	}
	if extra == nil {
		extra = map[string]any{}
	}
	if prev, ok := extra["encrypted_content"]; ok {
		other = append([]any{prev}, other...)
		extra["encrypted_content"] = other
		return extra
	}
	if len(other) == 1 {
		extra["encrypted_content"] = other[0]
		return extra
	}
	extra["encrypted_content"] = other
	return extra
}

// stripCodexImages drops inline image bytes from a copied user message.
// A path or an empty list stays. The bytes remain in the raw blob.
func stripCodexImages(extra map[string]any) map[string]any {
	if extra == nil {
		return extra
	}
	for _, key := range []string{"images", "local_images"} {
		items, ok := extra[key].([]any)
		if !ok {
			continue
		}
		cleaned := make([]any, len(items))
		var nbytes int
		for i, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				cleaned[i] = item
				continue
			}
			next := map[string]any{}
			for k, v := range obj {
				if n, drop := codexImageBytes(k, v); drop {
					nbytes += n
					continue
				}
				next[k] = v
			}
			cleaned[i] = next
		}
		extra[key] = cleaned
		if nbytes > 0 {
			extra["data_bytes"] = nbytes
		}
	}
	return extra
}

func codexImageBytes(key string, v any) (int, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return 0, false
	}
	switch key {
	case "data":
		if n, ok := decodedLen(json.RawMessage(strconvQuote(s))); ok {
			return n, true
		}
		return len(s), true
	case "image_url", "url":
		if strings.HasPrefix(s, "data:") || strings.Contains(s, "base64,") {
			return len(s), true
		}
	}
	return 0, false
}

func strconvQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

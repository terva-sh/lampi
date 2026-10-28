package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	// calls names a response_item output from its function call, and
	// begun names an event_msg *_end from its *_begin. One call id
	// appears in both with different names (shell and exec_command), so
	// each result takes the name of its own kind of call.
	calls callNames
	begun callNames
	// cell is the call id of the latest code-mode exec cell. An inner
	// item_completed call names it as its enclosing cell. It stays set
	// after the cell's output, because a command can outlive its cell.
	cell string
}

// Normalize projects raw. raw is not modified. A line that is not a
// JSON object becomes an error event that names the line but not its
// bytes, which may hold a secret; a blob with no JSON object line fails
// (unreadable.go).
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
	// objects and unreadable count the lines that are and are not JSON
	// objects; firstBad is the first that is not. See unreadable.go.
	objects, unreadable := 0, 0
	var firstBad error
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
		var bad unreadableLine
		if errors.As(err, &bad) {
			if firstBad == nil {
				firstBad = err
			}
			if unreadable++; unreadable > maxUnreadable {
				return nil, tooUnreadable(firstBad)
			}
			ev, err := c.emit(&st, "", EventError, "", time.Time{}, offset, unreadableText(lineNo, len(line)), Tool{}, Usage{}, unreadableExtra(lineNo, len(line)))
			if err != nil {
				return nil, err
			}
			events = append(events, ev)
			continue
		}
		if err != nil {
			return nil, err
		}
		objects++
		events = append(events, evs...)
	}
	if objects == 0 && firstBad != nil {
		return nil, firstBad
	}
	return events, nil
}

func (c Codex) line(lineNo, offset int, line []byte, st *codexState) ([]Event, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return nil, unreadableLine{lineNo}
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
	case "item_completed":
		return c.itemCompleted(lineNo, offset, when, payload, lineExtra, st)
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
	server := ""
	if inv := payload["invocation"]; !isNull(inv) {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(inv, &obj); err == nil && obj != nil {
			if s, ok := jsonString(obj["tool"]); ok && s != "" {
				name = s
				server, _ = jsonString(obj["server"])
				if n := mcpToolName(server, s); n != "" {
					name = n
				}
			}
		}
	}
	st.begun.remember(callID, name)
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
	if server != "" {
		extra["mcp_server"] = server
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
	isErr := codexExitError(payload["exit_code"])
	if isErr == nil && kind == "mcp_tool_call_end" {
		isErr = codexMCPEndError(payload["result"])
	}
	tool := Tool{Name: strPtr(st.begun.name(callID)), CallID: strPtr(callID), IsError: isErr}
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

// codexMCPEndError reads an mcp_tool_call_end result: {"Err": ...} is a
// failure, and {"Ok": {"isError": b}} carries the server's own flag.
func codexMCPEndError(raw json.RawMessage) *bool {
	obj := rawObject(raw)
	if obj == nil {
		return nil
	}
	if hasKey(obj, "Err") {
		return boolPtr(true)
	}
	if inner := rawObject(obj["Ok"]); inner != nil {
		return jsonBoolPtr(inner["isError"])
	}
	return jsonBoolPtr(obj["isError"])
}

// codexStatusError maps an item status: completed is false and failed or
// declined is true. Any other status, such as in_progress, is null.
func codexStatusError(raw json.RawMessage) *bool {
	s, _ := jsonString(raw)
	switch s {
	case "completed":
		return boolPtr(false)
	case "failed", "declined":
		return boolPtr(true)
	}
	return nil
}

// codexInnerKinds are the item_completed items that are tool calls made
// from inside a code-mode exec cell, with the tool.name each gets. An
// McpToolCall is named from its server and tool instead. Other items
// (Reasoning, AgentMessage, UserMessage, SubAgentActivity, Extension,
// ImageView, ContextCompaction, CollabAgentToolCall) stay unknown: an
// Extension repeats a top-level function call, and the rest are not
// tool work.
var codexInnerKinds = map[string]string{
	"CommandExecution": "exec_command",
	"FileChange":       "apply_patch",
	"McpToolCall":      "mcp_tool_call",
}

// itemCompleted projects a code-mode inner call as a tool_call and a
// tool_result, both with raw_type item_completed and the item id as the
// call id. raw_type is how an inner call is told apart from its exec
// cell: the cell's own events are custom_tool_call and
// custom_tool_call_output. extra.cell_call_id names the cell, the latest
// exec call at the time the item completed. The inner result is not
// what the model read; the cell output is.
func (c Codex) itemCompleted(lineNo, offset int, when time.Time, payload map[string]json.RawMessage, lineExtra map[string]any, st *codexState) ([]Event, error) {
	item := rawObject(payload["item"])
	kind, _ := jsonString(item["type"])
	name, inner := codexInnerKinds[kind]
	if !inner {
		return c.unknownLine(lineNo, offset, when, "item_completed", payload, lineExtra, st)
	}
	callID, _ := jsonString(item["id"])
	base, err := codexPayloadExtra(payload, "type", "item")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	base = mergeExtra(lineExtra, base)
	base["item_type"] = kind
	if st.cell != "" {
		base["cell_call_id"] = st.cell
	}

	// callKeys go on the call event and textKeys become content_text;
	// every other item field goes on the result.
	var callText, resultText string
	var callKeys, textKeys []string
	var isErr *bool
	var result map[string]json.RawMessage
	switch kind {
	case "CommandExecution":
		callText = codexCommandText(item["command"])
		resultText, _ = jsonString(item["aggregated_output"])
		callKeys = []string{"cwd", "parsed_cmd", "process_id", "source"}
		textKeys = []string{"command", "aggregated_output"}
		isErr = codexExitError(item["exit_code"])
	case "McpToolCall":
		server, _ := jsonString(item["server"])
		tool, _ := jsonString(item["tool"])
		if n := mcpToolName(server, tool); n != "" {
			name = n
		}
		if server != "" {
			base["mcp_server"] = server
		}
		callText = argumentsText(item["arguments"])
		callKeys = []string{"server", "tool", "readOnlyHint"}
		textKeys = []string{"arguments"}
		if result = rawObject(item["result"]); result != nil {
			resultText = mcpContentText(result["content"])
			isErr = jsonBoolPtr(result["isError"])
		} else {
			resultText = argumentsText(item["result"])
		}
		if hasKey(item, "error") {
			if resultText == "" {
				resultText = codexPlainText(item["error"])
			}
			if isErr == nil {
				isErr = boolPtr(true)
			}
		}
	case "FileChange":
		callText = argumentsText(item["changes"])
		textKeys = []string{"changes"}
		var parts []string
		for _, key := range []string{"stdout", "stderr"} {
			if s, ok := jsonString(item[key]); ok && s != "" {
				parts = append(parts, s)
				textKeys = append(textKeys, key)
			}
		}
		resultText = strings.Join(parts, "\n")
	}
	if isErr == nil {
		isErr = codexStatusError(item["status"])
	}

	callExtra, err := extraFrom(pickRaw(item, callKeys...))
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	drop := append(append([]string{"type", "id"}, callKeys...), textKeys...)
	if kind == "McpToolCall" {
		drop = append(drop, "result")
	}
	resultExtra, err := extraFrom(item, drop...)
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	if result != nil {
		// The MCP result's content is the text; its other fields, such
		// as structuredContent, stay in extra.
		rest, err := extraFrom(result, "content", "isError")
		if err != nil {
			return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
		}
		if len(rest) > 0 {
			resultExtra["result"] = rest
		}
	}
	if ms, ok := codexDurationMS(item["duration"]); ok {
		resultExtra["duration_ms"] = ms
	}

	started := when
	if n, ok := jsonInt64(payload["started_at_ms"]); ok {
		if t, ok := unixFlexible(n); ok {
			started = t
		}
	}
	call, err := c.emit(st, "item_completed", EventToolCall, ActorAssistant, started, offset, callText, Tool{Name: strPtr(name), CallID: strPtr(callID)}, Usage{}, mergeExtra(base, callExtra))
	if err != nil {
		return nil, err
	}
	out, err := c.emit(st, "item_completed", EventToolResult, ActorTool, when, offset, resultText, Tool{Name: strPtr(name), CallID: strPtr(callID), IsError: isErr}, Usage{}, mergeExtra(base, resultExtra))
	if err != nil {
		return nil, err
	}
	return []Event{call, out}, nil
}

// rawObject decodes a JSON object, or is nil when raw is not one. It does
// not check for a base64 wrapper, so it reads the bytes once.
func rawObject(raw json.RawMessage) map[string]json.RawMessage {
	if isNull(raw) {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	return obj
}

// mcpContentText joins the text blocks of an MCP result's content in one
// decode. Content of another shape goes through codexPlainText.
func mcpContentText(raw json.RawMessage) string {
	if isNull(raw) {
		return ""
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return codexPlainText(raw)
	}
	var parts []string
	for _, b := range blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// pickRaw is the subset of obj with these keys.
func pickRaw(obj map[string]json.RawMessage, keys ...string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(keys))
	for _, k := range keys {
		if raw, ok := obj[k]; ok {
			out[k] = raw
		}
	}
	return out
}

// codexDurationMS reads a Rust Duration, {"secs": s, "nanos": n}, as
// whole milliseconds.
func codexDurationMS(raw json.RawMessage) (int64, bool) {
	obj := rawObject(raw)
	if obj == nil {
		return 0, false
	}
	secs, okS := jsonInt64(obj["secs"])
	nanos, okN := jsonInt64(obj["nanos"])
	if !okS && !okN {
		return 0, false
	}
	return secs*1000 + nanos/1_000_000, true
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
	st.calls.remember(callID, name)
	if kind == "custom_tool_call" && name == "exec" && len(callID) <= callNameMaxBytes {
		st.cell = callID
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
	tool := Tool{Name: strPtr(st.calls.name(callID)), CallID: strPtr(callID), IsError: jsonBoolPtr(payload["is_error"])}
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

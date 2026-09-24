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

// Claude projects one Claude Code transcript_jsonl blob. The bytes are
// the file the adapter uploaded. The struct does not open a file.
//
// session_id is "claude:" plus the native id (NativeID, else sessionId
// on a line). parentUuid is a message pointer, not a parent session,
// so it stays in extra. Unknown keys are copied into extra.
// encrypted_content is copied as stored and is not written into
// content_text. Image bytes stay in the raw blob. HarnessVersion is
// the caller's pinned reader version, not the CLI version on a line.
type Claude struct {
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

var _ Normalizer = Claude{}

type claudeState struct {
	session string
	cwd     string
	model   string
	branch  string
}

// Normalize projects raw. raw is not modified. A line that is not a
// JSON object fails the whole blob; the error text does not include
// the line, which may hold a secret.
func (c Claude) Normalize(ctx context.Context, raw []byte) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
	c.Now = c.Now.UTC()

	events := []Event{}
	var st claudeState
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

func (c Claude) line(lineNo, offset int, line []byte, st *claudeState) ([]Event, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("normalize: line %d is not a JSON object", lineNo)
	}
	rawType, _ := jsonString(obj["type"])
	when, _ := jsonTime(obj["timestamp"])
	if s, ok := jsonString(obj["sessionId"]); ok && s != "" {
		st.session = s
	}
	if s, ok := jsonString(obj["cwd"]); ok && s != "" {
		st.cwd = s
	}
	if s, ok := jsonString(obj["gitBranch"]); ok && s != "" {
		st.branch = s
	}
	switch rawType {
	case "user", "assistant":
		return c.messageLine(lineNo, offset, when, rawType, obj, st)
	case "system":
		return c.systemLine(lineNo, offset, when, obj, st)
	case "summary", "ai-title":
		return c.titleLine(lineNo, offset, when, rawType, obj, st)
	default:
		return c.unknownLine(lineNo, offset, when, rawType, obj, st)
	}
}

func (c Claude) messageLine(lineNo, offset int, when time.Time, rawType string, obj map[string]json.RawMessage, st *claudeState) ([]Event, error) {
	lineExtra, err := extraFrom(obj, "type", "sessionId", "cwd", "timestamp", "gitBranch", "message")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	compact := jsonBool(obj["isCompactSummary"])
	meta := jsonBool(obj["isMeta"])
	raw, ok := obj["message"]
	if !ok || isNull(raw) {
		ev, err := c.emit(st, rawType, messageEventType(compact, meta), "", when, offset, "", Tool{}, Usage{}, lineExtra)
		if err != nil {
			return nil, err
		}
		return []Event{ev}, nil
	}
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &msg); err != nil || msg == nil {
		return nil, fmt.Errorf("normalize: line %d message is not an object", lineNo)
	}
	if s, ok := jsonString(msg["model"]); ok && s != "" {
		st.model = s
	}
	role, _ := jsonString(msg["role"])
	msgExtra, err := extraFrom(msg, "role", "content", "model", "usage")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	base := mergeExtra(lineExtra, msgExtra)
	evs, err := c.contentEvents(lineNo, offset, when, rawType, role, compact, meta, msg["content"], base, st)
	if err != nil {
		return nil, err
	}
	if usage, ok := msg["usage"]; ok && !isNull(usage) {
		u, err := c.usageEvent(lineNo, offset, when, usage, base, st)
		if err != nil {
			return nil, err
		}
		evs = append(evs, u)
	}
	if len(evs) == 0 {
		ev, err := c.emit(st, rawType, messageEventType(compact, meta), role, when, offset, "", Tool{}, Usage{}, base)
		if err != nil {
			return nil, err
		}
		return []Event{ev}, nil
	}
	return evs, nil
}

func messageEventType(compact, meta bool) string {
	switch {
	case compact:
		return EventCompaction
	case meta:
		return EventMeta
	default:
		return EventMessage
	}
}

func (c Claude) contentEvents(lineNo, offset int, when time.Time, rawType, role string, compact, meta bool, raw json.RawMessage, base map[string]any, st *claudeState) ([]Event, error) {
	if isNull(raw) {
		return nil, nil
	}
	if text, ok := jsonString(raw); ok {
		ev, err := c.emit(st, rawType, messageEventType(compact, meta), role, when, offset, text, Tool{}, Usage{}, base)
		if err != nil {
			return nil, err
		}
		return []Event{ev}, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("normalize: line %d content is not text or blocks", lineNo)
	}
	out := make([]Event, 0, len(blocks))
	for _, b := range blocks {
		ev, err := c.blockEvent(lineNo, offset, when, rawType, role, compact, meta, b, base, st)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func (c Claude) blockEvent(lineNo, offset int, when time.Time, rawType, role string, compact, meta bool, raw json.RawMessage, base map[string]any, st *claudeState) (Event, error) {
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil || block == nil {
		return Event{}, fmt.Errorf("normalize: line %d content block is not an object", lineNo)
	}
	kind, _ := jsonString(block["type"])
	switch kind {
	case "text":
		text, _ := jsonString(block["text"])
		extra, err := blockExtraDrop(block, "text")
		if err != nil {
			return Event{}, err
		}
		return c.emit(st, rawType, messageEventType(compact, meta), role, when, offset, text, Tool{}, Usage{}, mergeExtra(base, extra))
	case "thinking":
		text, _ := jsonString(block["thinking"])
		extra, err := blockExtraDrop(block, "thinking")
		if err != nil {
			return Event{}, err
		}
		return c.emit(st, rawType, messageEventType(compact, meta), role, when, offset, text, Tool{}, Usage{}, mergeExtra(base, extra))
	case "redacted_thinking":
		extra, err := blockExtraDrop(block)
		if err != nil {
			return Event{}, err
		}
		extra["block_type"] = kind
		return c.emit(st, rawType, EventUnknown, role, when, offset, "", Tool{}, Usage{}, mergeExtra(base, extra))
	case "tool_use", "server_tool_use":
		name, _ := jsonString(block["name"])
		callID, _ := jsonString(block["id"])
		extra, err := blockExtraDrop(block, "id", "name", "input")
		if err != nil {
			return Event{}, err
		}
		tool := Tool{Name: strPtr(name), CallID: strPtr(callID)}
		return c.emit(st, rawType, EventToolCall, ActorAssistant, when, offset, argumentsText(block["input"]), tool, Usage{}, mergeExtra(base, extra))
	case "tool_result":
		return c.toolResultEvent(lineNo, offset, when, rawType, block, base, st)
	case "web_search_tool_result":
		return c.searchResultEvent(lineNo, offset, when, rawType, block, base, st)
	case "image":
		extra, err := imageExtra(block)
		if err != nil {
			return Event{}, err
		}
		return c.emit(st, rawType, EventUnknown, role, when, offset, "", Tool{}, Usage{}, mergeExtra(base, extra))
	default:
		extra, err := blockExtraDrop(block)
		if err != nil {
			return Event{}, err
		}
		if kind != "" {
			extra["block_type"] = kind
		}
		return c.emit(st, rawType, EventUnknown, role, when, offset, "", Tool{}, Usage{}, mergeExtra(base, extra))
	}
}

func (c Claude) toolResultEvent(lineNo, offset int, when time.Time, rawType string, block map[string]json.RawMessage, base map[string]any, st *claudeState) (Event, error) {
	callID, _ := jsonString(block["tool_use_id"])
	text, enc, err := flattenClaudeBlocks(block["content"])
	if err != nil {
		return Event{}, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	extra, err := blockExtraDrop(block, "tool_use_id", "is_error", "content")
	if err != nil {
		return Event{}, err
	}
	extra = putEncrypted(extra, enc)
	tool := Tool{CallID: strPtr(callID), IsError: jsonBoolPtr(block["is_error"])}
	return c.emit(st, rawType, EventToolResult, ActorTool, when, offset, text, tool, Usage{}, mergeExtra(base, extra))
}

func (c Claude) searchResultEvent(lineNo, offset int, when time.Time, rawType string, block map[string]json.RawMessage, base map[string]any, st *claudeState) (Event, error) {
	callID, _ := jsonString(block["tool_use_id"])
	text, enc, kept, isErr, err := claudeSearchPieces(block["content"])
	if err != nil {
		return Event{}, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	extra, err := blockExtraDrop(block, "tool_use_id", "content")
	if err != nil {
		return Event{}, err
	}
	if kept != nil {
		extra["content"] = kept
	}
	extra = putEncrypted(extra, enc)
	tool := Tool{CallID: strPtr(callID)}
	if isErr {
		t := true
		tool.IsError = &t
	}
	return c.emit(st, rawType, EventToolResult, ActorTool, when, offset, text, tool, Usage{}, mergeExtra(base, extra))
}

func (c Claude) usageEvent(lineNo, offset int, when time.Time, raw json.RawMessage, base map[string]any, st *claudeState) (Event, error) {
	usage, extra, err := parseClaudeUsage(raw)
	if err != nil {
		return Event{}, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	return c.emit(st, "usage", EventUsage, "", when, offset, "", Tool{}, usage, mergeExtra(base, extra))
}

func (c Claude) systemLine(lineNo, offset int, when time.Time, obj map[string]json.RawMessage, st *claudeState) ([]Event, error) {
	subtype, _ := jsonString(obj["subtype"])
	level, _ := jsonString(obj["level"])
	text, contentIsString := jsonString(obj["content"])
	errText, hasErrText := jsonString(obj["error"])
	if text == "" && hasErrText {
		text = errText
	}
	eventType := EventUnknown
	switch {
	case subtype == "compact_boundary":
		eventType = EventCompaction
	case level == "error" || subtype == "api_error" || strings.HasSuffix(subtype, "_error"):
		eventType = EventError
	}
	drop := []string{"type", "sessionId", "cwd", "timestamp", "gitBranch"}
	if contentIsString {
		drop = append(drop, "content")
	}
	if text == errText && hasErrText {
		drop = append(drop, "error")
	}
	extra, err := extraFrom(obj, drop...)
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	ev, err := c.emit(st, "system", eventType, "", when, offset, text, Tool{}, Usage{}, extra)
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Claude) titleLine(lineNo, offset int, when time.Time, rawType string, obj map[string]json.RawMessage, st *claudeState) ([]Event, error) {
	text, _ := jsonString(obj["summary"])
	drop := []string{"type", "sessionId", "cwd", "timestamp", "gitBranch"}
	if text != "" {
		drop = append(drop, "summary")
	}
	if text == "" {
		if s, ok := jsonString(obj["aiTitle"]); ok {
			text = s
			drop = append(drop, "aiTitle")
		}
	}
	if text == "" {
		if s, ok := jsonString(obj["title"]); ok {
			text = s
			drop = append(drop, "title")
		}
	}
	extra, err := extraFrom(obj, drop...)
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	ev, err := c.emit(st, rawType, EventUnknown, "", when, offset, text, Tool{}, Usage{}, extra)
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Claude) unknownLine(lineNo, offset int, when time.Time, rawType string, obj map[string]json.RawMessage, st *claudeState) ([]Event, error) {
	extra, err := extraFrom(obj, "type", "sessionId", "cwd", "timestamp", "gitBranch")
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	if rawType == "" {
		rawType = "unknown"
	}
	ev, err := c.emit(st, rawType, EventUnknown, "", when, offset, "", Tool{}, Usage{}, extra)
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (c Claude) emit(st *claudeState, rawType, eventType, role string, recorded time.Time, offset int, text string, tool Tool, usage Usage, extra map[string]any) (Event, error) {
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
	return Event{
		SchemaVersion:   SchemaVersion,
		EventID:         eid,
		SessionID:       claudeSessionID(c.NativeID, st.session),
		ParentSessionID: claudeParentID(c.ParentNativeID),
		Harness:         protocol.HarnessClaude,
		HarnessVersion:  strPtr(c.HarnessVersion),
		RecordedAt:      recordedAt(recorded, c.Now),
		IngestedAt:      c.Now.Format(time.RFC3339Nano),
		CWDHash:         adapter.CWDHash(cwd),
		ProjectID:       strPtr(c.ProjectID),
		Git: Git{
			Branch: strPtr(branch),
			Commit: strPtr(c.GitCommit),
			Dirty:  c.GitDirty,
		},
		Actor:       actorFor(eventType, role),
		EventType:   eventType,
		Role:        knownRole(role),
		Model:       Model{ID: strPtr(st.model)},
		ContentText: strPtr(text),
		ContentRef:  contentRef(c.Digest, offset),
		Tool:        tool,
		Usage:       usage,
		RawType:     rawType,
		Redaction:   Redaction{Status: "none", Ruleset: "v1"},
		Extra:       extraMap(extra),
	}, nil
}

func claudeSessionID(native, fromLine string) string {
	id := native
	if id == "" {
		id = fromLine
	}
	if id == "" {
		id = "unknown"
	}
	return protocol.HarnessClaude + ":" + id
}

func claudeParentID(fromManifest string) *string {
	if fromManifest == "" {
		return nil
	}
	s := protocol.HarnessClaude + ":" + fromManifest
	return &s
}

func contentRef(digest string, offset int) *string {
	if digest == "" {
		return nil
	}
	s := "sha256/" + digest + "#" + fmt.Sprintf("%d", offset)
	return &s
}

// blockExtraDrop keeps keys that are not columns. encrypted_content is
// stored once, as the original value, and is not left inside another
// field that was also copied.
func blockExtraDrop(block map[string]json.RawMessage, drop ...string) (map[string]any, error) {
	drop = append(drop, "type", "encrypted_content")
	extra, err := extraFrom(block, drop...)
	if err != nil {
		return nil, err
	}
	return hoistEncrypted(extra, block["encrypted_content"]), nil
}

func hoistEncrypted(extra map[string]any, raw json.RawMessage) map[string]any {
	if isNull(raw) {
		return extra
	}
	if s, ok := jsonString(raw); ok {
		if s == "" {
			return extra
		}
		return putEncrypted(extra, []string{s})
	}
	if extra == nil {
		extra = map[string]any{}
	}
	if _, ok := extra["encrypted_content"]; ok {
		return extra
	}
	extra["encrypted_content"] = opaque(raw)
	return extra
}

func imageExtra(block map[string]json.RawMessage) (map[string]any, error) {
	extra, err := blockExtraDrop(block, "source", "data")
	if err != nil {
		return nil, err
	}
	extra["block_type"] = "image"
	if n, ok := decodedLen(block["data"]); ok {
		extra["data_bytes"] = n
	}
	srcRaw, ok := block["source"]
	if !ok || isNull(srcRaw) {
		return extra, nil
	}
	var src map[string]json.RawMessage
	if err := json.Unmarshal(srcRaw, &src); err != nil || src == nil {
		return extra, nil
	}
	if n, ok := decodedLen(src["data"]); ok {
		extra["data_bytes"] = n
	}
	srcExtra, err := extraFrom(src, "data", "encrypted_content")
	if err != nil {
		return nil, err
	}
	if len(srcExtra) > 0 {
		extra["source"] = srcExtra
	}
	return hoistEncrypted(extra, src["encrypted_content"]), nil
}

func flattenClaudeBlocks(raw json.RawMessage) (string, []string, error) {
	if isNull(raw) {
		return "", nil, nil
	}
	if s, ok := jsonString(raw); ok {
		return s, nil, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		var one map[string]json.RawMessage
		if err2 := json.Unmarshal(raw, &one); err2 != nil || one == nil {
			return "", nil, fmt.Errorf("content is not text or blocks")
		}
		return flattenClaudeBlock(one)
	}
	var texts []string
	var enc []string
	for _, rawBlock := range blocks {
		var b map[string]json.RawMessage
		if err := json.Unmarshal(rawBlock, &b); err != nil || b == nil {
			return "", nil, fmt.Errorf("content block is not an object")
		}
		text, more, err := flattenClaudeBlock(b)
		if err != nil {
			return "", nil, err
		}
		if text != "" {
			texts = append(texts, text)
		}
		enc = append(enc, more...)
	}
	return strings.Join(texts, "\n"), enc, nil
}

func flattenClaudeBlock(b map[string]json.RawMessage) (string, []string, error) {
	var enc []string
	if s, ok := jsonString(b["encrypted_content"]); ok && s != "" {
		enc = append(enc, s)
	}
	kind, _ := jsonString(b["type"])
	switch kind {
	case "text":
		s, _ := jsonString(b["text"])
		return s, enc, nil
	case "thinking":
		s, _ := jsonString(b["thinking"])
		return s, enc, nil
	case "tool_result":
		text, more, err := flattenClaudeBlocks(b["content"])
		return text, append(enc, more...), err
	case "web_search_tool_result":
		text, more, _, _, err := claudeSearchPieces(b["content"])
		return text, append(enc, more...), err
	case "web_search_result":
		s, _ := jsonString(b["title"])
		if s == "" {
			s, _ = jsonString(b["url"])
		}
		return s, enc, nil
	default:
		return "", enc, nil
	}
}

func claudeSearchPieces(raw json.RawMessage) (text string, enc []string, kept any, isErr bool, err error) {
	if isNull(raw) {
		return "", nil, nil, false, nil
	}
	if s, ok := jsonString(raw); ok {
		return s, nil, nil, false, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		var titles []string
		var items []any
		for _, itemRaw := range arr {
			var item map[string]json.RawMessage
			if err := json.Unmarshal(itemRaw, &item); err != nil || item == nil {
				return "", nil, nil, false, fmt.Errorf("search result is not an object")
			}
			if s, ok := jsonString(item["encrypted_content"]); ok && s != "" {
				enc = append(enc, s)
			}
			title, _ := jsonString(item["title"])
			if title == "" {
				title, _ = jsonString(item["url"])
			}
			if title != "" {
				titles = append(titles, title)
			}
			cleaned, err := extraFrom(item, "encrypted_content")
			if err != nil {
				return "", nil, nil, false, err
			}
			items = append(items, cleaned)
		}
		return strings.Join(titles, "\n"), enc, items, false, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return "", nil, nil, false, fmt.Errorf("search content is not text or results")
	}
	if s, ok := jsonString(obj["encrypted_content"]); ok && s != "" {
		enc = append(enc, s)
	}
	kind, _ := jsonString(obj["type"])
	code, _ := jsonString(obj["error_code"])
	cleaned, err := extraFrom(obj, "encrypted_content")
	if err != nil {
		return "", nil, nil, false, err
	}
	return code, enc, cleaned, strings.HasSuffix(kind, "_error") || code != "", nil
}

func parseClaudeUsage(raw json.RawMessage) (Usage, map[string]any, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return Usage{}, nil, fmt.Errorf("usage is not an object")
	}
	var u Usage
	if n, ok := jsonInt(obj["input_tokens"]); ok {
		u.Input = &n
	}
	if n, ok := jsonInt(obj["output_tokens"]); ok {
		u.Output = &n
	}
	if n, ok := jsonInt(obj["cache_read_input_tokens"]); ok {
		u.CacheRead = &n
	}
	if n, ok := jsonInt(obj["cache_creation_input_tokens"]); ok {
		u.CacheWrite = &n
	}
	if n, ok := jsonFloat(obj["cost_usd"]); ok {
		u.CostUSD = &n
	}
	extra, err := extraFrom(obj, "input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "cost_usd")
	if err != nil {
		return Usage{}, nil, err
	}
	return u, extra, nil
}

func jsonBool(raw json.RawMessage) bool {
	p := jsonBoolPtr(raw)
	return p != nil && *p
}

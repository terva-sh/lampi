package normalize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"terva.sh/lampi/internal/adapter/terva"
	"terva.sh/lampi/internal/id"
	"terva.sh/lampi/internal/protocol"
)

// Terva projects one terva JSONL blob. Kind is transcript_jsonl or
// errors_jsonl; an empty Kind is a transcript. Digest, when set, is
// copied into content_ref. The struct does not open a file.
//
// session_id is "terva:" plus the native id (NativeID, else the meta
// id). Unknown harness keys are copied into extra. encrypted_content
// is copied as a string and is not written into content_text.
// Pre-compaction rows stay in the projection so a prompt from before
// a checkpoint is still searchable. Image bytes stay in the raw blob;
// the event records only that an image was there.
type Terva struct {
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
	Kind           string
}

var _ Normalizer = Terva{}

type metaState struct {
	id       string
	cwd      string
	parent   string
	model    string
	provider string
	version  string
}

// Normalize projects raw. raw is not modified. A line that is not a
// JSON object fails the whole blob; the error text does not include
// the line, which may hold a secret.
func (t Terva) Normalize(ctx context.Context, raw []byte) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.Now.IsZero() {
		t.Now = time.Now()
	}
	t.Now = t.Now.UTC()

	events := []Event{}
	var st metaState
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
		evs, err := t.line(lineNo, offset, line, &st)
		if err != nil {
			return nil, err
		}
		events = append(events, evs...)
	}
	return events, nil
}

func (t Terva) line(lineNo, offset int, line []byte, st *metaState) ([]Event, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("normalize: line %d is not a JSON object", lineNo)
	}
	if t.Kind == protocol.KindErrorsJSONL {
		ev, err := t.errorEvent(lineNo, offset, obj, st)
		if err != nil {
			return nil, err
		}
		return []Event{ev}, nil
	}
	rawType, _ := jsonString(obj["type"])
	when, _ := jsonTime(obj["at"])
	switch rawType {
	case "meta":
		return t.metaLine(lineNo, offset, when, obj, st)
	case "message":
		return t.messageLine(lineNo, offset, when, obj, st)
	case "usage":
		return t.usageLine(lineNo, offset, when, obj, st)
	case "compaction":
		return t.compactionLine(lineNo, offset, when, obj, st)
	case "error":
		ev, err := t.errorEvent(lineNo, offset, obj, st)
		if err != nil {
			return nil, err
		}
		return []Event{ev}, nil
	default:
		return t.unknownLine(lineNo, offset, when, rawType, obj, st)
	}
}

func (t Terva) metaLine(lineNo, offset int, when time.Time, obj map[string]json.RawMessage, st *metaState) ([]Event, error) {
	lineExtra, err := extraFrom(obj, "type", "meta", "at")
	if err != nil {
		return nil, err
	}
	var metaExtra map[string]any
	recorded := when
	if raw, ok := obj["meta"]; ok && !isNull(raw) {
		var started time.Time
		var metaErr error
		metaExtra, started, metaErr = applyMeta(st, raw)
		if metaErr != nil {
			return nil, fmt.Errorf("normalize: line %d: %w", lineNo, metaErr)
		}
		// `at` is when this row was appended. started is the session's
		// original start, which later meta rows repeat.
		if recorded.IsZero() {
			recorded = started
		}
	}
	ev, err := t.emit(st, "meta", EventMeta, "", recorded, offset, "", Tool{}, Usage{}, mergeExtra(lineExtra, metaExtra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func applyMeta(st *metaState, raw json.RawMessage) (map[string]any, time.Time, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, time.Time{}, fmt.Errorf("meta is not an object")
	}
	if s, ok := jsonString(obj["id"]); ok {
		st.id = s
	}
	if s, ok := jsonString(obj["cwd"]); ok {
		st.cwd = s
	}
	if _, ok := obj["parent"]; ok {
		s, _ := jsonString(obj["parent"])
		st.parent = s
	}
	if s, ok := jsonString(obj["model"]); ok {
		st.model = s
	}
	if s, ok := jsonString(obj["provider"]); ok {
		st.provider = s
	}
	if s, ok := jsonString(obj["version"]); ok {
		st.version = s
	}
	started, _ := jsonTime(obj["started"])
	extra, err := extraFrom(obj, "id", "cwd", "parent", "model", "provider", "version", "started")
	if err != nil {
		return nil, time.Time{}, err
	}
	return extra, started, nil
}

func (t Terva) messageLine(lineNo, offset int, when time.Time, obj map[string]json.RawMessage, st *metaState) ([]Event, error) {
	lineExtra, err := extraFrom(obj, "type", "message", "at")
	if err != nil {
		return nil, err
	}
	raw, ok := obj["message"]
	if !ok || isNull(raw) {
		ev, err := t.emit(st, "message", EventMessage, "", when, offset, "", Tool{}, Usage{}, lineExtra)
		if err != nil {
			return nil, err
		}
		return []Event{ev}, nil
	}
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &msg); err != nil || msg == nil {
		return nil, fmt.Errorf("normalize: line %d message is not an object", lineNo)
	}
	role, _ := jsonString(msg["role"])
	msgWhen, hasWhen := jsonTime(msg["time"])
	if hasWhen {
		when = msgWhen
	}
	msgExtra, err := extraFrom(msg, "role", "content", "time")
	if err != nil {
		return nil, err
	}
	base := mergeExtra(lineExtra, msgExtra)
	var blocks []json.RawMessage
	if c, ok := msg["content"]; ok && !isNull(c) {
		if err := json.Unmarshal(c, &blocks); err != nil {
			return nil, fmt.Errorf("normalize: line %d content is not an array", lineNo)
		}
	}
	if len(blocks) == 0 {
		ev, err := t.emit(st, "message", EventMessage, role, when, offset, "", Tool{}, Usage{}, base)
		if err != nil {
			return nil, err
		}
		return []Event{ev}, nil
	}
	out := make([]Event, 0, len(blocks))
	for _, b := range blocks {
		ev, err := t.blockEvent(lineNo, offset, when, role, b, base, st)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func (t Terva) blockEvent(lineNo, offset int, when time.Time, role string, raw json.RawMessage, base map[string]any, st *metaState) (Event, error) {
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil || block == nil {
		return Event{}, fmt.Errorf("normalize: line %d content block is not an object", lineNo)
	}
	kind := blockKind(block)
	extra, err := blockExtra(block, kind)
	if err != nil {
		return Event{}, err
	}
	extra = mergeExtra(base, extra)
	switch kind {
	case "text":
		text, _ := jsonString(block["text"])
		return t.emit(st, "message", EventMessage, role, when, offset, text, Tool{}, Usage{}, extra)
	case "reasoning":
		text, _ := jsonString(block["summary"])
		return t.emit(st, "message", EventMessage, role, when, offset, text, Tool{}, Usage{}, extra)
	case "tool_call":
		name, _ := jsonString(block["name"])
		callID, _ := jsonString(block["id"])
		tool := Tool{Name: strPtr(name), CallID: strPtr(callID)}
		return t.emit(st, "message", EventToolCall, role, when, offset, argumentsText(block["arguments"]), tool, Usage{}, extra)
	case "tool_result":
		callID, _ := jsonString(block["call_id"])
		tool := Tool{CallID: strPtr(callID), IsError: jsonBoolPtr(block["is_error"])}
		text, enc := flattenContent(block["content"])
		extra = putEncrypted(extra, enc)
		return t.emit(st, "message", EventToolResult, role, when, offset, text, tool, Usage{}, extra)
	case "compaction_summary":
		text, _ := jsonString(block["text"])
		return t.emit(st, "message", EventCompaction, role, when, offset, text, Tool{}, Usage{}, extra)
	case "image":
		extra["block_type"] = "image"
		if n, ok := decodedLen(block["data"]); ok {
			extra["data_bytes"] = n
		}
		return t.emit(st, "message", EventUnknown, role, when, offset, "", Tool{}, Usage{}, extra)
	default:
		if kind != "" {
			extra["block_type"] = kind
		}
		return t.emit(st, "message", EventUnknown, role, when, offset, "", Tool{}, Usage{}, extra)
	}
}

func (t Terva) usageLine(lineNo, offset int, when time.Time, obj map[string]json.RawMessage, st *metaState) ([]Event, error) {
	usage, usageExtra, err := parseUsage(obj["usage"])
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	lineExtra, err := extraFrom(obj, "type", "usage", "at")
	if err != nil {
		return nil, err
	}
	ev, err := t.emit(st, "usage", EventUsage, "", when, offset, "", Tool{}, usage, mergeExtra(lineExtra, usageExtra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (t Terva) compactionLine(lineNo, offset int, when time.Time, obj map[string]json.RawMessage, st *metaState) ([]Event, error) {
	text, enc, err := flattenMessages(obj["messages"])
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	usage, usageExtra, err := parseUsage(obj["usage"])
	if err != nil {
		return nil, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	// Keep the checkpoint messages in extra so fields that are not
	// columns (provider, id, encrypted_content) survive the projection.
	lineExtra, err := extraFrom(obj, "type", "usage", "at")
	if err != nil {
		return nil, err
	}
	extra := putEncrypted(mergeExtra(lineExtra, usageExtra), enc)
	ev, err := t.emit(st, "compaction", EventCompaction, "", when, offset, text, Tool{}, usage, extra)
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (t Terva) unknownLine(lineNo, offset int, when time.Time, rawType string, obj map[string]json.RawMessage, st *metaState) ([]Event, error) {
	extra, err := extraFrom(obj, "type", "at")
	if err != nil {
		return nil, err
	}
	text, _ := jsonString(obj["title"])
	if rawType == "" {
		rawType = "unknown"
	}
	ev, err := t.emit(st, rawType, EventUnknown, "", when, offset, text, Tool{}, Usage{}, extra)
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (t Terva) errorEvent(lineNo, offset int, obj map[string]json.RawMessage, st *metaState) (Event, error) {
	when, _ := jsonTime(obj["time"])
	if at, ok := jsonTime(obj["at"]); ok {
		when = at
	}
	if s, ok := jsonString(obj["provider"]); ok && s != "" {
		st.provider = s
	}
	if s, ok := jsonString(obj["model"]); ok && s != "" {
		st.model = s
	}
	text, _ := jsonString(obj["error"])
	rawType, _ := jsonString(obj["type"])
	if rawType == "" {
		rawType = "error"
	}
	extra, err := extraFrom(obj, "type", "error", "time", "at", "provider", "model")
	if err != nil {
		return Event{}, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	return t.emit(st, rawType, EventError, "", when, offset, text, Tool{}, Usage{}, extra)
}

func (t Terva) emit(st *metaState, rawType, eventType, role string, recorded time.Time, offset int, text string, tool Tool, usage Usage, extra map[string]any) (Event, error) {
	eid, err := id.New(t.Now)
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
		cwd = t.CWD
	}
	return Event{
		SchemaVersion:   SchemaVersion,
		EventID:         eid,
		SessionID:       sessionID(t.NativeID, st.id),
		ParentSessionID: parentID(t.ParentNativeID, st.parent),
		Harness:         protocol.HarnessTerva,
		HarnessVersion:  strPtr(firstNonEmpty(t.HarnessVersion, st.version)),
		RecordedAt:      recordedAt(recorded, t.Now),
		IngestedAt:      t.Now.Format(time.RFC3339Nano),
		CWDHash:         terva.CWDHash(cwd),
		ProjectID:       strPtr(t.ProjectID),
		Git: Git{
			Branch: strPtr(t.GitBranch),
			Commit: strPtr(t.GitCommit),
			Dirty:  t.GitDirty,
		},
		Actor:       actorFor(eventType, role),
		EventType:   eventType,
		Role:        knownRole(role),
		Model:       Model{Provider: strPtr(st.provider), ID: strPtr(st.model)},
		ContentText: strPtr(text),
		ContentRef:  t.contentRef(offset),
		Tool:        tool,
		Usage:       usage,
		RawType:     rawType,
		Redaction:   Redaction{Status: "none", Ruleset: "v1"},
		Extra:       extraMap(extra),
	}, nil
}

func (t Terva) contentRef(offset int) *string {
	if t.Digest == "" {
		return nil
	}
	s := "sha256/" + t.Digest + "#" + strconv.Itoa(offset)
	return &s
}

func sessionID(native, metaID string) string {
	id := native
	if id == "" {
		id = metaID
	}
	if id == "" {
		id = "unknown"
	}
	return protocol.HarnessTerva + ":" + id
}

func parentID(fromManifest, fromMeta string) *string {
	id := fromMeta
	if id == "" {
		id = fromManifest
	}
	if id == "" {
		return nil
	}
	s := protocol.HarnessTerva + ":" + id
	return &s
}

func actorFor(eventType, role string) string {
	switch eventType {
	case EventToolCall:
		return ActorAssistant
	case EventToolResult:
		return ActorTool
	case EventMessage:
		switch role {
		case ActorUser, ActorAssistant, ActorSystem, ActorTool:
			return role
		default:
			return ActorHarness
		}
	default:
		return ActorHarness
	}
}

func knownRole(role string) *string {
	switch role {
	case ActorUser, ActorAssistant, ActorSystem, ActorTool:
		s := role
		return &s
	default:
		return nil
	}
}

func recordedAt(recorded, ingested time.Time) string {
	if recorded.IsZero() {
		recorded = ingested
	}
	return recorded.UTC().Format(time.RFC3339Nano)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func blockKind(block map[string]json.RawMessage) string {
	if s, ok := jsonString(block["type"]); ok && s != "" {
		return s
	}
	switch {
	case hasKey(block, "text"):
		return "text"
	case hasKey(block, "call_id"):
		return "tool_result"
	case hasKey(block, "name"):
		return "tool_call"
	case hasKey(block, "encrypted_content"):
		return "reasoning"
	case hasKey(block, "mime_type"), hasKey(block, "data"):
		return "image"
	default:
		return ""
	}
}

// blockExtra keeps harness fields that do not have a column of their
// own. encrypted_content is stored as the original string. Image bytes
// are counted and left in the raw blob.
func blockExtra(block map[string]json.RawMessage, kind string) (map[string]any, error) {
	drop := []string{"type"}
	switch kind {
	case "text":
		drop = append(drop, "text")
	case "reasoning":
		drop = append(drop, "summary", "encrypted_content")
	case "tool_call":
		drop = append(drop, "id", "name")
	case "tool_result":
		drop = append(drop, "call_id", "is_error", "content", "encrypted_content")
	case "compaction_summary":
		drop = append(drop, "text", "encrypted_content")
	case "image":
		drop = append(drop, "data")
	}
	extra, err := extraFrom(block, drop...)
	if err != nil {
		return nil, err
	}
	if raw, ok := block["encrypted_content"]; ok && !isNull(raw) {
		extra["encrypted_content"] = opaque(raw)
	}
	return extra, nil
}

func opaque(raw json.RawMessage) any {
	if s, ok := jsonString(raw); ok {
		return s
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

func putEncrypted(extra map[string]any, enc []string) map[string]any {
	if len(enc) == 0 {
		return extra
	}
	if extra == nil {
		extra = map[string]any{}
	}
	if prev, ok := extra["encrypted_content"]; ok {
		enc = append(encrypteds(prev), enc...)
	}
	if len(enc) == 1 {
		extra["encrypted_content"] = enc[0]
		return extra
	}
	extra["encrypted_content"] = enc
	return extra
}

func encrypteds(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func flattenMessages(raw json.RawMessage) (string, []string, error) {
	if isNull(raw) {
		return "", nil, nil
	}
	var msgs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return "", nil, fmt.Errorf("compaction messages are not an array")
	}
	var texts []string
	var enc []string
	for _, m := range msgs {
		text, more := flattenContent(m["content"])
		if text != "" {
			texts = append(texts, text)
		}
		enc = append(enc, more...)
	}
	return strings.Join(texts, "\n"), enc, nil
}

func flattenContent(raw json.RawMessage) (string, []string) {
	if isNull(raw) {
		return "", nil
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		if s, ok := jsonString(raw); ok {
			return s, nil
		}
		return "", nil
	}
	var texts []string
	var enc []string
	for _, b := range blocks {
		kind := blockKind(b)
		switch kind {
		case "text":
			if s, ok := jsonString(b["text"]); ok && s != "" {
				texts = append(texts, s)
			}
		case "reasoning":
			if s, ok := jsonString(b["summary"]); ok && s != "" {
				texts = append(texts, s)
			}
		case "tool_result":
			text, more := flattenContent(b["content"])
			if text != "" {
				texts = append(texts, text)
			}
			enc = append(enc, more...)
		}
		if s, ok := jsonString(b["encrypted_content"]); ok && s != "" {
			enc = append(enc, s)
		}
	}
	return strings.Join(texts, "\n"), enc
}

func parseUsage(raw json.RawMessage) (Usage, map[string]any, error) {
	if isNull(raw) {
		return Usage{}, nil, nil
	}
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
	if n, ok := jsonInt(obj["cache_read_tokens"]); ok {
		u.CacheRead = &n
	}
	if n, ok := jsonInt(obj["cache_write_tokens"]); ok {
		u.CacheWrite = &n
	}
	if n, ok := jsonFloat(obj["cost_usd"]); ok {
		u.CostUSD = &n
	}
	extra, err := extraFrom(obj, "input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "cost_usd")
	if err != nil {
		return Usage{}, nil, err
	}
	return u, extra, nil
}

func argumentsText(raw json.RawMessage) string {
	if isNull(raw) {
		return ""
	}
	if s, ok := jsonString(raw); ok {
		return s
	}
	return string(bytes.TrimSpace(raw))
}

func extraFrom(obj map[string]json.RawMessage, drop ...string) (map[string]any, error) {
	skip := make(map[string]bool, len(drop))
	for _, d := range drop {
		skip[d] = true
	}
	out := map[string]any{}
	for k, raw := range obj {
		if skip[k] {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("normalize: extra %s: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

func mergeExtra(parts ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

func jsonString(raw json.RawMessage) (string, bool) {
	if isNull(raw) {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func jsonInt(raw json.RawMessage) (int, bool) {
	if isNull(raw) {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

func jsonFloat(raw json.RawMessage) (float64, bool) {
	if isNull(raw) {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

func jsonBoolPtr(raw json.RawMessage) *bool {
	if isNull(raw) {
		return nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil
	}
	return &b
}

func jsonTime(raw json.RawMessage) (time.Time, bool) {
	s, ok := jsonString(raw)
	if !ok || s == "" {
		return time.Time{}, false
	}
	if tm, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return tm.UTC(), true
	}
	if tm, err := time.Parse(time.RFC3339, s); err == nil {
		return tm.UTC(), true
	}
	return time.Time{}, false
}

func decodedLen(raw json.RawMessage) (int, bool) {
	if isNull(raw) {
		return 0, false
	}
	var b []byte
	if err := json.Unmarshal(raw, &b); err != nil {
		return 0, false
	}
	return len(b), true
}

func hasKey(obj map[string]json.RawMessage, key string) bool {
	raw, ok := obj[key]
	return ok && !isNull(raw)
}

func isNull(raw json.RawMessage) bool {
	return len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

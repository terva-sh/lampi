package normalize

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/id"
	"terva.sh/lampi/internal/protocol"
)

// OpenCode projects one OpenCode export document stored as
// transcript_jsonl. The bytes are the file the adapter uploaded. The
// struct does not open a file. A SQLite database blob is not an export
// and is not projected.
//
// The document is one JSON object, {info, messages}, not JSONL.
// session_id is "opencode:" plus the native id (NativeID, else info.id).
// info.parentID is a parent session. A message parentID is a message
// pointer, so it stays in extra. Unknown keys are copied into extra.
// encrypted_content is copied as stored and is not written into
// content_text. File bytes stay in the raw blob. HarnessVersion is
// the caller's pinned reader version, not the version on info.
type OpenCode struct {
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

var _ Normalizer = OpenCode{}

type openCodeState struct {
	session  string
	parent   string
	cwd      string
	model    string
	provider string
}

// Normalize projects raw. raw is not modified. A body that is not one
// JSON object fails the whole blob; the error text does not include
// the body, which may hold a secret. A SQLite database is that failure.
func (o OpenCode) Normalize(ctx context.Context, raw []byte) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	o.Now = o.Now.UTC()

	obj, err := decodeOpenCodeExport(raw)
	if err != nil {
		return nil, err
	}
	return o.project(ctx, raw, obj)
}

func decodeOpenCodeExport(raw []byte) (map[string]json.RawMessage, error) {
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 || trim[0] != '{' {
		return nil, fmt.Errorf("normalize: export is not a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, fmt.Errorf("normalize: export is not a JSON object")
	}
	var trail json.RawMessage
	if err := dec.Decode(&trail); err != io.EOF {
		return nil, fmt.Errorf("normalize: export is not a JSON object")
	}
	return obj, nil
}

func (o OpenCode) project(ctx context.Context, raw []byte, obj map[string]json.RawMessage) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	top, err := extraFrom(obj, "info", "messages")
	if err != nil {
		return nil, err
	}
	var st openCodeState
	var events []Event
	cursor := 0
	if rawInfo, ok := obj["info"]; ok && !isNull(rawInfo) {
		ev, next, err := o.sessionInfo(raw, rawInfo, top, &st, cursor)
		if err != nil {
			return nil, err
		}
		cursor = next
		events = append(events, ev)
	} else if len(top) > 0 {
		ev, err := o.emit(&st, "export", EventMeta, "", time.Time{}, 0, "", Tool{}, Usage{}, top)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	if rawMsgs, ok := obj["messages"]; ok && !isNull(rawMsgs) {
		evs, err := o.messages(ctx, raw, rawMsgs, &st, cursor)
		if err != nil {
			return nil, err
		}
		events = append(events, evs...)
	}
	return events, nil
}

func (o OpenCode) sessionInfo(raw []byte, rawInfo json.RawMessage, top map[string]any, st *openCodeState, cursor int) (Event, int, error) {
	var info map[string]json.RawMessage
	if err := json.Unmarshal(rawInfo, &info); err != nil || info == nil {
		return Event{}, cursor, fmt.Errorf("normalize: info is not an object")
	}
	offset := rawOffset(raw, rawInfo, cursor)
	if s, ok := jsonString(info["id"]); ok && s != "" {
		st.session = s
	}
	if s, ok := jsonString(info["directory"]); ok && s != "" {
		st.cwd = s
	}
	if s, ok := jsonString(info["parentID"]); ok {
		st.parent = s
	}
	when, _ := openCodeTime(info["time"])
	extra, err := extraFrom(info, "id", "directory", "parentID")
	if err != nil {
		return Event{}, cursor, fmt.Errorf("normalize: info: %w", err)
	}
	ev, err := o.emit(st, "info", EventMeta, "", when, offset, "", Tool{}, Usage{}, mergeExtra(top, extra))
	if err != nil {
		return Event{}, cursor, err
	}
	return ev, offset + len(rawInfo), nil
}

func (o OpenCode) messages(ctx context.Context, raw []byte, rawMsgs json.RawMessage, st *openCodeState, cursor int) ([]Event, error) {
	var msgs []json.RawMessage
	if err := json.Unmarshal(rawMsgs, &msgs); err != nil {
		return nil, fmt.Errorf("normalize: messages is not an array")
	}
	var events []Event
	for i, rawMsg := range msgs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		evs, next, err := o.message(raw, rawMsg, st, cursor, i+1)
		if err != nil {
			return nil, err
		}
		cursor = next
		events = append(events, evs...)
	}
	return events, nil
}

func (o OpenCode) message(raw []byte, rawMsg json.RawMessage, st *openCodeState, cursor, n int) ([]Event, int, error) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(rawMsg, &msg); err != nil || msg == nil {
		return nil, cursor, fmt.Errorf("normalize: message %d is not an object", n)
	}
	offset := rawOffset(raw, rawMsg, cursor)
	next := offset + len(rawMsg)
	wrap, err := extraFrom(msg, "info", "parts")
	if err != nil {
		return nil, cursor, fmt.Errorf("normalize: message %d: %w", n, err)
	}
	var info map[string]json.RawMessage
	when := time.Time{}
	role := ""
	if rawInfo, ok := msg["info"]; ok && !isNull(rawInfo) {
		if err := json.Unmarshal(rawInfo, &info); err != nil || info == nil {
			return nil, cursor, fmt.Errorf("normalize: message %d info is not an object", n)
		}
		role, _ = jsonString(info["role"])
		when, _ = openCodeTime(info["time"])
		applyOpenCodeModel(st, info)
		applyOpenCodePath(st, info)
	}
	infoExtra, err := openCodeInfoExtra(info)
	if err != nil {
		return nil, cursor, fmt.Errorf("normalize: message %d: %w", n, err)
	}
	base := mergeExtra(wrap, infoExtra)

	var events []Event
	if rawParts, ok := msg["parts"]; ok && !isNull(rawParts) {
		evs, err := o.parts(raw, rawParts, st, role, when, base, n)
		if err != nil {
			return nil, cursor, err
		}
		events = append(events, evs...)
	}
	if info != nil {
		_, hasTokens := info["tokens"]
		if (hasTokens && !isNull(info["tokens"])) || hasKey(info, "cost") {
			ev, err := o.usageFrom(st, "tokens", when, offset, info["tokens"], info["cost"], base)
			if err != nil {
				return nil, cursor, fmt.Errorf("normalize: message %d: %w", n, err)
			}
			events = append(events, ev)
		}
		if rawErr, ok := info["error"]; ok && !isNull(rawErr) {
			ev, err := o.errorFrom(st, "error", when, offset, rawErr, base)
			if err != nil {
				return nil, cursor, fmt.Errorf("normalize: message %d: %w", n, err)
			}
			events = append(events, ev)
		}
	}
	if len(events) == 0 && len(base) > 0 {
		ev, err := o.emit(st, "message", EventMessage, role, when, offset, "", Tool{}, Usage{}, base)
		if err != nil {
			return nil, cursor, err
		}
		events = append(events, ev)
	}
	return events, next, nil
}

func openCodeInfoExtra(info map[string]json.RawMessage) (map[string]any, error) {
	if info == nil {
		return map[string]any{}, nil
	}
	extra, err := extraFrom(info, "role", "time", "model", "modelID", "providerID", "tokens", "cost", "error", "path")
	if err != nil {
		return nil, err
	}
	if raw, ok := info["model"]; ok && !isNull(raw) {
		kept, err := modelExtra(raw)
		if err != nil {
			return nil, err
		}
		if len(kept) > 0 {
			extra["model"] = kept
		}
	}
	if raw, ok := info["path"]; ok && !isNull(raw) {
		var path map[string]json.RawMessage
		if err := json.Unmarshal(raw, &path); err != nil || path == nil {
			extra["path"] = opaque(raw)
		} else {
			pathExtra, err := extraFrom(path, "cwd")
			if err != nil {
				return nil, err
			}
			if len(pathExtra) > 0 {
				extra["path"] = pathExtra
			}
		}
	}
	return extra, nil
}

func modelExtra(raw json.RawMessage) (map[string]any, error) {
	var model map[string]json.RawMessage
	if err := json.Unmarshal(raw, &model); err != nil || model == nil {
		return nil, nil
	}
	return extraFrom(model, "providerID", "modelID")
}

func applyOpenCodeModel(st *openCodeState, info map[string]json.RawMessage) {
	if s, ok := jsonString(info["modelID"]); ok && s != "" {
		st.model = s
	}
	if s, ok := jsonString(info["providerID"]); ok && s != "" {
		st.provider = s
	}
	raw, ok := info["model"]
	if !ok || isNull(raw) {
		return
	}
	var model map[string]json.RawMessage
	if err := json.Unmarshal(raw, &model); err != nil || model == nil {
		return
	}
	if s, ok := jsonString(model["modelID"]); ok && s != "" {
		st.model = s
	}
	if s, ok := jsonString(model["providerID"]); ok && s != "" {
		st.provider = s
	}
}

func applyOpenCodePath(st *openCodeState, info map[string]json.RawMessage) {
	raw, ok := info["path"]
	if !ok || isNull(raw) {
		return
	}
	var path map[string]json.RawMessage
	if err := json.Unmarshal(raw, &path); err != nil || path == nil {
		return
	}
	if s, ok := jsonString(path["cwd"]); ok && s != "" {
		st.cwd = s
	}
}

func (o OpenCode) parts(raw []byte, rawParts json.RawMessage, st *openCodeState, role string, when time.Time, base map[string]any, n int) ([]Event, error) {
	var parts []json.RawMessage
	if err := json.Unmarshal(rawParts, &parts); err != nil {
		return nil, fmt.Errorf("normalize: message %d parts is not an array", n)
	}
	var events []Event
	cursor := rawOffset(raw, rawParts, 0)
	for i, rawPart := range parts {
		evs, next, err := o.part(raw, rawPart, st, role, when, base, n, i+1, cursor)
		if err != nil {
			return nil, err
		}
		cursor = next
		events = append(events, evs...)
	}
	return events, nil
}

func (o OpenCode) part(raw []byte, rawPart json.RawMessage, st *openCodeState, role string, when time.Time, base map[string]any, n, p, cursor int) ([]Event, int, error) {
	var part map[string]json.RawMessage
	if err := json.Unmarshal(rawPart, &part); err != nil || part == nil {
		return nil, cursor, fmt.Errorf("normalize: message %d part %d is not an object", n, p)
	}
	offset := rawOffset(raw, rawPart, cursor)
	next := offset + len(rawPart)
	kind, _ := jsonString(part["type"])
	if partWhen, ok := openCodeTime(part["time"]); ok {
		when = partWhen
	}
	var (
		evs []Event
		err error
	)
	switch kind {
	case "text", "reasoning":
		evs, err = o.textPart(st, kind, role, when, offset, part, base)
	case "tool":
		evs, err = o.toolPart(st, when, offset, part, base)
	case "file":
		evs, err = o.filePart(st, role, when, offset, part, base)
	case "compaction":
		evs, err = o.typedPart(st, kind, EventCompaction, role, when, offset, part, base, "")
	case "subtask":
		text, _ := jsonString(part["prompt"])
		evs, err = o.typedPart(st, kind, EventMessage, role, when, offset, part, base, text)
	case "step-finish":
		evs, err = o.stepFinish(st, when, offset, part, base)
	case "retry":
		evs, err = o.retryPart(st, when, offset, part, base)
	default:
		evs, err = o.typedPart(st, kind, EventUnknown, role, when, offset, part, base, "")
	}
	if err != nil {
		return nil, cursor, fmt.Errorf("normalize: message %d part %d: %w", n, p, err)
	}
	return evs, next, nil
}

func (o OpenCode) textPart(st *openCodeState, kind, role string, when time.Time, offset int, part map[string]json.RawMessage, base map[string]any) ([]Event, error) {
	text, _ := jsonString(part["text"])
	extra, err := extraFrom(part, "type", "text")
	if err != nil {
		return nil, err
	}
	ev, err := o.emit(st, kind, EventMessage, role, when, offset, text, Tool{}, Usage{}, mergeExtra(base, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (o OpenCode) typedPart(st *openCodeState, kind, eventType, role string, when time.Time, offset int, part map[string]json.RawMessage, base map[string]any, text string) ([]Event, error) {
	if kind == "" {
		kind = "unknown"
	}
	drop := []string{"type"}
	if text != "" {
		drop = append(drop, "prompt")
	}
	extra, err := extraFrom(part, drop...)
	if err != nil {
		return nil, err
	}
	ev, err := o.emit(st, kind, eventType, role, when, offset, text, Tool{}, Usage{}, mergeExtra(base, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (o OpenCode) toolPart(st *openCodeState, when time.Time, offset int, part map[string]json.RawMessage, base map[string]any) ([]Event, error) {
	name, _ := jsonString(part["tool"])
	callID, _ := jsonString(part["callID"])
	partExtra, err := extraFrom(part, "type", "tool", "callID", "state")
	if err != nil {
		return nil, err
	}
	tool := Tool{Name: strPtr(name), CallID: strPtr(callID)}
	var state map[string]json.RawMessage
	if raw, ok := part["state"]; ok && !isNull(raw) {
		if err := json.Unmarshal(raw, &state); err != nil || state == nil {
			partExtra["state"] = opaque(raw)
			state = nil
		}
	}
	if stateWhen, ok := openCodeTime(state["time"]); ok {
		when = stateWhen
	}
	inputText, inputEnc := argumentsWithoutEncrypted(state["input"])
	stateExtra, err := toolStateExtra(state)
	if err != nil {
		return nil, err
	}
	extra := mergeExtra(base, partExtra, stateExtra)
	extra = putEncryptedAny(extra, inputEnc)
	call, err := o.emit(st, "tool", EventToolCall, ActorAssistant, when, offset, inputText, tool, Usage{}, extra)
	if err != nil {
		return nil, err
	}
	events := []Event{call}
	status, _ := jsonString(state["status"])
	switch status {
	case "completed":
		out, _ := jsonString(state["output"])
		resultTool := tool
		resultTool.IsError = boolPtr(false)
		result, err := o.emit(st, "tool", EventToolResult, ActorTool, when, offset, out, resultTool, Usage{}, extra)
		if err != nil {
			return nil, err
		}
		events = append(events, result)
	case "error":
		out, _ := jsonString(state["error"])
		resultTool := tool
		resultTool.IsError = boolPtr(true)
		result, err := o.emit(st, "tool", EventToolResult, ActorTool, when, offset, out, resultTool, Usage{}, extra)
		if err != nil {
			return nil, err
		}
		events = append(events, result)
	}
	return events, nil
}

func toolStateExtra(state map[string]json.RawMessage) (map[string]any, error) {
	if state == nil {
		return map[string]any{}, nil
	}
	return extraFrom(state, "status", "input", "output", "error", "time")
}

func (o OpenCode) filePart(st *openCodeState, role string, when time.Time, offset int, part map[string]json.RawMessage, base map[string]any) ([]Event, error) {
	extra, err := extraFrom(part, "type")
	if err != nil {
		return nil, err
	}
	ev, err := o.emit(st, "file", EventUnknown, role, when, offset, "", Tool{}, Usage{}, mergeExtra(base, extra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (o OpenCode) stepFinish(st *openCodeState, when time.Time, offset int, part map[string]json.RawMessage, base map[string]any) ([]Event, error) {
	partExtra, err := extraFrom(part, "type", "tokens", "cost")
	if err != nil {
		return nil, err
	}
	ev, err := o.usageFrom(st, "step-finish", when, offset, part["tokens"], part["cost"], mergeExtra(base, partExtra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (o OpenCode) retryPart(st *openCodeState, when time.Time, offset int, part map[string]json.RawMessage, base map[string]any) ([]Event, error) {
	if partWhen, ok := openCodeTime(part["time"]); ok {
		when = partWhen
	}
	partExtra, err := extraFrom(part, "type", "error", "time")
	if err != nil {
		return nil, err
	}
	ev, err := o.errorFrom(st, "retry", when, offset, part["error"], mergeExtra(base, partExtra))
	if err != nil {
		return nil, err
	}
	return []Event{ev}, nil
}

func (o OpenCode) usageFrom(st *openCodeState, rawType string, when time.Time, offset int, tokens, cost json.RawMessage, base map[string]any) (Event, error) {
	usage, extra, err := openCodeUsage(tokens, cost)
	if err != nil {
		return Event{}, err
	}
	return o.emit(st, rawType, EventUsage, "", when, offset, "", Tool{}, usage, mergeExtra(base, extra))
}

func (o OpenCode) errorFrom(st *openCodeState, rawType string, when time.Time, offset int, raw json.RawMessage, base map[string]any) (Event, error) {
	text := openCodeErrorText(raw)
	extra, err := errorExtra(raw)
	if err != nil {
		return Event{}, err
	}
	return o.emit(st, rawType, EventError, "", when, offset, text, Tool{}, Usage{}, mergeExtra(base, extra))
}

func (o OpenCode) emit(st *openCodeState, rawType, eventType, role string, recorded time.Time, offset int, text string, tool Tool, usage Usage, extra map[string]any) (Event, error) {
	eid, err := id.New(o.Now)
	if err != nil {
		return Event{}, err
	}
	extra = scrubNestedDataURLs(extra)
	extra = hoistOpenCodeEncrypted(extra)
	text = withoutCiphertext(text, extra)
	if role != "" && knownRole(role) == nil {
		extra["role"] = role
	}
	cwd := st.cwd
	if cwd == "" {
		cwd = o.CWD
	}
	return Event{
		SchemaVersion:   SchemaVersion,
		EventID:         eid,
		SessionID:       opencodeSessionID(o.NativeID, st.session),
		ParentSessionID: opencodeParentID(st.parent, o.ParentNativeID),
		Harness:         protocol.HarnessOpenCode,
		HarnessVersion:  strPtr(o.HarnessVersion),
		RecordedAt:      recordedAt(recorded, o.Now),
		IngestedAt:      o.Now.Format(time.RFC3339Nano),
		CWDHash:         adapter.CWDHash(cwd),
		ProjectID:       strPtr(o.ProjectID),
		Git: Git{
			Branch: strPtr(o.GitBranch),
			Commit: strPtr(o.GitCommit),
			Dirty:  o.GitDirty,
		},
		Actor:       actorFor(eventType, role),
		EventType:   eventType,
		Role:        knownRole(role),
		Model:       Model{Provider: strPtr(st.provider), ID: strPtr(st.model)},
		ContentText: strPtr(text),
		ContentRef:  contentRef(o.Digest, offset),
		Tool:        tool,
		Usage:       usage,
		RawType:     rawType,
		Redaction:   Redaction{Status: "none", Ruleset: "v1"},
		Extra:       extraMap(extra),
	}, nil
}

func opencodeSessionID(native, fromInfo string) string {
	id := native
	if id == "" {
		id = fromInfo
	}
	if id == "" {
		id = "unknown"
	}
	return protocol.HarnessOpenCode + ":" + id
}

func opencodeParentID(fromInfo, fromManifest string) *string {
	id := fromInfo
	if id == "" {
		id = fromManifest
	}
	if id == "" {
		return nil
	}
	s := protocol.HarnessOpenCode + ":" + id
	return &s
}

func openCodeUsage(tokens, cost json.RawMessage) (Usage, map[string]any, error) {
	var u Usage
	extra := map[string]any{}
	if !isNull(tokens) {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(tokens, &obj); err != nil || obj == nil {
			return Usage{}, nil, fmt.Errorf("tokens is not an object")
		}
		if n, ok := jsonInt(obj["input"]); ok {
			u.Input = &n
		}
		if n, ok := jsonInt(obj["output"]); ok {
			u.Output = &n
		}
		if n, ok := jsonInt(obj["reasoning"]); ok {
			extra["reasoning"] = n
		}
		if n, ok := jsonInt(obj["total"]); ok {
			extra["total"] = n
		}
		read, write, cacheExtra, err := openCodeCache(obj["cache"])
		if err != nil {
			return Usage{}, nil, err
		}
		u.CacheRead = read
		u.CacheWrite = write
		kept, err := extraFrom(obj, "input", "output", "reasoning", "total", "cache")
		if err != nil {
			return Usage{}, nil, err
		}
		extra = mergeExtra(extra, kept)
		if len(cacheExtra) > 0 {
			extra["cache"] = cacheExtra
		}
	}
	if n, ok := jsonFloat(cost); ok {
		u.CostUSD = &n
	}
	return u, extra, nil
}

func openCodeCache(raw json.RawMessage) (read, write *int, extra map[string]any, err error) {
	if isNull(raw) {
		return nil, nil, nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, nil, nil, fmt.Errorf("cache is not an object")
	}
	if n, ok := jsonInt(obj["read"]); ok {
		read = &n
	}
	if n, ok := jsonInt(obj["write"]); ok {
		write = &n
	}
	extra, err = extraFrom(obj, "read", "write")
	return read, write, extra, err
}

func openCodeErrorText(raw json.RawMessage) string {
	if isNull(raw) {
		return ""
	}
	if s, ok := jsonString(raw); ok {
		return s
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return ""
	}
	if s, ok := jsonString(obj["message"]); ok && s != "" {
		return s
	}
	if s := nestedMessage(obj["data"]); s != "" {
		return s
	}
	if s, ok := jsonString(obj["name"]); ok {
		return s
	}
	return ""
}

func nestedMessage(raw json.RawMessage) string {
	if isNull(raw) {
		return ""
	}
	if s, ok := jsonString(raw); ok {
		return s
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return ""
	}
	s, _ := jsonString(obj["message"])
	return s
}

func errorExtra(raw json.RawMessage) (map[string]any, error) {
	if isNull(raw) {
		return map[string]any{}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return map[string]any{"error": opaque(raw)}, nil
	}
	return extraFrom(obj)
}

func argumentsWithoutEncrypted(raw json.RawMessage) (string, []any) {
	if isNull(raw) {
		return "", nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return argumentsText(raw), nil
	}
	cleaned, enc := stripEncryptedValue(v)
	if len(enc) == 0 {
		return argumentsText(raw), nil
	}
	if s, ok := cleaned.(string); ok {
		return s, enc
	}
	var buf bytes.Buffer
	encJSON := json.NewEncoder(&buf)
	encJSON.SetEscapeHTML(false)
	if err := encJSON.Encode(cleaned); err != nil {
		return "", enc
	}
	return strings.TrimSpace(buf.String()), enc
}

func scrubNestedDataURLs(extra map[string]any) map[string]any {
	if extra == nil {
		return map[string]any{}
	}
	scrubbed, ok := scrubDataValue(extra).(map[string]any)
	if !ok || scrubbed == nil {
		return map[string]any{}
	}
	return scrubbed
}

func scrubDataValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = scrubDataValue(val)
		}
		url, ok := out["url"].(string)
		if !ok {
			return out
		}
		n, data := dataURLBytes(url)
		if !data {
			return out
		}
		delete(out, "url")
		out["data_bytes"] = n
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = scrubDataValue(val)
		}
		return out
	default:
		return v
	}
}

func dataURLBytes(url string) (int, bool) {
	if !strings.HasPrefix(url, "data:") {
		return 0, false
	}
	comma := strings.IndexByte(url, ',')
	if comma < 0 {
		return 0, true
	}
	payload := url[comma+1:]
	if strings.Contains(url[:comma], ";base64") {
		if b, err := base64.StdEncoding.DecodeString(payload); err == nil {
			return len(b), true
		}
	}
	return len(payload), true
}

func hoistOpenCodeEncrypted(extra map[string]any) map[string]any {
	if extra == nil {
		return map[string]any{}
	}
	cleaned, enc := stripEncryptedValue(extra)
	m, _ := cleaned.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return putEncryptedAny(m, enc)
}

func stripEncryptedValue(v any) (any, []any) {
	switch t := v.(type) {
	case map[string]any:
		var enc []any
		out := make(map[string]any, len(t))
		for k, val := range t {
			if k == "encrypted_content" {
				enc = append(enc, val)
				continue
			}
			cleaned, more := stripEncryptedValue(val)
			out[k] = cleaned
			enc = append(enc, more...)
		}
		return out, enc
	case []any:
		var enc []any
		out := make([]any, len(t))
		for i, val := range t {
			cleaned, more := stripEncryptedValue(val)
			out[i] = cleaned
			enc = append(enc, more...)
		}
		return out, enc
	default:
		return v, nil
	}
}

func putEncryptedAny(extra map[string]any, enc []any) map[string]any {
	if len(enc) == 0 {
		return extra
	}
	if extra == nil {
		extra = map[string]any{}
	}
	if prev, ok := extra["encrypted_content"]; ok {
		enc = append([]any{prev}, enc...)
	}
	if len(enc) == 1 {
		extra["encrypted_content"] = enc[0]
		return extra
	}
	extra["encrypted_content"] = enc
	return extra
}

func withoutCiphertext(text string, extra map[string]any) string {
	if text == "" || extra == nil {
		return text
	}
	for _, s := range encrypteds(extra["encrypted_content"]) {
		if s == "" {
			continue
		}
		text = strings.ReplaceAll(text, s, "")
	}
	return text
}

func openCodeTime(raw json.RawMessage) (time.Time, bool) {
	if t, ok := jsonTime(raw); ok {
		return t, true
	}
	if n, ok := jsonInt64(raw); ok {
		if t, ok := unixFlexible(n); ok {
			return t, true
		}
	}
	if isNull(raw) {
		return time.Time{}, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return time.Time{}, false
	}
	for _, key := range []string{"created", "start", "end", "updated"} {
		if t, ok := openCodeTime(obj[key]); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func jsonInt64(raw json.RawMessage) (int64, bool) {
	if isNull(raw) {
		return 0, false
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

func unixFlexible(n int64) (time.Time, bool) {
	switch {
	case n >= 1_000_000_000_000:
		return time.UnixMilli(n).UTC(), true
	case n >= 1_000_000_000:
		return time.Unix(n, 0).UTC(), true
	default:
		return time.Time{}, false
	}
}

func rawOffset(raw []byte, frag json.RawMessage, from int) int {
	if len(frag) == 0 {
		return from
	}
	if from < 0 || from > len(raw) {
		from = 0
	}
	i := bytes.Index(raw[from:], frag)
	if i < 0 {
		return from
	}
	return from + i
}

func boolPtr(b bool) *bool {
	return &b
}

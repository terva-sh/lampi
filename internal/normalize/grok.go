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

// Grok projects one Grok Build updates.jsonl blob. The bytes are the
// file the adapter uploaded. The struct does not open a file.
//
// session_id is "grok:" plus the native id (NativeID, else sessionId
// on a line). updates.jsonl is the ACP session-update stream.
// chat_history.jsonl is not a transcript and summary.json is not the
// event list; the caller does not pass those files. Unknown methods
// and unknown sessionUpdate values, including xAI extensions, are
// skipped. A line that is not a JSON object follows the shared
// unreadable-line policy. HarnessVersion is the caller's pinned
// reader version. This projector has no Confidence field: the bytes
// are a JSONL peer of Claude Code, not a Cursor export.
type Grok struct {
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

var _ Normalizer = Grok{}

type grokState struct {
	session string
	model   string
}

// grokBuf is the message currently being coalesced. Consecutive chunks
// of one sessionUpdate kind, with the same prompt marker, are one
// message. A kind change or a prompt-marker change flushes it.
type grokBuf struct {
	kind      string
	role      string
	boundary  string
	messageID string
	model     string
	when      time.Time
	offset    int
	text      strings.Builder
}

type grokPiece struct {
	kind      string
	role      string
	rawType   string
	boundary  string
	messageID string
	model     string
	when      time.Time
	text      string
	tool      Tool
	extra     map[string]any
}

// Normalize projects raw. raw is not modified. Message chunks coalesce.
// tool_call becomes a tool_call. tool_call_update becomes a tool_result
// only when its status is completed or failed. Anything else that is
// still a JSON object is skipped, so an unknown xAI line does not fail
// the file.
func (g Grok) Normalize(ctx context.Context, raw []byte) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g.Now.IsZero() {
		g.Now = time.Now()
	}
	g.Now = g.Now.UTC()

	events := []Event{}
	var st grokState
	var open *grokBuf
	rest := raw
	lineNo := 0
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
		piece, skip, err := g.line(lineNo, line, &st)
		var bad unreadableLine
		if errors.As(err, &bad) {
			if firstBad == nil {
				firstBad = err
			}
			if unreadable++; unreadable > maxUnreadable {
				return nil, tooUnreadable(firstBad)
			}
			events, open, err = g.flush(events, open, &st)
			if err != nil {
				return nil, err
			}
			ev, err := g.emit(&st, "", EventError, "", time.Time{}, offset, unreadableText(lineNo, len(line)), Tool{}, unreadableExtra(lineNo, len(line)))
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
		if skip {
			continue
		}
		if piece.kind == "message" {
			if open != nil && !open.same(piece) {
				events, open, err = g.flush(events, open, &st)
				if err != nil {
					return nil, err
				}
			}
			if piece.text == "" {
				continue
			}
			if open == nil {
				open = &grokBuf{
					kind:      piece.rawType,
					role:      piece.role,
					boundary:  piece.boundary,
					messageID: piece.messageID,
					model:     piece.model,
					when:      piece.when,
					offset:    offset,
				}
			}
			if open.messageID == "" {
				open.messageID = piece.messageID
			}
			if open.model == "" {
				open.model = piece.model
			}
			if open.when.IsZero() {
				open.when = piece.when
			}
			open.text.WriteString(piece.text)
			continue
		}
		events, open, err = g.flush(events, open, &st)
		if err != nil {
			return nil, err
		}
		ev, err := g.emit(&st, piece.rawType, piece.kind, piece.role, piece.when, offset, piece.text, piece.tool, piece.extra)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	if objects == 0 && firstBad != nil {
		return nil, firstBad
	}
	events, _, err := g.flush(events, open, &st)
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (b *grokBuf) same(p grokPiece) bool {
	if b.kind != p.rawType || b.boundary != p.boundary {
		return false
	}
	return b.messageID == "" || p.messageID == "" || b.messageID == p.messageID
}

func (g Grok) flush(events []Event, open *grokBuf, st *grokState) ([]Event, *grokBuf, error) {
	if open == nil || open.text.Len() == 0 {
		return events, nil, nil
	}
	if open.model != "" {
		st.model = open.model
	}
	extra := map[string]any{}
	if open.messageID != "" {
		extra["messageId"] = open.messageID
	}
	ev, err := g.emit(st, open.kind, EventMessage, open.role, open.when, open.offset, open.text.String(), Tool{}, extra)
	if err != nil {
		return nil, nil, err
	}
	return append(events, ev), nil, nil
}

func (g Grok) line(lineNo int, line []byte, st *grokState) (grokPiece, bool, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return grokPiece{}, false, unreadableLine{lineNo}
	}
	if s, ok := jsonString(obj["sessionId"]); ok && s != "" && st.session == "" {
		st.session = s
	}
	method, _ := jsonString(obj["method"])
	if method != "" && method != "session/update" && method != "_x.ai/session/update" {
		return grokPiece{}, true, nil
	}
	update := grokUpdate(obj)
	if update == nil {
		return grokPiece{}, true, nil
	}
	if s, ok := grokSessionID(obj); ok && st.session == "" {
		st.session = s
	}
	kind, _ := jsonString(update["sessionUpdate"])
	when := grokWhen(obj, update)
	switch kind {
	case "user_message_chunk", "agent_message_chunk", "agent_thought_chunk":
		role := ActorAssistant
		if kind == "user_message_chunk" {
			role = ActorUser
		}
		if model := grokModel(update); model != "" {
			st.model = model
		}
		id, _ := jsonString(update["messageId"])
		return grokPiece{
			kind:      "message",
			role:      role,
			rawType:   kind,
			boundary:  grokBoundary(obj, update),
			messageID: id,
			model:     grokModel(update),
			when:      when,
			text:      grokBlocks(update["content"]),
		}, false, nil
	case "tool_call":
		return g.toolCall(update, when, st)
	case "tool_call_update":
		return g.toolUpdate(lineNo, update, when, st)
	default:
		return grokPiece{}, true, nil
	}
}

func (g Grok) toolCall(update map[string]json.RawMessage, when time.Time, st *grokState) (grokPiece, bool, error) {
	if model := grokModel(update); model != "" {
		st.model = model
	}
	name := grokToolName(update)
	callID, _ := jsonString(update["toolCallId"])
	extra, err := extraFrom(update, "sessionUpdate", "content", "rawInput", "rawOutput", "toolCallId", "title", "status", "_meta")
	if err != nil {
		return grokPiece{}, false, err
	}
	return grokPiece{
		kind:    EventToolCall,
		role:    ActorAssistant,
		rawType: "tool_call",
		model:   grokModel(update),
		when:    when,
		text:    argumentsText(update["rawInput"]),
		tool:    Tool{Name: strPtr(name), CallID: strPtr(callID)},
		extra:   extra,
	}, false, nil
}

func (g Grok) toolUpdate(lineNo int, update map[string]json.RawMessage, when time.Time, st *grokState) (grokPiece, bool, error) {
	status, _ := jsonString(update["status"])
	if status != "completed" && status != "failed" {
		return grokPiece{}, true, nil
	}
	if model := grokModel(update); model != "" {
		st.model = model
	}
	text := grokBlocks(update["content"])
	if text == "" {
		text = argumentsText(update["rawOutput"])
	}
	name := grokToolName(update)
	callID, _ := jsonString(update["toolCallId"])
	extra, err := extraFrom(update, "sessionUpdate", "content", "rawInput", "rawOutput", "toolCallId", "title", "status", "_meta")
	if err != nil {
		return grokPiece{}, false, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	isErr := status == "failed"
	return grokPiece{
		kind:    EventToolResult,
		role:    ActorTool,
		rawType: "tool_call_update",
		model:   grokModel(update),
		when:    when,
		text:    text,
		tool:    Tool{Name: strPtr(name), CallID: strPtr(callID), IsError: &isErr},
		extra:   extra,
	}, false, nil
}

func (g Grok) emit(st *grokState, rawType, eventType, role string, recorded time.Time, offset int, text string, tool Tool, extra map[string]any) (Event, error) {
	eid, err := id.New(g.Now)
	if err != nil {
		return Event{}, err
	}
	if extra == nil {
		extra = map[string]any{}
	}
	if role != "" && knownRole(role) == nil {
		extra["role"] = role
	}
	model := st.model
	return Event{
		SchemaVersion:   SchemaVersion,
		EventID:         eid,
		SessionID:       grokSession(g.NativeID, st.session),
		ParentSessionID: grokParent(g.ParentNativeID),
		Harness:         protocol.HarnessGrok,
		HarnessVersion:  strPtr(g.HarnessVersion),
		RecordedAt:      recordedAt(recorded, g.Now),
		IngestedAt:      g.Now.Format(time.RFC3339Nano),
		CWDHash:         adapter.CWDHash(g.CWD),
		ProjectID:       strPtr(g.ProjectID),
		Git: Git{
			Branch: strPtr(g.GitBranch),
			Commit: strPtr(g.GitCommit),
			Dirty:  g.GitDirty,
		},
		Actor:       actorFor(eventType, role),
		EventType:   eventType,
		Role:        knownRole(role),
		Model:       Model{ID: strPtr(model)},
		ContentText: strPtr(text),
		ContentRef:  contentRef(g.Digest, offset),
		Tool:        tool,
		Usage:       Usage{},
		RawType:     rawType,
		Redaction:   Redaction{Status: "none", Ruleset: "v1"},
		Extra:       extraMap(extra),
	}, nil
}

func grokSession(native, fromLine string) string {
	id := native
	if id == "" {
		id = fromLine
	}
	if id == "" {
		id = "unknown"
	}
	return protocol.HarnessGrok + ":" + id
}

func grokParent(fromManifest string) *string {
	if fromManifest == "" {
		return nil
	}
	s := protocol.HarnessGrok + ":" + fromManifest
	return &s
}

func grokUpdate(obj map[string]json.RawMessage) map[string]json.RawMessage {
	if params, ok := jsonObject(obj["params"]); ok {
		if update, ok := jsonObject(params["update"]); ok {
			return update
		}
	}
	if update, ok := jsonObject(obj["update"]); ok {
		return update
	}
	if _, ok := jsonString(obj["sessionUpdate"]); ok {
		return obj
	}
	return nil
}

func grokSessionID(obj map[string]json.RawMessage) (string, bool) {
	if s, ok := jsonString(obj["sessionId"]); ok && s != "" {
		return s, true
	}
	params, ok := jsonObject(obj["params"])
	if !ok {
		return "", false
	}
	return jsonString(params["sessionId"])
}

func grokBoundary(obj, update map[string]json.RawMessage) string {
	if params, ok := jsonObject(obj["params"]); ok {
		if meta, ok := jsonObject(params["_meta"]); ok {
			if id, ok := jsonString(meta["promptId"]); ok && id != "" {
				return "pid:" + id
			}
		}
	}
	meta, ok := jsonObject(update["_meta"])
	if !ok {
		return ""
	}
	raw, ok := meta["promptIndex"]
	if !ok || isNull(raw) {
		return ""
	}
	if s, ok := jsonString(raw); ok && s != "" {
		return "pi:" + s
	}
	if n, ok := jsonInt(raw); ok {
		return fmt.Sprintf("pi:%d", n)
	}
	return ""
}

func grokWhen(obj, update map[string]json.RawMessage) time.Time {
	if params, ok := jsonObject(obj["params"]); ok {
		if meta, ok := jsonObject(params["_meta"]); ok {
			if t, ok := grokTime(meta["agentTimestampMs"], true); ok {
				return t
			}
		}
	}
	if meta, ok := jsonObject(update["_meta"]); ok {
		if t, ok := grokTime(meta["agentTimestampMs"], true); ok {
			return t
		}
	}
	if t, ok := grokTime(obj["timestamp"], false); ok {
		return t
	}
	return time.Time{}
}

func grokTime(raw json.RawMessage, millis bool) (time.Time, bool) {
	if isNull(raw) {
		return time.Time{}, false
	}
	if t, ok := jsonTime(raw); ok {
		return t, true
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil || n <= 0 {
		return time.Time{}, false
	}
	if millis || n > 1e11 {
		return time.UnixMilli(int64(n)).UTC(), true
	}
	return time.Unix(int64(n), 0).UTC(), true
}

func grokModel(update map[string]json.RawMessage) string {
	meta, ok := jsonObject(update["_meta"])
	if !ok {
		return ""
	}
	s, _ := jsonString(meta["modelId"])
	return s
}

func grokToolName(update map[string]json.RawMessage) string {
	if s, ok := jsonString(update["title"]); ok && s != "" {
		return s
	}
	if meta, ok := jsonObject(update["_meta"]); ok {
		if s, ok := jsonString(meta["x.ai/tool"]); ok && s != "" {
			return s
		}
	}
	s, _ := jsonString(update["kind"])
	return s
}

// grokBlocks flattens an ACP content block or a tool content array.
// Text is kept. A diff contributes its path and a terminal contributes
// its id. Image bytes and other payloads stay in the raw blob.
func grokBlocks(raw json.RawMessage) string {
	if isNull(raw) {
		return ""
	}
	if s, ok := jsonString(raw); ok {
		return s
	}
	if obj, ok := jsonObject(raw); ok {
		kind, _ := jsonString(obj["type"])
		switch kind {
		case "text", "":
			if s, ok := jsonString(obj["text"]); ok && s != "" {
				return s
			}
			if nested, ok := obj["content"]; ok {
				return grokBlocks(nested)
			}
			return ""
		case "content":
			return grokBlocks(obj["content"])
		case "diff":
			s, _ := jsonString(obj["path"])
			return s
		case "terminal":
			s, _ := jsonString(obj["terminalId"])
			return s
		default:
			if s, ok := jsonString(obj["text"]); ok {
				return s
			}
			return ""
		}
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return ""
	}
	var parts []string
	for _, item := range arr {
		if s := grokBlocks(item); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

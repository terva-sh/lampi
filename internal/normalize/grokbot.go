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

// GrokBot projects one Grok Bot transcript_jsonl blob. The bytes are
// the file the adapter uploaded. The struct does not open a file.
//
// Each line is {role, message:{content:[{type,text}]}}. One message
// event is emitted per line whose role is user or assistant. Text is
// the concatenation of content parts whose type is text. An unknown
// role, a part that is not text, and a message that is not that shape
// are skipped. They do not fail the file. A line that is not a JSON
// object follows the shared unreadable-line policy. There is no tool
// promotion. session_id is "grokbot:" plus the native id. HarnessVersion
// is the caller's pinned reader version.
type GrokBot struct {
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

var _ Normalizer = GrokBot{}

// Normalize projects raw. raw is not modified. Unknown roles and
// non-text parts are skipped. A line that is not a JSON object becomes
// an error event that names the line but not its bytes.
func (g GrokBot) Normalize(ctx context.Context, raw []byte) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g.Now.IsZero() {
		g.Now = time.Now()
	}
	g.Now = g.Now.UTC()

	events := []Event{}
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
		ev, skip, err := g.line(lineNo, offset, line)
		var bad unreadableLine
		if errors.As(err, &bad) {
			if firstBad == nil {
				firstBad = err
			}
			if unreadable++; unreadable > maxUnreadable {
				return nil, tooUnreadable(firstBad)
			}
			ev, err = g.emit("", time.Time{}, offset, unreadableText(lineNo, len(line)), unreadableExtra(lineNo, len(line)), EventError)
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
		events = append(events, ev)
	}
	if objects == 0 && firstBad != nil {
		return nil, firstBad
	}
	return events, nil
}

func (g GrokBot) line(lineNo, offset int, line []byte) (Event, bool, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return Event{}, false, unreadableLine{lineNo}
	}
	role, _ := jsonString(obj["role"])
	if role != ActorUser && role != ActorAssistant {
		return Event{}, true, nil
	}
	extra, err := extraFrom(obj, "role", "message")
	if err != nil {
		return Event{}, false, fmt.Errorf("normalize: line %d: %w", lineNo, err)
	}
	ev, err := g.emit(role, time.Time{}, offset, grokbotText(obj["message"]), extra, EventMessage)
	if err != nil {
		return Event{}, false, err
	}
	return ev, false, nil
}

// grokbotText concatenates type==text parts of message.content. A
// missing message, a content value that is not an array, and a part
// that is not text contribute nothing and are not errors.
func grokbotText(message json.RawMessage) string {
	msg, ok := jsonObject(message)
	if !ok {
		return ""
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(msg["content"], &parts); err != nil {
		return ""
	}
	var b strings.Builder
	for _, raw := range parts {
		part, ok := jsonObject(raw)
		if !ok {
			continue
		}
		kind, _ := jsonString(part["type"])
		if kind != "text" {
			continue
		}
		s, _ := jsonString(part["text"])
		b.WriteString(s)
	}
	return b.String()
}

func (g GrokBot) emit(role string, recorded time.Time, offset int, text string, extra map[string]any, eventType string) (Event, error) {
	eid, err := id.New(g.Now)
	if err != nil {
		return Event{}, err
	}
	if extra == nil {
		extra = map[string]any{}
	}
	return Event{
		SchemaVersion:   SchemaVersion,
		EventID:         eid,
		SessionID:       grokbotSession(g.NativeID),
		ParentSessionID: grokbotParent(g.ParentNativeID),
		Harness:         protocol.HarnessGrokBot,
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
		Model:       Model{},
		ContentText: strPtr(text),
		ContentRef:  contentRef(g.Digest, offset),
		Tool:        Tool{},
		Usage:       Usage{},
		RawType:     role,
		Redaction:   Redaction{Status: "none", Ruleset: "v1"},
		Extra:       extraMap(extra),
	}, nil
}

func grokbotSession(native string) string {
	id := native
	if id == "" {
		id = "unknown"
	}
	return protocol.HarnessGrokBot + ":" + id
}

func grokbotParent(fromManifest string) *string {
	if fromManifest == "" {
		return nil
	}
	s := protocol.HarnessGrokBot + ":" + fromManifest
	return &s
}

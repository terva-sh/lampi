package normalize

import (
	"encoding/json"
	"fmt"
	"io"

	"terva.sh/lampi/internal/redact"
)

// ShareGPTRecord is one training trajectory. It is a filtered
// projection of a normalized session, not a copy of the raw blob.
// RawSHA256 is the transcript digest the events were projected from.
// Encrypted content stays on the turn that carried it and is not
// decoded.
type ShareGPTRecord struct {
	SessionUID    string         `json:"session_uid"`
	SessionID     string         `json:"session_id,omitempty"`
	ProjectID     string         `json:"project_id,omitempty"`
	RawSHA256     string         `json:"raw_sha256"`
	Conversations []ShareGPTTurn `json:"conversations"`
}

// ShareGPTTurn is one ShareGPT message. From is human, gpt, system,
// or tool. Value is content_text after ruleset v1 stripping. Name and
// CallID are set on a tool call or result and are stripped the same
// way. EncryptedContent is the opaque extra field when the event had
// one; it is not stripped.
type ShareGPTTurn struct {
	From             string `json:"from"`
	Value            string `json:"value"`
	Name             string `json:"name,omitempty"`
	CallID           string `json:"call_id,omitempty"`
	EncryptedContent any    `json:"encrypted_content,omitempty"`
}

// ShareGPT projects events into one trajectory. ok is false when
// rawSHA256 is empty or the events have no training turn, so a caller
// does not write a row that cannot be traced to a blob. Meta, usage,
// and unknown rows are left out. Plaintext training fields (value,
// name, and call id) are copied and then stripped with ruleset v1.
// encrypted_content is copied as stored and is not scanned. The events
// and the raw blob are not modified.
func ShareGPT(sessionUID, rawSHA256 string, events []Event) (ShareGPTRecord, bool) {
	if rawSHA256 == "" {
		return ShareGPTRecord{}, false
	}
	rec := ShareGPTRecord{
		SessionUID:    sessionUID,
		RawSHA256:     rawSHA256,
		Conversations: []ShareGPTTurn{},
	}
	for _, ev := range events {
		if rec.SessionID == "" {
			rec.SessionID = ev.SessionID
		}
		if rec.ProjectID == "" && ev.ProjectID != nil {
			rec.ProjectID = *ev.ProjectID
		}
		turn, ok := trainingTurn(ev)
		if !ok {
			continue
		}
		rec.Conversations = append(rec.Conversations, turn)
	}
	if len(rec.Conversations) == 0 {
		return ShareGPTRecord{}, false
	}
	return rec, true
}

// WriteShareGPT writes one trajectory per line. HTML escaping is off,
// matching WriteJSONL, so a prompt that contains < or & is stored as
// itself.
func WriteShareGPT(w io.Writer, records []ShareGPTRecord) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("normalize: %w", err)
		}
	}
	return nil
}

func trainingTurn(ev Event) (ShareGPTTurn, bool) {
	switch ev.EventType {
	case EventMessage, EventToolCall, EventToolResult, EventCompaction, EventError:
	default:
		return ShareGPTTurn{}, false
	}
	turn := ShareGPTTurn{From: shareFrom(ev)}
	if ev.ContentText != nil {
		turn.Value = stripTraining(*ev.ContentText)
	}
	if ev.Tool.Name != nil {
		turn.Name = stripTraining(*ev.Tool.Name)
	}
	if ev.Tool.CallID != nil {
		turn.CallID = stripTraining(*ev.Tool.CallID)
	}
	turn.EncryptedContent = copyEncrypted(ev.Extra)
	if turn.Value == "" && turn.Name == "" && turn.CallID == "" && turn.EncryptedContent == nil {
		return ShareGPTTurn{}, false
	}
	return turn, true
}

func shareFrom(ev Event) string {
	if ev.Role != nil {
		if from, ok := shareRole(*ev.Role); ok {
			return from
		}
	}
	if from, ok := shareRole(ev.Actor); ok {
		return from
	}
	return "system"
}

func shareRole(role string) (string, bool) {
	switch role {
	case ActorUser:
		return "human", true
	case ActorAssistant:
		return "gpt", true
	case ActorSystem:
		return "system", true
	case ActorTool:
		return "tool", true
	default:
		return "", false
	}
}

// stripTraining applies ruleset v1 to one plaintext training field.
// The caller's string is not rewritten. encrypted_content is not passed
// here.
func stripTraining(s string) string {
	return (redact.Ruleset{}).Strip(s)
}

// copyEncrypted returns the extra field unchanged. A missing field is
// nil. The value is not parsed and is not merged into the turn text.
func copyEncrypted(extra map[string]any) any {
	if extra == nil {
		return nil
	}
	v, ok := extra["encrypted_content"]
	if !ok || v == nil {
		return nil
	}
	return v
}

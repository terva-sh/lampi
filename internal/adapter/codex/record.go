package codex

import (
	"bytes"
	"encoding/json"
)

// Version is the pinned reader for Codex CLI rollout JSONL.
// The object on each line is internal to this package. It is not capture
// protocol 1, and it is not a promise about a later Codex release.
// Bump Version when a key this reader interprets changes meaning.
const Version = "1"

// Record is one JSON object from a rollout file. Payload is the raw
// payload object when the line has one, including keys this version
// reads. Extra holds every other top-level key.
type Record struct {
	Timestamp string
	Type      string
	Payload   map[string]json.RawMessage
	Extra     map[string]json.RawMessage
}

// SessionID is the id on a session_meta payload. Other line types do
// not carry one for this reader.
func (r Record) SessionID() string {
	if r.Type != "session_meta" {
		return ""
	}
	s, _ := jsonString(r.Payload["id"])
	return s
}

// CWD is the cwd on a session_meta payload.
func (r Record) CWD() string {
	if r.Type != "session_meta" {
		return ""
	}
	s, _ := jsonString(r.Payload["cwd"])
	return s
}

// ParseLine reads one JSONL line. A non-object is an error.
func ParseLine(line []byte) (Record, error) {
	line = bytes.TrimSpace(line)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return Record{}, errNotObject
	}
	rec := Record{Extra: map[string]json.RawMessage{}}
	for k, raw := range obj {
		switch k {
		case "timestamp":
			rec.Timestamp, _ = jsonString(raw)
		case "type":
			rec.Type, _ = jsonString(raw)
		case "payload":
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
				rec.Extra[k] = append(json.RawMessage(nil), raw...)
				continue
			}
			rec.Payload = payload
		default:
			rec.Extra[k] = append(json.RawMessage(nil), raw...)
		}
	}
	return rec, nil
}

// MarshalJSON writes the named fields, the payload, and Extra.
func (r Record) MarshalJSON() ([]byte, error) {
	obj := map[string]json.RawMessage{}
	for k, raw := range r.Extra {
		obj[k] = append(json.RawMessage(nil), raw...)
	}
	putString(obj, "timestamp", r.Timestamp)
	putString(obj, "type", r.Type)
	if r.Payload != nil {
		raw, err := json.Marshal(r.Payload)
		if err != nil {
			return nil, err
		}
		obj["payload"] = raw
	}
	return json.Marshal(obj)
}

func putString(obj map[string]json.RawMessage, key, val string) {
	if val == "" {
		return
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return
	}
	obj[key] = raw
}

func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

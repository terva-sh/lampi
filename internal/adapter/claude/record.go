package claude

import (
	"bytes"
	"encoding/json"
)

// Version is the pinned reader for Claude Code's on-disk JSONL.
// The object on each line is internal to this package. It is not capture
// protocol 1, and it is not a promise about a later Claude Code release.
// Bump Version when a key this reader interprets changes meaning.
const Version = "1"

// Record is one JSON object from a session file. The named fields are
// the ones Version reads. Extra holds every other key, as raw JSON, so
// a field this version does not know is not dropped.
type Record struct {
	Type      string
	SessionID string
	CWD       string
	Extra     map[string]json.RawMessage
}

// ParseLine reads one JSONL line. A non-object is an error. The caller
// decides whether a torn line fails the file.
func ParseLine(line []byte) (Record, error) {
	line = bytes.TrimSpace(line)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return Record{}, errNotObject
	}
	rec := Record{Extra: map[string]json.RawMessage{}}
	for k, raw := range obj {
		switch k {
		case "type":
			rec.Type, _ = jsonString(raw)
		case "sessionId":
			rec.SessionID, _ = jsonString(raw)
		case "cwd":
			rec.CWD, _ = jsonString(raw)
		default:
			rec.Extra[k] = append(json.RawMessage(nil), raw...)
		}
	}
	return rec, nil
}

// MarshalJSON writes the named fields and Extra. A key this reader
// stored in Extra comes back unchanged.
func (r Record) MarshalJSON() ([]byte, error) {
	obj := map[string]json.RawMessage{}
	for k, raw := range r.Extra {
		obj[k] = append(json.RawMessage(nil), raw...)
	}
	putString(obj, "type", r.Type)
	putString(obj, "sessionId", r.SessionID)
	putString(obj, "cwd", r.CWD)
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

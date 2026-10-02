package grok

import (
	"bytes"
	"encoding/json"
)

// Version is the pinned reader for Grok Build's on-disk JSONL.
// The object on each line is internal to this package. It is not capture
// protocol 1, and it is not a promise about a later Grok Build release.
// Bump Version when a key this reader interprets changes meaning.
// This reader interprets no keys. It has no Confidence field: the
// bytes are a JSONL peer of Claude Code, not a Cursor export.
const Version = "1"

// Record is one JSON object from updates.jsonl. Extra holds every key,
// as raw JSON, so a field this version does not know is not dropped.
type Record struct {
	Extra map[string]json.RawMessage
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
		rec.Extra[k] = append(json.RawMessage(nil), raw...)
	}
	return rec, nil
}

// MarshalJSON writes Extra. A key this reader stored there comes back
// unchanged.
func (r Record) MarshalJSON() ([]byte, error) {
	obj := map[string]json.RawMessage{}
	for k, raw := range r.Extra {
		obj[k] = append(json.RawMessage(nil), raw...)
	}
	return json.Marshal(obj)
}

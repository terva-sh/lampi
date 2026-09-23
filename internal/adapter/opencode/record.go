package opencode

import (
	"bytes"
	"encoding/json"
)

// Version is the pinned reader for an `opencode export` document.
// The object is internal to this package. It is not capture protocol 1,
// and it is not a promise about a later OpenCode release. Bump Version
// when a key this reader interprets changes meaning.
const Version = "1"

// Record is one export document. The named fields are the ones Version
// reads. Extra holds every other top-level key, as raw JSON, so a field
// this version does not know is not dropped.
type Record struct {
	Info  Info
	Extra map[string]json.RawMessage
}

// Info is the session object on an export. ID and Directory are the
// fields Version reads. Extra holds every other key on info.
type Info struct {
	ID        string
	Directory string
	Extra     map[string]json.RawMessage
}

// ParseExport reads one export document. A non-object is an error.
// The caller decides whether a file that does not parse still has a
// path-shaped session id.
func ParseExport(raw []byte) (Record, error) {
	raw = bytes.TrimSpace(raw)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return Record{}, errNotObject
	}
	rec := Record{Extra: map[string]json.RawMessage{}}
	for k, v := range obj {
		if k == "info" {
			info, err := parseInfo(v)
			if err != nil {
				return Record{}, err
			}
			rec.Info = info
			continue
		}
		rec.Extra[k] = append(json.RawMessage(nil), v...)
	}
	return rec, nil
}

func parseInfo(raw json.RawMessage) (Info, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return Info{}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return Info{}, errNotObject
	}
	info := Info{Extra: map[string]json.RawMessage{}}
	for k, v := range obj {
		switch k {
		case "id":
			info.ID, _ = jsonString(v)
		case "directory":
			info.Directory, _ = jsonString(v)
		default:
			info.Extra[k] = append(json.RawMessage(nil), v...)
		}
	}
	return info, nil
}

// MarshalJSON writes the named fields and Extra. A key this reader
// stored in Extra comes back unchanged.
func (r Record) MarshalJSON() ([]byte, error) {
	obj := map[string]json.RawMessage{}
	for k, raw := range r.Extra {
		obj[k] = append(json.RawMessage(nil), raw...)
	}
	info, err := json.Marshal(r.Info)
	if err != nil {
		return nil, err
	}
	obj["info"] = info
	return json.Marshal(obj)
}

// MarshalJSON writes id, directory, and the keys kept on Extra.
func (info Info) MarshalJSON() ([]byte, error) {
	obj := map[string]json.RawMessage{}
	for k, raw := range info.Extra {
		obj[k] = append(json.RawMessage(nil), raw...)
	}
	putString(obj, "id", info.ID)
	putString(obj, "directory", info.Directory)
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

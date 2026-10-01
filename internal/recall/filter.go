package recall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"terva.sh/lampi/internal/normalize"
)

// EventFilter is the structured part of a search, applied to
// normalized JSONL as it is read instead of through the index. export
// and the read API stream use it, so for the same filters they select
// the same events search does: harness and project come from the
// session, and every other field from the line, read as the index
// reads it.
type EventFilter struct {
	Harness   string
	Project   string
	Since     *time.Time
	Until     *time.Time
	EventType string
	Actor     string
	ToolName  string
	ToolError *bool
	RawType   string
}

// FilterError is an invalid filter value. Param is the query parameter
// that holds it, as the web search API names it. It matches ErrInvalid.
type FilterError struct{ Param string }

func (e *FilterError) Error() string { return "recall: invalid " + e.Param }

func (e *FilterError) Unwrap() error { return ErrInvalid }

// Validate checks every value against the vocabulary and caps that
// search uses, and reports the first one that fails.
func (f EventFilter) Validate() error {
	switch {
	case !validHarness(f.Harness):
		return &FilterError{"harness"}
	case len(f.Project) > 4096:
		return &FilterError{"project"}
	case f.EventType != "" && !oneOf(f.EventType, EventTypes):
		return &FilterError{"event_type"}
	case f.Actor != "" && !oneOf(f.Actor, Actors):
		return &FilterError{"actor"}
	case len(f.ToolName) > filterMaxBytes || !utf8.ValidString(f.ToolName):
		return &FilterError{"tool"}
	case len(f.RawType) > filterMaxBytes || !utf8.ValidString(f.RawType):
		return &FilterError{"raw_type"}
	case f.Since != nil && f.Until != nil && !f.Since.Before(*f.Until):
		return &FilterError{"until"}
	}
	return nil
}

// IsZero reports whether f selects every event.
func (f EventFilter) IsZero() bool {
	return f.Harness == "" && f.Project == "" && f.Since == nil && f.Until == nil && !f.hasLineFilter()
}

func (f EventFilter) hasLineFilter() bool {
	return f.Since != nil || f.Until != nil || f.EventType != "" || f.Actor != "" || f.ToolName != "" || f.ToolError != nil || f.RawType != ""
}

// Session reports whether a session's events can match at all, so a
// reader skips a session without opening its file.
func (f EventFilter) Session(harness, project string) bool {
	return (f.Harness == "" || f.Harness == harness) && (f.Project == "" || f.Project == project)
}

// Line reports whether one JSONL line of a session that passed Session
// matches. A nil line is one too long to read, as the index treats it.
func (f EventFilter) Line(line []byte) bool {
	if !f.hasLineFilter() {
		return true
	}
	var d docRow
	d.fill(line)
	return f.match(&d)
}

func (f EventFilter) match(d *docRow) bool {
	switch {
	case f.EventType != "" && d.eventType != f.EventType,
		f.Actor != "" && d.actor != f.Actor,
		f.RawType != "" && d.raw != f.RawType,
		f.ToolName != "" && (d.tool == nil || *d.tool != f.ToolName),
		f.ToolError != nil && (d.toolError == nil || *d.toolError != *f.ToolError),
		f.Since != nil && (d.recorded == nil || *d.recorded < f.Since.UnixNano()),
		f.Until != nil && (d.recorded == nil || *d.recorded >= f.Until.UnixNano()):
		return false
	}
	return true
}

// ParseWhen reads a filter time: RFC 3339, or YYYY-MM-DD in UTC. A
// date-only end covers that whole day.
func ParseWhen(raw string, end bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return t, err
	}
	if end {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}

// Fields is a list of event paths to write instead of the whole event.
// A path is a schema_version 1 field, one field of a nested object such
// as tool.name, or a key of extra.
type Fields []string

// fieldPaths is every path ParseFields accepts besides extra.KEY.
var fieldPaths = func() map[string]bool {
	paths := map[string]bool{}
	t := reflect.TypeFor[normalize.Event]()
	for i := range t.NumField() {
		top := t.Field(i)
		name := jsonName(top)
		paths[name] = true
		if top.Type.Kind() != reflect.Struct {
			continue
		}
		for j := range top.Type.NumField() {
			paths[name+"."+jsonName(top.Type.Field(j))] = true
		}
	}
	return paths
}()

func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return name
}

// ParseFields reads a comma-separated list of paths. An empty list is
// nil, which writes whole events. An unknown, empty or repeated path is
// refused.
func ParseFields(list string) (Fields, error) {
	if list == "" {
		return nil, nil
	}
	var out Fields
	seen := map[string]bool{}
	for _, p := range strings.Split(list, ",") {
		key, ok := strings.CutPrefix(p, "extra.")
		if !(fieldPaths[p] || ok && key != "") {
			return nil, fmt.Errorf("unknown field %q", p)
		}
		if seen[p] {
			return nil, fmt.Errorf("field %q is listed twice", p)
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, nil
}

// Project writes one JSON object holding fs, in order, from line. A
// path the event does not have is null, so every row has the same keys.
// A line that is not a JSON object has every path null.
func (fs Fields) Project(line []byte) []byte {
	var top map[string]json.RawMessage
	_ = json.Unmarshal(line, &top)
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range fs {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(p)
		b.Write(key)
		b.WriteByte(':')
		if v := lookup(top, p); v == nil || json.Compact(&b, v) != nil {
			b.WriteString("null")
		}
	}
	b.WriteByte('}')
	return b.Bytes()
}

func lookup(top map[string]json.RawMessage, path string) json.RawMessage {
	head, rest, nested := strings.Cut(path, ".")
	v := top[head]
	if !nested || v == nil {
		return v
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(v, &obj) != nil {
		return nil
	}
	return obj[rest]
}

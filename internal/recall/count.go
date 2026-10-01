package recall

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// CountMaxValues and CountMaxBytes bound what one count holds: the
// distinct values, and their JSON text together, so a count by a path
// with a large value per event cannot grow without end (review 1706).
const (
	CountMaxValues = 100_000
	CountMaxBytes  = 32 << 20
)

// ErrTooManyValues is a count that reached CountMaxValues or
// CountMaxBytes.
var ErrTooManyValues = errors.New("recall: too many distinct values to count")

// Counter counts the selected events by the value at one path, so an
// agent that wants numbers is not sent the events (TKT-01M3V3KD).
type Counter struct {
	path   string
	counts map[string]int64
	bytes  int
}

// NewCounter counts by path, any path ParseFields accepts except a
// free-text one: a count of free text would hand back the text itself,
// which is what counting is meant to avoid.
func NewCounter(path string) (*Counter, error) {
	if _, err := ParseFields(path); err != nil || path == "" || strings.Contains(path, ",") {
		return nil, fmt.Errorf("unknown field %q", path)
	}
	if CountRefused(path) {
		return nil, fmt.Errorf("%q holds free text and cannot be counted", path)
	}
	return &Counter{path: path, counts: map[string]int64{}}, nil
}

// CountRefused reports whether path holds free text: content_text,
// content_ref, or extra, whose keys a harness names and fills as it
// likes.
func CountRefused(path string) bool {
	return path == "content_text" || path == "content_ref" || path == "extra" || strings.HasPrefix(path, "extra.")
}

// Add counts one selected line. A line that is not an event, or lacks
// the path, counts under null.
func (c *Counter) Add(line []byte) error {
	var top map[string]json.RawMessage
	_ = json.Unmarshal(line, &top)
	key := "null"
	if v := lookup(top, c.path); v != nil {
		var b bytes.Buffer
		if json.Compact(&b, v) == nil {
			key = b.String()
		}
	}
	if _, ok := c.counts[key]; !ok {
		if len(c.counts) >= CountMaxValues || c.bytes+len(key) > CountMaxBytes {
			return ErrTooManyValues
		}
		c.bytes += len(key)
	}
	c.counts[key]++
	return nil
}

// Rows returns one {"value":V,"count":N} line per distinct value, by
// count descending and then by the value's JSON text, so "Read" comes
// before null.
func (c *Counter) Rows() [][]byte {
	keys := make([]string, 0, len(c.counts))
	for k := range c.counts {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(c.counts[b], c.counts[a]), cmp.Compare(a, b))
	})
	out := make([][]byte, len(keys))
	for i, k := range keys {
		out[i] = []byte(`{"value":` + k + `,"count":` + strconv.FormatInt(c.counts[k], 10) + `}`)
	}
	return out
}

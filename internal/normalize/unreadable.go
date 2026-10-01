package normalize

import "fmt"

// A line of a JSONL file that is not a JSON object is skipped and marked
// where it stood (TKT-01M3NQ2R): an error event naming the line number
// and its length, never its bytes, which may hold a secret. A harness can
// tear a line itself, as Claude Code did when it cut a queue-operation
// record off mid-string and wrote the next record on the same line, and
// one torn line used to leave the whole session with no events, search
// entries or transcript. A file in which no line is a JSON object still
// fails with the first line's error: it is not this format at all. So
// does one with more than maxUnreadable such lines, and it fails as soon
// as it passes that, rather than projecting a marker for every line of
// a file that is not JSONL.

// maxUnreadable is how many lines a file may have that are not JSON
// objects before it is treated as not JSONL. A harness tears a line
// rarely; a file of random or binary bytes has one every few hundred.
const maxUnreadable = 64

// tooUnreadable is the failure for a file past maxUnreadable. It keeps
// the first bad line's error, which names the line and not its bytes.
func tooUnreadable(first error) error {
	return fmt.Errorf("%w; more than %d lines are not JSON objects", first, maxUnreadable)
}

// unreadableLine is the error a projector's line function returns for a
// line that is not a JSON object.
type unreadableLine struct{ line int }

func (e unreadableLine) Error() string {
	return fmt.Sprintf("normalize: line %d is not a JSON object", e.line)
}

// unreadableText is the marker's content_text.
func unreadableText(line, size int) string {
	return fmt.Sprintf("Line %d of the raw file is not a JSON record and was skipped (%d bytes). The raw file is unchanged in the lake.", line, size)
}

// unreadableExtra is the marker's extra.
func unreadableExtra(line, size int) map[string]any {
	return map[string]any{"unreadable_line": line, "line_bytes": size}
}

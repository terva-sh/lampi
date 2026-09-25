package upload

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// last_attempt.json keeps the skip count and the first few lines, each
// on one line and cut to a bounded length.
func TestAttemptRecordsSkipped(t *testing.T) {
	state := t.TempDir()
	skipped := []string{"terva: sessions: not a directory", "claude: a.jsonl:\npermission denied", strings.Repeat("é", 300)}
	for i := range 4 {
		skipped = append(skipped, "codex: file "+string(rune('a'+i)))
	}
	recordAttempt(state, time.Now(), skipped, errors.New("upload: POST /v1/hello: refused"))
	a, ok, err := ReadAttempt(state)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if a.Skipped != 7 || len(a.SkippedLines) != maxSkippedLines {
		t.Fatalf("attempt %+v", a)
	}
	if a.SkippedLines[1] != "claude: a.jsonl: permission denied" {
		t.Fatalf("line %q", a.SkippedLines[1])
	}
	long := a.SkippedLines[2]
	if len(long) > maxSkippedLine+len("...") || !strings.HasSuffix(long, "...") || !strings.HasPrefix(long, "é") {
		t.Fatalf("long line %d bytes: %q", len(long), long)
	}
	recordAttempt(state, time.Now(), nil, nil)
	if a, _, _ := ReadAttempt(state); a.Skipped != 0 || a.SkippedLines != nil {
		t.Fatalf("a clean pass kept the skips: %+v", a)
	}
}

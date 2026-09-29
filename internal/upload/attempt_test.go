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
	recordAttempt(state, time.Now(), skipped, nil, errors.New("upload: POST /v1/hello: refused"))
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
	recordAttempt(state, time.Now(), nil, nil, nil)
	if a, _, _ := ReadAttempt(state); a.Skipped != 0 || a.SkippedLines != nil {
		t.Fatalf("a clean pass kept the skips: %+v", a)
	}
}

// A pass that fails keeps the sessions the last pass saw waiting for a
// bay; only a pass that finishes clears them (review 1449).
func TestAFailedPassKeepsTheWaitingSessions(t *testing.T) {
	state := t.TempDir()
	recordAttempt(state, time.Now(), nil, []string{"codex s1", "codex s2"}, nil)
	recordAttempt(state, time.Now(), nil, nil, errors.New("upload: POST /v1/hello: refused"))
	if a, _, _ := ReadAttempt(state); a.NoBay != 2 || len(a.NoBayLines) != 2 || a.Error == "" {
		t.Fatalf("failed pass: %+v", a)
	}
	recordAttempt(state, time.Now(), nil, []string{"codex s3"}, errors.New("upload: POST /v1/manifests: 500"))
	if a, _, _ := ReadAttempt(state); a.NoBay != 3 || len(a.NoBayLines) != 3 {
		t.Fatalf("failed pass that met another: %+v", a)
	}
	recordAttempt(state, time.Now(), nil, nil, nil)
	if a, _, _ := ReadAttempt(state); a.NoBay != 0 || a.NoBayLines != nil {
		t.Fatalf("finished pass: %+v", a)
	}
}

// With the named lines full, a failed pass that meets a new waiting
// session still counts it (review 1454).
func TestAFailedPassCountsANewWaitingSession(t *testing.T) {
	state := t.TempDir()
	var five []string
	for i := range maxSkippedLines {
		five = append(five, "codex s"+string(rune('a'+i)))
	}
	recordAttempt(state, time.Now(), nil, five, nil)
	recordAttempt(state, time.Now(), nil, []string{"codex new"}, errors.New("upload: POST /v1/manifests: 500"))
	if a, _, _ := ReadAttempt(state); a.NoBay != maxSkippedLines+1 || len(a.NoBayLines) != maxSkippedLines {
		t.Fatalf("attempt %+v", a)
	}
}

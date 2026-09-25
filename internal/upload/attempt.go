package upload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Attempt is the last run of Sync, finished or not. last_sync.json is
// only the last finished run; this is how status shows why the runs
// since then failed. Error is empty when the last run finished.
// LastError and LastErrorAt keep the most recent failure after a later
// run succeeds. Skipped counts the files and harnesses that run left
// out because they could not be read; SkippedLines is the first
// maxSkippedLines of them, each on one line and cut at
// maxSkippedLine bytes.
type Attempt struct {
	At           time.Time `json:"at"`
	Error        string    `json:"error,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	LastErrorAt  time.Time `json:"last_error_at,omitzero"`
	Skipped      int       `json:"skipped,omitempty"`
	SkippedLines []string  `json:"skipped_lines,omitempty"`
}

const (
	maxSkippedLines = 5
	maxSkippedLine  = 200
)

// AttemptFile is the attempt record inside a lampi state directory.
func AttemptFile(stateDir string) string {
	return filepath.Join(stateDir, "last_attempt.json")
}

// ReadAttempt loads the record. A missing file is (zero, false, nil).
func ReadAttempt(stateDir string) (Attempt, bool, error) {
	b, err := os.ReadFile(AttemptFile(stateDir))
	if errors.Is(err, fs.ErrNotExist) {
		return Attempt{}, false, nil
	}
	if err != nil {
		return Attempt{}, false, err
	}
	var a Attempt
	if err := json.Unmarshal(b, &a); err != nil {
		return Attempt{}, false, fmt.Errorf("upload: last attempt: %w", err)
	}
	return a, true, nil
}

// recordAttempt rewrites the record after a run. A refusal is a
// finished run, as it is for last_sync.json. A record that cannot be
// written does not fail the run it describes.
func recordAttempt(stateDir string, now time.Time, skipped []string, err error) {
	if stateDir == "" {
		return
	}
	prev, _, _ := ReadAttempt(stateDir)
	a := Attempt{At: now.UTC(), LastError: prev.LastError, LastErrorAt: prev.LastErrorAt, Skipped: len(skipped)}
	for _, line := range skipped[:min(len(skipped), maxSkippedLines)] {
		a.SkippedLines = append(a.SkippedLines, oneLine(line, maxSkippedLine))
	}
	if !runFinished(err) {
		a.Error = err.Error()
		a.LastError = a.Error
		a.LastErrorAt = a.At
	}
	raw, mErr := json.MarshalIndent(a, "", "  ")
	if mErr != nil {
		return
	}
	_ = writeStateFile(stateDir, AttemptFile(stateDir), append(raw, '\n'))
}

// oneLine is s with its whitespace runs made one space, cut to at
// most n bytes on a rune boundary.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// writeStateFile renames a complete owner-only file over path.
func writeStateFile(stateDir, path string, raw []byte) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(stateDir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		tmp.Close()
		if name != "" {
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	name = ""
	return nil
}

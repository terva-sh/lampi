package redact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Record is one quarantined file. It names the rule and the digest.
// It does not contain the bytes that matched.
type Record struct {
	RelPath string    `json:"relpath"`
	CWD     string    `json:"cwd,omitempty"`
	SHA256  string    `json:"sha256"`
	Ruleset string    `json:"ruleset"`
	Hits    int       `json:"hits"`
	Rules   []string  `json:"rules"`
	At      time.Time `json:"at"`
}

// QuarantineFile is the append-only log inside a lampi state directory.
func QuarantineFile(stateDir string) string {
	return filepath.Join(stateDir, "quarantine.jsonl")
}

// AppendQuarantine adds rec to the log. The file is owner-read. A zero
// timestamp is filled with the current time. The matched secret is not
// a field on Record, and this function does not invent one.
func AppendQuarantine(stateDir string, rec Record) error {
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	path := QuarantineFile(stateDir)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	return nil
}

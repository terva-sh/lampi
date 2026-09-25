package redact

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Record is one quarantined file. It names the rule and the digest.
// It does not contain the bytes that matched. Manifest is set when the
// hit was in the manifest JSON (a path or project field), not in the
// file: that hit has no redaction stamp to carry an override.
type Record struct {
	RelPath  string    `json:"relpath"`
	CWD      string    `json:"cwd,omitempty"`
	SHA256   string    `json:"sha256"`
	Ruleset  string    `json:"ruleset"`
	Hits     int       `json:"hits"`
	Rules    []string  `json:"rules"`
	Manifest bool      `json:"manifest,omitempty"`
	At       time.Time `json:"at"`
}

// same reports whether r records the same finding as o: the same
// file, the same bytes, and the same rules.
func (r Record) same(o Record) bool {
	return r.RelPath == o.RelPath && r.SHA256 == o.SHA256 && r.Ruleset == o.Ruleset &&
		r.Manifest == o.Manifest && slices.Equal(r.Rules, o.Rules)
}

// QuarantineFile is the append-only log inside a lampi state directory.
func QuarantineFile(stateDir string) string {
	return filepath.Join(stateDir, "quarantine.jsonl")
}

// AppendQuarantine adds rec to the log. A record for the same relpath,
// digest, and rules as one already in the log is not added again, so a
// session that stays quarantined does not grow the log on every sync.
// The file is owner-read. A zero timestamp is filled with the current
// time. The matched secret is not a field on Record, and this function
// does not invent one.
func AppendQuarantine(stateDir string, rec Record) error {
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}
	have, err := ReadQuarantine(stateDir)
	if err != nil {
		return err
	}
	for _, old := range have {
		if old.same(rec) {
			return nil
		}
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

// ReadQuarantine lists the log in the order it was written. A missing
// log is empty. A line that does not parse, such as one torn by a
// crash, is left out.
func ReadQuarantine(stateDir string) ([]Record, error) {
	f, err := os.Open(QuarantineFile(stateDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redact: %w", err)
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("redact: %w", err)
	}
	return out, nil
}

// Allowed is one digest the operator acknowledged with quarantine
// allow. Only a file whose bytes hash to SHA256 uploads with an
// override stamp. A file that changes is scanned again.
type Allowed struct {
	SHA256  string    `json:"sha256"`
	RelPath string    `json:"relpath"`
	At      time.Time `json:"at"`
}

// AllowFile is the list of acknowledged digests in a state directory.
func AllowFile(stateDir string) string {
	return filepath.Join(stateDir, "quarantine_allow.json")
}

// ReadAllowed loads the acknowledged digests. A missing file is empty.
func ReadAllowed(stateDir string) ([]Allowed, error) {
	b, err := os.ReadFile(AllowFile(stateDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redact: %w", err)
	}
	var out []Allowed
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("redact: %s: %w", AllowFile(stateDir), err)
	}
	return out, nil
}

// AllowedDigests is ReadAllowed as a set.
func AllowedDigests(stateDir string) (map[string]bool, error) {
	list, err := ReadAllowed(stateDir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(list))
	for _, a := range list {
		out[a.SHA256] = true
	}
	return out, nil
}

// Allow adds a to the list. A digest already there is left as it is.
// The file is replaced by rename, so a reader sees the old list or
// the new one.
func Allow(stateDir string, a Allowed) error {
	list, err := ReadAllowed(stateDir)
	if err != nil {
		return err
	}
	for _, have := range list {
		if have.SHA256 == a.SHA256 {
			return nil
		}
	}
	if a.At.IsZero() {
		a.At = time.Now().UTC()
	}
	list = append(list, a)
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	tmp, err := os.CreateTemp(stateDir, ".quarantine-allow-*")
	if err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	name := tmp.Name()
	defer func() {
		tmp.Close()
		if name != "" {
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	if err := os.Rename(name, AllowFile(stateDir)); err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	name = ""
	return nil
}

// Package testharness writes synthetic harness homes on disk.
//
// Container suites and unit tests call it to plant session files a
// harness Discover walk already accepts. The writers do not read
// configuration, open a lake, upload, normalize, or start lampi.
//
// root is the absolute harness home. cwd is the absolute project
// directory recorded in session metadata. Both are cleaned with
// filepath.Clean. An empty SessionSpec.ID is replaced with a unique id
// prefixed with "syn-". An empty Prompt becomes "synthetic prompt "
// plus that id. Extra is reserved; unknown keys are ignored.
//
// Planting an id that is already on disk overwrites that session file
// and leaves every other file in the home alone.
//
// Terva sessions are sessions/<id>/<id>.jsonl. Discover walks
// sessions/**/*.jsonl and does not treat this name as an errors
// sidecar. A flat sessions/<id>.jsonl would also be discovered; this
// package uses the nested path so the session file has its own
// directory. The first line is a meta object with meta.id and meta.cwd.
// The second line is one user message whose text is the prompt.
//
// Claude Code sessions are projects/<slug>/<id>.jsonl. slug is cwd with
// each '/' replaced by '-', so /work/demo is -work-demo. Discover walks
// projects/**/*.jsonl. The file is one user line with sessionId, cwd,
// and message content set to the prompt.
//
// Codex rollouts are
// sessions/<YYYY>/<MM>/<DD>/rollout-<stamp>-<id>.jsonl, which matches
// sessions/**/rollout-*.jsonl. The date and stamp are UTC at the first
// plant of that id. A later plant overwrites the existing rollout
// instead of adding another day directory. The file is a session_meta
// line (payload id and cwd) and an event_msg user_message.
// history.jsonl is never written.
//
// OpenCode sessions are export/<id>.json and never a database file.
// The document is one object, info.id plus info.directory, and messages
// holding one user text part set to the prompt. Discover walks
// export/**/*.json and only falls back to a root *.db when that glob is
// empty, so a planted export keeps the database off the discover set.
package testharness

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionSpec is one synthetic session.
//
// ID is the stable native session id. Empty means invent a unique id
// under the "syn-" prefix. The id must be a single path segment: a
// separator, a leading dot, or an ".errors" suffix is rejected.
// Discover skips dotfiles, and a file named *.errors.jsonl is an errors
// sidecar rather than the primary Terva transcript.
//
// Prompt is the one user turn. Empty means "synthetic prompt <id>".
//
// Extra is reserved. Unknown keys are ignored.
type SessionSpec struct {
	ID     string
	Prompt string
	Extra  map[string]string
}

// PlantResult is the sessions one Plant call wrote.
//
// Files are absolute paths, in spec order. SessionIDs are the native
// ids in that same order, with an invented id filled in when the spec
// left ID empty.
type PlantResult struct {
	Files      []string
	SessionIDs []string
}

// PlantTerva writes sessions/<id>/<id>.jsonl under root.
// The file is a meta line (id, cwd) and one user message (prompt).
// Planting the same id again overwrites that file only.
func PlantTerva(root, cwd string, sessions []SessionSpec) (PlantResult, error) {
	return plant(root, cwd, sessions, tervaSession)
}

// PlantClaude writes projects/<slug>/<id>.jsonl under root.
// slug is cwd with '/' replaced by '-'. The file is one user line
// carrying sessionId, cwd, and the prompt. Planting the same id and
// cwd again overwrites that file only.
func PlantClaude(root, cwd string, sessions []SessionSpec) (PlantResult, error) {
	return plant(root, cwd, sessions, claudeSession)
}

// PlantCodex writes a rollout JSONL under root/sessions/<YYYY>/<MM>/<DD>/.
// The name matches rollout-*.jsonl. A later plant of the same id
// overwrites the rollout already on disk. history.jsonl is not written.
func PlantCodex(root, cwd string, sessions []SessionSpec) (PlantResult, error) {
	return plant(root, cwd, sessions, codexSession)
}

// PlantOpenCode writes export/<id>.json under root.
// The document is info (id, directory) and one user message. A database
// file is not written. Planting the same id again overwrites that file only.
func PlantOpenCode(root, cwd string, sessions []SessionSpec) (PlantResult, error) {
	return plant(root, cwd, sessions, openCodeSession)
}

type sessionFile func(root, cwd, id, prompt string, when time.Time) (string, []byte, error)

func plant(root, cwd string, sessions []SessionSpec, write sessionFile) (PlantResult, error) {
	root, cwd, err := cleanAbs(root, cwd)
	if err != nil {
		return PlantResult{}, err
	}
	ids, err := resolveIDs(sessions)
	if err != nil {
		return PlantResult{}, err
	}
	when := time.Now().UTC()
	var res PlantResult
	for i, spec := range sessions {
		id := ids[i]
		prompt := spec.Prompt
		if prompt == "" {
			prompt = "synthetic prompt " + id
		}
		// Extra has no known keys. Reading it would invent behavior.
		path, body, err := write(root, cwd, id, prompt, when)
		if err != nil {
			return res, err
		}
		if err := within(root, path); err != nil {
			return res, err
		}
		if err := writeFile(path, body); err != nil {
			return res, err
		}
		res.Files = append(res.Files, path)
		res.SessionIDs = append(res.SessionIDs, id)
	}
	return res, nil
}

func cleanAbs(root, cwd string) (string, string, error) {
	root, err := absPath("root", root)
	if err != nil {
		return "", "", err
	}
	cwd, err = absPath("cwd", cwd)
	if err != nil {
		return "", "", err
	}
	return root, cwd, nil
}

func absPath(name, p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("testharness: %s must be an absolute path", name)
	}
	return filepath.Clean(p), nil
}

func resolveIDs(sessions []SessionSpec) ([]string, error) {
	ids := make([]string, len(sessions))
	seen := make(map[string]struct{}, len(sessions))
	for i, spec := range sessions {
		id := spec.ID
		if id == "" {
			invented, err := uniqueID(seen)
			if err != nil {
				return nil, err
			}
			id = invented
		} else if err := checkID(id); err != nil {
			return nil, err
		}
		ids[i] = id
		seen[id] = struct{}{}
	}
	return ids, nil
}

func uniqueID(seen map[string]struct{}) (string, error) {
	for range 8 {
		id, err := inventID()
		if err != nil {
			return "", err
		}
		if _, taken := seen[id]; taken {
			continue
		}
		return id, nil
	}
	return "", fmt.Errorf("testharness: could not invent a session id")
}

func inventID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("testharness: invent session id: %w", err)
	}
	return "syn-" + hex.EncodeToString(buf[:]), nil
}

func checkID(id string) error {
	if id == "" || id == "." || id == ".." || strings.HasPrefix(id, ".") || strings.HasSuffix(id, ".errors") {
		return fmt.Errorf("testharness: invalid session id %q", id)
	}
	for _, r := range id {
		if r <= 0x1f || r == 0x7f || r == '/' || r == '\\' {
			return fmt.Errorf("testharness: invalid session id %q", id)
		}
	}
	return nil
}

func within(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("testharness: %s is outside %s", path, root)
	}
	return nil
}

func writeFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("testharness: %w", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("testharness: %w", err)
	}
	return nil
}

func encodeLines(lines ...any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			return nil, fmt.Errorf("testharness: encode: %w", err)
		}
	}
	return buf.Bytes(), nil
}

func tervaSession(root, cwd, id, prompt string, when time.Time) (string, []byte, error) {
	stamp := when.UTC().Format(time.RFC3339Nano)
	body, err := encodeLines(
		tervaLine{
			Type: "meta",
			Meta: &tervaMeta{ID: id, CWD: cwd},
			At:   stamp,
		},
		tervaLine{
			Type: "message",
			Message: &tervaMessage{
				Role:    "user",
				Content: []tervaBlock{{Type: "text", Text: prompt}},
				Time:    stamp,
			},
		},
	)
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(root, "sessions", id, id+".jsonl"), body, nil
}

type tervaLine struct {
	Type    string        `json:"type"`
	Meta    *tervaMeta    `json:"meta,omitempty"`
	Message *tervaMessage `json:"message,omitempty"`
	At      string        `json:"at,omitempty"`
}

type tervaMeta struct {
	ID  string `json:"id"`
	CWD string `json:"cwd"`
}

type tervaMessage struct {
	Role    string       `json:"role"`
	Content []tervaBlock `json:"content"`
	Time    string       `json:"time"`
}

type tervaBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func claudeSession(root, cwd, id, prompt string, when time.Time) (string, []byte, error) {
	slug := claudeSlug(cwd)
	if slug == "" || slug == "." || slug == ".." || strings.ContainsAny(slug, `/\`) {
		return "", nil, fmt.Errorf("testharness: cwd %s has no Claude project slug", cwd)
	}
	body, err := encodeLines(claudeLine{
		Type:      "user",
		SessionID: id,
		CWD:       cwd,
		Timestamp: when.UTC().Format(time.RFC3339Nano),
		Message: claudeMessage{
			Role:    "user",
			Content: prompt,
		},
	})
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(root, "projects", slug, id+".jsonl"), body, nil
}

// claudeSlug is the Claude Code project directory name for cwd.
// Claude replaces each path separator, and the fixtures use that for
// '/' only: /work/demo becomes -work-demo.
func claudeSlug(cwd string) string {
	return strings.ReplaceAll(cwd, "/", "-")
}

type claudeLine struct {
	Type      string        `json:"type"`
	SessionID string        `json:"sessionId"`
	CWD       string        `json:"cwd"`
	Timestamp string        `json:"timestamp"`
	Message   claudeMessage `json:"message"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func codexSession(root, cwd, id, prompt string, when time.Time) (string, []byte, error) {
	path, err := codexPath(root, id, when)
	if err != nil {
		return "", nil, err
	}
	stamp := when.UTC().Format(time.RFC3339Nano)
	body, err := encodeLines(
		codexLine{
			Timestamp: stamp,
			Type:      "session_meta",
			Payload:   codexMeta{ID: id, CWD: cwd},
		},
		codexLine{
			Timestamp: stamp,
			Type:      "event_msg",
			Payload:   codexUser{Type: "user_message", Message: prompt},
		},
	)
	if err != nil {
		return "", nil, err
	}
	return path, body, nil
}

func codexPath(root, id string, when time.Time) (string, error) {
	existing, err := findCodex(root, id)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}
	when = when.UTC()
	dir := filepath.Join(root, "sessions", when.Format("2006"), when.Format("01"), when.Format("02"))
	name := "rollout-" + when.Format("2006-01-02T15-04-05") + "-" + id + ".jsonl"
	return filepath.Join(dir, name), nil
}

func findCodex(root, id string) (string, error) {
	sessions := filepath.Join(root, "sessions")
	info, err := os.Stat(sessions)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("testharness: %s is not a directory", sessions)
	}
	var found string
	err = filepath.WalkDir(sessions, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if codexNameFor(d.Name(), id) {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return found, nil
}

// codexNameFor reports whether name is rollout-<YYYY-MM-DD>T<hh-mm-ss>-<id>.jsonl.
// The stamp is fixed-width so an id that is a suffix of another id does
// not match that other file.
func codexNameFor(name, id string) bool {
	const prefix = "rollout-"
	suffix := "-" + id + ".jsonl"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	stamp := name[len(prefix) : len(name)-len(suffix)]
	_, err := time.Parse("2006-01-02T15-04-05", stamp)
	return err == nil
}

type codexLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   any    `json:"payload"`
}

type codexMeta struct {
	ID  string `json:"id"`
	CWD string `json:"cwd"`
}

type codexUser struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func openCodeSession(root, cwd, id, prompt string, _ time.Time) (string, []byte, error) {
	body, err := encodeLines(openCodeDoc{
		Info: openCodeInfo{ID: id, Directory: cwd},
		Messages: []openCodeMessage{{
			Info: openCodeMsgInfo{
				ID:        "msg-" + id,
				SessionID: id,
				Role:      "user",
			},
			Parts: []openCodePart{{Type: "text", Text: prompt}},
		}},
	})
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(root, "export", id+".json"), body, nil
}

type openCodeDoc struct {
	Info     openCodeInfo      `json:"info"`
	Messages []openCodeMessage `json:"messages"`
}

type openCodeInfo struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
}

type openCodeMessage struct {
	Info  openCodeMsgInfo `json:"info"`
	Parts []openCodePart  `json:"parts"`
}

type openCodeMsgInfo struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Role      string `json:"role"`
}

type openCodePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

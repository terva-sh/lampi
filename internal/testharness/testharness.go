// Package testharness writes synthetic harness homes on disk.
//
// Container suites and unit tests call it to plant session files a
// harness Discover walk already accepts. The writers do not read
// configuration, open a lake, upload, normalize, or start lampi.
//
// root is the absolute harness home. cwd is the absolute project
// directory recorded in session metadata. Both are cleaned with
// filepath.Clean. An empty SessionSpec.ID is replaced with a unique id
// prefixed with "syn-". PlantGrok invents a UUID instead. An empty
// Prompt becomes "synthetic prompt " plus that id. Extra is reserved;
// unknown keys are ignored.
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
//
// Grok Build sessions are sessions/<encoded-cwd>/<uuid>/updates.jsonl.
// The id is a UUID. An empty ID is invented as one. The transcript is
// one ACP user_message_chunk whose text is the prompt. summary.json is
// written beside it with info.cwd, a title, and a model. When the
// URL-encoded cwd is longer than 255 bytes, the group directory is
// the slug-hash form and a .cwd file holds the original path.
// chat_history.jsonl is not written. Extra keys are ignored.
//
// Grok Bot transcripts are agent-transcripts/<uuid>/<uuid>.jsonl.
// PlantGrokBot takes no cwd: the manifest cwd is that agent directory,
// which is absolute because root is. store.db and conversation-blobs.db
// are not written. The file is one user line whose text is the prompt.
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

	"terva.sh/lampi/internal/adapter/grok"
	"terva.sh/lampi/internal/adapter/grokbot"
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

// PlantGrok writes sessions/<encoded-cwd>/<uuid>/updates.jsonl under
// root, plus summary.json in that directory. The id must be a UUID.
// An empty ID is replaced with a new UUID. A cwd whose URL-encoding
// exceeds 255 bytes uses the slug-hash directory and a .cwd file.
// Planting the same id and cwd again overwrites that session's files.
func PlantGrok(root, cwd string, sessions []SessionSpec) (PlantResult, error) {
	specs := make([]SessionSpec, len(sessions))
	copy(specs, sessions)
	seen := map[string]struct{}{}
	for i := range specs {
		id := specs[i].ID
		if id == "" {
			invented, err := inventUUID(seen)
			if err != nil {
				return PlantResult{}, err
			}
			id = invented
			specs[i].ID = id
		} else if !grok.ValidSessionID(id) {
			return PlantResult{}, fmt.Errorf("testharness: session id must be a UUID")
		}
		if _, taken := seen[id]; taken {
			return PlantResult{}, fmt.Errorf("testharness: duplicate session id %s", id)
		}
		seen[id] = struct{}{}
	}
	res, err := plant(root, cwd, specs, grokSession)
	if err != nil {
		return res, err
	}
	for i, path := range res.Files {
		if err := writeGrokCompanion(path, cwd, res.SessionIDs[i]); err != nil {
			return res, err
		}
	}
	return res, nil
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

func grokSession(root, cwd, id, prompt string, _ time.Time) (string, []byte, error) {
	body, err := encodeLines(grokACPLine{
		Method: "session/update",
		Params: grokACPParams{
			SessionID: id,
			Update: grokACPUpdate{
				SessionUpdate: "user_message_chunk",
				Content:       &grokACPBlock{Type: "text", Text: prompt},
			},
		},
	})
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(root, "sessions", grok.EncodeCWDDirname(cwd), id, "updates.jsonl"), body, nil
}

type grokACPLine struct {
	Method string        `json:"method"`
	Params grokACPParams `json:"params"`
}

type grokACPParams struct {
	SessionID string        `json:"sessionId"`
	Update    grokACPUpdate `json:"update"`
}

type grokACPUpdate struct {
	SessionUpdate string        `json:"sessionUpdate"`
	Content       *grokACPBlock `json:"content,omitempty"`
}

type grokACPBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type grokSummary struct {
	Info           grokInfo `json:"info"`
	GeneratedTitle string   `json:"generated_title"`
	ModelID        string   `json:"current_model_id"`
}

type grokInfo struct {
	ID  string `json:"id"`
	CWD string `json:"cwd"`
}

// PlantGrokBot writes agent-transcripts/<uuid>/<uuid>.jsonl under root.
// The id must be a UUID. An empty ID is replaced with a new UUID. The
// file is one user message whose text is the prompt. The absolute cwd
// of the session is the planted agent-transcripts/<uuid> directory.
// store.db and conversation-blobs.db are not written. Planting the
// same id again overwrites that file only.
func PlantGrokBot(root string, sessions []SessionSpec) (PlantResult, error) {
	root, err := absPath("root", root)
	if err != nil {
		return PlantResult{}, err
	}
	specs := make([]SessionSpec, len(sessions))
	copy(specs, sessions)
	seen := map[string]struct{}{}
	for i := range specs {
		id := specs[i].ID
		if id == "" {
			invented, err := inventUUID(seen)
			if err != nil {
				return PlantResult{}, err
			}
			id = invented
			specs[i].ID = id
		} else if !grokbot.ValidSessionID(id) {
			return PlantResult{}, fmt.Errorf("testharness: session id must be a UUID")
		}
		if _, taken := seen[id]; taken {
			return PlantResult{}, fmt.Errorf("testharness: duplicate session id %s", id)
		}
		seen[id] = struct{}{}
	}
	var res PlantResult
	for _, spec := range specs {
		prompt := spec.Prompt
		if prompt == "" {
			prompt = "synthetic prompt " + spec.ID
		}
		body, err := encodeLines(grokBotLine{
			Role: "user",
			Message: grokBotMessage{
				Content: []grokBotPart{{Type: "text", Text: prompt}},
			},
		})
		if err != nil {
			return res, err
		}
		path := filepath.Join(root, "agent-transcripts", spec.ID, spec.ID+".jsonl")
		if err := within(root, path); err != nil {
			return res, err
		}
		if err := writeFile(path, body); err != nil {
			return res, err
		}
		res.Files = append(res.Files, path)
		res.SessionIDs = append(res.SessionIDs, spec.ID)
	}
	return res, nil
}

type grokBotLine struct {
	Role    string         `json:"role"`
	Message grokBotMessage `json:"message"`
}

type grokBotMessage struct {
	Content []grokBotPart `json:"content"`
}

type grokBotPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func writeGrokCompanion(updates, cwd, id string) error {
	dir := filepath.Dir(updates)
	body, err := encodeLines(grokSummary{
		Info:           grokInfo{ID: id, CWD: cwd},
		GeneratedTitle: "synthetic " + id,
		ModelID:        "grok-build",
	})
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "summary.json"), body); err != nil {
		return err
	}
	if !grok.UsesCWDFile(cwd) {
		return nil
	}
	group := filepath.Dir(dir)
	marker := filepath.Join(group, ".cwd")
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	return writeFile(marker, []byte(cwd))
}

func inventUUID(seen map[string]struct{}) (string, error) {
	for range 8 {
		var buf [16]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return "", fmt.Errorf("testharness: invent session id: %w", err)
		}
		buf[6] = (buf[6] & 0x0f) | 0x40
		buf[8] = (buf[8] & 0x3f) | 0x80
		id := fmt.Sprintf("%s-%s-%s-%s-%s",
			hex.EncodeToString(buf[0:4]),
			hex.EncodeToString(buf[4:6]),
			hex.EncodeToString(buf[6:8]),
			hex.EncodeToString(buf[8:10]),
			hex.EncodeToString(buf[10:16]),
		)
		if _, taken := seen[id]; taken {
			continue
		}
		return id, nil
	}
	return "", fmt.Errorf("testharness: could not invent a session id")
}

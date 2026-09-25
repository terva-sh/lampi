// Package claude is the Claude Code harness adapter.
//
// Session files live at $CLAUDE_CONFIG_DIR/projects/**/*.jsonl. When
// CLAUDE_CONFIG_DIR is unset, the directory is ~/.claude, which is the
// default Claude Code documents (on Windows, %USERPROFILE%\.claude).
// The resolution order matches terva: the environment variable, then
// that default. There is no XDG fallback.
//
// The JSON object on each line is internal. Version is the pinned
// reader. Keys that reader does not interpret stay on Record.Extra.
// Sync uploads the file bytes; it does not rewrite them.
package claude

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/protocol"
)

// errNotObject is a line that is not a JSON object. A torn tail is
// common in a file that is still being appended. The walk skips it.
var errNotObject = errors.New("claude: line is not a JSON object")

// Adapter implements adapter.Harness for Claude Code session JSONL.
type Adapter struct{}

var _ adapter.Harness = Adapter{}

// Name is the harness string written into manifests.
func (Adapter) Name() string { return protocol.HarnessClaude }

// Home resolves the Claude Code config directory.
func (Adapter) Home(getenv func(string) string) (string, error) {
	return Home(getenv)
}

// Home resolves the Claude Code config directory. CLAUDE_CONFIG_DIR
// wins. Otherwise the directory is ~/.claude.
func Home(getenv func(string) string) (string, error) {
	if v := getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v, nil
	}
	home, err := adapter.HomeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// WatchDir is the directory under the config dir that holds JSONL.
func (Adapter) WatchDir() string { return "projects" }

// Match reports whether rel, slash-separated from the config dir, is a
// session file under projects/. Dotfiles are skipped. The glob is
// projects/**/*.jsonl.
func (Adapter) Match(rel string) (string, bool) {
	rel = path.Clean(rel)
	if rel == "." || !strings.HasPrefix(rel, "projects/") {
		return "", false
	}
	name := path.Base(rel)
	if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	return protocol.KindTranscriptJSONL, true
}

// Discover lists projects/**/*.jsonl. A missing projects directory is
// an empty list.
func (a Adapter) Discover(ctx context.Context, root string) ([]adapter.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return adapter.Walk(root, a.WatchDir(), a.Match)
}

// ReadSlice opens absPath at offset. Offset 0 reads the whole file.
func (Adapter) ReadSlice(ctx context.Context, absPath string, offset int64) (io.ReadCloser, error) {
	return adapter.OpenSlice(ctx, absPath, offset)
}

// Manifests implements adapter.Harness.
func (Adapter) Manifests(root, machineID string) (adapter.Bundle, error) {
	return Manifests(root, machineID)
}

// identity is what the memo keeps for one file.
type identity struct {
	Session string `json:"session,omitempty"`
	CWD     string `json:"cwd,omitempty"`
}

type item struct {
	ref     adapter.Ref
	sum     string
	session string
	cwd     string
}

// Manifests builds one manifest per Claude session id. Files that do
// not carry a session id of their own use the relative path, so two
// unrelated transcripts are not merged. harness_version is Version.
// Redaction is left empty. The upload path scans the file.
func Manifests(root, machineID string) (adapter.Bundle, error) {
	return ManifestsMemo(root, machineID, nil)
}

// ManifestsMemo is Manifests with a memo. A file the memo recalls at
// the stat the walk saw is not opened: its digest and its session id
// and cwd come from the memo.
func ManifestsMemo(root, machineID string, memo adapter.Memo) (adapter.Bundle, error) {
	refs, skipped, err := adapter.WalkSkipped(root, Adapter{}.WatchDir(), Adapter{}.Match)
	if err != nil {
		return adapter.Bundle{}, err
	}
	items := make([]item, 0, len(refs))
	for _, ref := range refs {
		// One file that cannot be read is left out. The rest of the
		// harness still uploads.
		sum, id, err := adapter.Identify(memo, ref, func() (identity, error) {
			session, cwd, err := readIdentity(ref.AbsPath)
			return identity{Session: session, CWD: cwd}, err
		})
		if err != nil {
			skipped = append(skipped, fmt.Errorf("claude: %s: %w", ref.RelPath, err))
			continue
		}
		session := id.Session
		if session == "" {
			session = strings.TrimSuffix(ref.RelPath, ".jsonl")
		}
		items = append(items, item{ref: ref, sum: sum, session: session, cwd: id.CWD})
	}

	order := make([]string, 0)
	groups := map[string][]item{}
	for _, it := range items {
		if _, ok := groups[it.session]; !ok {
			order = append(order, it.session)
		}
		groups[it.session] = append(groups[it.session], it)
	}

	b := adapter.Bundle{Root: root, Paths: map[string]string{}, Skipped: skipped}
	for _, id := range order {
		group := groups[id]
		var cwd string
		arts := make([]protocol.Artifact, 0, len(group))
		for _, it := range group {
			if cwd == "" {
				cwd = it.cwd
			}
			b.Paths[it.ref.RelPath] = it.ref.AbsPath
			arts = append(arts, protocol.Artifact{
				Kind:              it.ref.Kind,
				RelPath:           it.ref.RelPath,
				Size:              it.ref.Size,
				MTime:             it.ref.ModTime.UTC(),
				SHA256:            it.sum,
				ByteWatermarkPrev: 0,
				TailSHA256:        it.sum,
			})
		}
		b.Manifests = append(b.Manifests, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machineID,
			Harness:         protocol.HarnessClaude,
			HarnessVersion:  Version,
			NativeSessionID: id,
			Project:         adapter.ProjectAt(cwd),
			Artifacts:       arts,
		})
	}
	return b, nil
}

// maxLine is the longest line readIdentity parses. A longer one is read
// past, not held in memory.
const maxLine = 8 << 20

// readIdentity scans until it has seen a session id and a cwd, or the
// file ends. A line that is not a JSON object is skipped. A line over
// maxLine is skipped too, but when the id or the cwd is still missing
// at the end the file is a *adapter.LongLineError. parentUuid is
// a message pointer in this reader, not a parent session, so it stays
// in Extra and is not copied onto lineage.
func readIdentity(path string) (sessionID, cwd string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	long := false
	err = adapter.ScanLines(f, maxLine, func(line []byte, over bool) bool {
		if over {
			long = true
			return true
		}
		rec, err := ParseLine(line)
		if err != nil {
			return true
		}
		if sessionID == "" {
			sessionID = rec.SessionID
		}
		if cwd == "" {
			cwd = rec.CWD
		}
		return sessionID == "" || cwd == ""
	})
	if err != nil {
		return "", "", err
	}
	// The id or the cwd may have been on the line that was too long.
	// A relpath id would split this session from the rest of it.
	if long && (sessionID == "" || cwd == "") {
		return "", "", &adapter.LongLineError{Max: maxLine}
	}
	return sessionID, cwd, nil
}

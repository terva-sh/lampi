// Package grokbot is the Grok Bot harness adapter.
//
// Session files live at $GROK_BOT_HOME/agent-transcripts/<uuid>/<uuid>.jsonl.
// When GROK_BOT_HOME is unset, there is no default directory and no
// ~/.grok fallback. A config root (harnesses.grokbot.root) is applied
// by the caller before Home, and it wins over the environment variable.
// A relative GROK_BOT_HOME is used as given.
//
// The transcript is that JSONL. Both path segments are UUIDs and they
// are the same id. The native session id is that UUID. The file has no
// cwd; the manifest cwd is the agent-transcripts/<uuid> directory, which
// exists and is not a git checkout. store.db, conversation-blobs.db, and
// *.journal-mode are not artifacts. Sync uploads the JSONL bytes; it
// does not rewrite them. Workers project that JSONL.
package grokbot

import (
	"context"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/protocol"
)

// Version is the pinned reader for Grok Bot transcript JSONL.
// The object on each line is internal. It is not capture protocol 1.
// Bump Version when a key this reader interprets changes meaning.
const Version = "1"

// sessionUUID is the directory and file name this pin reads. A name
// that is not a UUID is not a session here.
var sessionUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Adapter implements adapter.Harness for Grok Bot transcripts.
type Adapter struct{}

var _ adapter.Harness = Adapter{}

// Name is the harness string written into manifests.
func (Adapter) Name() string { return protocol.HarnessGrokBot }

// Home resolves the Grok Bot home. GROK_BOT_HOME wins. There is no
// default directory.
func (Adapter) Home(getenv func(string) string) (string, error) {
	return Home(getenv)
}

// Home resolves the Grok Bot home. GROK_BOT_HOME wins. There is no
// default directory and no ~/.grok fallback.
func Home(getenv func(string) string) (string, error) {
	if v := getenv("GROK_BOT_HOME"); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("grokbot: GROK_BOT_HOME is not set")
}

// WatchDir is the directory under the home that holds transcripts.
func (Adapter) WatchDir() string { return "agent-transcripts" }

// Match reports whether rel, slash-separated from the home, is a
// transcript. The path is agent-transcripts/<uuid>/<same-uuid>.jsonl.
// Both segments are UUIDs and they are equal. *.journal-mode and every
// other name, including store.db and conversation-blobs.db, are skipped.
func (Adapter) Match(rel string) (string, bool) {
	if _, ok := sessionFile(rel); !ok {
		return "", false
	}
	return protocol.KindTranscriptJSONL, true
}

// Discover lists transcripts. A missing agent-transcripts directory is
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

// ValidSessionID reports whether id is a session UUID this pin reads.
func ValidSessionID(id string) bool {
	return sessionUUID.MatchString(id)
}

// identity is what the memo keeps. The session id is the path, so the
// file is not parsed.
type identity struct{}

// Manifests builds one manifest per session UUID. The JSONL is the only
// artifact. harness_version is Version. The manifest cwd is the agent
// directory, so an allowlist prefix can name it. A file that cannot be
// read is left out and named.
func Manifests(root, machineID string) (adapter.Bundle, error) {
	return ManifestsMemo(root, machineID, nil)
}

// ManifestsMemo is Manifests with a memo. A file the memo recalls at
// the stat the walk saw is not opened.
func ManifestsMemo(root, machineID string, memo adapter.Memo) (adapter.Bundle, error) {
	refs, skipped, err := adapter.WalkSkipped(root, Adapter{}.WatchDir(), Adapter{}.Match)
	if err != nil {
		return adapter.Bundle{}, err
	}
	b := adapter.Bundle{Root: root, Paths: map[string]string{}, Skipped: skipped}
	for _, ref := range refs {
		uuid, ok := sessionFile(ref.RelPath)
		if !ok {
			continue
		}
		sum, _, err := adapter.Identify(memo, ref, func() (identity, error) {
			return identity{}, nil
		})
		if err != nil {
			b.Skipped = append(b.Skipped, fmt.Errorf("grokbot: %s: %w", ref.RelPath, err))
			continue
		}
		b.Paths[ref.RelPath] = ref.AbsPath
		cwd := filepath.Join(root, "agent-transcripts", uuid)
		b.Manifests = append(b.Manifests, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machineID,
			Harness:         protocol.HarnessGrokBot,
			HarnessVersion:  Version,
			NativeSessionID: uuid,
			Project:         adapter.ProjectAt(cwd),
			Artifacts: []protocol.Artifact{{
				Kind:              ref.Kind,
				RelPath:           ref.RelPath,
				Size:              ref.Size,
				MTime:             ref.ModTime.UTC(),
				SHA256:            sum,
				ByteWatermarkPrev: 0,
				TailSHA256:        sum,
			}},
		})
	}
	return b, nil
}

// sessionFile reports the UUID of agent-transcripts/<uuid>/<uuid>.jsonl.
// ok is false for every other relative path, including a journal file
// and a pair of UUIDs that are not the same string.
func sessionFile(rel string) (uuid string, ok bool) {
	rel = path.Clean(rel)
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || parts[0] != "agent-transcripts" {
		return "", false
	}
	dir, name := parts[1], parts[2]
	if dir == "" || name == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".journal-mode") {
		return "", false
	}
	if !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	fileID := strings.TrimSuffix(name, ".jsonl")
	if fileID != dir || !sessionUUID.MatchString(dir) {
		return "", false
	}
	return dir, true
}

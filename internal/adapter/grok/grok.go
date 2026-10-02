// Package grok is the Grok Build harness adapter.
//
// Session files live at $GROK_HOME/sessions/<encoded-cwd>/<uuid>/.
// When GROK_HOME is unset, the directory is ~/.grok. There is no XDG
// fallback. A relative GROK_HOME is used as given.
//
// updates.jsonl is the transcript and the source of truth. summary.json
// beside it is the companion that carries cwd, title, and model. The
// group directory name is the URL-encoding of the cwd. When that
// encoding is longer than 255 bytes, the name is a slug plus a BLAKE3
// prefix and a .cwd file in the group directory holds the original
// path. The native session id is the UUID directory.
//
// chat_history.jsonl and the other files in the session directory are
// not artifacts. Sync uploads the file bytes; it does not rewrite them.
// Workers do not project this harness yet.
package grok

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/protocol"
)

// errNotObject is a line that is not a JSON object.
var errNotObject = errors.New("grok: line is not a JSON object")

// sessionUUID is the session directory name this pin reads. A client
// supplied id that is not a UUID is not a session here.
var sessionUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Adapter implements adapter.Harness for Grok Build sessions.
type Adapter struct{}

var _ adapter.Harness = Adapter{}

// Name is the harness string written into manifests.
func (Adapter) Name() string { return protocol.HarnessGrok }

// Home resolves the Grok Build home. GROK_HOME wins. Otherwise the
// directory is ~/.grok.
func (Adapter) Home(getenv func(string) string) (string, error) {
	return Home(getenv)
}

// Home resolves the Grok Build home. GROK_HOME wins. Otherwise the
// directory is ~/.grok.
func Home(getenv func(string) string) (string, error) {
	if v := getenv("GROK_HOME"); v != "" {
		return v, nil
	}
	home, err := adapter.HomeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grok"), nil
}

// WatchDir is the directory under the home that holds sessions.
func (Adapter) WatchDir() string { return "sessions" }

// Match reports whether rel, slash-separated from the home, is a
// session transcript or its summary companion. The transcript is
// sessions/<encoded-cwd>/<uuid>/updates.jsonl. The companion is
// summary.json in that same directory. Dotfiles and every other
// name are skipped.
func (Adapter) Match(rel string) (string, bool) {
	_, _, name, ok := sessionParts(rel)
	if !ok {
		return "", false
	}
	switch name {
	case "updates.jsonl":
		return protocol.KindTranscriptJSONL, true
	case "summary.json":
		return protocol.KindSummaryJSON, true
	default:
		return "", false
	}
}

// Discover lists session transcripts and summary companions. A missing
// sessions directory is an empty list.
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

type item struct {
	ref  adapter.Ref
	sum  string
	id   fileIdent
	uuid string
}

// Manifests builds one manifest per session UUID. updates.jsonl is the
// transcript. summary.json, when it is readable, is the companion.
// harness_version is Version. A session id that is not a UUID is not
// a session. A file that cannot be read is left out and named.
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
	order := make([]string, 0)
	groups := map[string][]item{}
	for _, ref := range refs {
		_, uuid, name, ok := sessionParts(ref.RelPath)
		if !ok {
			continue
		}
		sum, ident, err := adapter.Identify(memo, ref, func() (fileIdent, error) {
			if name != "summary.json" {
				return fileIdent{}, nil
			}
			return readSummary(ref.AbsPath)
		})
		if err != nil {
			skipped = append(skipped, fmt.Errorf("grok: %s: %w", ref.RelPath, err))
			continue
		}
		if _, seen := groups[uuid]; !seen {
			order = append(order, uuid)
		}
		groups[uuid] = append(groups[uuid], item{ref: ref, sum: sum, id: ident, uuid: uuid})
	}

	b := adapter.Bundle{Root: root, Paths: map[string]string{}, Skipped: skipped}
	for _, id := range order {
		group := groups[id]
		transcript := false
		for _, it := range group {
			if it.ref.Kind == protocol.KindTranscriptJSONL {
				transcript = true
				break
			}
		}
		// summary.json alone is not a session. updates.jsonl is the
		// source of truth.
		if !transcript {
			continue
		}
		var cwd string
		var groupDir string
		arts := make([]protocol.Artifact, 0, len(group))
		// The transcript is the head. The companion follows it.
		// summary.json's title and model stay in the companion bytes
		// and in the memo ident readSummary stored.
		ordered := make([]item, 0, len(group))
		for _, it := range group {
			if it.ref.Kind == protocol.KindTranscriptJSONL {
				ordered = append(ordered, it)
			}
		}
		for _, it := range group {
			if it.ref.Kind != protocol.KindTranscriptJSONL {
				ordered = append(ordered, it)
			}
		}
		for _, it := range ordered {
			if groupDir == "" {
				groupDir = filepath.Dir(filepath.Dir(it.ref.AbsPath))
			}
			if cwd == "" {
				cwd = it.id.CWD
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
		if cwd == "" {
			cwd, _ = CWDFromGroup(groupDir)
		}
		b.Manifests = append(b.Manifests, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machineID,
			Harness:         protocol.HarnessGrok,
			HarnessVersion:  Version,
			NativeSessionID: id,
			Project:         adapter.ProjectAt(cwd),
			Artifacts:       arts,
		})
	}
	return b, nil
}

// sessionParts splits sessions/<encoded-cwd>/<uuid>/<name>. ok is
// false when rel is not that shape, or the UUID is not one this pin
// reads. name may still be a file this pin does not upload.
func sessionParts(rel string) (enc, uuid, name string, ok bool) {
	rel = path.Clean(rel)
	parts := strings.Split(rel, "/")
	if len(parts) != 4 || parts[0] != "sessions" {
		return "", "", "", false
	}
	enc, uuid, name = parts[1], parts[2], parts[3]
	if enc == "" || enc == "." || enc == ".." || strings.HasPrefix(enc, ".") || strings.HasPrefix(name, ".") {
		return "", "", "", false
	}
	if !sessionUUID.MatchString(uuid) {
		return "", "", "", false
	}
	return enc, uuid, name, true
}

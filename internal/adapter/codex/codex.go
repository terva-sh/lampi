// Package codex is the Codex CLI harness adapter.
//
// Rollouts live at $CODEX_HOME/sessions/**/rollout-*.jsonl. When
// CODEX_HOME is unset, the directory is ~/.codex, which is the default
// the Codex CLI documents (on Windows, %USERPROFILE%\.codex). The
// resolution order matches terva: the environment variable, then that
// default. There is no XDG fallback.
//
// history.jsonl is prompt history. It is not a rollout, including when
// a copy sits under sessions/. archived_sessions/ is a different tree
// and is not watched. A compressed rollout (rollout-*.jsonl.zst) is not
// a JSONL file this version reads.
//
// The JSON object on each line is internal. Version is the pinned
// reader. Keys that reader does not interpret stay on the record.
package codex

import (
	"bufio"
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
var errNotObject = errors.New("codex: line is not a JSON object")

// Adapter implements adapter.Harness for Codex CLI rollout JSONL.
type Adapter struct{}

var _ adapter.Harness = Adapter{}

// Name is the harness string written into manifests.
func (Adapter) Name() string { return protocol.HarnessCodex }

// Home resolves the Codex CLI state directory.
func (Adapter) Home(getenv func(string) string) (string, error) {
	return Home(getenv)
}

// Home resolves the Codex CLI state directory. CODEX_HOME wins.
// Otherwise the directory is ~/.codex.
func Home(getenv func(string) string) (string, error) {
	if v := getenv("CODEX_HOME"); v != "" {
		return v, nil
	}
	home, err := adapter.HomeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// WatchDir is the directory under CODEX_HOME that holds rollouts.
func (Adapter) WatchDir() string { return "sessions" }

// Match reports whether rel, slash-separated from CODEX_HOME, is a
// rollout. The glob is sessions/**/rollout-*.jsonl. history.jsonl does
// not match, wherever it sits under sessions/.
func (Adapter) Match(rel string) (string, bool) {
	rel = path.Clean(rel)
	if rel == "." || !strings.HasPrefix(rel, "sessions/") {
		return "", false
	}
	name := path.Base(rel)
	if strings.HasPrefix(name, ".") {
		return "", false
	}
	ok, err := path.Match("rollout-*.jsonl", name)
	if err != nil || !ok {
		return "", false
	}
	return protocol.KindTranscriptJSONL, true
}

// Discover lists rollout files. A missing sessions directory is an
// empty list.
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

type item struct {
	ref     adapter.Ref
	sum     string
	session string
	cwd     string
}

// Manifests builds one manifest per rollout session id. A file with no
// session_meta id uses its relative path. history.jsonl is not in the
// discover set, so it is not a manifest. harness_version is Version.
func Manifests(root, machineID string) (adapter.Bundle, error) {
	refs, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		return adapter.Bundle{}, err
	}
	items := make([]item, 0, len(refs))
	for _, ref := range refs {
		session, cwd, err := readIdentity(ref.AbsPath)
		if err != nil {
			return adapter.Bundle{}, fmt.Errorf("codex: %s: %w", ref.RelPath, err)
		}
		if session == "" {
			session = strings.TrimSuffix(ref.RelPath, ".jsonl")
		}
		sum, err := adapter.HashFile(ref.AbsPath)
		if err != nil {
			return adapter.Bundle{}, err
		}
		items = append(items, item{ref: ref, sum: sum, session: session, cwd: cwd})
	}

	order := make([]string, 0)
	groups := map[string][]item{}
	for _, it := range items {
		if _, ok := groups[it.session]; !ok {
			order = append(order, it.session)
		}
		groups[it.session] = append(groups[it.session], it)
	}

	b := adapter.Bundle{Root: root, Paths: map[string]string{}}
	for _, id := range order {
		group := groups[id]
		var cwd string
		arts := make([]protocol.Artifact, 0, len(group))
		for _, it := range group {
			if cwd == "" {
				cwd = it.cwd
			}
			b.Paths[it.sum] = it.ref.AbsPath
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
		remote, commit := adapter.ProjectGit(cwd)
		b.Manifests = append(b.Manifests, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machineID,
			Harness:         protocol.HarnessCodex,
			HarnessVersion:  Version,
			NativeSessionID: id,
			Project: protocol.Project{
				CWD:       cwd,
				CWDHash:   adapter.CWDHash(cwd),
				GitRemote: remote,
				GitCommit: commit,
			},
			Artifacts: arts,
		})
	}
	return b, nil
}

func readIdentity(path string) (sessionID, cwd string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		rec, err := ParseLine(sc.Bytes())
		if err != nil {
			continue
		}
		if sessionID == "" {
			sessionID = rec.SessionID()
		}
		if cwd == "" {
			cwd = rec.CWD()
		}
		if sessionID != "" && cwd != "" {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", err
	}
	return sessionID, cwd, nil
}

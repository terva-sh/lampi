// Package cursor is the Cursor IDE harness adapter.
//
// Cursor stores chat and editor state in undocumented SQLite databases
// named state.vscdb. One database is global. Each workspace has another.
// This reader is version 2 and its confidence is low. A major Cursor
// upgrade can rename a table or a key, and this pin will not follow it.
//
// The live database is never opened and never written. A read copies
// state.vscdb and, when they are present, state.vscdb-wal and
// state.vscdb-shm into a temporary directory, then opens that snapshot
// read-only. The copy is removed after the export is built. Sync uploads
// the export, not the database.
//
// Version 2 reads ItemTable (key TEXT, value BLOB). When cursorDiskKV
// exists it is read too. Current Cursor builds keep chat bodies in the
// global database. A workspace export snapshots that global database
// the same way and merges cursorDiskKV rows for composers named by the
// workspace ItemTable key composer.composerHeaders
// (allComposers[].composerId). composer.composerData is the older
// workspace list and is not the registry. A database with no ItemTable
// is an error. Keys under cursorAuth/ are dropped and do not appear in
// the export. Other keys are kept, including ones this version does not
// interpret. The export is what ruleset v1 scans. The key filter runs
// first, so a cursorAuth value is not in those bytes.
//
// The user-data directory is the Electron path Cursor inherits from VS
// Code. On Linux it is $XDG_CONFIG_HOME/Cursor, or ~/.config/Cursor when
// that variable is unset. On macOS it is ~/Library/Application Support/Cursor.
// On Windows it is %APPDATA%\Cursor, or %USERPROFILE%\AppData\Roaming\Cursor
// when APPDATA is unset. Databases live at User/globalStorage/state.vscdb
// and User/workspaceStorage/<id>/state.vscdb. The workspace directory
// name is opaque. The project cwd is the folder URI in the sibling
// workspace.json. A vscode-remote URI has no local path, so that
// workspace has an empty cwd and the allowlist keeps it. The global
// database holds every workspace, so it has an empty cwd and does not
// leave the machine. A workspace export is the session that carries
// that workspace's composers. Its session id stays workspace/<id>.
//
// VSCODE_APPDATA, VSCODE_PORTABLE, and a Windows user-data directory
// seen from WSL were not confirmed for current Cursor and are not
// consulted. Other Unix systems follow the Linux XDG path; that was
// not checked against a Cursor build. The Cursor CLI store.db is a
// different corpus and is not read.
//
// Workers project cursor_state_json. A stored manifest whose artifact
// is not that kind records normalize_error.
package cursor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/protocol"
)

const (
	// Version is the pinned reader. Bump it when ItemTable or
	// cursorDiskKV stops meaning what this package reads. Version 2
	// merges a workspace's composers from the global cursorDiskKV.
	// A Cursor release that renames the registry or the table breaks
	// this pin on purpose.
	Version = "2"
	// Confidence is low because the schema is undocumented and has
	// already moved between major Cursor versions.
	Confidence = "low"

	dbName     = "state.vscdb"
	globalRel  = "User/globalStorage/state.vscdb"
	userGlobal = "User/globalStorage"
	userSpaces = "User/workspaceStorage"
)

// Adapter implements adapter.Harness for Cursor IDE state.vscdb files.
type Adapter struct{}

var (
	_ adapter.Harness    = Adapter{}
	_ adapter.WatchRoots = Adapter{}
)

// Name is the harness string written into manifests.
func (Adapter) Name() string { return protocol.HarnessCursor }

// Home resolves the Cursor IDE user-data directory.
func (Adapter) Home(getenv func(string) string) (string, error) {
	return Home(getenv)
}

// Home resolves the Cursor IDE user-data directory for this process.
func Home(getenv func(string) string) (string, error) {
	return userDataDir(runtime.GOOS, getenv)
}

// userDataDir is Home with the operating system passed in, so a test
// can name the other two layouts without running there.
func userDataDir(goos string, getenv func(string) string) (string, error) {
	switch goos {
	case "windows":
		if app := getenv("APPDATA"); app != "" {
			return filepath.Join(app, "Cursor"), nil
		}
		home, err := adapter.HomeDir(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Roaming", "Cursor"), nil
	case "darwin":
		home, err := adapter.HomeDir(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Cursor"), nil
	default:
		if v := getenv("XDG_CONFIG_HOME"); v != "" {
			return filepath.Join(v, "Cursor"), nil
		}
		home, err := adapter.HomeDir(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "Cursor"), nil
	}
}

// WatchDir is the global storage directory. WatchDirs also names
// workspace storage. A harness that only asks for one directory still
// sees the global database.
func (Adapter) WatchDir() string { return userGlobal }

// WatchDirs is the two trees that hold state.vscdb. A missing directory
// is an empty tree.
func (Adapter) WatchDirs() []string {
	return []string{userGlobal, userSpaces}
}

// Match reports whether rel, slash-separated from the user-data
// directory, is a state database or one of its WAL sidecars. The
// sidecars are not artifacts. The watcher uses them so a write that
// lands in the WAL is noticed. Discovery lists only state.vscdb.
func (Adapter) Match(rel string) (string, bool) {
	rel = path.Clean(rel)
	if !statePath(rel) {
		return "", false
	}
	return protocol.KindCursorStateJSON, true
}

// statePath reports a global or per-workspace state file, including the
// -wal and -shm sidecars that sit beside it.
func statePath(rel string) bool {
	name := path.Base(rel)
	switch name {
	case dbName, dbName + "-wal", dbName + "-shm":
	default:
		return false
	}
	if rel == userGlobal+"/"+name {
		return true
	}
	parts := strings.Split(rel, "/")
	return len(parts) == 4 && parts[0] == "User" && parts[1] == "workspaceStorage" && parts[3] == name && parts[2] != "" && parts[2] != "." && parts[2] != ".."
}

// Discover lists global and workspace state.vscdb files. WAL sidecars
// are not listed. A missing directory is an empty list.
func (a Adapter) Discover(ctx context.Context, root string) ([]adapter.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []adapter.Ref
	for _, dir := range a.WatchDirs() {
		refs, err := adapter.Walk(root, dir, func(rel string) (string, bool) {
			if path.Base(rel) != dbName {
				return "", false
			}
			return a.Match(rel)
		})
		if err != nil {
			return nil, err
		}
		out = append(out, refs...)
	}
	return out, nil
}

// ReadSlice returns the filtered export when absPath is a state.vscdb.
// A WAL sidecar is refused. Any other path is opened as a file, which
// is how a caller reads an export this package already wrote.
func (Adapter) ReadSlice(ctx context.Context, absPath string, offset int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch filepath.Base(absPath) {
	case dbName + "-wal", dbName + "-shm":
		return nil, fmt.Errorf("cursor: %s is a snapshot sidecar, not an export", filepath.Base(absPath))
	case dbName:
		body, err := exportDatabase(ctx, absPath, "", "")
		if err != nil {
			return nil, err
		}
		if offset < 0 {
			return nil, fmt.Errorf("cursor: negative offset")
		}
		if offset > int64(len(body)) {
			offset = int64(len(body))
		}
		return io.NopCloser(bytes.NewReader(body[offset:])), nil
	default:
		return adapter.OpenSlice(ctx, absPath, offset)
	}
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
	abs     string
}

// Manifests builds one manifest per state.vscdb. The artifact bytes are
// the filtered JSON export. harness_version is Version. The global
// database has an empty project. A workspace project comes from
// workspace.json.
func Manifests(root, machineID string) (adapter.Bundle, error) {
	refs, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		return adapter.Bundle{}, err
	}
	if len(refs) == 0 {
		return adapter.Bundle{Root: root, Paths: map[string]string{}}, nil
	}
	dir, err := os.MkdirTemp("", "lampi-cursor-export-")
	if err != nil {
		return adapter.Bundle{}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	ok := false
	defer func() {
		if !ok {
			cleanup()
		}
	}()

	items := make([]item, 0, len(refs))
	for _, ref := range refs {
		rel := exportRel(ref.RelPath)
		body, err := exportDatabase(context.Background(), ref.AbsPath, ref.RelPath, scopeOf(ref.RelPath))
		if err != nil {
			return adapter.Bundle{}, fmt.Errorf("cursor: %s: %w", ref.RelPath, err)
		}
		out := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return adapter.Bundle{}, err
		}
		if err := os.WriteFile(out, body, 0o600); err != nil {
			return adapter.Bundle{}, err
		}
		sum, err := adapter.HashFile(out)
		if err != nil {
			return adapter.Bundle{}, err
		}
		info, err := os.Stat(out)
		if err != nil {
			return adapter.Bundle{}, err
		}
		ref.Kind = protocol.KindCursorStateJSON
		ref.AbsPath = out
		ref.RelPath = rel
		ref.Size = info.Size()
		items = append(items, item{
			ref:     ref,
			sum:     sum,
			session: sessionID(strings.TrimSuffix(rel, ".json")),
			cwd:     projectCWD(root, rel),
			abs:     out,
		})
	}

	b := adapter.Bundle{Root: root, Paths: map[string]string{}, Cleanup: cleanup}
	for _, it := range items {
		b.Paths[it.ref.RelPath] = it.abs
		b.Manifests = append(b.Manifests, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machineID,
			Harness:         protocol.HarnessCursor,
			HarnessVersion:  Version,
			NativeSessionID: it.session,
			Project:         adapter.ProjectAt(it.cwd),
			Artifacts: []protocol.Artifact{{
				Kind:              it.ref.Kind,
				RelPath:           it.ref.RelPath,
				Size:              it.ref.Size,
				MTime:             it.ref.ModTime.UTC(),
				SHA256:            it.sum,
				ByteWatermarkPrev: 0,
				TailSHA256:        it.sum,
			}},
		})
	}
	ok = true
	return b, nil
}

func exportRel(dbRel string) string {
	return strings.TrimSuffix(dbRel, ".vscdb") + ".json"
}

func scopeOf(dbRel string) string {
	if strings.HasPrefix(dbRel, userSpaces+"/") {
		return "workspace"
	}
	return "global"
}

// sessionID is global for the shared database, and workspace/<id> for
// one workspaceStorage directory. The id is the directory name Cursor
// chose. It is not a hash this package computes.
func sessionID(exportStem string) string {
	if exportStem == "User/globalStorage/state" {
		return "global"
	}
	return "workspace/" + path.Base(path.Dir(exportStem))
}

// projectCWD reads workspace.json beside the database. The global
// database has none. A URI this reader cannot place on the local
// filesystem is an empty cwd.
func projectCWD(root, exportRelPath string) string {
	if !strings.HasPrefix(exportRelPath, userSpaces+"/") {
		return ""
	}
	dbRel := strings.TrimSuffix(exportRelPath, ".json") + ".vscdb"
	return workspaceCWD(runtime.GOOS, filepath.Join(root, filepath.FromSlash(dbRel)))
}

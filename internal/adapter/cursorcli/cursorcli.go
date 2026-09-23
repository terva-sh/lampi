// Package cursorcli is the Cursor CLI harness adapter.
//
// The CLI writes one SQLite database per chat, named store.db. That
// corpus is not the IDE state.vscdb reader in internal/adapter/cursor.
// This package does not open state.vscdb. The IDE package does not
// open store.db. The harness name, the catalog session, and the
// watermark key are all different, so a CLI chat is not treated as
// an IDE session and the two stores are not assumed to match.
//
// This reader is version 1 and its confidence is low. A major Cursor
// CLI upgrade can rename a table or a path, and this pin will not
// follow it.
//
// The live database is never opened and never written. A read copies
// store.db and, when they are present, store.db-wal and store.db-shm
// into a temporary directory, then opens that snapshot read-only.
// The copy is removed after the export is built. Sync uploads the
// export, not the database.
//
// Version 1 requires blobs (id TEXT, data BLOB) and meta (key TEXT,
// value TEXT). A database missing either table is an error. Other
// tables are not read. A meta value that is JSON, or hexadecimal
// that decodes to JSON, is stored as that JSON. Blob bytes are not
// hex-decoded, and protobuf is not parsed. Other values follow the
// IDE reader: JSON stays JSON, other UTF-8 becomes a string, and the
// rest is base64.
//
// A key named cursorAuth, or whose first slash-separated segment is
// cursorAuth, is dropped. The compare is case-insensitive. The same
// rule drops a JSON object key at any depth. Exact names
// accessToken, refreshToken, idToken, sessionToken, the same names
// with underscores, and workosCursorSessionToken are dropped too.
// Those are the credential fields this pin treats as equivalent to
// cursorAuth/*. A secret that sits only inside a string or an opaque
// blob is not pulled out. Ruleset v1 still scans the export.
// auth.json is not a table in store.db and this package does not
// open it.
//
// The config directory is the directory Cursor documents for
// cli-config.json. CURSOR_CONFIG_DIR replaces it. On Linux and other
// Unix systems except macOS, $XDG_CONFIG_HOME/cursor is used when
// that variable is set. Otherwise the directory is ~/.cursor on
// macOS and Linux, and %USERPROFILE%\.cursor on Windows. Chats live
// at chats/<workspace>/<session>/store.db under that directory. The
// workspace directory name is an opaque hash. It is not reversed
// into a path.
//
// Cursor's CLI docs name the config file and the two overrides.
// They do not name store.db. The chats layout, the two tables, a
// hex-encoded meta JSON value, WAL sidecars, and a cwd field on the
// sibling meta.json are what on-disk readers report for Linux, WSL,
// and macOS. This pin reads that layout. Windows chats/ under the
// documented config directory was not checked against a Cursor CLI
// build. That chats follow CURSOR_CONFIG_DIR, rather than only
// XDG_CONFIG_HOME, was not observed separately: the override
// replaces the config directory, and chats are read from it. ACP
// sessions under acp-sessions/ and JSONL under
// projects/*/agent-transcripts/ are different stores and are not
// read. Other Unix systems follow the Linux rule. That was not
// checked against a Cursor CLI build.
//
// The session cwd is the cwd field of the sibling meta.json when
// that value is an absolute path. A missing file, a relative path,
// or a file URI is an empty cwd, and the allowlist refuses the
// export the same way it refuses the global IDE database.
//
// Normalize workers still implement terva only, so a stored Cursor
// CLI manifest records normalize_error.
package cursorcli

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
	// Version is the pinned reader. Bump it when blobs or meta stops
	// meaning what this package reads. A Cursor CLI release that does
	// that breaks this pin on purpose.
	Version = "1"
	// Confidence is low because the schema and the chats path are
	// undocumented and can move between major Cursor CLI versions.
	Confidence = "low"

	dbName   = "store.db"
	chatsRel = "chats"
	metaName = "meta.json"
)

// Adapter implements adapter.Harness for Cursor CLI store.db files.
type Adapter struct{}

var _ adapter.Harness = Adapter{}

// Name is the harness string written into manifests. It is not the
// IDE harness.
func (Adapter) Name() string { return protocol.HarnessCursorCLI }

// Home resolves the Cursor CLI config directory.
func (Adapter) Home(getenv func(string) string) (string, error) {
	return Home(getenv)
}

// Home resolves the Cursor CLI config directory for this process.
func Home(getenv func(string) string) (string, error) {
	return configDir(runtime.GOOS, getenv)
}

// configDir is Home with the operating system passed in, so a test
// can name the other layouts without running there.
func configDir(goos string, getenv func(string) string) (string, error) {
	if v := getenv("CURSOR_CONFIG_DIR"); v != "" {
		return v, nil
	}
	switch goos {
	case "windows":
		if v := getenv("USERPROFILE"); v != "" {
			return filepath.Join(v, ".cursor"), nil
		}
		home, err := adapter.HomeDir(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".cursor"), nil
	case "darwin":
		home, err := adapter.HomeDir(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".cursor"), nil
	default:
		if v := getenv("XDG_CONFIG_HOME"); v != "" {
			return filepath.Join(v, "cursor"), nil
		}
		home, err := adapter.HomeDir(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".cursor"), nil
	}
}

// WatchDir is the chats tree. A missing directory is an empty tree.
func (Adapter) WatchDir() string { return chatsRel }

// Match reports whether rel, slash-separated from the config
// directory, is a store.db or one of its WAL sidecars. The sidecars
// are not artifacts. The watcher uses them so a write that lands in
// the WAL is noticed. Discovery lists only store.db.
func (Adapter) Match(rel string) (string, bool) {
	rel = path.Clean(rel)
	if !storePath(rel) {
		return "", false
	}
	return protocol.KindCursorCLIStoreJSON, true
}

// storePath reports a chat store at chats/<workspace>/<session>/store.db,
// including the -wal and -shm sidecars that sit beside it. A shallower
// or deeper path is not a session this pin knows.
func storePath(rel string) bool {
	name := path.Base(rel)
	switch name {
	case dbName, dbName + "-wal", dbName + "-shm":
	default:
		return false
	}
	parts := strings.Split(rel, "/")
	if len(parts) != 4 || parts[0] != chatsRel {
		return false
	}
	return validSegment(parts[1]) && validSegment(parts[2])
}

func validSegment(s string) bool {
	return s != "" && s != "." && s != ".."
}

// Discover lists store.db files. WAL sidecars are not listed. A
// missing directory is an empty list.
func (Adapter) Discover(ctx context.Context, root string) ([]adapter.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return adapter.Walk(root, chatsRel, func(rel string) (string, bool) {
		if path.Base(rel) != dbName {
			return "", false
		}
		return Adapter{}.Match(rel)
	})
}

// ReadSlice returns the filtered export when absPath is a store.db.
// A WAL sidecar is refused. Any other path is opened as a file, which
// is how a caller reads an export this package already wrote.
func (Adapter) ReadSlice(ctx context.Context, absPath string, offset int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch filepath.Base(absPath) {
	case dbName + "-wal", dbName + "-shm":
		return nil, fmt.Errorf("cursor-cli: %s is a snapshot sidecar, not an export", filepath.Base(absPath))
	case dbName:
		body, err := exportDatabase(ctx, absPath, "")
		if err != nil {
			return nil, err
		}
		if offset < 0 {
			return nil, fmt.Errorf("cursor-cli: negative offset")
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

// Manifests builds one manifest per store.db. The artifact bytes are
// the filtered JSON export. harness_version is Version. The project
// cwd comes from meta.json beside that database. An empty cwd is left
// empty so the allowlist can refuse it.
func Manifests(root, machineID string) (adapter.Bundle, error) {
	refs, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		return adapter.Bundle{}, err
	}
	if len(refs) == 0 {
		return adapter.Bundle{Root: root, Paths: map[string]string{}}, nil
	}
	dir, err := os.MkdirTemp("", "lampi-cursorcli-export-")
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
		body, err := exportDatabase(context.Background(), ref.AbsPath, ref.RelPath)
		if err != nil {
			return adapter.Bundle{}, fmt.Errorf("cursor-cli: %s: %w", ref.RelPath, err)
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
		ref.Kind = protocol.KindCursorCLIStoreJSON
		ref.AbsPath = out
		ref.RelPath = rel
		ref.Size = info.Size()
		items = append(items, item{
			ref:     ref,
			sum:     sum,
			session: sessionID(ref.RelPath),
			cwd:     projectCWD(root, rel),
			abs:     out,
		})
	}

	b := adapter.Bundle{Root: root, Paths: map[string]string{}, Cleanup: cleanup}
	for _, it := range items {
		b.Paths[it.sum] = it.abs
		b.Manifests = append(b.Manifests, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machineID,
			Harness:         protocol.HarnessCursorCLI,
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
	return strings.TrimSuffix(dbRel, ".db") + ".json"
}

// sessionID is the chat directory, chats/<workspace>/<session>. The
// workspace segment is the hash Cursor chose. It is not a project
// path, and it is not an IDE composer id.
func sessionID(exportRelPath string) string {
	return path.Dir(exportRelPath)
}

// projectCWD reads meta.json beside the database. A value that is not
// an absolute local path is an empty cwd.
func projectCWD(root, exportRelPath string) string {
	dbRel := strings.TrimSuffix(exportRelPath, ".json") + ".db"
	return sessionCWD(filepath.Join(root, filepath.FromSlash(dbRel)))
}

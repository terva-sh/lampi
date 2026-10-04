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
// store.db and, when it is present, store.db-wal into a temporary
// directory with sqlitesnap.Take, which copies again when a checkpoint
// tore the copy and runs quick_check, then opens that snapshot
// read-only. The copy is removed after the export is built. Sync
// uploads the export, not the database. With a Permit, as sync passes,
// a chat the allowlist refuses is not snapshotted or exported.
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
// cursorAuth/*. blobEncryptionKey, a key the session record under meta
// key "0" carries, is dropped by the same rule. A secret that sits only inside a string or an opaque
// blob is not pulled out. Ruleset v2 still scans the export.
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
// replaces the config directory, and chats are read from it. Other
// Unix systems follow the Linux rule. That was not checked against a
// Cursor CLI build.
//
// A session an ACP client starts is acp-sessions/<session>/store.db
// under the same directory, with the same tables, hex-encoded meta
// record, WAL sidecars, and sibling meta.json cwd. That layout was read
// off a Linux workstation in October 2026, not from Cursor's docs. A
// directory there with only meta.json is a session the client opened
// and never wrote, and is not listed. JSONL under
// projects/*/agent-transcripts/ is a different store and is not read.
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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/discover"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/redact"
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
	acpRel   = "acp-sessions"
	metaName = "meta.json"

	// SettleAfter is how long the agent waits for a session's store.db
	// and WAL to go unwritten before it exports the session. The export
	// is the whole database on every change, so a session is uploaded
	// once it pauses rather than on every write of an agent turn.
	SettleAfter = 5 * time.Minute
)

// now is the clock the settle check reads. A test sets it.
var now = time.Now

// maxStoreBytes is the largest store.db plus WAL this reader exports.
// The export is built whole in memory, and the upload reads it whole
// again: a 359 MiB store made a 467 MB export and a peak near 2.6 GB.
// A larger session is skipped with a line that says so. A test lowers
// it.
var maxStoreBytes int64 = 256 << 20

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

// WatchDirs is the chats tree and the ACP sessions tree.
func (Adapter) WatchDirs() []string { return []string{chatsRel, acpRel} }

var _ adapter.WatchRoots = Adapter{}

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

// storePath reports a chat store at chats/<workspace>/<session>/store.db
// or an ACP session store at acp-sessions/<session>/store.db, including
// the -wal and -shm sidecars that sit beside it. A shallower or deeper
// path is not a session this pin knows.
func storePath(rel string) bool {
	name := path.Base(rel)
	switch name {
	case dbName, dbName + "-wal", dbName + "-shm":
	default:
		return false
	}
	parts := strings.Split(rel, "/")
	switch {
	case len(parts) == 4 && parts[0] == chatsRel:
		return validSegment(parts[1]) && validSegment(parts[2])
	case len(parts) == 3 && parts[0] == acpRel:
		return validSegment(parts[1])
	default:
		return false
	}
}

func validSegment(s string) bool {
	return s != "" && s != "." && s != ".."
}

// Discover lists store.db files under chats/ and acp-sessions/. WAL
// sidecars are not listed, and an ACP directory that holds only
// meta.json lists nothing. A missing directory is an empty list.
func (Adapter) Discover(ctx context.Context, root string) ([]adapter.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []adapter.Ref
	for _, dir := range (Adapter{}).WatchDirs() {
		refs, err := adapter.Walk(root, dir, func(rel string) (string, bool) {
			if path.Base(rel) != dbName {
				return "", false
			}
			return Adapter{}.Match(rel)
		})
		if err != nil {
			return nil, err
		}
		out = append(out, refs...)
	}
	return out, nil
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

// Manifests builds one manifest per store.db. The artifact bytes are
// the filtered JSON export. harness_version is Version. The project
// cwd comes from meta.json beside that database. An empty cwd is left
// empty so the allowlist can refuse it.
func Manifests(root, machineID string) (adapter.Bundle, error) {
	return ManifestsPermit(root, machineID, nil)
}

// ManifestsPermit is Manifests that asks permit about each session
// before its export is built. A refused session is not snapshotted or
// exported; its manifest is kept with no digest and no path.
func ManifestsPermit(root, machineID string, permit adapter.Permit) (adapter.Bundle, error) {
	return ManifestsMemo(root, machineID, nil, permit, 0)
}

// ManifestsMemo is ManifestsPermit with a memo. A session whose
// store.db and store.db-wal have the stat they had when memo last saw
// them is not snapshotted: its manifest carries the remembered digest
// and size and has no entry in Paths. If the upload still needs its
// bytes, the bundle's Load exports it then. A nil memo exports every
// permitted session.
//
// A permitted session whose store.db or WAL was written within settle
// goes on Held, not Manifests, and is not exported. Zero holds nothing.
func ManifestsMemo(root, machineID string, memo adapter.Memo, permit adapter.Permit, settle time.Duration) (adapter.Bundle, error) {
	ctx := context.Background()
	refs, err := Adapter{}.Discover(ctx, root)
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

	b := adapter.Bundle{Root: root, Paths: map[string]string{}, Hidden: map[string]redact.Result{}, Cuts: map[string][]int64{}, Cleanup: cleanup}
	// lazy is each session left unexported on a memo hit, by export
	// relpath.
	lazy := map[string]adapter.Ref{}
	export := func(ref adapter.Ref) (path string, sum string, size int64, err error) {
		wal := walStat(ref.AbsPath)
		rel := exportRel(ref.RelPath)
		body, cuts, hidden, err := exportScanned(ctx, ref.AbsPath, ref.RelPath)
		if err != nil {
			return "", "", 0, fmt.Errorf("cursor-cli: %s: %w", ref.RelPath, err)
		}
		out := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return "", "", 0, err
		}
		if err := os.WriteFile(out, body, 0o600); err != nil {
			return "", "", 0, err
		}
		sum, err = adapter.HashFile(out)
		if err != nil {
			return "", "", 0, err
		}
		size = int64(len(body))
		b.Paths[rel] = out
		if hidden.Hits > 0 {
			b.Hidden[sum] = hidden
		}
		b.Cuts[sum] = cuts
		remember(memo, ref, wal, sum, size, hidden)
		return out, sum, size, nil
	}
	for _, ref := range refs {
		rel := exportRel(ref.RelPath)
		m := protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machineID,
			Harness:         protocol.HarnessCursorCLI,
			HarnessVersion:  Version,
			NativeSessionID: sessionID(rel),
			Project:         adapter.ProjectAt(projectCWD(root, rel)),
			Artifacts: []protocol.Artifact{{
				Kind:    protocol.KindCursorCLIStoreJSON,
				RelPath: rel,
				MTime:   ref.ModTime.UTC(),
			}},
		}
		if permit != nil && !permit(m) {
			b.Manifests = append(b.Manifests, m)
			continue
		}
		if size := ref.Size + walStat(ref.AbsPath).Size; size > maxStoreBytes {
			b.Skipped = append(b.Skipped, fmt.Errorf("cursor-cli: %s: store.db and WAL are %d MiB, over the %d MiB this reader exports whole", ref.RelPath, size>>20, maxStoreBytes>>20))
			continue
		}
		if until, held := settling(ref, settle); held {
			b.Held = append(b.Held, m)
			if b.HeldUntil.IsZero() || until.Before(b.HeldUntil) {
				b.HeldUntil = until
			}
			continue
		}
		a := &m.Artifacts[0]
		if sum, size, hidden, hit := recall(memo, ref); hit {
			a.Size = size
			a.SHA256 = sum
			a.TailSHA256 = sum
			if hidden.Hits > 0 {
				b.Hidden[sum] = hidden
			}
			lazy[rel] = ref
			b.Manifests = append(b.Manifests, m)
			continue
		}
		_, sum, size, err := export(ref)
		if err != nil {
			return adapter.Bundle{}, err
		}
		a.Size = size
		a.SHA256 = sum
		a.ByteWatermarkPrev = 0
		a.TailSHA256 = sum
		b.Manifests = append(b.Manifests, m)
	}
	if len(lazy) > 0 {
		b.Load = func(rel string) (string, error) {
			if p := b.Paths[rel]; p != "" {
				return p, nil
			}
			ref, ok := lazy[rel]
			if !ok {
				return "", fmt.Errorf("cursor-cli: %s: not a session in this bundle", rel)
			}
			p, _, _, err := export(ref)
			return p, err
		}
	}
	ok = true
	return b, nil
}

// settling reports a session written within settle, and when it may
// next be read. The last write is the later of store.db's mtime and its
// WAL's. A zero settle holds nothing.
func settling(ref adapter.Ref, settle time.Duration) (until time.Time, held bool) {
	if settle <= 0 {
		return time.Time{}, false
	}
	last := ref.ModTime
	if wal := walStat(ref.AbsPath); wal.ModTime.After(last) {
		last = wal.ModTime
	}
	until = last.Add(settle)
	return until, now().Before(until)
}

// memoIdent is what the memo keeps beside a store.db digest: the WAL
// stat the export was taken at, the export size, and what the ruleset
// found in bytes the export holds only in encoded form.
type memoIdent struct {
	WAL    adapter.FileStat `json:"wal"`
	Size   int64            `json:"size"`
	Hidden redact.Result    `json:"hidden"`
}

// walStat is the stat of store.db-wal beside db. A missing WAL is the
// zero stat.
func walStat(db string) adapter.FileStat {
	info, err := os.Stat(db + "-wal")
	if err != nil {
		return adapter.FileStat{}
	}
	return adapter.FileStat{Size: info.Size(), ModTime: info.ModTime().UTC(), Inode: discover.Inode(info)}
}

// recall is the remembered export of ref, when store.db has the stat
// the walk saw and the WAL has the stat it had at that export.
func recall(memo adapter.Memo, ref adapter.Ref) (sum string, size int64, hidden redact.Result, ok bool) {
	if memo == nil {
		return "", 0, redact.Result{}, false
	}
	seen, ok := memo.Recall(ref.AbsPath, ref.Stat())
	if !ok {
		return "", 0, redact.Result{}, false
	}
	var id memoIdent
	if err := json.Unmarshal(seen.Ident, &id); err != nil || seen.SHA256 == "" {
		return "", 0, redact.Result{}, false
	}
	if wal := walStat(ref.AbsPath); wal.Size != id.WAL.Size || wal.Inode != id.WAL.Inode || !wal.ModTime.Equal(id.WAL.ModTime) {
		return "", 0, redact.Result{}, false
	}
	return seen.SHA256, id.Size, id.Hidden, true
}

// remember keeps an export of ref at the store.db stat the walk saw
// and the WAL stat taken before the snapshot. A write after either
// stat is a different stat on the next pass, so it is exported again.
func remember(memo adapter.Memo, ref adapter.Ref, wal adapter.FileStat, sum string, size int64, hidden redact.Result) {
	if memo == nil {
		return
	}
	raw, err := json.Marshal(memoIdent{WAL: wal, Size: size, Hidden: hidden})
	if err != nil {
		return
	}
	memo.Remember(ref.AbsPath, ref.Stat(), adapter.Seen{SHA256: sum, Ident: raw})
}

func exportRel(dbRel string) string {
	return strings.TrimSuffix(dbRel, ".db") + ".json"
}

// sessionID is the session directory: chats/<workspace>/<session> for
// a chat, acp-sessions/<session> for an ACP session. The prefixes keep
// the two apart. The workspace segment is the hash Cursor chose. It is
// not a project path, and it is not an IDE composer id.
func sessionID(exportRelPath string) string {
	return path.Dir(exportRelPath)
}

// projectCWD reads meta.json beside the database. A value that is not
// an absolute local path is an empty cwd.
func projectCWD(root, exportRelPath string) string {
	dbRel := strings.TrimSuffix(exportRelPath, ".json") + ".db"
	return sessionCWD(filepath.Join(root, filepath.FromSlash(dbRel)))
}

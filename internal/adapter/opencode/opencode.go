// Package opencode is the OpenCode harness adapter.
//
// OpenCode stores sessions in a SQLite database in WAL mode. This
// adapter does not open that database and does not read the WAL, SHM,
// or rollback-journal sidecars. Those files are not the ingest path.
//
// The preferred path is a scheduled `opencode export`. The command
// writes one JSON document to stdout, {info, messages}, and does not
// write a file of its own. A schedule redirects that into
// export/**/*.json under the data directory. info.id is the session id.
// info.directory is the project cwd the allowlist already matches.
// The watcher follows export/, the same way Claude follows projects/
// and Codex follows sessions/.
//
// When export/ has no JSON, discovery falls back to a *.db file at the
// data-directory root. That is the file `opencode db path` names for a
// default or channel install (opencode.db, opencode-stable.db, or the
// relative name OPENCODE_DB places there). The file is one raw blob.
// It has no session directory on the outside, so the allowlist refuses
// it: one database holds every project, and an allow rule for one
// directory must not carry the others off the machine. An export names
// one directory, so that is the path that can leave. If any export JSON
// exists, the database file is not in the discover set.
//
// An export is opencode_export_json. It is a snapshot: a re-export is a
// new JSON object, so the lake replaces the head instead of storing a
// divergent copy. The database file is KindDatabase, a local label that
// is not a protocol kind.
//
// The data directory is $XDG_DATA_HOME/opencode when XDG_DATA_HOME is
// set, and ~/.local/share/opencode otherwise (on Windows,
// %USERPROFILE%\.local\share\opencode). That is the xdg-basedir path
// OpenCode uses. There is no OPENCODE_HOME.
//
// Version is the pinned reader. Keys it does not interpret stay on the
// record. Sync uploads the file bytes. Normalize workers project an
// export. A database blob is not an export and records normalize_error.
package opencode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/protocol"
)

// KindDatabase labels a database file found by the fallback. It is not
// a protocol kind and is not a transcript. The allowlist refuses that
// blob before a manifest is sent; the lake does not project it.
const KindDatabase = "opencode_db"

// errNotObject is an export that is not a JSON object. A schedule can
// be interrupted mid-write. Discovery still lists the file; the session
// id then falls back to the relative path.
var errNotObject = errors.New("opencode: export is not a JSON object")

// Adapter implements adapter.Harness for a scheduled opencode export.
type Adapter struct{}

var _ adapter.Harness = Adapter{}

// Name is the harness string written into manifests.
func (Adapter) Name() string { return protocol.HarnessOpenCode }

// Home resolves the OpenCode data directory.
func (Adapter) Home(getenv func(string) string) (string, error) {
	return Home(getenv)
}

// Home resolves the OpenCode data directory. XDG_DATA_HOME/opencode
// wins. Otherwise the directory is ~/.local/share/opencode.
func Home(getenv func(string) string) (string, error) {
	if v := getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "opencode"), nil
	}
	home, err := adapter.HomeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "opencode"), nil
}

// WatchDir is the directory under the data dir that holds export JSON.
// The database file sits beside this directory, not inside it, and is
// not watched: growth of the live database is not the read path.
func (Adapter) WatchDir() string { return "export" }

// Match reports whether rel, slash-separated from the data directory,
// is an export document. The glob is export/**/*.json. Dotfiles are
// skipped. A database file and its WAL sidecars do not match: Discover
// considers the database only when this glob is empty.
func (Adapter) Match(rel string) (string, bool) {
	rel = path.Clean(rel)
	if rel == "." || !strings.HasPrefix(rel, "export/") {
		return "", false
	}
	name := path.Base(rel)
	if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
		return "", false
	}
	return protocol.KindOpenCodeExportJSON, true
}

// Discover lists export JSON. When that list is empty, it lists database
// files at the data-directory root. WAL sidecars are never listed. A
// missing data directory is an empty list.
func (a Adapter) Discover(ctx context.Context, root string) ([]adapter.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	exports, err := adapter.Walk(root, a.WatchDir(), a.Match)
	if err != nil {
		return nil, err
	}
	if len(exports) > 0 {
		return exports, nil
	}
	return discoverDB(root)
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

// Manifests builds one manifest per export session id. A file with no
// info.id uses its relative path. A database fallback uses the file
// name and an empty project: the allowlist then refuses it. When export
// JSON exists, the database is not a manifest. Exports that share a
// session id are ordered oldest mtime first, so the newest one is the
// last artifact and the lake makes it the head. harness_version is Version.
func Manifests(root, machineID string) (adapter.Bundle, error) {
	refs, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		return adapter.Bundle{}, err
	}
	items := make([]item, 0, len(refs))
	for _, ref := range refs {
		session, cwd, err := readIdentity(ref)
		if err != nil {
			return adapter.Bundle{}, fmt.Errorf("opencode: %s: %w", ref.RelPath, err)
		}
		if session == "" {
			session = strings.TrimSuffix(ref.RelPath, path.Ext(ref.RelPath))
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
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].ref.ModTime.Before(group[j].ref.ModTime)
		})
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
		b.Manifests = append(b.Manifests, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machineID,
			Harness:         protocol.HarnessOpenCode,
			HarnessVersion:  Version,
			NativeSessionID: id,
			Project:         adapter.ProjectAt(cwd),
			Artifacts:       arts,
		})
	}
	return b, nil
}

func readIdentity(ref adapter.Ref) (sessionID, cwd string, err error) {
	if isDatabase(path.Base(ref.RelPath)) && !strings.Contains(ref.RelPath, "/") {
		// The file name is the session id. There is no info.id on a
		// database blob, and trimming the suffix would collide with an
		// export whose id is the bare stem.
		return ref.RelPath, "", nil
	}
	f, err := os.Open(ref.AbsPath)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		return "", "", err
	}
	rec, err := ParseExport(body)
	if err != nil {
		return "", "", nil
	}
	return rec.Info.ID, rec.Info.Directory, nil
}

// discoverDB lists database files at the data-directory root. A missing
// directory is an empty list. Sidecars are not database files.
func discoverDB(root string) ([]adapter.Ref, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []adapter.Ref
	for _, e := range entries {
		if e.IsDir() || !isDatabase(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, adapter.Ref{
			Kind:    KindDatabase,
			AbsPath: filepath.Join(root, e.Name()),
			RelPath: e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
		})
	}
	return out, nil
}

// isDatabase reports a SQLite database file name at the data-directory
// root. The WAL, SHM, and journal sidecars end in -wal, -shm, and
// -journal, not .db, so this suffix check leaves them out. A leading
// dot is a hidden file and is left out too.
func isDatabase(name string) bool {
	return !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".db")
}

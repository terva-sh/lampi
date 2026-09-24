package cursor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// document is the filtered export. It is internal to this package.
// harness_version is Version. Keys this reader does not interpret stay
// on the row.
type document struct {
	HarnessVersion string `json:"harness_version"`
	Confidence     string `json:"confidence"`
	Source         string `json:"source"`
	Scope          string `json:"scope"`
	ItemTable      []row  `json:"item_table"`
	CursorDiskKV   *[]row `json:"cursor_disk_kv,omitempty"`
}

type row struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// exportDatabase copies the WAL trio, opens the snapshot, and returns
// the filtered JSON. src is the live state.vscdb. sourceRel and scope
// are written into the document. An empty sourceRel is used by ReadSlice,
// which does not have a home-relative path; the base name is recorded
// instead.
func exportDatabase(ctx context.Context, src, sourceRel, scope string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snap, err := copyTrio(src)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(snap)

	db, err := openSnapshot(ctx, filepath.Join(snap, filepath.Base(src)))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	ok, err := tableExists(ctx, db, "ItemTable")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("cursor: reader %s: no ItemTable", Version)
	}
	items, err := readKV(ctx, db, `SELECT key, value FROM ItemTable ORDER BY key`)
	if err != nil {
		return nil, err
	}
	doc := document{
		HarnessVersion: Version,
		Confidence:     Confidence,
		Source:         sourceRel,
		Scope:          scope,
		ItemTable:      items,
	}
	if sourceRel == "" {
		doc.Source = filepath.Base(src)
	}
	if scope == "" {
		doc.Scope = "unknown"
	}
	disk, err := tableExists(ctx, db, "cursorDiskKV")
	if err != nil {
		return nil, err
	}
	if disk {
		kv, err := readKV(ctx, db, `SELECT key, value FROM cursorDiskKV ORDER BY key`)
		if err != nil {
			return nil, err
		}
		doc.CursorDiskKV = &kv
	}
	// Close before the deferred RemoveAll so the snapshot files are not
	// still mapped when the directory goes away. The global snapshot
	// below opens its own copy.
	if err := db.Close(); err != nil {
		return nil, err
	}
	if err := mergeWorkspaceComposers(ctx, src, &doc); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// composerHeadersKey is the ItemTable registry that names the composers
// for one workspace. The normalize fixtures and the worker's cursor
// document use this key, with allComposers[].composerId.
// composer.composerData is the older workspace list and is not read
// here.
const composerHeadersKey = "composer.composerHeaders"

// mergeWorkspaceComposers copies the global state.vscdb and appends
// cursorDiskKV rows for composers this workspace's composer.composerHeaders
// names. A path that is not a workspace database is left alone. A
// missing global file adds nothing. The live global file is not opened.
func mergeWorkspaceComposers(ctx context.Context, src string, doc *document) error {
	if _, ok := workspaceStorageID(src); !ok {
		return nil
	}
	ids := composerIDs(doc.ItemTable)
	if len(ids) == 0 {
		return nil
	}
	rows, err := snapshotDisk(ctx, globalDBBeside(src))
	if err != nil {
		return fmt.Errorf("cursor: global snapshot: %w", err)
	}
	doc.CursorDiskKV = mergeDisk(doc.CursorDiskKV, filterDisk(rows, ids))
	return nil
}

// workspaceStorageID reports the directory name under
// User/workspaceStorage when dbPath is that workspace's state.vscdb.
func workspaceStorageID(dbPath string) (string, bool) {
	if filepath.Base(dbPath) != dbName {
		return "", false
	}
	idDir := filepath.Dir(dbPath)
	if filepath.Base(filepath.Dir(idDir)) != "workspaceStorage" {
		return "", false
	}
	if filepath.Base(filepath.Dir(filepath.Dir(idDir))) != "User" {
		return "", false
	}
	id := filepath.Base(idDir)
	if id == "" || id == "." || id == ".." {
		return "", false
	}
	return id, true
}

// globalDBBeside is User/globalStorage/state.vscdb next to a workspace
// database. workspaceStorageID must already have accepted dbPath.
func globalDBBeside(workspaceDB string) string {
	idDir := filepath.Dir(workspaceDB)
	user := filepath.Dir(filepath.Dir(idDir))
	return filepath.Join(user, "globalStorage", dbName)
}

// composerIDs reads composer.composerHeaders. A composer named only on
// composer.composerData is not included.
func composerIDs(items []row) map[string]struct{} {
	ids := map[string]struct{}{}
	for _, item := range items {
		if item.Key != composerHeadersKey {
			continue
		}
		var doc struct {
			AllComposers []struct {
				ComposerID string `json:"composerId"`
			} `json:"allComposers"`
		}
		if err := json.Unmarshal(item.Value, &doc); err != nil {
			continue
		}
		for _, c := range doc.AllComposers {
			if c.ComposerID != "" {
				ids[c.ComposerID] = struct{}{}
			}
		}
	}
	return ids
}

// snapshotDisk copies src and returns its cursorDiskKV rows. A missing
// file or a database without that table is an empty slice. cursorAuth
// keys are already dropped by readKV.
func snapshotDisk(ctx context.Context, src string) ([]row, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	snap, err := copyTrio(src)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(snap)

	db, err := openSnapshot(ctx, filepath.Join(snap, filepath.Base(src)))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ok, err := tableExists(ctx, db, "cursorDiskKV")
	if err != nil || !ok {
		return nil, err
	}
	return readKV(ctx, db, `SELECT key, value FROM cursorDiskKV ORDER BY key`)
}

// filterDisk keeps rows whose composer id is in ids. The id is the
// segment the existing bubble and composerData keys use. A longer id
// that only starts with a selected id does not match. Keys this reader
// does not treat as one composer's row stay on the global database.
func filterDisk(rows []row, ids map[string]struct{}) []row {
	if len(ids) == 0 || len(rows) == 0 {
		return nil
	}
	out := make([]row, 0)
	for _, item := range rows {
		id, ok := diskRowComposer(item.Key)
		if !ok {
			continue
		}
		if _, want := ids[id]; !want {
			continue
		}
		out = append(out, item)
	}
	return out
}

func diskRowComposer(key string) (string, bool) {
	switch {
	case strings.HasPrefix(key, "composerData:"):
		id := strings.TrimPrefix(key, "composerData:")
		if id == "" || strings.Contains(id, ":") {
			return "", false
		}
		return id, true
	case strings.HasPrefix(key, "bubbleId:"):
		return composerSegment(key, "bubbleId:")
	case strings.HasPrefix(key, "checkpointId:"):
		return composerSegment(key, "checkpointId:")
	case strings.HasPrefix(key, "messageRequestContext:"):
		return composerSegment(key, "messageRequestContext:")
	case strings.HasPrefix(key, "codeBlockDiff:"):
		return composerSegment(key, "codeBlockDiff:")
	default:
		return "", false
	}
}

func composerSegment(key, prefix string) (string, bool) {
	rest := strings.TrimPrefix(key, prefix)
	id, tail, ok := strings.Cut(rest, ":")
	if !ok || id == "" || tail == "" {
		return "", false
	}
	return id, true
}

// mergeDisk appends extra onto base. A key already on the workspace
// document stays. The result is ordered by key, matching readKV.
func mergeDisk(base *[]row, extra []row) *[]row {
	if len(extra) == 0 {
		return base
	}
	seen := map[string]bool{}
	out := []row{}
	if base != nil {
		out = append(out, (*base)...)
		for _, item := range *base {
			seen[item.Key] = true
		}
	}
	for _, item := range extra {
		if seen[item.Key] {
			continue
		}
		seen[item.Key] = true
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return &out
}

// copyTrio copies state.vscdb and the sidecars that exist beside it.
// The copy is the only database this package opens. A missing sidecar
// is left absent. The main file is required.
func copyTrio(src string) (string, error) {
	dir, err := os.MkdirTemp("", "lampi-cursor-snap-")
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	base := filepath.Base(src)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		from := src + suffix
		_, err := os.Stat(from)
		if errors.Is(err, os.ErrNotExist) {
			if suffix == "" {
				return "", err
			}
			continue
		}
		if err != nil {
			return "", err
		}
		if err := copyFile(from, filepath.Join(dir, base+suffix)); err != nil {
			return "", err
		}
	}
	ok = true
	return dir, nil
}

// copyFile reads from and writes to. The source is opened read-only.
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// openSnapshot opens a copied database read-only. The path is inside
// the snapshot directory, never the live file. If the copied shm stops
// the open, it is removed from the snapshot and the open is tried once
// more. SQLite then rebuilds the index in the snapshot. The live shm
// is not touched.
func openSnapshot(ctx context.Context, path string) (*sql.DB, error) {
	db, err := openRO(ctx, path)
	if err == nil {
		return db, nil
	}
	shm := path + "-shm"
	if _, statErr := os.Stat(shm); statErr != nil {
		return nil, err
	}
	if rmErr := os.Remove(shm); rmErr != nil {
		return nil, err
	}
	return openRO(ctx, path)
}

func openRO(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteROURI(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// sqliteROURI is a read-only file URI. mode=ro refuses a write. The
// WAL copied beside the database is applied. immutable is not set:
// that flag tells SQLite to ignore the WAL.
func sqliteROURI(path string) string {
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	u := &url.URL{Scheme: "file", Path: slash, RawQuery: "mode=ro"}
	return u.String()
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var got string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func readKV(ctx context.Context, db *sql.DB, query string) ([]row, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []row{}
	for rows.Next() {
		var key string
		var val []byte
		if err := rows.Scan(&key, &val); err != nil {
			return nil, err
		}
		if excludedKey(key) {
			continue
		}
		out = append(out, row{Key: key, Value: encodeValue(val)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// excludedKey reports a cursorAuth key. The match is the whole key, or
// a key whose first slash-separated segment is cursorAuth. The segment
// compare is case-insensitive so CursorAuth/accessToken does not slip
// through a pin that names cursorAuth/*.
func excludedKey(key string) bool {
	if strings.EqualFold(key, "cursorAuth") {
		return true
	}
	head, _, ok := strings.Cut(key, "/")
	return ok && strings.EqualFold(head, "cursorAuth")
}

// encodeValue keeps a JSON value as JSON. Other UTF-8 bytes become a
// JSON string. Anything else is base64. The key is not interpreted.
func encodeValue(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	if json.Valid(b) {
		var buf bytes.Buffer
		if err := json.Compact(&buf, b); err == nil {
			return buf.Bytes()
		}
	}
	if utf8.Valid(b) {
		raw, err := json.Marshal(string(b))
		if err == nil {
			return raw
		}
	}
	raw, err := json.Marshal(struct {
		Base64 string `json:"base64"`
	}{Base64: base64.StdEncoding.EncodeToString(b)})
	if err != nil {
		return json.RawMessage("null")
	}
	return raw
}

// workspaceCWD reads the sibling workspace.json. folder is a directory
// URI. workspace is a .code-workspace file, and the cwd is the directory
// that contains it. A URI that is not a local file is empty.
func workspaceCWD(goos, dbPath string) string {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(dbPath), "workspace.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		Folder    string `json:"folder"`
		Workspace string `json:"workspace"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	uri := doc.Folder
	asFile := false
	if uri == "" {
		uri = doc.Workspace
		asFile = true
	}
	p, ok := localPath(goos, uri)
	if !ok {
		return ""
	}
	if asFile {
		return filepath.Dir(p)
	}
	return p
}

func localPath(goos, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if p, ok := fileURIPath(goos, raw); ok {
		return p, true
	}
	if filepath.IsAbs(raw) {
		return raw, true
	}
	return "", false
}

func fileURIPath(goos, raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return "", false
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "", false
	}
	p := u.Path
	if strings.Contains(p, "%") {
		if dec, err := url.PathUnescape(p); err == nil {
			p = dec
		}
	}
	if goos == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	if p == "" {
		return "", false
	}
	if goos == "windows" {
		p = filepath.FromSlash(p)
	}
	return p, true
}

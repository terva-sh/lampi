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
	// still mapped when the directory goes away.
	if err := db.Close(); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
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

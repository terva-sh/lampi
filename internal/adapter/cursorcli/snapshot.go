package cursorcli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
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
// harness_version is Version. Rows this reader does not interpret stay
// on the document.
type document struct {
	HarnessVersion string    `json:"harness_version"`
	Confidence     string    `json:"confidence"`
	Source         string    `json:"source"`
	Scope          string    `json:"scope"`
	Meta           []metaRow `json:"meta"`
	Blobs          []blobRow `json:"blobs"`
}

type metaRow struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type blobRow struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

// exportDatabase copies the WAL trio, opens the snapshot, and returns
// the filtered JSON. src is the live store.db. sourceRel is written
// into the document. An empty sourceRel is used by ReadSlice, which
// does not have a home-relative path; the base name is recorded
// instead.
func exportDatabase(ctx context.Context, src, sourceRel string) ([]byte, error) {
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

	for _, table := range []string{"blobs", "meta"} {
		ok, err := tableExists(ctx, db, table)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("cursor-cli: reader %s: no %s table", Version, table)
		}
	}
	meta, err := readMeta(ctx, db)
	if err != nil {
		return nil, err
	}
	blobs, err := readBlobs(ctx, db)
	if err != nil {
		return nil, err
	}
	doc := document{
		HarnessVersion: Version,
		Confidence:     Confidence,
		Source:         sourceRel,
		Scope:          "session",
		Meta:           meta,
		Blobs:          blobs,
	}
	if sourceRel == "" {
		doc.Source = filepath.Base(src)
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

func readMeta(ctx context.Context, db *sql.DB) ([]metaRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM meta ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []metaRow{}
	for rows.Next() {
		var key string
		var val sql.NullString
		if err := rows.Scan(&key, &val); err != nil {
			return nil, err
		}
		if excludedKey(key) {
			continue
		}
		raw := ""
		if val.Valid {
			raw = val.String
		}
		out = append(out, metaRow{Key: key, Value: presentMeta(raw)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func readBlobs(ctx context.Context, db *sql.DB) ([]blobRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, data FROM blobs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []blobRow{}
	for rows.Next() {
		var id string
		var val []byte
		if err := rows.Scan(&id, &val); err != nil {
			return nil, err
		}
		if excludedKey(id) {
			continue
		}
		out = append(out, blobRow{ID: id, Data: presentBlob(val)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// presentMeta decodes a meta value. JSON is kept. Hexadecimal text
// that decodes to JSON is kept as that JSON, which is how current
// CLI builds store the session record under meta key "0". Anything
// else is encoded as text. Credential keys inside a JSON object are
// removed before the value is returned.
func presentMeta(s string) json.RawMessage {
	trimmed := strings.TrimSpace(s)
	if scrubbed, ok := scrubIfJSON([]byte(trimmed)); ok {
		return scrubbed
	}
	if decoded, err := hex.DecodeString(trimmed); err == nil {
		if scrubbed, ok := scrubIfJSON(decoded); ok {
			return scrubbed
		}
	}
	return encodeValue([]byte(s))
}

// presentBlob encodes blob bytes. They are not hex-decoded. JSON
// objects still lose credential keys.
func presentBlob(b []byte) json.RawMessage {
	encoded := encodeValue(b)
	if scrubbed, ok := scrubIfJSON(encoded); ok {
		return scrubbed
	}
	return encoded
}

func scrubIfJSON(raw []byte) (json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, false
	}
	var buf bytes.Buffer
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := scrubTokens(dec, &buf); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

func scrubTokens(dec *json.Decoder, w *bytes.Buffer) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return scrubObject(dec, w)
		case '[':
			return scrubArray(dec, w)
		default:
			return fmt.Errorf("cursor-cli: unexpected json delim")
		}
	case string:
		writeString(w, t)
		return nil
	case json.Number:
		w.WriteString(t.String())
		return nil
	case bool:
		if t {
			w.WriteString("true")
		} else {
			w.WriteString("false")
		}
		return nil
	case nil:
		w.WriteString("null")
		return nil
	default:
		return fmt.Errorf("cursor-cli: unexpected json token")
	}
}

func scrubObject(dec *json.Decoder, w *bytes.Buffer) error {
	w.WriteByte('{')
	first := true
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("cursor-cli: json object key is not a string")
		}
		if excludedKey(key) {
			if err := skipValue(dec); err != nil {
				return err
			}
			continue
		}
		if !first {
			w.WriteByte(',')
		}
		first = false
		writeString(w, key)
		w.WriteByte(':')
		if err := scrubTokens(dec, w); err != nil {
			return err
		}
	}
	end, err := dec.Token()
	if err != nil {
		return err
	}
	if end != json.Delim('}') {
		return fmt.Errorf("cursor-cli: json object did not close")
	}
	w.WriteByte('}')
	return nil
}

func scrubArray(dec *json.Decoder, w *bytes.Buffer) error {
	w.WriteByte('[')
	first := true
	for dec.More() {
		if !first {
			w.WriteByte(',')
		}
		first = false
		if err := scrubTokens(dec, w); err != nil {
			return err
		}
	}
	end, err := dec.Token()
	if err != nil {
		return err
	}
	if end != json.Delim(']') {
		return fmt.Errorf("cursor-cli: json array did not close")
	}
	w.WriteByte(']')
	return nil
}

func skipValue(dec *json.Decoder) error {
	var discard bytes.Buffer
	return scrubTokens(dec, &discard)
}

func writeString(w *bytes.Buffer, s string) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	w.Write(bytes.TrimRight(buf.Bytes(), "\n"))
}

// excludedKey reports a credential key. cursorAuth matches the whole
// key or the first slash-separated segment, ignoring case. The other
// names are exact credential fields, also ignoring case.
func excludedKey(key string) bool {
	if strings.EqualFold(key, "cursorAuth") {
		return true
	}
	head, _, ok := strings.Cut(key, "/")
	if ok && strings.EqualFold(head, "cursorAuth") {
		return true
	}
	switch strings.ToLower(key) {
	case "accesstoken", "refreshtoken", "idtoken", "sessiontoken",
		"access_token", "refresh_token", "id_token", "session_token",
		"workoscursorsessiontoken":
		return true
	default:
		return false
	}
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

// copyTrio copies store.db and the sidecars that exist beside it.
// The copy is the only database this package opens. A missing sidecar
// is left absent. The main file is required.
func copyTrio(src string) (string, error) {
	dir, err := os.MkdirTemp("", "lampi-cursorcli-snap-")
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

// sessionCWD reads the sibling meta.json. cwd is used only when it is
// an absolute path. A file URI, a relative path, and a missing file
// are empty. The workspace directory name is not consulted.
func sessionCWD(dbPath string) string {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(dbPath), metaName))
	if err != nil {
		return ""
	}
	var doc struct {
		CWD string `json:"cwd"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	cwd := strings.TrimSpace(doc.CWD)
	if cwd == "" || !filepath.IsAbs(cwd) {
		return ""
	}
	return cwd
}

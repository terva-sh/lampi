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
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"terva.sh/lampi/internal/adapter/sqlitesnap"
	"terva.sh/lampi/internal/redact"
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

// metaRow and blobRow carry hidden, the ruleset's scan of bytes the
// export holds only in encoded form. It is not part of the export.
type metaRow struct {
	Key    string          `json:"key"`
	Value  json.RawMessage `json:"value"`
	hidden redact.Result
}

type blobRow struct {
	ID     string          `json:"id"`
	Data   json.RawMessage `json:"data"`
	hidden redact.Result
}

// hidden adds up the rows' scans of encoded bytes.
func (d document) hidden() redact.Result {
	var out redact.Result
	for _, r := range d.Meta {
		out = out.Add(r.hidden)
	}
	for _, r := range d.Blobs {
		out = out.Add(r.hidden)
	}
	return out
}

// takeSnapshot is sqlitesnap.Take. A test swaps it to count copies.
var takeSnapshot = sqlitesnap.Take

// scanner is ruleset v2. A test swaps it for one that fails.
var scanner redact.Redactor = redact.Ruleset{}

// exportDatabase snapshots src and returns the filtered JSON. src is the live store.db. sourceRel is written
// into the document. An empty sourceRel is used by ReadSlice, which
// does not have a home-relative path; the base name is recorded
// instead.
func exportDatabase(ctx context.Context, src, sourceRel string) ([]byte, error) {
	body, _, err := exportScanned(ctx, src, sourceRel)
	return body, err
}

// exportScanned is exportDatabase plus the ruleset's scan of the raw
// values the export holds as base64 or hex text, which a scan of the
// JSON cannot read.
func exportScanned(ctx context.Context, src, sourceRel string) ([]byte, redact.Result, error) {
	doc, err := exportDocument(ctx, src, sourceRel)
	if err != nil {
		return nil, redact.Result{}, err
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, redact.Result{}, err
	}
	return body, doc.hidden(), nil
}

func exportDocument(ctx context.Context, src, sourceRel string) (document, error) {
	if err := ctx.Err(); err != nil {
		return document{}, err
	}
	snap, err := takeSnapshot(ctx, src, "lampi-cursorcli-snap-")
	if err != nil {
		return document{}, err
	}
	defer snap.Close()
	db := snap.DB

	for _, table := range []string{"blobs", "meta"} {
		ok, err := tableExists(ctx, db, table)
		if err != nil {
			return document{}, err
		}
		if !ok {
			return document{}, fmt.Errorf("cursor-cli: reader %s: no %s table", Version, table)
		}
	}
	meta, err := readMeta(ctx, db)
	if err != nil {
		return document{}, err
	}
	blobs, err := readBlobs(ctx, db)
	if err != nil {
		return document{}, err
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
	return doc, nil
}

// readMeta reads the meta table. A value the ruleset cannot scan fails
// the read with an error that names its key.
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
		value, hidden, err := presentMeta(raw)
		if err != nil {
			return nil, fmt.Errorf("cursor-cli: meta key %q: %w", key, err)
		}
		out = append(out, metaRow{Key: key, Value: value, hidden: hidden})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// readBlobs reads the blobs table. A blob the ruleset cannot scan
// fails the read with an error that names its id.
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
		data, hidden, err := presentBlob(val)
		if err != nil {
			return nil, fmt.Errorf("cursor-cli: blob id %q: %w", id, err)
		}
		out = append(out, blobRow{ID: id, Data: data, hidden: hidden})
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
// removed before the value is returned. Hexadecimal text that does not
// decode to JSON stays hex, so the Result scans the decoded bytes too.
// A scan that fails is an error, so the value is never exported
// unscanned.
func presentMeta(s string) (json.RawMessage, redact.Result, error) {
	trimmed := strings.TrimSpace(s)
	if scrubbed, ok := scrubIfJSON([]byte(trimmed)); ok {
		return scrubbed, redact.Result{}, nil
	}
	var hidden redact.Result
	if decoded, err := hex.DecodeString(trimmed); err == nil {
		if scrubbed, ok := scrubIfJSON(decoded); ok {
			return scrubbed, redact.Result{}, nil
		}
		scan, err := scanner.Scan(decoded)
		if err != nil {
			return nil, redact.Result{}, fmt.Errorf("ruleset %s scan: %w", redact.RulesetV2, err)
		}
		hidden = scan
	}
	encoded, inner, err := encodeValue([]byte(s))
	if err != nil {
		return nil, redact.Result{}, err
	}
	return encoded, hidden.Add(inner), nil
}

// presentBlob encodes blob bytes. They are not hex-decoded. JSON
// objects still lose credential keys.
func presentBlob(b []byte) (json.RawMessage, redact.Result, error) {
	encoded, hidden, err := encodeValue(b)
	if err != nil {
		return nil, redact.Result{}, err
	}
	if scrubbed, ok := scrubIfJSON(encoded); ok {
		return scrubbed, hidden, nil
	}
	return encoded, hidden, nil
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
// The Result is the ruleset's scan of b when b became base64, which a
// scan of the export cannot read. It is zero otherwise. A scan that
// fails is an error, so the value is never exported unscanned.
func encodeValue(b []byte) (json.RawMessage, redact.Result, error) {
	if len(b) == 0 {
		return json.RawMessage("null"), redact.Result{}, nil
	}
	if json.Valid(b) {
		var buf bytes.Buffer
		if err := json.Compact(&buf, b); err == nil {
			return buf.Bytes(), redact.Result{}, nil
		}
	}
	if utf8.Valid(b) {
		raw, err := json.Marshal(string(b))
		if err == nil {
			return raw, redact.Result{}, nil
		}
	}
	hidden, err := scanner.Scan(b)
	if err != nil {
		return nil, redact.Result{}, fmt.Errorf("ruleset %s scan: %w", redact.RulesetV2, err)
	}
	raw, err := json.Marshal(struct {
		Base64 string `json:"base64"`
	}{Base64: base64.StdEncoding.EncodeToString(b)})
	if err != nil {
		return nil, redact.Result{}, err
	}
	return raw, hidden, nil
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

package cursor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"terva.sh/lampi/internal/adapter/sqlitesnap"
	"terva.sh/lampi/internal/redact"
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
	// hidden is the ruleset's scan of a value exported as base64. It
	// is not part of the export.
	hidden redact.Result
}

// hidden adds up the base64 scans of every row the document keeps.
// Rows dropped by the composer filter do not count.
func (d document) hidden() redact.Result {
	var out redact.Result
	for _, r := range d.ItemTable {
		out = out.Add(r.hidden)
	}
	if d.CursorDiskKV != nil {
		for _, r := range *d.CursorDiskKV {
			out = out.Add(r.hidden)
		}
	}
	return out
}

// takeSnapshot is sqlitesnap.Take. A test swaps it to count copies.
var takeSnapshot = sqlitesnap.Take

// scanner is ruleset v2. A test swaps it for one that fails.
var scanner redact.Redactor = redact.Ruleset{}

const snapPrefix = "lampi-cursor-snap-"

// globalSource is the global state.vscdb one Manifests call, or one
// ReadSlice, shares. The snapshot is taken on first use and at most
// once. A missing file is a nil snapshot and no error.
type globalSource struct {
	src   string
	taken bool
	snap  *sqlitesnap.Snapshot
	err   error
}

func (g *globalSource) get(ctx context.Context) (*sqlitesnap.Snapshot, error) {
	if g.taken {
		return g.snap, g.err
	}
	g.taken = true
	g.snap, g.err = takeSnapshot(ctx, g.src, snapPrefix)
	if errors.Is(g.err, os.ErrNotExist) {
		g.snap, g.err = nil, nil
	}
	return g.snap, g.err
}

// close removes the snapshot. A nil source and a second call are safe.
func (g *globalSource) close() {
	if g != nil {
		_ = g.snap.Close()
	}
}

// globalFor is the global database a read of src shares: the one beside
// a workspace database, or src itself.
func globalFor(src string) *globalSource {
	if _, ok := workspaceStorageID(src); ok {
		return &globalSource{src: globalDBBeside(src)}
	}
	return &globalSource{src: src}
}

// exportDatabase snapshots src and returns the filtered JSON. src is
// the live state.vscdb. sourceRel and scope are written into the
// document. An empty sourceRel is used by ReadSlice, which does not
// have a home-relative path; the base name is recorded instead.
func exportDatabase(ctx context.Context, src, sourceRel, scope string) ([]byte, error) {
	g := globalFor(src)
	defer g.close()
	body, _, err := exportScanned(ctx, src, sourceRel, scope, g)
	return body, err
}

// exportScanned is exportDatabase plus the ruleset's scan of the raw
// values the export holds as base64, which a scan of the JSON cannot
// read. g is the global database the export shares.
func exportScanned(ctx context.Context, src, sourceRel, scope string, g *globalSource) ([]byte, redact.Result, error) {
	doc, err := exportDocument(ctx, src, sourceRel, scope, g)
	if err != nil {
		return nil, redact.Result{}, err
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, redact.Result{}, err
	}
	return body, doc.hidden(), nil
}

// exportDocument reads src from a snapshot. When src is the global
// database, that snapshot is g's, so the global export and the
// workspace merges share one copy.
func exportDocument(ctx context.Context, src, sourceRel, scope string, g *globalSource) (document, error) {
	if err := ctx.Err(); err != nil {
		return document{}, err
	}
	var db *sql.DB
	if filepath.Clean(g.src) == filepath.Clean(src) {
		snap, err := g.get(ctx)
		if err != nil {
			return document{}, err
		}
		if snap == nil {
			return document{}, fmt.Errorf("cursor: %s: %w", filepath.Base(src), os.ErrNotExist)
		}
		db = snap.DB
	} else {
		snap, err := takeSnapshot(ctx, src, snapPrefix)
		if err != nil {
			return document{}, err
		}
		defer snap.Close()
		db = snap.DB
	}

	ok, err := tableExists(ctx, db, "ItemTable")
	if err != nil {
		return document{}, err
	}
	if !ok {
		return document{}, fmt.Errorf("cursor: reader %s: no ItemTable", Version)
	}
	items, err := readKV(ctx, db, `SELECT key, value FROM ItemTable ORDER BY key`)
	if err != nil {
		return document{}, err
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
		return document{}, err
	}
	if disk {
		kv, err := readKV(ctx, db, `SELECT key, value FROM cursorDiskKV ORDER BY key`)
		if err != nil {
			return document{}, err
		}
		doc.CursorDiskKV = &kv
	}
	if err := mergeWorkspaceComposers(ctx, src, &doc, g); err != nil {
		return document{}, err
	}
	return doc, nil
}

// composerHeadersKey is the ItemTable registry that names the composers
// for one workspace. The normalize fixtures and the worker's cursor
// document use this key, with allComposers[].composerId.
// composer.composerData is the older workspace list and is not read
// here.
const composerHeadersKey = "composer.composerHeaders"

// mergeWorkspaceComposers appends the global cursorDiskKV rows for
// composers this workspace's composer.composerHeaders names. The rows
// come from g, the shared global snapshot, filtered in SQL. A path that
// is not a workspace database is left alone. A missing global file adds
// nothing.
func mergeWorkspaceComposers(ctx context.Context, src string, doc *document, g *globalSource) error {
	if _, ok := workspaceStorageID(src); !ok {
		return nil
	}
	ids := composerIDs(doc.ItemTable)
	if len(ids) == 0 {
		return nil
	}
	snap, err := g.get(ctx)
	if err != nil {
		return fmt.Errorf("cursor: global snapshot: %w", err)
	}
	if snap == nil {
		return nil
	}
	rows, err := composerRows(ctx, snap.DB, ids)
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

// composerRowPrefixes open the cursorDiskKV keys that belong to one
// composer as <prefix><composerId>:<rest>. composerData:<composerId> is
// the other shape.
var composerRowPrefixes = []string{"bubbleId:", "checkpointId:", "messageRequestContext:", "codeBlockDiff:"}

// composerRowsQuery selects one composer's cursorDiskKV rows by key: the
// composerData key by equality, and each prefix by the key range
// "<prefix><id>:" to "<prefix><id>;", which the key index serves. ';'
// is the byte after ':'. The lower bound is exclusive, so a key with
// nothing after the id is not selected.
var composerRowsQuery = func() string {
	parts := []string{`SELECT key, value FROM cursorDiskKV WHERE key = ?`}
	for range composerRowPrefixes {
		parts = append(parts, `SELECT key, value FROM cursorDiskKV WHERE key > ? AND key < ?`)
	}
	return strings.Join(parts, " UNION ALL ")
}()

func composerRowsArgs(id string) []any {
	args := []any{"composerData:" + id}
	for _, p := range composerRowPrefixes {
		args = append(args, p+id+":", p+id+";")
	}
	return args
}

// composerRows reads the cursorDiskKV rows of the composers in ids. A
// database without that table is an empty slice. cursorAuth keys are
// already dropped by readKV. Rows of other composers are not read.
func composerRows(ctx context.Context, db *sql.DB, ids map[string]struct{}) ([]row, error) {
	ok, err := tableExists(ctx, db, "cursorDiskKV")
	if err != nil || !ok {
		return nil, err
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	out := []row{}
	for _, id := range sorted {
		part, err := readKV(ctx, db, composerRowsQuery, composerRowsArgs(id)...)
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
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
	if id, ok := strings.CutPrefix(key, "composerData:"); ok {
		if id == "" || strings.Contains(id, ":") {
			return "", false
		}
		return id, true
	}
	for _, p := range composerRowPrefixes {
		if strings.HasPrefix(key, p) {
			return composerSegment(key, p)
		}
	}
	return "", false
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

// readKV reads key and value rows. A value the ruleset cannot scan
// fails the read with an error that names its key.
func readKV(ctx context.Context, db *sql.DB, query string, args ...any) ([]row, error) {
	rows, err := db.QueryContext(ctx, query, args...)
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
		value, hidden, err := encodeValue(val)
		if err != nil {
			return nil, fmt.Errorf("cursor: key %q: %w", key, err)
		}
		out = append(out, row{Key: key, Value: value, hidden: hidden})
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

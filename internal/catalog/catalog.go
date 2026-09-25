// Package catalog is the session index. SQLite is the MVP store; a move
// to Postgres is a later problem and is not sketched here beyond the
// choice of a boring SQL schema.
//
// Identity is (harness, native_session_id). session_uid is assigned once
// for that pair. aliases maps (harness, native_id, machine_id) back to
// the uid. A second machine posting the same digest adds a provenance
// row and does not store another blob; the blob store is what refuses
// the second copy. Bytes that are not a prefix of the head are a
// divergent_copy artifact. When Ingest is given a BlobReader, it
// recomputes the relation inside the write transaction from the current
// head and the client blob. A GrownFrom decision is not applied once
// that head is no longer a prefix of the client bytes.
//
// project_id is the Layer C key. Ingest recomputes it from the manifest's
// git remote and root commit. Sessions that share a non-empty id are the
// same project. An empty id is not a group.
package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/id"
	"terva.sh/lampi/internal/protocol"

	_ "modernc.org/sqlite"
)

// Catalog is one SQLite file.
type Catalog struct {
	db *sql.DB
}

// Decision is the merge result for one manifest artifact.
//
// Record is false for unchanged and stale: the artifact row already
// exists and the head is not replaced. Head is set only when this
// artifact's digest becomes the session head.
//
// A nil BlobReader stores the decision as given. A non-nil reader
// replaces it, under the same transaction as the head move, with the
// relation of the current head bytes and the client blob. The HTTP
// handler always passes the CAS. A GrownFrom that does not extend the
// head read in that transaction is stored as a divergent copy.
//
// Base is the relpath of the session head this artifact was compared
// with when that is not the artifact's own path. The same session from
// a second machine or a moved home arrives under a new relpath. An
// unchanged or stale artifact then names the head's row, and a head
// move clears current on that row, so the session keeps one head.
type Decision struct {
	Relation  string
	GrownFrom string
	Record    bool
	Head      bool
	Base      string
}

// ArtifactRow is one stored object linked to a session.
type ArtifactRow struct {
	ID         string
	SessionUID string
	Kind       string
	RelPath    string
	SHA256     string
	Size       int64
	Relation   string
	GrownFrom  string
	Current    bool
}

// ProvenanceRow is one machine's record of a digest for a session.
type ProvenanceRow struct {
	SessionUID string
	MachineID  string
	SHA256     string
	RelPath    string
}

// Open creates the catalog file and brings its schema to the version
// this binary knows. The file is owner-read because session rows
// describe private transcripts. A file written by a newer binary is
// refused, not read with a schema that may not match it.
func Open(path string) (*Catalog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	dsn, err := dataSource(path)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	// One writer. The lake process is the only client of this file.
	db.SetMaxOpenConns(1)
	if err := upgrade(db, path); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return &Catalog{db: db}, nil
}

// OpenReadOnly opens an existing catalog without write access, for a
// command that reads while serve runs. It does not create or migrate
// the file. WAL lets it read while serve writes.
func OpenReadOnly(path string) (*Catalog, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "busy_timeout(5000)")
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: q.Encode()}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return &Catalog{db: db}, nil
}

// VacuumInto writes a consistent copy of the catalog to dest, which
// must not exist. It works on a read-only catalog and while serve
// writes: the copy is one read transaction.
func (c *Catalog) VacuumInto(ctx context.Context, dest string) error {
	if _, err := c.db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("catalog: vacuum into %s: %w", dest, err)
	}
	return nil
}

// dataSource is a file URI carrying the per-connection pragmas. The
// driver runs them on every connection it opens, so a reopened
// connection keeps busy_timeout. synchronous is FULL because the
// manifest ACK follows the commit.
func dataSource(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	for _, p := range []string{
		"busy_timeout(5000)",
		"foreign_keys(1)",
		"journal_mode(WAL)",
		"synchronous(FULL)",
	} {
		q.Add("_pragma", p)
	}
	// _txlock=immediate makes every transaction BEGIN IMMEDIATE. A
	// deferred one that reads and then writes cannot wait on
	// busy_timeout for the write lock: the upgrade fails with
	// SQLITE_BUSY at once when another connection is writing.
	q.Set("_txlock", "immediate")
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: q.Encode()}
	return u.String(), nil
}

// migrations[i] moves PRAGMA user_version from i to i+1. Append only:
// an entry that has shipped is never edited, because a file that
// already ran it will not run it again. len(migrations) is the version
// this binary writes.
var migrations = []func(*sql.Tx) error{
	migrate1,
}

// upgrade runs each migration above the file's user_version, one
// transaction per step with the version bump inside it. A file above
// len(migrations) is refused.
func upgrade(db *sql.DB, path string) error {
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if v > len(migrations) {
		return fmt.Errorf("catalog: %s has schema version %d; this binary knows up to %d, so upgrade terva-lampi", path, v, len(migrations))
	}
	for ; v < len(migrations); v++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("catalog: %w", err)
		}
		if err := migrations[v](tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("catalog: migration %d: %w", v+1, err)
		}
		// PRAGMA does not take a bound parameter. v is an int.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("catalog: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("catalog: %w", err)
		}
	}
	return nil
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    session_uid TEXT PRIMARY KEY,
    harness TEXT NOT NULL,
    native_session_id TEXT NOT NULL,
    head_sha256 TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    ingested_at TEXT NOT NULL,
    normalize_error TEXT,
    project_id TEXT NOT NULL DEFAULT '',
    normalize_gen INTEGER NOT NULL DEFAULT 0,
    UNIQUE (harness, native_session_id)
);
CREATE TABLE IF NOT EXISTS normalize_jobs (
    session_uid TEXT PRIMARY KEY,
    gen INTEGER NOT NULL,
    enqueued_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS aliases (
    harness TEXT NOT NULL,
    native_session_id TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    session_uid TEXT NOT NULL,
    PRIMARY KEY (harness, native_session_id, machine_id)
);
CREATE TABLE IF NOT EXISTS provenance (
    session_uid TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    relpath TEXT NOT NULL DEFAULT '',
    ingested_at TEXT NOT NULL,
    PRIMARY KEY (session_uid, machine_id, sha256)
);
CREATE TABLE IF NOT EXISTS artifacts (
    artifact_id TEXT PRIMARY KEY,
    session_uid TEXT NOT NULL,
    kind TEXT NOT NULL,
    relpath TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size INTEGER NOT NULL,
    relation TEXT NOT NULL DEFAULT '',
    grown_from TEXT NOT NULL DEFAULT '',
    current INTEGER NOT NULL DEFAULT 0,
    UNIQUE (session_uid, relpath, sha256)
);
`

// migrate1 is the schema as it stood when versioning began. A file
// from before then has user_version 0 and any older shape: before
// aliases, relations, sessions.normalize_error, project_id, or the
// normalize queue. CREATE TABLE IF NOT EXISTS does not alter an old
// table, so the columns are added when missing. Every step is safe on
// a file that already has them.
func migrate1(tx *sql.Tx) error {
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	ok, err := columnExists(tx, "provenance", "sha256")
	if err != nil {
		return err
	}
	if !ok {
		if _, err := tx.Exec(`ALTER TABLE provenance RENAME TO provenance_v1`); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			CREATE TABLE provenance (
				session_uid TEXT NOT NULL,
				machine_id TEXT NOT NULL,
				sha256 TEXT NOT NULL,
				relpath TEXT NOT NULL DEFAULT '',
				ingested_at TEXT NOT NULL,
				PRIMARY KEY (session_uid, machine_id, sha256)
			)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO provenance (session_uid, machine_id, sha256, relpath, ingested_at)
			SELECT session_uid, machine_id, '', '', '' FROM provenance_v1`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE provenance_v1`); err != nil {
			return err
		}
	}
	for _, alt := range []struct{ col, stmt string }{
		{"relation", `ALTER TABLE artifacts ADD COLUMN relation TEXT NOT NULL DEFAULT ''`},
		{"grown_from", `ALTER TABLE artifacts ADD COLUMN grown_from TEXT NOT NULL DEFAULT ''`},
		{"current", `ALTER TABLE artifacts ADD COLUMN current INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := addColumn(tx, "artifacts", alt.col, alt.stmt); err != nil {
			return err
		}
	}
	// A head written before the current flag existed is still the head.
	if _, err := tx.Exec(`
		UPDATE artifacts SET current = 1
		WHERE current = 0 AND sha256 IN (
			SELECT head_sha256 FROM sessions s WHERE s.session_uid = artifacts.session_uid
		)`); err != nil {
		return err
	}
	for _, alt := range []struct{ col, stmt string }{
		{"normalize_error", `ALTER TABLE sessions ADD COLUMN normalize_error TEXT`},
		{"project_id", `ALTER TABLE sessions ADD COLUMN project_id TEXT NOT NULL DEFAULT ''`},
		{"normalize_gen", `ALTER TABLE sessions ADD COLUMN normalize_gen INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := addColumn(tx, "sessions", alt.col, alt.stmt); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`CREATE INDEX IF NOT EXISTS sessions_by_project ON sessions (project_id)`)
	return err
}

func addColumn(tx *sql.Tx, table, column, stmt string) error {
	ok, err := columnExists(tx, table, column)
	if err != nil || ok {
		return err
	}
	_, err = tx.Exec(stmt)
	return err
}

func columnExists(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Close releases the database.
func (c *Catalog) Close() error {
	return c.db.Close()
}

// Counts is the size of the catalog. Machines is the number of distinct
// machine ids in provenance, not one row per session.
type Counts struct {
	Sessions  int
	Artifacts int
	Machines  int
}

// Counts reads how many sessions, artifacts, and machines are stored.
func (c *Catalog) Counts(ctx context.Context) (Counts, error) {
	var n Counts
	err := c.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM sessions),
			(SELECT COUNT(*) FROM artifacts),
			(SELECT COUNT(DISTINCT machine_id) FROM provenance)`).Scan(&n.Sessions, &n.Artifacts, &n.Machines)
	if err != nil {
		return Counts{}, fmt.Errorf("catalog: %w", err)
	}
	return n, nil
}

// BlobReader reads one immutable object. CAS objects are content
// addressed, so a read during the ingest transaction is stable.
type BlobReader interface {
	Read(digest string) ([]byte, error)
}

// Ingest records m under the relations in decisions and returns the
// stable session uid. decisions[i] is the decision for m.Artifacts[i].
// Repeating an unchanged digest returns the same ids. A grown_from
// digest moves head_sha256. A divergent_copy does not.
//
// blobs, when set, is read inside the write transaction. The catalog
// uses one SQLite connection, so a second ingest cannot read the head
// until this transaction commits. The relation applied is the one for
// that head, not a decision computed against an earlier one.
func (c *Catalog) Ingest(ctx context.Context, m protocol.Manifest, now time.Time, decisions []Decision, blobs BlobReader) (protocol.ManifestAck, error) {
	if m.CaptureProtocol != protocol.Version {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: capture_protocol %d", m.CaptureProtocol)
	}
	if m.MachineID == "" || m.Harness == "" || m.NativeSessionID == "" {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: machine_id, harness, and native_session_id are required")
	}
	if len(m.Artifacts) == 0 {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: manifest has no artifacts")
	}
	if len(decisions) != len(m.Artifacts) {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: %d decisions for %d artifacts", len(decisions), len(m.Artifacts))
	}
	for i, a := range m.Artifacts {
		if !protocol.ValidDigest(a.SHA256) {
			return protocol.ManifestAck{}, fmt.Errorf("catalog: artifact %q has an invalid sha256", a.RelPath)
		}
		switch decisions[i].Relation {
		case protocol.RelationHead, protocol.RelationGrownFrom, protocol.RelationDivergentCopy, protocol.RelationUnchanged, protocol.RelationStale:
		default:
			return protocol.ManifestAck{}, fmt.Errorf("catalog: artifact %q: unknown relation %q", a.RelPath, decisions[i].Relation)
		}
	}
	m.Project.ProjectID = protocol.ProjectLinkID(m.Project.GitRemote, m.Project.GitRoot)
	raw, err := json.Marshal(m)
	if err != nil {
		return protocol.ManifestAck{}, err
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.ManifestAck{}, err
	}
	defer func() { _ = tx.Rollback() }()

	uid, head, exists, err := lookupSession(ctx, tx, m.Harness, m.NativeSessionID)
	if err != nil {
		return protocol.ManifestAck{}, err
	}
	if !exists {
		uid, err = id.New(now)
		if err != nil {
			return protocol.ManifestAck{}, err
		}
		head = ""
	}
	if blobs != nil {
		revised, err := reviseDecisions(ctx, tx, blobs, uid, head, m)
		if err != nil {
			return protocol.ManifestAck{}, err
		}
		decisions = revised
	}
	newHead := head
	for i, d := range decisions {
		if d.Head {
			newHead = m.Artifacts[i].SHA256
		}
	}
	if newHead == "" {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: manifest did not name a session head")
	}
	ingested := now.UTC().Format(time.RFC3339Nano)
	if !exists {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sessions (session_uid, harness, native_session_id, head_sha256, manifest_json, ingested_at, project_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			uid, m.Harness, m.NativeSessionID, newHead, string(raw), ingested, m.Project.ProjectID); err != nil {
			return protocol.ManifestAck{}, fmt.Errorf("catalog: session: %w", err)
		}
	} else if newHead != head {
		if _, err := tx.ExecContext(ctx, `
			UPDATE sessions SET head_sha256 = ?, manifest_json = ?, ingested_at = ?
			WHERE session_uid = ?`,
			newHead, string(raw), ingested, uid); err != nil {
			return protocol.ManifestAck{}, fmt.Errorf("catalog: session: %w", err)
		}
	}
	// A later manifest can learn the root. Write that id onto the stored
	// manifest too, including when the head did not move, so the column
	// and manifest_json agree. Do not clear an id when this post has
	// none: an empty project_id is not a group, and a second machine
	// whose checkout has no .git must not unlink the session.
	if m.Project.ProjectID != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE sessions SET project_id = ?, manifest_json = ? WHERE session_uid = ?`,
			m.Project.ProjectID, string(raw), uid); err != nil {
			return protocol.ManifestAck{}, fmt.Errorf("catalog: project: %w", err)
		}
	}

	if err := insertAlias(ctx, tx, m, uid); err != nil {
		return protocol.ManifestAck{}, err
	}
	for _, a := range m.Artifacts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO provenance (session_uid, machine_id, sha256, relpath, ingested_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT DO NOTHING`,
			uid, m.MachineID, a.SHA256, a.RelPath, ingested); err != nil {
			return protocol.ManifestAck{}, fmt.Errorf("catalog: provenance: %w", err)
		}
	}

	ack := protocol.ManifestAck{
		SessionUID:  uid,
		HeadSHA256:  newHead,
		Relation:    ackRelation(m.Artifacts, decisions),
		ArtifactIDs: make([]string, 0, len(m.Artifacts)),
	}
	for i, a := range m.Artifacts {
		got, err := applyArtifact(ctx, tx, now, uid, a, decisions[i])
		if err != nil {
			return protocol.ManifestAck{}, err
		}
		ack.ArtifactIDs = append(ack.ArtifactIDs, got)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT size FROM artifacts
		WHERE session_uid = ? AND sha256 = ?
		ORDER BY current DESC, size DESC LIMIT 1`, uid, newHead).Scan(&ack.HeadSize); err != nil {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: head size: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return protocol.ManifestAck{}, err
	}
	return ack, nil
}

// reviseDecisions relates each artifact to the current row at its
// relpath. The manifest's head artifact at a relpath the session has no
// current row for is related to the session head instead, when that
// head is a head-bearing artifact of the same kind at another path.
// Other artifacts stay keyed by relpath: a Claude subagent transcript,
// a terva error sidecar, and a raati or tasks file are separate files
// of the same session.
func reviseDecisions(ctx context.Context, tx *sql.Tx, blobs BlobReader, uid, sessionHead string, m protocol.Manifest) ([]Decision, error) {
	out := make([]Decision, len(m.Artifacts))
	head := transcriptIndex(m.Artifacts)
	for i, a := range m.Artifacts {
		cur, ok, err := currentDigest(ctx, tx, uid, a.RelPath)
		if err != nil {
			return nil, err
		}
		var base string
		if !ok && i == head && headBearing(a.Kind) {
			row, found, err := headRow(ctx, tx, uid, sessionHead)
			if err != nil {
				return nil, err
			}
			if found && row.Kind == a.Kind && row.RelPath != a.RelPath {
				cur, ok, base = row.SHA256, true, row.RelPath
			}
		}
		if !ok {
			out[i] = Decision{Relation: protocol.RelationHead, Record: true, Head: i == head}
			continue
		}
		if cur == a.SHA256 {
			out[i] = Decision{Relation: protocol.RelationUnchanged, Base: base}
			continue
		}
		stored, err := blobs.Read(cur)
		if err != nil {
			return nil, fmt.Errorf("catalog: head %s: %w", cur, err)
		}
		client, err := blobs.Read(a.SHA256)
		if err != nil {
			return nil, fmt.Errorf("catalog: blob %s: %w", a.SHA256, err)
		}
		sum := sha256.Sum256(client)
		if hex.EncodeToString(sum[:]) != a.SHA256 {
			return nil, fmt.Errorf("catalog: artifact %q sha256 does not match the blob", a.RelPath)
		}
		switch protocol.RelationOf(stored, client) {
		case protocol.RelationUnchanged:
			out[i] = Decision{Relation: protocol.RelationUnchanged, Base: base}
		case protocol.RelationGrownFrom:
			out[i] = Decision{
				Relation:  protocol.RelationGrownFrom,
				GrownFrom: cur,
				Record:    true,
				Head:      i == head,
				Base:      base,
			}
		case protocol.RelationStale:
			if d, ok := snapshotDecision(a.Kind); ok {
				d.Base = base
				out[i] = d
				continue
			}
			out[i] = Decision{Relation: protocol.RelationStale, Base: base}
		default:
			if d, ok := snapshotDecision(a.Kind); ok {
				d.Base = base
				out[i] = d
				continue
			}
			// The copy stays under its own path and is not current,
			// so the row at base stays the only head.
			out[i] = Decision{Relation: protocol.RelationDivergentCopy, Record: true}
		}
	}
	return out, nil
}

// snapshotArtifact is a whole-file snapshot. A raati record is written
// once. A task board is replaced, and that file holds the archived
// generations. A Cursor IDE state export, a Cursor CLI store export,
// and an OpenCode export are replaced the same way. A raati or tasks
// rewrite becomes the current artifact for the path and does not move
// the session head. A Cursor or OpenCode export is the session, so a
// rewrite does.
func snapshotArtifact(kind string) bool {
	switch kind {
	case protocol.KindRaatiJSON, protocol.KindTasksJSON, protocol.KindCursorStateJSON, protocol.KindCursorCLIStoreJSON, protocol.KindOpenCodeExportJSON:
		return true
	default:
		return false
	}
}

func snapshotDecision(kind string) (Decision, bool) {
	if !snapshotArtifact(kind) {
		return Decision{}, false
	}
	return Decision{
		Relation: protocol.RelationHead,
		Record:   true,
		Head:     headBearing(kind),
	}, true
}

// headBearing is a kind that is the session itself: a transcript, or
// a Cursor or OpenCode export. errors_jsonl, raati_json, and
// tasks_json sit beside it and never stand in for it.
func headBearing(kind string) bool {
	switch kind {
	case protocol.KindTranscriptJSONL, protocol.KindCursorStateJSON, protocol.KindCursorCLIStoreJSON, protocol.KindOpenCodeExportJSON:
		return true
	default:
		return false
	}
}

// headRow is the current artifact holding the session head digest.
func headRow(ctx context.Context, tx *sql.Tx, uid, head string) (ArtifactRow, bool, error) {
	if head == "" {
		return ArtifactRow{}, false, nil
	}
	a := ArtifactRow{SessionUID: uid, Current: true}
	err := tx.QueryRowContext(ctx, `
		SELECT artifact_id, kind, relpath, sha256 FROM artifacts
		WHERE session_uid = ? AND sha256 = ? AND current = 1
		ORDER BY relpath LIMIT 1`, uid, head).Scan(&a.ID, &a.Kind, &a.RelPath, &a.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return ArtifactRow{}, false, nil
	}
	if err != nil {
		return ArtifactRow{}, false, fmt.Errorf("catalog: head: %w", err)
	}
	return a, true, nil
}

func currentDigest(ctx context.Context, tx *sql.Tx, uid, rel string) (string, bool, error) {
	var sha string
	err := tx.QueryRowContext(ctx, `
		SELECT sha256 FROM artifacts
		WHERE session_uid = ? AND relpath = ? AND current = 1`, uid, rel).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("catalog: artifact: %w", err)
	}
	return sha, true, nil
}

func transcriptIndex(arts []protocol.Artifact) int {
	for i, a := range arts {
		if a.Kind == protocol.KindTranscriptJSONL {
			return i
		}
	}
	if len(arts) == 0 {
		return 0
	}
	return len(arts) - 1
}

func lookupSession(ctx context.Context, tx *sql.Tx, harness, native string) (uid, head string, ok bool, err error) {
	err = tx.QueryRowContext(ctx, `
		SELECT session_uid, head_sha256 FROM sessions
		WHERE harness = ? AND native_session_id = ?`, harness, native).Scan(&uid, &head)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("catalog: session: %w", err)
	}
	return uid, head, true, nil
}

func insertAlias(ctx context.Context, tx *sql.Tx, m protocol.Manifest, uid string) error {
	var existing string
	err := tx.QueryRowContext(ctx, `
		SELECT session_uid FROM aliases
		WHERE harness = ? AND native_session_id = ? AND machine_id = ?`,
		m.Harness, m.NativeSessionID, m.MachineID).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO aliases (harness, native_session_id, machine_id, session_uid)
			VALUES (?, ?, ?, ?)`,
			m.Harness, m.NativeSessionID, m.MachineID, uid); err != nil {
			return fmt.Errorf("catalog: alias: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("catalog: alias: %w", err)
	}
	if existing != uid {
		return fmt.Errorf("catalog: alias for %s/%s/%s points at %s, not %s", m.Harness, m.NativeSessionID, m.MachineID, existing, uid)
	}
	return nil
}

func applyArtifact(ctx context.Context, tx *sql.Tx, now time.Time, uid string, a protocol.Artifact, d Decision) (string, error) {
	if !d.Record {
		// An artifact related to the head under another path names
		// that head's row. No row is stored under its own path.
		rel := a.RelPath
		if d.Base != "" {
			rel = d.Base
		}
		var got string
		q := `
			SELECT artifact_id FROM artifacts
			WHERE session_uid = ? AND relpath = ? AND sha256 = ?`
		args := []any{uid, rel, a.SHA256}
		if d.Relation == protocol.RelationStale {
			q = `
				SELECT artifact_id FROM artifacts
				WHERE session_uid = ? AND relpath = ? AND current = 1`
			args = []any{uid, rel}
		}
		err := tx.QueryRowContext(ctx, q, args...).Scan(&got)
		if err != nil {
			return "", fmt.Errorf("catalog: artifact %q: %w", a.RelPath, err)
		}
		return got, nil
	}
	if d.Relation == protocol.RelationHead || d.Relation == protocol.RelationGrownFrom {
		for _, rel := range []string{a.RelPath, d.Base} {
			if rel == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE artifacts SET current = 0
				WHERE session_uid = ? AND relpath = ? AND current = 1`, uid, rel); err != nil {
				return "", fmt.Errorf("catalog: artifact: %w", err)
			}
		}
	}
	current := 0
	if d.Relation == protocol.RelationHead || d.Relation == protocol.RelationGrownFrom {
		current = 1
	}
	aid, err := id.New(now)
	if err != nil {
		return "", err
	}
	var got string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO artifacts (
			artifact_id, session_uid, kind, relpath, sha256, size, relation, grown_from, current
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (session_uid, relpath, sha256) DO UPDATE SET
			size = excluded.size,
			current = MAX(artifacts.current, excluded.current)
		RETURNING artifact_id`,
		aid, uid, a.Kind, a.RelPath, a.SHA256, a.Size, d.Relation, d.GrownFrom, current,
	).Scan(&got)
	if err != nil {
		return "", fmt.Errorf("catalog: artifact: %w", err)
	}
	return got, nil
}

// ackRelation is the transcript's relation. A sidecar does not speak
// for the session head.
func ackRelation(arts []protocol.Artifact, ds []Decision) string {
	for i, a := range arts {
		if a.Kind == protocol.KindTranscriptJSONL {
			return ds[i].Relation
		}
	}
	return ds[len(ds)-1].Relation
}

// Alias returns the session uid for the (harness, native id, machine id)
// triple. ok is false when that machine has not posted the session.
func (c *Catalog) Alias(ctx context.Context, harness, nativeID, machineID string) (string, bool, error) {
	var uid string
	err := c.db.QueryRowContext(ctx, `
		SELECT session_uid FROM aliases
		WHERE harness = ? AND native_session_id = ? AND machine_id = ?`,
		harness, nativeID, machineID).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("catalog: alias: %w", err)
	}
	return uid, true, nil
}

// Provenance lists the machine/digest rows for a session uid.
func (c *Catalog) Provenance(ctx context.Context, sessionUID string) ([]ProvenanceRow, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT session_uid, machine_id, sha256, relpath FROM provenance
		WHERE session_uid = ?
		ORDER BY machine_id, sha256`, sessionUID)
	if err != nil {
		return nil, fmt.Errorf("catalog: provenance: %w", err)
	}
	defer rows.Close()
	var out []ProvenanceRow
	for rows.Next() {
		var p ProvenanceRow
		if err := rows.Scan(&p.SessionUID, &p.MachineID, &p.SHA256, &p.RelPath); err != nil {
			return nil, fmt.Errorf("catalog: provenance: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Artifacts lists every artifact linked to a session, oldest first.
func (c *Catalog) Artifacts(ctx context.Context, sessionUID string) ([]ArtifactRow, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT artifact_id, session_uid, kind, relpath, sha256, size, relation, grown_from, current
		FROM artifacts WHERE session_uid = ? ORDER BY rowid`, sessionUID)
	if err != nil {
		return nil, fmt.Errorf("catalog: artifact: %w", err)
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

// Current returns the session uid and the current artifact for each
// path. ok is false when the logical session has not been ingested.
func (c *Catalog) Current(ctx context.Context, harness, nativeID string) (string, []ArtifactRow, bool, error) {
	var uid string
	err := c.db.QueryRowContext(ctx, `
		SELECT session_uid FROM sessions
		WHERE harness = ? AND native_session_id = ?`, harness, nativeID).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("catalog: session: %w", err)
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT artifact_id, session_uid, kind, relpath, sha256, size, relation, grown_from, current
		FROM artifacts WHERE session_uid = ? AND current = 1 ORDER BY relpath`, uid)
	if err != nil {
		return "", nil, false, fmt.Errorf("catalog: artifact: %w", err)
	}
	defer rows.Close()
	arts, err := scanArtifacts(rows)
	if err != nil {
		return "", nil, false, err
	}
	return uid, arts, true, nil
}

// HeadView is a session's head digest and its current artifacts, read
// in one transaction so the head names one of those rows.
type HeadView struct {
	SessionUID string
	HeadSHA256 string
	Current    []ArtifactRow
}

// Head returns the session head and the current artifact for each
// path, ordered by relpath. ok is false when the logical session has
// not been ingested.
func (c *Catalog) Head(ctx context.Context, harness, nativeID string) (HeadView, bool, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return HeadView{}, false, fmt.Errorf("catalog: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var v HeadView
	err = tx.QueryRowContext(ctx, `
		SELECT session_uid, head_sha256 FROM sessions
		WHERE harness = ? AND native_session_id = ?`, harness, nativeID).Scan(&v.SessionUID, &v.HeadSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return HeadView{}, false, nil
	}
	if err != nil {
		return HeadView{}, false, fmt.Errorf("catalog: session: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT artifact_id, session_uid, kind, relpath, sha256, size, relation, grown_from, current
		FROM artifacts WHERE session_uid = ? AND current = 1 ORDER BY relpath`, v.SessionUID)
	if err != nil {
		return HeadView{}, false, fmt.Errorf("catalog: artifact: %w", err)
	}
	defer rows.Close()
	v.Current, err = scanArtifacts(rows)
	if err != nil {
		return HeadView{}, false, err
	}
	return v, true, nil
}

func scanArtifacts(rows *sql.Rows) ([]ArtifactRow, error) {
	var out []ArtifactRow
	for rows.Next() {
		var a ArtifactRow
		var current int
		if err := rows.Scan(&a.ID, &a.SessionUID, &a.Kind, &a.RelPath, &a.SHA256, &a.Size, &a.Relation, &a.GrownFrom, &current); err != nil {
			return nil, fmt.Errorf("catalog: artifact: %w", err)
		}
		a.Current = current != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// SessionInfo is one catalog session, including the last normalize
// failure. NormalizeError is empty when the last projection succeeded
// or the session has not been projected yet. ProjectID is the Layer C
// key. Empty means this session is not linked to another.
type SessionInfo struct {
	UID            string
	Harness        string
	NativeID       string
	NormalizeError string
	ProjectID      string
	Manifest       protocol.Manifest
}

// SetNormalizeError records msg on the session. An empty msg clears it.
// The CAS blob is not touched.
func (c *Catalog) SetNormalizeError(ctx context.Context, sessionUID, msg string) error {
	res, err := c.db.ExecContext(ctx, `
		UPDATE sessions SET normalize_error = NULLIF(?, '') WHERE session_uid = ?`, msg, sessionUID)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("catalog: unknown session %s", sessionUID)
	}
	return nil
}

// NormalizeError returns the stored failure. ok is false when the
// session does not exist. An empty string means no recorded failure.
func (c *Catalog) NormalizeError(ctx context.Context, sessionUID string) (string, bool, error) {
	var msg sql.NullString
	err := c.db.QueryRowContext(ctx, `
		SELECT normalize_error FROM sessions WHERE session_uid = ?`, sessionUID).Scan(&msg)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("catalog: %w", err)
	}
	return msg.String, true, nil
}

// ListSessions returns every session, oldest ingest first.
func (c *Catalog) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	return c.listSessions(ctx, `
		SELECT session_uid, harness, native_session_id, COALESCE(normalize_error, ''), COALESCE(project_id, ''), manifest_json
		FROM sessions
		ORDER BY ingested_at, session_uid`)
}

// SessionsByProject returns the sessions that share projectID, oldest
// ingest first. An empty id returns no rows. Checkouts that never
// learned a root are not a single project.
func (c *Catalog) SessionsByProject(ctx context.Context, projectID string) ([]SessionInfo, error) {
	if projectID == "" {
		return nil, nil
	}
	return c.listSessions(ctx, `
		SELECT session_uid, harness, native_session_id, COALESCE(normalize_error, ''), COALESCE(project_id, ''), manifest_json
		FROM sessions
		WHERE project_id = ?
		ORDER BY ingested_at, session_uid`, projectID)
}

func (c *Catalog) listSessions(ctx context.Context, query string, args ...any) ([]SessionInfo, error) {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var info SessionInfo
		var raw string
		if err := rows.Scan(&info.UID, &info.Harness, &info.NativeID, &info.NormalizeError, &info.ProjectID, &raw); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if err := json.Unmarshal([]byte(raw), &info.Manifest); err != nil {
			return nil, fmt.Errorf("catalog: session %s: %w", info.UID, err)
		}
		out = append(out, info)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

// DivergentCopy is one artifact stored with relation divergent_copy.
// The session head did not move. Machines posted this digest.
// HeadMachines posted the head digest, under any path.
type DivergentCopy struct {
	SessionUID   string
	ArtifactID   string
	Harness      string
	NativeID     string
	Kind         string
	RelPath      string
	SHA256       string
	Size         int64
	HeadSHA256   string
	HeadSize     int64
	Machines     []string
	HeadMachines []string
}

// DivergentCopies lists every divergent_copy artifact, oldest first.
// The rows are the ones Ingest stored. This does not read the CAS.
// Machine lists come from provenance for that session, path, and digest.
// An empty catalog returns an empty slice.
func (c *Catalog) DivergentCopies(ctx context.Context) ([]DivergentCopy, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT
			a.session_uid,
			a.artifact_id,
			s.harness,
			s.native_session_id,
			a.kind,
			a.relpath,
			a.sha256,
			a.size,
			s.head_sha256,
			COALESCE((
				SELECT size FROM artifacts
				WHERE session_uid = a.session_uid AND sha256 = s.head_sha256
				ORDER BY current DESC, size DESC LIMIT 1
			), 0)
		FROM artifacts a
		JOIN sessions s ON s.session_uid = a.session_uid
		WHERE a.relation = ?
		ORDER BY a.rowid`, protocol.RelationDivergentCopy)
	if err != nil {
		return nil, fmt.Errorf("catalog: divergent_copy: %w", err)
	}
	out, scanErr := scanDivergentCopies(rows)
	rows.Close()
	if scanErr != nil {
		return nil, scanErr
	}
	// The catalog uses one SQLite connection. The artifact query has to
	// be closed before these provenance reads, or the second query waits
	// on itself.
	for i := range out {
		machines, err := c.digestMachines(ctx, out[i].SessionUID, out[i].RelPath, out[i].SHA256)
		if err != nil {
			return nil, err
		}
		out[i].Machines = machines
		// The head may sit under another relpath: the copy can come
		// from a second machine whose path embeds a different cwd.
		head, err := c.digestMachines(ctx, out[i].SessionUID, "", out[i].HeadSHA256)
		if err != nil {
			return nil, err
		}
		out[i].HeadMachines = head
	}
	return out, nil
}

func scanDivergentCopies(rows *sql.Rows) ([]DivergentCopy, error) {
	out := []DivergentCopy{}
	for rows.Next() {
		var d DivergentCopy
		if err := rows.Scan(
			&d.SessionUID, &d.ArtifactID, &d.Harness, &d.NativeID,
			&d.Kind, &d.RelPath, &d.SHA256, &d.Size, &d.HeadSHA256, &d.HeadSize,
		); err != nil {
			return nil, fmt.Errorf("catalog: divergent_copy: %w", err)
		}
		d.Machines = []string{}
		d.HeadMachines = []string{}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: divergent_copy: %w", err)
	}
	return out, nil
}

// digestMachines lists the machines that posted sha for the session.
// An empty rel matches any path.
func (c *Catalog) digestMachines(ctx context.Context, uid, rel, sha string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT machine_id FROM provenance
		WHERE session_uid = ? AND (? = '' OR relpath = ?) AND sha256 = ?
		ORDER BY machine_id`, uid, rel, rel, sha)
	if err != nil {
		return nil, fmt.Errorf("catalog: provenance: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("catalog: provenance: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: provenance: %w", err)
	}
	return out, nil
}

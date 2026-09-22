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
package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
type Decision struct {
	Relation  string
	GrownFrom string
	Record    bool
	Head      bool
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

// Open creates the catalog file and its tables. The file is owner-read
// because session rows describe private transcripts.
func Open(path string) (*Catalog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	// One writer. The lake process is the only client of this file.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return &Catalog{db: db}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    session_uid TEXT PRIMARY KEY,
    harness TEXT NOT NULL,
    native_session_id TEXT NOT NULL,
    head_sha256 TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    ingested_at TEXT NOT NULL,
    UNIQUE (harness, native_session_id)
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

// migrate adds columns a database created before aliases and relations
// would not have. CREATE TABLE IF NOT EXISTS does not alter an old file.
func migrate(db *sql.DB) error {
	ok, err := columnExists(db, "provenance", "sha256")
	if err != nil {
		return err
	}
	if !ok {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("catalog: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE provenance RENAME TO provenance_v1`); err != nil {
			tx.Rollback()
			return fmt.Errorf("catalog: %w", err)
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
			tx.Rollback()
			return fmt.Errorf("catalog: %w", err)
		}
		if _, err := tx.Exec(`
			INSERT INTO provenance (session_uid, machine_id, sha256, relpath, ingested_at)
			SELECT session_uid, machine_id, '', '', '' FROM provenance_v1`); err != nil {
			tx.Rollback()
			return fmt.Errorf("catalog: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE provenance_v1`); err != nil {
			tx.Rollback()
			return fmt.Errorf("catalog: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("catalog: %w", err)
		}
	}
	for _, alt := range []struct{ col, stmt string }{
		{"relation", `ALTER TABLE artifacts ADD COLUMN relation TEXT NOT NULL DEFAULT ''`},
		{"grown_from", `ALTER TABLE artifacts ADD COLUMN grown_from TEXT NOT NULL DEFAULT ''`},
		{"current", `ALTER TABLE artifacts ADD COLUMN current INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := addColumn(db, "artifacts", alt.col, alt.stmt); err != nil {
			return err
		}
	}
	// A head written before the current flag existed is still the head.
	if _, err := db.Exec(`
		UPDATE artifacts SET current = 1
		WHERE current = 0 AND sha256 IN (
			SELECT head_sha256 FROM sessions s WHERE s.session_uid = artifacts.session_uid
		)`); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

func addColumn(db *sql.DB, table, column, stmt string) error {
	ok, err := columnExists(db, table, column)
	if err != nil || ok {
		return err
	}
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, fmt.Errorf("catalog: %w", err)
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
		revised, err := reviseDecisions(ctx, tx, blobs, uid, m)
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
			INSERT INTO sessions (session_uid, harness, native_session_id, head_sha256, manifest_json, ingested_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			uid, m.Harness, m.NativeSessionID, newHead, string(raw), ingested); err != nil {
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

func reviseDecisions(ctx context.Context, tx *sql.Tx, blobs BlobReader, uid string, m protocol.Manifest) ([]Decision, error) {
	out := make([]Decision, len(m.Artifacts))
	head := transcriptIndex(m.Artifacts)
	for i, a := range m.Artifacts {
		cur, ok, err := currentDigest(ctx, tx, uid, a.RelPath)
		if err != nil {
			return nil, err
		}
		if !ok {
			out[i] = Decision{Relation: protocol.RelationHead, Record: true, Head: i == head}
			continue
		}
		if cur == a.SHA256 {
			out[i] = Decision{Relation: protocol.RelationUnchanged}
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
			out[i] = Decision{Relation: protocol.RelationUnchanged}
		case protocol.RelationGrownFrom:
			out[i] = Decision{
				Relation:  protocol.RelationGrownFrom,
				GrownFrom: cur,
				Record:    true,
				Head:      i == head,
			}
		case protocol.RelationStale:
			out[i] = Decision{Relation: protocol.RelationStale}
		default:
			out[i] = Decision{Relation: protocol.RelationDivergentCopy, Record: true}
		}
	}
	return out, nil
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
		var got string
		q := `
			SELECT artifact_id FROM artifacts
			WHERE session_uid = ? AND relpath = ? AND sha256 = ?`
		args := []any{uid, a.RelPath, a.SHA256}
		if d.Relation == protocol.RelationStale {
			q = `
				SELECT artifact_id FROM artifacts
				WHERE session_uid = ? AND relpath = ? AND current = 1`
			args = []any{uid, a.RelPath}
		}
		err := tx.QueryRowContext(ctx, q, args...).Scan(&got)
		if err != nil {
			return "", fmt.Errorf("catalog: artifact %q: %w", a.RelPath, err)
		}
		return got, nil
	}
	if d.Relation == protocol.RelationHead || d.Relation == protocol.RelationGrownFrom {
		if _, err := tx.ExecContext(ctx, `
			UPDATE artifacts SET current = 0
			WHERE session_uid = ? AND relpath = ? AND current = 1`, uid, a.RelPath); err != nil {
			return "", fmt.Errorf("catalog: artifact: %w", err)
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

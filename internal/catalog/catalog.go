// Package catalog is the session index. SQLite is the MVP store; a move
// to Postgres is a later problem and is not sketched here beyond the
// choice of a boring SQL schema.
//
// Identity is (harness, native_session_id). A second machine posting the
// same native id joins that row and adds a provenance record. Divergent
// copies — same id, bytes that are not a prefix — are not detected yet.
// Both artifact digests are kept.
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
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
CREATE TABLE IF NOT EXISTS provenance (
    session_uid TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    PRIMARY KEY (session_uid, machine_id)
);
CREATE TABLE IF NOT EXISTS artifacts (
    artifact_id TEXT PRIMARY KEY,
    session_uid TEXT NOT NULL,
    kind TEXT NOT NULL,
    relpath TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size INTEGER NOT NULL,
    UNIQUE (session_uid, relpath, sha256)
);
`

// Close releases the database.
func (c *Catalog) Close() error {
	return c.db.Close()
}

// Ingest records m and returns the stable session uid. Repeating the same
// manifest returns the same ids. A new digest for an existing relpath adds
// an artifact and moves head_sha256; the previous blob stays in the CAS.
func (c *Catalog) Ingest(ctx context.Context, m protocol.Manifest, now time.Time) (protocol.ManifestAck, error) {
	if m.CaptureProtocol != protocol.Version {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: capture_protocol %d", m.CaptureProtocol)
	}
	if m.MachineID == "" || m.Harness == "" || m.NativeSessionID == "" {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: machine_id, harness, and native_session_id are required")
	}
	if len(m.Artifacts) == 0 {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: manifest has no artifacts")
	}
	for _, a := range m.Artifacts {
		if !protocol.ValidDigest(a.SHA256) {
			return protocol.ManifestAck{}, fmt.Errorf("catalog: artifact %q has an invalid sha256", a.RelPath)
		}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return protocol.ManifestAck{}, err
	}
	head := headSHA(m.Artifacts)
	uid, err := id.New(now)
	if err != nil {
		return protocol.ManifestAck{}, err
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.ManifestAck{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var sessionUID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO sessions (session_uid, harness, native_session_id, head_sha256, manifest_json, ingested_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (harness, native_session_id) DO UPDATE SET
			head_sha256 = excluded.head_sha256,
			manifest_json = excluded.manifest_json,
			ingested_at = excluded.ingested_at
		RETURNING session_uid`,
		uid, m.Harness, m.NativeSessionID, head, string(raw), now.UTC().Format(time.RFC3339Nano),
	).Scan(&sessionUID)
	if err != nil {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: session: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provenance (session_uid, machine_id) VALUES (?, ?)
		ON CONFLICT DO NOTHING`, sessionUID, m.MachineID); err != nil {
		return protocol.ManifestAck{}, fmt.Errorf("catalog: provenance: %w", err)
	}

	ack := protocol.ManifestAck{
		SessionUID:  sessionUID,
		HeadSHA256:  head,
		ArtifactIDs: make([]string, 0, len(m.Artifacts)),
	}
	for _, a := range m.Artifacts {
		aid, err := id.New(now)
		if err != nil {
			return protocol.ManifestAck{}, err
		}
		var got string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO artifacts (artifact_id, session_uid, kind, relpath, sha256, size)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (session_uid, relpath, sha256) DO UPDATE SET
				size = excluded.size
			RETURNING artifact_id`,
			aid, sessionUID, a.Kind, a.RelPath, a.SHA256, a.Size,
		).Scan(&got)
		if err != nil {
			return protocol.ManifestAck{}, fmt.Errorf("catalog: artifact: %w", err)
		}
		ack.ArtifactIDs = append(ack.ArtifactIDs, got)
	}
	if err := tx.Commit(); err != nil {
		return protocol.ManifestAck{}, err
	}
	return ack, nil
}

func headSHA(arts []protocol.Artifact) string {
	last := arts[len(arts)-1].SHA256
	for _, a := range arts {
		if a.Kind == protocol.KindTranscriptJSONL {
			return a.SHA256
		}
	}
	return last
}

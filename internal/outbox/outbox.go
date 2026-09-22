// Package outbox is the durable queue of blob digests and manifest
// versions shared by the agent and by sync.
//
// Both open the same file (see File). A row stays pending until Ack.
// Closing the database without Ack — crash, sleep, reboot — leaves the
// row in place for the next Open. Ack deletes it. A second Ack is a
// no-op, so a retry after the server has already accepted the upload
// does not resurrect the work.
//
// Identity groups versions of one piece of work. A higher Version
// replaces the pending digest and manifest. An equal or lower Version
// leaves the pending row alone.
package outbox

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/protocol"

	_ "modernc.org/sqlite"
)

// Item is one pending upload. Digest may be empty when only a manifest
// is waiting. Manifest may be empty when only a blob is waiting.
type Item struct {
	ID       int64
	Identity string
	Digest   string
	Manifest []byte
	Version  int64
}

// Queue is the disk-backed outbox.
type Queue interface {
	Enqueue(ctx context.Context, item Item) error
	Pending(ctx context.Context) ([]Item, error)
	Ack(ctx context.Context, item Item) error
}

// DB is one SQLite file.
type DB struct {
	db *sql.DB
}

var _ Queue = (*DB)(nil)

// File is the outbox path inside a lampi state directory.
func File(stateDir string) string {
	return filepath.Join(stateDir, "outbox.db")
}

// Open creates the queue. The file is owner-read: the rows name
// private transcripts. Re-opening the same path resumes pending work.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("outbox: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("outbox: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("outbox: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("outbox: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("outbox: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("outbox: %w", err)
	}
	return &DB{db: db}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS items (
    id INTEGER PRIMARY KEY,
    identity TEXT NOT NULL UNIQUE,
    digest TEXT NOT NULL DEFAULT '',
    manifest BLOB,
    version INTEGER NOT NULL DEFAULT 0,
    enqueued_at TEXT NOT NULL
);
`

// Close releases the database.
func (q *DB) Close() error {
	return q.db.Close()
}

// Enqueue records item. The same Identity at an equal or lower Version
// is a no-op. A higher Version replaces the pending body.
func (q *DB) Enqueue(ctx context.Context, item Item) error {
	ident, err := identityOf(item)
	if err != nil {
		return err
	}
	if item.Digest != "" && !protocol.ValidDigest(item.Digest) {
		return fmt.Errorf("outbox: invalid digest %q", item.Digest)
	}
	var manifest any
	if len(item.Manifest) > 0 {
		manifest = item.Manifest
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var curVer int64
	err = tx.QueryRowContext(ctx, `SELECT version FROM items WHERE identity = ?`, ident).Scan(&curVer)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO items (identity, digest, manifest, version, enqueued_at)
			VALUES (?, ?, ?, ?, ?)`,
			ident, item.Digest, manifest, item.Version, now); err != nil {
			return fmt.Errorf("outbox: %w", err)
		}
	case err != nil:
		return fmt.Errorf("outbox: %w", err)
	case item.Version > curVer:
		if _, err := tx.ExecContext(ctx, `
			UPDATE items
			SET digest = ?, manifest = ?, version = ?, enqueued_at = ?
			WHERE identity = ?`,
			item.Digest, manifest, item.Version, now, ident); err != nil {
			return fmt.Errorf("outbox: %w", err)
		}
	default:
		// Equal or older generation: the pending row is already the work.
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	return nil
}

// Pending returns queued work in id order. A row is pending until Ack,
// including one this process already tried to upload.
func (q *DB) Pending(ctx context.Context) ([]Item, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, identity, digest, manifest, version
		FROM items ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("outbox: %w", err)
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Identity, &it.Digest, &it.Manifest, &it.Version); err != nil {
			return nil, fmt.Errorf("outbox: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox: %w", err)
	}
	return out, nil
}

// Ack drops item. Missing rows are success: the server ACK was already
// applied, or a peer dequeued it. ID wins when set; otherwise the
// identity (or the identity derived from the digest and manifest) is
// the key.
func (q *DB) Ack(ctx context.Context, item Item) error {
	var err error
	if item.ID != 0 {
		_, err = q.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, item.ID)
	} else {
		ident, idErr := identityOf(item)
		if idErr != nil {
			return nil
		}
		_, err = q.db.ExecContext(ctx, `DELETE FROM items WHERE identity = ?`, ident)
	}
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	return nil
}

func identityOf(item Item) (string, error) {
	if item.Identity != "" {
		return item.Identity, nil
	}
	switch {
	case item.Digest != "" && len(item.Manifest) == 0:
		return "blob:" + item.Digest, nil
	case item.Digest == "" && len(item.Manifest) > 0:
		sum := sha256.Sum256(item.Manifest)
		return "manifest:" + hex.EncodeToString(sum[:]), nil
	case item.Digest != "" && len(item.Manifest) > 0:
		sum := sha256.Sum256(item.Manifest)
		return "work:" + item.Digest + ":" + hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf("outbox: item has no digest or manifest")
	}
}

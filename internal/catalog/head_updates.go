package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// headUpdatesSinceKey is the lake_meta row holding when head_updates
// began recording. Buckets before it were not measured.
const headUpdatesSinceKey = "head_updates_since"

// migrateHeadUpdates adds head_updates, one row per accepted change of a
// session head, written in the transaction that moves the head. It is
// prospective: sessions already stored get no invented rows, and
// lake_meta records when recording began. The migration has no clock
// argument, so that time is SQLite's.
//
// There is no uniqueness on digests. A snapshot rewritten back to an
// earlier export is a real head change and gets its own row. An
// unchanged or stale repost moves no head and writes no row, which is
// what keeps retries from counting twice.
//
// Sizes are logical sizes of the head artifact, not network bytes and
// not CAS disk use. old_size is stored rather than looked up later
// because purge and later rewrites change the rows it would come from.
// The time indexes carry both sizes so a bucketed sum reads the index
// alone.
func migrateHeadUpdates(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE head_updates (
		update_id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_uid TEXT NOT NULL,
		machine_id TEXT NOT NULL,
		harness TEXT NOT NULL,
		received_ns INTEGER NOT NULL,
		old_sha256 TEXT NOT NULL,
		new_sha256 TEXT NOT NULL,
		old_size INTEGER NOT NULL,
		new_size INTEGER NOT NULL,
		relation TEXT NOT NULL
	);
	CREATE INDEX head_updates_time ON head_updates(received_ns, old_size, new_size);
	CREATE INDEX head_updates_harness_time ON head_updates(harness, received_ns, old_size, new_size);
	CREATE INDEX head_updates_session ON head_updates(session_uid);
	INSERT INTO lake_meta(key, value) VALUES('` + headUpdatesSinceKey + `', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`)
	return err
}

// headUpdate is one accepted head change. oldSHA is empty and oldSize
// zero for a new session.
type headUpdate struct {
	uid, machine, harness string
	received              time.Time
	oldSHA, newSHA        string
	oldSize, newSize      int64
	relation              string
}

func recordHeadUpdate(ctx context.Context, tx *sql.Tx, u headUpdate) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO head_updates (session_uid, machine_id, harness, received_ns, old_sha256, new_sha256, old_size, new_size, relation)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.uid, u.machine, u.harness, u.received.UnixNano(), u.oldSHA, u.newSHA, u.oldSize, u.newSize, u.relation)
	if err != nil {
		return fmt.Errorf("catalog: head update: %w", err)
	}
	return nil
}

// headSize is the logical size of a session's artifact with digest sha.
// Rows sharing a digest share its bytes, so any one of them will do.
func headSize(ctx context.Context, tx *sql.Tx, uid, sha string) (int64, error) {
	var n int64
	if err := tx.QueryRowContext(ctx, `
		SELECT size FROM artifacts
		WHERE session_uid = ? AND sha256 = ?
		ORDER BY current DESC, size DESC LIMIT 1`, uid, sha).Scan(&n); err != nil {
		return 0, fmt.Errorf("catalog: head size: %w", err)
	}
	return n, nil
}

// HeadUpdatesSince is when this catalog began recording head updates.
// ok is false for a catalog that has not recorded it, such as a
// read-only open of a file from before schema 8.
func (c *Catalog) HeadUpdatesSince(ctx context.Context) (time.Time, bool, error) {
	var raw string
	err := c.db.QueryRowContext(ctx, `SELECT value FROM lake_meta WHERE key = ?`, headUpdatesSinceKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || err != nil && strings.Contains(err.Error(), "no such table: lake_meta") {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("catalog: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("catalog: %s: %w", headUpdatesSinceKey, err)
	}
	return t.UTC(), true, nil
}

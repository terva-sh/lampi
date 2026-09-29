package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/protocol"
)

// A conflict is an artifact stored as divergent_copy. Its relation
// stays the record of how the bytes compared. A resolution records what
// a person or the lake decided about it, and a resolved conflict leaves
// the default lists (TKT-01M3PTMKG9). No resolution deletes bytes.
const (
	// ResolutionKeptHead: an operator kept the session's head.
	ResolutionKeptHead = "kept_head"
	// ResolutionMadeHead: an operator made this copy the session's head.
	ResolutionMadeHead = "made_head"
	// ResolutionSuperseded: a later copy that extends this one was made
	// the head.
	ResolutionSuperseded = "superseded"
	// ResolutionNotAConflict: the lake compared the copy with another
	// file, as TKT-01M3M5VEQ did with a Claude subagent transcript.
	ResolutionNotAConflict = "not_a_conflict"
)

var (
	ErrNoConflict       = errors.New("catalog: no such conflict")
	ErrConflictResolved = errors.New("catalog: conflict is already resolved")
	ErrConflictOpen     = errors.New("catalog: conflict is not resolved")
)

// ValidResolution reports whether r is a resolution ResolveConflict
// records on its own. made_head and superseded say the head moved, so
// only the operation that moves it records them, in its transaction.
func ValidResolution(r string) bool {
	return r == ResolutionKeptHead || r == ResolutionNotAConflict
}

// Resolution is what was decided about one conflict.
type Resolution struct {
	Resolution string
	At         time.Time
	By         string
	Note       string
}

// unresolvedSQL matches a divergent_copy artifact a with no resolution.
const unresolvedSQL = `NOT EXISTS (SELECT 1 FROM conflict_resolutions r WHERE r.artifact_id = a.artifact_id)`

// migrateConflictResolutions adds conflict_resolutions and resolves the
// divergent copies TKT-01M3M5VEQ left. Before that fix, a new Claude
// subagent file with no current row at its path was related to the
// session's own transcript as if the transcript had moved, and every
// growth of the file was stored again as a divergent copy of it.
// migrateSubagentHeads made the newest current and left the rest.
//
// A copy qualifies when its path is a companion of the session head's,
// under the directory named for the head's file, and no earlier
// artifact at its session and path is current or anything but a
// divergent copy: it had nothing at its own path to diverge from, so it
// was compared with another file. migrateSubagentHeads relabelled the
// copy it made current as head, and an earlier current row counts
// whatever its relation. A companion that diverged from an earlier row
// at its own path is a real conflict and stays open.
func migrateConflictResolutions(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE conflict_resolutions (
		artifact_id TEXT PRIMARY KEY,
		session_uid TEXT NOT NULL,
		resolution TEXT NOT NULL,
		resolved_at TEXT NOT NULL,
		resolved_by TEXT NOT NULL,
		note TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX conflict_resolutions_session ON conflict_resolutions(session_uid)`); err != nil {
		return err
	}
	heads := map[string]string{}
	rows, err := tx.Query(`
		SELECT s.session_uid, a.relpath
		FROM sessions s JOIN artifacts a
		  ON a.session_uid = s.session_uid AND a.sha256 = s.head_sha256 AND a.current = 1`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var uid, rel string
		if err := rows.Scan(&uid, &rel); err != nil {
			rows.Close()
			return err
		}
		// A digest current at two paths: the one outside the other's
		// directory is the head, as in migrateSubagentHeads.
		if prev, ok := heads[uid]; !ok || companion(prev, rel) {
			heads[uid] = rel
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	type copyRow struct{ id, uid, rel string }
	var leftovers []copyRow
	rows, err = tx.Query(`
		SELECT a.artifact_id, a.session_uid, a.relpath FROM artifacts a
		WHERE a.relation = 'divergent_copy'
		  AND NOT EXISTS (SELECT 1 FROM artifacts b
		                  WHERE b.session_uid = a.session_uid AND b.relpath = a.relpath
		                    AND b.artifact_id < a.artifact_id AND (b.relation <> 'divergent_copy' OR b.current = 1))
		ORDER BY a.artifact_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c copyRow
		if err := rows.Scan(&c.id, &c.uid, &c.rel); err != nil {
			rows.Close()
			return err
		}
		if head, ok := heads[c.uid]; ok && companion(c.rel, head) {
			leftovers = append(leftovers, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	const note = "compared with the session's own transcript before TKT-01M3M5VEQ"
	for _, c := range leftovers {
		if _, err := tx.Exec(`INSERT INTO conflict_resolutions(artifact_id, session_uid, resolution, resolved_at, resolved_by, note) VALUES(?,?,?,?,?,?)`,
			c.id, c.uid, ResolutionNotAConflict, stamp(now), migrationActor, note); err != nil {
			return err
		}
		if err := queueAudit(context.Background(), tx, now, audit.Event{Kind: audit.ConflictResolved, Actor: migrationActor, Detail: conflictDetail(c.id, c.uid, ResolutionNotAConflict)}); err != nil {
			return err
		}
	}
	return nil
}

// migrationActor names the catalog migration in a resolution and its
// audit event.
const migrationActor = "catalog migration"

func conflictDetail(artifactID, uid, resolution string) string {
	d := fmt.Sprintf("artifact=%s session=%s", artifactID, uid)
	if resolution != "" {
		d += " resolution=" + resolution
	}
	return d
}

// conflictSession returns the session of the divergent copy artifactID,
// or ErrNoConflict when the artifact is not one.
func conflictSession(ctx context.Context, tx *sql.Tx, artifactID string) (string, error) {
	var uid string
	err := tx.QueryRowContext(ctx, `SELECT session_uid FROM artifacts WHERE artifact_id = ? AND relation = ?`, artifactID, protocol.RelationDivergentCopy).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: %s", ErrNoConflict, artifactID)
	}
	if err != nil {
		return "", fmt.Errorf("catalog: %w", err)
	}
	return uid, nil
}

// ResolveConflict records resolution, kept_head or not_a_conflict, for
// the divergent copy artifactID and queues its conflict.resolved event
// in the same transaction. A conflict already resolved is
// ErrConflictResolved and nothing changes.
func (c *Catalog) ResolveConflict(ctx context.Context, artifactID, resolution, by, note string, now time.Time) error {
	if !ValidResolution(resolution) {
		return fmt.Errorf("catalog: unknown resolution %q", resolution)
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	if err := resolveConflict(ctx, tx, artifactID, resolution, by, note, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

func resolveConflict(ctx context.Context, tx *sql.Tx, artifactID, resolution, by, note string, now time.Time) error {
	uid, err := conflictSession(ctx, tx, artifactID)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO conflict_resolutions(artifact_id, session_uid, resolution, resolved_at, resolved_by, note) VALUES(?,?,?,?,?,?) ON CONFLICT(artifact_id) DO NOTHING`,
		artifactID, uid, resolution, stamp(now), by, note)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("catalog: %w", err)
	} else if n == 0 {
		return ErrConflictResolved
	}
	return queueAudit(ctx, tx, now, audit.Event{Kind: audit.ConflictResolved, Actor: by, Detail: conflictDetail(artifactID, uid, resolution)})
}

// ReopenConflict removes the resolution of the divergent copy
// artifactID and queues its conflict.reopened event in the same
// transaction. It does not move a head a resolution moved. A conflict
// with no resolution is ErrConflictOpen.
func (c *Catalog) ReopenConflict(ctx context.Context, artifactID, by string, now time.Time) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	uid, err := conflictSession(ctx, tx, artifactID)
	if err != nil {
		return err
	}
	var was string
	err = tx.QueryRowContext(ctx, `DELETE FROM conflict_resolutions WHERE artifact_id = ? RETURNING resolution`, artifactID).Scan(&was)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflictOpen
	}
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.ConflictReopened, Actor: by, Detail: conflictDetail(artifactID, uid, "") + " was=" + was}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

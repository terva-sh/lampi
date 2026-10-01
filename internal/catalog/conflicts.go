package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
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
	// ErrConflictIsHead: the copy is its session's head now, so it
	// cannot be reopened as a conflict with that head.
	ErrConflictIsHead = errors.New("catalog: the copy is the session head")
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
// A copy qualifies when it is a Claude transcript_jsonl under the
// subagents directory of the session head's file, the only shape that
// bug produced, and no earlier
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
		JOIN sessions s ON s.session_uid = a.session_uid
		WHERE a.relation = 'divergent_copy' AND s.harness = ? AND a.kind = ?
		  AND NOT EXISTS (SELECT 1 FROM artifacts b
		                  WHERE b.session_uid = a.session_uid AND b.relpath = a.relpath
		                    AND b.artifact_id < a.artifact_id AND (b.relation <> 'divergent_copy' OR b.current = 1))
		ORDER BY a.artifact_id`, protocol.HarnessClaude, protocol.KindTranscriptJSONL)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c copyRow
		if err := rows.Scan(&c.id, &c.uid, &c.rel); err != nil {
			rows.Close()
			return err
		}
		if head, ok := heads[c.uid]; ok && subagentOf(c.rel, head) {
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

// subagentOf reports whether rel is a Claude subagent transcript of the
// session file head: <head without .jsonl>/subagents/NAME.
func subagentOf(rel, head string) bool {
	stem := strings.TrimSuffix(path.Clean(head), path.Ext(head))
	name, ok := strings.CutPrefix(path.Clean(rel), stem+"/subagents/")
	return ok && name != "" && !strings.Contains(name, "/")
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
// transaction. It does not move a head a resolution moved, so a copy
// that is its session's head now is ErrConflictIsHead. A conflict with
// no resolution is ErrConflictOpen.
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
	var isHead bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM artifacts a JOIN sessions s ON s.session_uid = a.session_uid
		WHERE a.artifact_id = ? AND a.current = 1 AND a.sha256 = s.head_sha256)`, artifactID).Scan(&isHead); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if isHead {
		return ErrConflictIsHead
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

var (
	// ErrHeadMoved: the session's head is not the one the caller saw.
	ErrHeadMoved = errors.New("catalog: the session head moved")
	// ErrNotHeadCandidate: the copy cannot be a head: its kind carries
	// none, it is another kind than the head, or it is a companion of
	// the head, such as a Claude subagent transcript.
	ErrNotHeadCandidate = errors.New("catalog: this copy cannot be the session head")
)

// MadeHead is what MakeConflictHead changed.
type MadeHead struct {
	SessionUID string
	OldHead    string
	// Superseded is the other open copies at the copy's path whose bytes
	// the new head extends, now resolved as superseded.
	Superseded []string
}

// MakeConflictHead makes the open divergent copy artifactID the head of
// its session, when the session's head is still expectHead. The copy
// becomes the current artifact at its path, and the row that held the
// head, at that path or another, stops being current, as a moved file
// does on ingest. Its relation stays divergent_copy; it is resolved as
// made_head. Every other open copy at the same path whose bytes the new
// head extends, read through blobs, is resolved as superseded. A
// head_updates row records the change, attributed to a machine that
// posted the copy at its path, and the session is queued for normalization. The
// head's bytes stay stored. Everything, with one audit event per
// resolution, commits in one transaction (TKT-01M3PTMWM9).
func (c *Catalog) MakeConflictHead(ctx context.Context, blobs BlobReader, artifactID, expectHead, by, note string, now time.Time) (MadeHead, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	var cp ArtifactRow
	var size int64
	err = tx.QueryRowContext(ctx, `SELECT session_uid, kind, relpath, sha256, size FROM artifacts WHERE artifact_id = ? AND relation = ?`,
		artifactID, protocol.RelationDivergentCopy).Scan(&cp.SessionUID, &cp.Kind, &cp.RelPath, &cp.SHA256, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return MadeHead{}, fmt.Errorf("%w: %s", ErrNoConflict, artifactID)
	}
	if err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	var resolved bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM conflict_resolutions WHERE artifact_id = ?)`, artifactID).Scan(&resolved); err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	if resolved {
		return MadeHead{}, ErrConflictResolved
	}
	uid := cp.SessionUID
	var head, harness string
	if err := tx.QueryRowContext(ctx, `SELECT head_sha256, harness FROM sessions WHERE session_uid = ?`, uid).Scan(&head, &harness); err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	if head != expectHead {
		return MadeHead{}, ErrHeadMoved
	}
	row, found, err := headRow(ctx, tx, uid, head)
	if err != nil {
		return MadeHead{}, err
	}
	if !headBearing(cp.Kind) || found && (row.Kind != cp.Kind || companion(cp.RelPath, row.RelPath)) {
		return MadeHead{}, ErrNotHeadCandidate
	}
	oldSize, err := headSize(ctx, tx, uid, head)
	if err != nil {
		return MadeHead{}, err
	}
	for _, rel := range []string{cp.RelPath, row.RelPath} {
		if rel == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE artifacts SET current = 0 WHERE session_uid = ? AND relpath = ? AND current = 1`, uid, rel); err != nil {
			return MadeHead{}, fmt.Errorf("catalog: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE artifacts SET current = 1 WHERE artifact_id = ?`, artifactID); err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET head_sha256 = ? WHERE session_uid = ?`, cp.SHA256, uid); err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	out := MadeHead{SessionUID: uid, OldHead: head}
	detail := conflictDetail(artifactID, uid, ResolutionMadeHead) + " old_head=" + head
	if err := resolveConflict(ctx, tx, artifactID, ResolutionMadeHead, by, note, now); err != nil {
		return MadeHead{}, err
	}
	// The event resolveConflict queued names the resolution; this one
	// names the head it replaced.
	if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.ConflictHeadChanged, Actor: by, Detail: detail}); err != nil {
		return MadeHead{}, err
	}

	type other struct{ id, sha string }
	var others []other
	rows, err := tx.QueryContext(ctx, `SELECT a.artifact_id, a.sha256 FROM artifacts a
		WHERE a.session_uid = ? AND a.relpath = ? AND a.relation = 'divergent_copy' AND a.artifact_id <> ? AND `+unresolvedSQL+`
		ORDER BY a.artifact_id`, uid, cp.RelPath, artifactID)
	if err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	for rows.Next() {
		var o other
		if err := rows.Scan(&o.id, &o.sha); err != nil {
			rows.Close()
			return MadeHead{}, fmt.Errorf("catalog: %w", err)
		}
		others = append(others, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	for _, o := range others {
		rel, err := Relate(blobs, o.sha, cp.SHA256)
		if err != nil {
			return MadeHead{}, fmt.Errorf("catalog: conflict %s: %w", o.id, err)
		}
		if rel != protocol.RelationGrownFrom && rel != protocol.RelationUnchanged {
			continue
		}
		if err := resolveConflict(ctx, tx, o.id, ResolutionSuperseded, by, "extended by "+artifactID, now); err != nil {
			return MadeHead{}, err
		}
		out.Superseded = append(out.Superseded, o.id)
	}

	var machine string
	// A machine that posted these bytes at this path.
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(machine_id), '') FROM provenance WHERE session_uid = ? AND sha256 = ? AND relpath = ?`, uid, cp.SHA256, cp.RelPath).Scan(&machine); err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	if err := recordHeadUpdate(ctx, tx, headUpdate{uid: uid, machine: machine, harness: harness, received: now,
		oldSHA: head, newSHA: cp.SHA256, oldSize: oldSize, newSize: size, relation: ResolutionMadeHead}); err != nil {
		return MadeHead{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET normalize_gen = normalize_gen + 1 WHERE session_uid = ?`, uid); err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO normalize_jobs (session_uid, gen, enqueued_at)
		SELECT session_uid, normalize_gen, ? FROM sessions WHERE session_uid = ?
		ON CONFLICT(session_uid) DO UPDATE
		SET gen = excluded.gen, enqueued_at = excluded.enqueued_at`, now.UTC().Format(time.RFC3339Nano), uid); err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return MadeHead{}, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

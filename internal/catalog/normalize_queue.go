package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// NormalizeJob is one session waiting for a worker. Gen is the
// sessions.normalize_gen value captured at enqueue. A publish for an
// older gen is stale: a later ingest has already replaced the job.
// Attempt counts failed publishes in this process. It is not a column.
type NormalizeJob struct {
	SessionUID string
	Gen        int64
	Attempt    int
}

// EnqueueNormalize bumps the session generation and upserts the job.
// The returned gen is the one a worker must publish. A second call
// before the first worker finishes replaces the row, so the queue
// holds the newest ingest only.
func (c *Catalog) EnqueueNormalize(ctx context.Context, sessionUID string, now time.Time) (int64, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		UPDATE sessions SET normalize_gen = normalize_gen + 1 WHERE session_uid = ?`, sessionUID)
	if err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	if n == 0 {
		return 0, fmt.Errorf("catalog: unknown session %s", sessionUID)
	}
	var gen int64
	if err := tx.QueryRowContext(ctx, `
		SELECT normalize_gen FROM sessions WHERE session_uid = ?`, sessionUID).Scan(&gen); err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO normalize_jobs (session_uid, gen, enqueued_at)
		VALUES (?, ?, ?)
		ON CONFLICT(session_uid) DO UPDATE
		SET gen = excluded.gen, enqueued_at = excluded.enqueued_at`,
		sessionUID, gen, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	return gen, nil
}

// ListNormalizeJobs returns queued work, oldest enqueue first. Open
// loads this after a restart. The row stays until a matching gen is
// published, so a crash does not drop the session.
func (c *Catalog) ListNormalizeJobs(ctx context.Context) ([]NormalizeJob, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT session_uid, gen FROM normalize_jobs
		ORDER BY enqueued_at, session_uid`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []NormalizeJob
	for rows.Next() {
		var job NormalizeJob
		if err := rows.Scan(&job.SessionUID, &job.Gen); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

// NormalizeGen is the session's current generation. ok is false when
// the session does not exist.
func (c *Catalog) NormalizeGen(ctx context.Context, sessionUID string) (int64, bool, error) {
	var gen int64
	err := c.db.QueryRowContext(ctx, `
		SELECT normalize_gen FROM sessions WHERE session_uid = ?`, sessionUID).Scan(&gen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("catalog: %w", err)
	}
	return gen, true, nil
}

// DeleteNormalizeJob removes the job when gen is still the queued
// generation. A newer enqueue changes gen and is left in place.
func (c *Catalog) DeleteNormalizeJob(ctx context.Context, sessionUID string, gen int64) error {
	if _, err := c.db.ExecContext(ctx, `
		DELETE FROM normalize_jobs WHERE session_uid = ? AND gen = ?`, sessionUID, gen); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

// Session returns one catalog session. ok is false when the uid is
// not stored.
func (c *Catalog) Session(ctx context.Context, sessionUID string) (SessionInfo, bool, error) {
	list, err := c.listSessions(ctx, `
		SELECT session_uid, harness, native_session_id, COALESCE(normalize_error, ''), COALESCE(project_id, ''), manifest_json
		FROM sessions
		WHERE session_uid = ?`, sessionUID)
	if err != nil {
		return SessionInfo{}, false, err
	}
	if len(list) == 0 {
		return SessionInfo{}, false, nil
	}
	return list[0], true, nil
}

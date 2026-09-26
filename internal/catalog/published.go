package catalog

import (
	"context"
	"database/sql"
	"errors"
)

func migratePublished(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE sessions ADD COLUMN published_gen INTEGER;
	ALTER TABLE sessions ADD COLUMN published_head TEXT;`)
	return err
}

// NormalizeVersion binds projection to both the queued generation and its head.
// Ingest commits before enqueue: the head also prevents a false ready state in
// that short interval, including an ingest concurrent with an old publish.
func (c *Catalog) NormalizeVersion(ctx context.Context, uid string) (gen int64, head string, ok bool, err error) {
	err = c.db.QueryRowContext(ctx, `SELECT normalize_gen, head_sha256 FROM sessions WHERE session_uid=?`, uid).Scan(&gen, &head)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", false, nil
	}
	return gen, head, err == nil, err
}

// MarkPublished is called only after every derived file has been published.
// Old workers cannot mark a changed generation or head as ready.
func (c *Catalog) MarkPublished(ctx context.Context, uid string, gen int64, head string) error {
	_, err := c.db.ExecContext(ctx, `UPDATE sessions SET published_gen=?, published_head=?, normalize_error=NULL
	WHERE session_uid=? AND normalize_gen=? AND head_sha256=?`, gen, head, uid, gen, head)
	return err
}

const normalizationStateSQL = `CASE
	WHEN EXISTS(SELECT 1 FROM normalize_jobs j WHERE j.session_uid=s.session_uid) THEN 'pending'
	WHEN COALESCE(s.normalize_error,'')!='' THEN 'failed'
	WHEN s.published_gen=s.normalize_gen AND s.published_head=s.head_sha256 THEN 'ready'
	ELSE 'unknown' END`

func (c *Catalog) NormalizationState(ctx context.Context, uid string) (string, error) {
	var state string
	err := c.db.QueryRowContext(ctx, `SELECT `+normalizationStateSQL+` FROM sessions s WHERE s.session_uid=?`, uid).Scan(&state)
	return state, err
}

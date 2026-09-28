package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// SessionsInNormalizationState lists the UIDs of the sessions whose
// state (pending, failed, ready or unknown, as the dashboard counts
// them) is state, oldest first. serve normalize uses it to requeue.
func (c *Catalog) SessionsInNormalizationState(ctx context.Context, state string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT s.session_uid FROM sessions s
	WHERE (`+normalizationStateSQL+`) = ? ORDER BY s.ingested_at, s.session_uid`, state)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

// NormalizationCounts is how many sessions are in each normalization
// state. Every state is present, zero when no session is in it.
func (c *Catalog) NormalizationCounts(ctx context.Context) (map[string]int, error) {
	out := map[string]int{"pending": 0, "failed": 0, "ready": 0, "unknown": 0}
	rows, err := c.db.QueryContext(ctx, `SELECT `+normalizationStateSQL+`, COUNT(*) FROM sessions s GROUP BY 1`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out[state] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

// NormalizationStates is every session's UID and normalization state,
// oldest ingest first.
func (c *Catalog) NormalizationStates(ctx context.Context) (uids, states []string, err error) {
	rows, err := c.db.QueryContext(ctx, `SELECT s.session_uid, `+normalizationStateSQL+` FROM sessions s
	ORDER BY s.ingested_at, s.session_uid`)
	if err != nil {
		return nil, nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var uid, state string
		if err := rows.Scan(&uid, &state); err != nil {
			return nil, nil, fmt.Errorf("catalog: %w", err)
		}
		uids, states = append(uids, uid), append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("catalog: %w", err)
	}
	return uids, states, nil
}

func (c *Catalog) NormalizationState(ctx context.Context, uid string) (string, error) {
	var state string
	err := c.db.QueryRowContext(ctx, `SELECT `+normalizationStateSQL+` FROM sessions s WHERE s.session_uid=?`, uid).Scan(&state)
	return state, err
}

// Publication is what a reader of the derived files needs: the
// normalization state and, when ready, the generation and head that
// the published JSONL holds.
type Publication struct {
	State string
	Gen   int64
	Head  string
}

// Publication reads the publication state of uid. An unknown uid is
// sql.ErrNoRows. Gen and Head are zero unless State is ready.
func (c *Catalog) Publication(ctx context.Context, uid string) (Publication, error) {
	var p Publication
	var gen sql.NullInt64
	var head sql.NullString
	err := c.db.QueryRowContext(ctx, `SELECT `+normalizationStateSQL+`,s.published_gen,s.published_head FROM sessions s WHERE s.session_uid=?`, uid).Scan(&p.State, &gen, &head)
	if err != nil {
		return Publication{}, err
	}
	if p.State == "ready" {
		p.Gen, p.Head = gen.Int64, head.String
	}
	return p, nil
}

// PublishedSession is one session's publication, for a reader that
// keeps its own derived view in step with the catalog.
type PublishedSession struct {
	UID       string
	State     string
	Gen       int64
	Head      string
	Harness   string
	ProjectID string
}

// PublishedSessions lists every session with its publication state.
// Gen and Head are set only when State is ready. The rows are read
// into memory so the caller can use the catalog while it walks them.
func (c *Catalog) PublishedSessions(ctx context.Context) ([]PublishedSession, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT s.session_uid,`+normalizationStateSQL+`,s.published_gen,s.published_head,s.harness,s.project_id FROM sessions s ORDER BY s.session_uid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishedSession
	for rows.Next() {
		var p PublishedSession
		var gen sql.NullInt64
		var head sql.NullString
		if err := rows.Scan(&p.UID, &p.State, &gen, &head, &p.Harness, &p.ProjectID); err != nil {
			return nil, err
		}
		if p.State == "ready" {
			p.Gen, p.Head = gen.Int64, head.String
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

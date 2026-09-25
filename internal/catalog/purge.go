package catalog

import (
	"context"
	"fmt"
)

// OtherDigests returns every artifact digest stored for a session other
// than sessionUID. Purge keeps these blobs.
func (c *Catalog) OtherDigests(ctx context.Context, sessionUID string) (map[string]bool, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT DISTINCT sha256 FROM artifacts WHERE session_uid <> ?`, sessionUID)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out[d] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

// DeleteSession removes the session and every row that names it:
// artifacts, provenance, aliases, and its normalize job. It does not
// touch the CAS or the derived files. ok is false when the session was
// not stored.
func (c *Catalog) DeleteSession(ctx context.Context, sessionUID string) (bool, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	for _, table := range []string{"artifacts", "provenance", "aliases", "normalize_jobs"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE session_uid = ?`, sessionUID); err != nil {
			return false, fmt.Errorf("catalog: %s: %w", table, err)
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE session_uid = ?`, sessionUID)
	if err != nil {
		return false, fmt.Errorf("catalog: sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	return n > 0, nil
}

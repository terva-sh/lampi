package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// migrateLakeMeta adds lake_meta, a key/value table for facts about the
// lake itself. The first is lake_id, recorded when the lake's identity
// is made, so a lost identity.json is refused instead of replaced.
func migrateLakeMeta(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE lake_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)
	return err
}

// LakeID is the recorded lake id, or "" when none is recorded.
func (c *Catalog) LakeID(ctx context.Context) (string, error) {
	var id string
	err := c.db.QueryRowContext(ctx, `SELECT value FROM lake_meta WHERE key='lake_id'`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	// A read-only open of a catalog from before schema 4 has no table.
	// That catalog has recorded nothing.
	if err != nil && strings.Contains(err.Error(), "no such table: lake_meta") {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("catalog: %w", err)
	}
	return id, nil
}

// RecordLakeID records id as the lake's id. It is written once: a
// second call with the same id does nothing, and one with another id
// is refused.
func (c *Catalog) RecordLakeID(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("catalog: empty lake id")
	}
	if _, err := c.db.ExecContext(ctx, `INSERT INTO lake_meta(key, value) VALUES('lake_id', ?) ON CONFLICT(key) DO NOTHING`, id); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	got, err := c.LakeID(ctx)
	if err != nil {
		return err
	}
	if got != id {
		return fmt.Errorf("catalog: lake id is already %s; refusing to record %s", got, id)
	}
	return nil
}

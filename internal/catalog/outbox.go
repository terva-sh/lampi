package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"terva.sh/lampi/internal/audit"
)

// migrateAuditOutbox adds audit_outbox, the audit events of catalog
// changes that are not in audit.jsonl yet. A change queues its events in
// its own transaction, so they commit or roll back with it, and
// FlushAudit appends them afterwards. An append that fails leaves the
// event queued for the next flush: a line can be written twice, after a
// crash between the append and the delete, but it is not lost.
func migrateAuditOutbox(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE audit_outbox (
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		event TEXT NOT NULL
	)`)
	return err
}

// queueAudit adds events to the outbox inside tx. An event with no time
// gets now.
func queueAudit(ctx context.Context, tx *sql.Tx, now time.Time, events ...audit.Event) error {
	for _, e := range events {
		if e.Time.IsZero() {
			e.Time = now
		}
		raw, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("catalog: audit event: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_outbox(event) VALUES(?)`, string(raw)); err != nil {
			return fmt.Errorf("catalog: %w", err)
		}
	}
	return nil
}

// QueueAudit adds events to the outbox on their own, for an event that
// records no catalog change, so it reaches the log in order with the
// events queued before it.
func (c *Catalog) QueueAudit(ctx context.Context, now time.Time, events ...audit.Event) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	if err := queueAudit(ctx, tx, now, events...); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

// FlushAudit appends the queued audit events to audit.jsonl in dir, in
// the order they were queued, removing each once it is written. It stops
// at the first append that fails and returns that error; the rest stay
// queued. Flushes in one process take turns, so they do not write one
// event twice.
func (c *Catalog) FlushAudit(ctx context.Context, dir string) error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	for {
		var seq int64
		var raw string
		err := c.db.QueryRowContext(ctx, `SELECT seq, event FROM audit_outbox ORDER BY seq LIMIT 1`).Scan(&seq, &raw)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("catalog: %w", err)
		}
		var e audit.Event
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return fmt.Errorf("catalog: audit_outbox %d: %w", seq, err)
		}
		if err := audit.Append(dir, e); err != nil {
			return err
		}
		if _, err := c.db.ExecContext(ctx, `DELETE FROM audit_outbox WHERE seq=?`, seq); err != nil {
			return fmt.Errorf("catalog: %w", err)
		}
	}
}

// PendingAudit counts the queued audit events.
func (c *Catalog) PendingAudit(ctx context.Context) (int, error) {
	var n int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_outbox`).Scan(&n); err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	return n, nil
}

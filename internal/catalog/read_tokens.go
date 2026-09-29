package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
)

// migrateReadTokens adds read_tokens, the bearer tokens an admin mints
// so that a tool without a browser session can read (TKT-01M3NM6FW7).
// Only the SHA-256 of a token is stored. permissions is a
// space-separated set, so that a later read scope (TKT-01M3KAMD1Z) adds
// a permission rather than a second token table. sessions is a JSON
// array of session UIDs the token may read, or ” for every session.
func migrateReadTokens(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE read_tokens (
		id TEXT PRIMARY KEY,
		label TEXT NOT NULL,
		token_sha256 TEXT NOT NULL UNIQUE,
		permissions TEXT NOT NULL,
		sessions TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		created_by TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		revoked_at TEXT,
		revoked_by TEXT,
		last_used_at TEXT
	)`)
	return err
}

// PermRawRead lets a read token fetch raw session artifacts.
const PermRawRead = "raw:read"

// MaxReadTokenSessions bounds a token's session list.
const MaxReadTokenSessions = 100

// ReadToken is one minted read token. It never holds the secret.
type ReadToken struct {
	ID          string
	Label       string
	Permissions []string
	// Sessions is the session UIDs the token may read. Empty is every
	// session in the lake.
	Sessions  []string
	Created   time.Time
	CreatedBy string
	Expires   time.Time
	Revoked   time.Time
	RevokedBy string
	LastUsed  time.Time
}

// State is active, expired, or revoked at now.
func (t ReadToken) State(now time.Time) string {
	switch {
	case !t.Revoked.IsZero():
		return "revoked"
	case !now.Before(t.Expires):
		return "expired"
	default:
		return "active"
	}
}

// Allows reports whether the token, active at now, holds perm for
// sessionUID.
func (t ReadToken) Allows(perm, sessionUID string, now time.Time) bool {
	if t.State(now) != "active" || !slices.Contains(t.Permissions, perm) {
		return false
	}
	return len(t.Sessions) == 0 || slices.Contains(t.Sessions, sessionUID)
}

var (
	ErrNoReadToken      = errors.New("catalog: no such read token")
	ErrReadTokenRevoked = errors.New("catalog: read token was already revoked")
)

const readTokenCols = `id, label, permissions, sessions, created_at, created_by, expires_at, COALESCE(revoked_at, ''), COALESCE(revoked_by, ''), COALESCE(last_used_at, '')`

func scanReadToken(row interface{ Scan(...any) error }) (ReadToken, error) {
	var t ReadToken
	var perms, sessions, created, expires, revoked, used string
	if err := row.Scan(&t.ID, &t.Label, &perms, &sessions, &created, &t.CreatedBy, &expires, &revoked, &t.RevokedBy, &used); err != nil {
		return ReadToken{}, err
	}
	t.Permissions = strings.Fields(perms)
	if sessions != "" {
		if err := json.Unmarshal([]byte(sessions), &t.Sessions); err != nil {
			return ReadToken{}, fmt.Errorf("read token %s sessions: %w", t.ID, err)
		}
	}
	t.Created, t.Expires, t.Revoked, t.LastUsed = parseStamp(created), parseStamp(expires), parseStamp(revoked), parseStamp(used)
	return t, nil
}

// CreateReadToken records a token whose secret hashes to secretSHA256
// and queues its read_token.created event in the same transaction. t's
// ID is assigned here; its Created is now.
func (c *Catalog) CreateReadToken(ctx context.Context, t ReadToken, secretSHA256 string, now time.Time) (ReadToken, error) {
	if len(t.Permissions) == 0 {
		return ReadToken{}, errors.New("catalog: a read token needs a permission")
	}
	if len(t.Sessions) > MaxReadTokenSessions {
		return ReadToken{}, fmt.Errorf("catalog: a read token names at most %d sessions", MaxReadTokenSessions)
	}
	id, err := newDeviceID()
	if err != nil {
		return ReadToken{}, err
	}
	t.ID = "rtk_" + strings.TrimPrefix(id, "dev_")
	t.Created, t.Expires = now.UTC(), t.Expires.UTC()
	sessions := ""
	if len(t.Sessions) > 0 {
		b, err := json.Marshal(t.Sessions)
		if err != nil {
			return ReadToken{}, err
		}
		sessions = string(b)
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return ReadToken{}, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO read_tokens(id, label, token_sha256, permissions, sessions, created_at, created_by, expires_at) VALUES(?,?,?,?,?,?,?,?)`,
		t.ID, t.Label, secretSHA256, strings.Join(t.Permissions, " "), sessions, stamp(t.Created), t.CreatedBy, stamp(t.Expires)); err != nil {
		return ReadToken{}, fmt.Errorf("catalog: %w", err)
	}
	if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.ReadTokenCreated, Actor: t.CreatedBy, Detail: readTokenDetail(t)}); err != nil {
		return ReadToken{}, err
	}
	if err := tx.Commit(); err != nil {
		return ReadToken{}, fmt.Errorf("catalog: %w", err)
	}
	return t, nil
}

// readTokenDetail names a token in the audit log: its id, label,
// permissions, scope and expiry. Never the secret.
func readTokenDetail(t ReadToken) string {
	scope := "lake"
	if len(t.Sessions) > 0 {
		scope = "sessions:" + strings.Join(t.Sessions, ",")
	}
	return fmt.Sprintf("read_token=%s label=%q permissions=%s scope=%s expires=%s", t.ID, t.Label, strings.Join(t.Permissions, ","), scope, stamp(t.Expires))
}

// ReadTokens lists every token, newest first.
func (c *Catalog) ReadTokens(ctx context.Context) ([]ReadToken, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT `+readTokenCols+` FROM read_tokens ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []ReadToken
	for rows.Next() {
		t, err := scanReadToken(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ReadTokenBySecret finds the token whose secret hashes to
// secretSHA256, in any state. ok is false when there is none.
func (c *Catalog) ReadTokenBySecret(ctx context.Context, secretSHA256 string) (ReadToken, bool, error) {
	t, err := scanReadToken(c.db.QueryRowContext(ctx, `SELECT `+readTokenCols+` FROM read_tokens WHERE token_sha256=?`, secretSHA256))
	if errors.Is(err, sql.ErrNoRows) {
		return ReadToken{}, false, nil
	}
	if err != nil {
		return ReadToken{}, false, fmt.Errorf("catalog: %w", err)
	}
	return t, true, nil
}

// TouchReadToken records that the token was used at now.
func (c *Catalog) TouchReadToken(ctx context.Context, id string, now time.Time) error {
	if _, err := c.db.ExecContext(ctx, `UPDATE read_tokens SET last_used_at=? WHERE id=?`, stamp(now), id); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

// RevokeReadToken revokes the token with this id and queues its
// read_token.revoked event in the same transaction. A token already
// revoked is returned with ErrReadTokenRevoked, and nothing is queued.
func (c *Catalog) RevokeReadToken(ctx context.Context, id, by string, now time.Time) (ReadToken, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return ReadToken{}, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	t, err := scanReadToken(tx.QueryRowContext(ctx, `SELECT `+readTokenCols+` FROM read_tokens WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ReadToken{}, fmt.Errorf("%w: %s", ErrNoReadToken, id)
	}
	if err != nil {
		return ReadToken{}, fmt.Errorf("catalog: %w", err)
	}
	if !t.Revoked.IsZero() {
		return t, ErrReadTokenRevoked
	}
	if _, err := tx.ExecContext(ctx, `UPDATE read_tokens SET revoked_at=?, revoked_by=? WHERE id=? AND revoked_at IS NULL`, stamp(now), by, id); err != nil {
		return ReadToken{}, fmt.Errorf("catalog: %w", err)
	}
	t.Revoked, t.RevokedBy = now.UTC(), by
	if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.ReadTokenRevoked, Actor: by, Detail: readTokenDetail(t)}); err != nil {
		return ReadToken{}, err
	}
	if err := tx.Commit(); err != nil {
		return ReadToken{}, fmt.Errorf("catalog: %w", err)
	}
	return t, nil
}

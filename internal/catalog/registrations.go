package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// migrateRegistrations adds registrations, the pending codes an operator
// minted. Only the SHA-256 of a code's secret is stored. A code redeems
// once: used_at and device_id are set in the same transaction that
// creates the device.
func migrateRegistrations(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE registrations (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		secret_sha256 TEXT NOT NULL UNIQUE,
		profile TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		used_at TEXT,
		device_id TEXT,
		revoked_at TEXT
	)`)
	return err
}

// Registration is one minted code.
type Registration struct {
	ID       string
	Name     string
	Profile  string
	Created  time.Time
	Expires  time.Time
	Used     time.Time
	DeviceID string
	Revoked  time.Time
}

// State is pending, used, expired, or revoked at now.
func (r Registration) State(now time.Time) string {
	switch {
	case !r.Used.IsZero():
		return "used"
	case !r.Revoked.IsZero():
		return "revoked"
	case !now.Before(r.Expires):
		return "expired"
	default:
		return "pending"
	}
}

// Redemption refusals. The API answers each with the same refusal so a
// caller learns nothing about which codes exist; the audit log keeps the
// reason.
var (
	ErrRegistrationUnknown = errors.New("catalog: no registration has this secret")
	ErrRegistrationUsed    = errors.New("catalog: registration was already used")
	ErrRegistrationExpired = errors.New("catalog: registration expired")
	ErrRegistrationRevoked = errors.New("catalog: registration was revoked")
	ErrNoRegistration      = errors.New("catalog: no such registration")
	ErrTokenTaken          = errors.New("catalog: this token already belongs to a device")
	ErrNameTaken           = errors.New("catalog: name is taken")
)

const registrationCols = `id, name, profile, created_at, expires_at, COALESCE(used_at, ''), COALESCE(device_id, ''), COALESCE(revoked_at, '')`

func scanRegistration(row interface{ Scan(...any) error }) (Registration, error) {
	var r Registration
	var created, expires, used, revoked string
	if err := row.Scan(&r.ID, &r.Name, &r.Profile, &created, &expires, &used, &r.DeviceID, &revoked); err != nil {
		return Registration{}, err
	}
	r.Created, r.Expires, r.Used, r.Revoked = parseStamp(created), parseStamp(expires), parseStamp(used), parseStamp(revoked)
	return r, nil
}

// CreateRegistration records a pending code for a device called name.
// A name held by a device, or by another pending code, is ErrNameTaken.
func (c *Catalog) CreateRegistration(ctx context.Context, name, secretSHA256, profile string, now, expires time.Time) (Registration, error) {
	if DeviceName(name) != name || name == "" {
		return Registration{}, fmt.Errorf("catalog: %q is not a device name: lowercase letters, digits, '.', '-' and '_'", name)
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	var one int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM devices WHERE name=?`, name).Scan(&one)
	if err == nil {
		return Registration{}, fmt.Errorf("%w: a device is called %s", ErrNameTaken, name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM registrations WHERE name=? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?`, name, stamp(now)).Scan(&one)
	if err == nil {
		return Registration{}, fmt.Errorf("%w: a pending code is for %s; revoke it first", ErrNameTaken, name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	id, err := newDeviceID()
	if err != nil {
		return Registration{}, err
	}
	r := Registration{ID: "reg_" + strings.TrimPrefix(id, "dev_"), Name: name, Profile: profile, Created: now.UTC(), Expires: expires.UTC()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO registrations(id, name, secret_sha256, profile, created_at, expires_at) VALUES(?,?,?,?,?,?)`,
		r.ID, r.Name, secretSHA256, r.Profile, stamp(r.Created), stamp(r.Expires)); err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	return r, nil
}

// Registrations lists every code, newest first.
func (c *Catalog) Registrations(ctx context.Context) ([]Registration, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT `+registrationCols+` FROM registrations ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []Registration
	for rows.Next() {
		r, err := scanRegistration(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RevokeRegistration revokes the pending code with this id, or the
// pending code for this device name. A used code cannot be revoked; the
// device it made can. The read and the update are one transaction, so a
// redemption in serve cannot land between them.
func (c *Catalog) RevokeRegistration(ctx context.Context, ref string, now time.Time) (Registration, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	r, err := scanRegistration(tx.QueryRowContext(ctx, `SELECT `+registrationCols+` FROM registrations
		WHERE (id=? OR (name=? AND used_at IS NULL AND revoked_at IS NULL)) ORDER BY created_at DESC LIMIT 1`, ref, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return Registration{}, fmt.Errorf("%w: %s", ErrNoRegistration, ref)
	}
	if err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	if !r.Used.IsZero() {
		return Registration{}, fmt.Errorf("%w; revoke device %s instead", ErrRegistrationUsed, r.Name)
	}
	if !r.Revoked.IsZero() {
		return r, nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE registrations SET revoked_at=? WHERE id=? AND used_at IS NULL AND revoked_at IS NULL`, stamp(now), r.ID)
	if err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return Registration{}, fmt.Errorf("catalog: registration %s changed while it was being revoked; run serve register --list and try again", r.ID)
	}
	if err := tx.Commit(); err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	r.Revoked = now.UTC()
	return r, nil
}

// Redeem spends the code whose secret hashes to secretSHA256 and creates
// its device in one transaction: the device takes the code's name and
// profile, holds tokenSHA256, and is bound to machineID. The registration
// is returned with every refusal it can be named for, so the audit line
// can name it.
func (c *Catalog) Redeem(ctx context.Context, secretSHA256, tokenSHA256, machineID string, now time.Time) (Device, Registration, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, Registration{}, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	r, err := scanRegistration(tx.QueryRowContext(ctx, `SELECT `+registrationCols+` FROM registrations WHERE secret_sha256=?`, secretSHA256))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, Registration{}, ErrRegistrationUnknown
	}
	if err != nil {
		return Device{}, Registration{}, fmt.Errorf("catalog: %w", err)
	}
	switch r.State(now) {
	case "used":
		return Device{}, r, ErrRegistrationUsed
	case "revoked":
		return Device{}, r, ErrRegistrationRevoked
	case "expired":
		return Device{}, r, ErrRegistrationExpired
	}
	var other string
	err = tx.QueryRowContext(ctx, `SELECT name FROM devices WHERE token_sha256=?`, tokenSHA256).Scan(&other)
	if err == nil {
		return Device{}, r, ErrTokenTaken
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Device{}, r, fmt.Errorf("catalog: %w", err)
	}
	// A machine re-registering after its old device was revoked takes
	// its machine_id back; the revoked row keeps its history without it.
	var otherID string
	var revoked sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id, name, revoked_at FROM devices WHERE machine_id=?`, machineID).Scan(&otherID, &other, &revoked)
	switch {
	case err == nil && revoked.Valid:
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET machine_id=NULL WHERE id=?`, otherID); err != nil {
			return Device{}, r, fmt.Errorf("catalog: %w", err)
		}
	case err == nil:
		return Device{}, r, fmt.Errorf("%w: %s", ErrMachineTaken, other)
	case !errors.Is(err, sql.ErrNoRows):
		return Device{}, r, fmt.Errorf("catalog: %w", err)
	}
	d := Device{Source: DeviceFromRegistration, TokenSHA256: tokenSHA256, Profile: r.Profile, MachineID: machineID, Created: now.UTC()}
	if d.ID, err = newDeviceID(); err != nil {
		return Device{}, r, err
	}
	// The name was free when the code was minted. A token-file device
	// that took it since gets a suffix rather than refusing the code.
	if d.Name, err = freeName(ctx, tx, r.Name); err != nil {
		return Device{}, r, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id, name, token_sha256, source, profile, machine_id, created_at) VALUES(?,?,?,?,?,?,?)`,
		d.ID, d.Name, d.TokenSHA256, d.Source, d.Profile, d.MachineID, stamp(d.Created)); err != nil {
		return Device{}, r, fmt.Errorf("catalog: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE registrations SET used_at=?, device_id=? WHERE id=?`, stamp(now), d.ID, r.ID); err != nil {
		return Device{}, r, fmt.Errorf("catalog: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Device{}, r, fmt.Errorf("catalog: %w", err)
	}
	r.Used, r.DeviceID = now.UTC(), d.ID
	return d, r, nil
}

// PublicURL is the lake's public base URL, set by serve identity
// set-url, or "" when none is set.
func (c *Catalog) PublicURL(ctx context.Context) (string, error) {
	var u string
	err := c.db.QueryRowContext(ctx, `SELECT value FROM lake_meta WHERE key='public_url'`).Scan(&u)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("catalog: %w", err)
	}
	return u, nil
}

// SetPublicURL records the lake's public base URL.
func (c *Catalog) SetPublicURL(ctx context.Context, u string) error {
	if _, err := c.db.ExecContext(ctx, `INSERT INTO lake_meta(key, value) VALUES('public_url', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, u); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

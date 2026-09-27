package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"
)

// migrateDevices adds devices: one row per device token the lake has
// accepted, with a name an operator can list and revoke it by. A token
// from --token-file becomes a row when the file loads. token_sha256 is
// the hash the file already holds; the token itself is never stored.
//
// A device binds to the first machine_id it uploads a manifest under,
// and a machine_id belongs to at most one device.
func migrateDevices(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE devices (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		token_sha256 TEXT NOT NULL UNIQUE,
		source TEXT NOT NULL,
		profile TEXT NOT NULL DEFAULT '',
		machine_id TEXT,
		created_at TEXT NOT NULL,
		detached_at TEXT,
		revoked_at TEXT
	);
	CREATE UNIQUE INDEX devices_machine ON devices(machine_id) WHERE machine_id IS NOT NULL;`)
	return err
}

// Device sources.
const (
	DeviceFromTokenFile    = "token-file"
	DeviceFromRegistration = "registration"
	// DeviceFromAllow is a token enrolled in code with api.Server.Allow,
	// by tests and synthetic fixtures. serve never makes one. It is not
	// bound to a machine, so a fixture can post as several machines.
	DeviceFromAllow = "allow"
)

// Device is one row of devices. Detached is set on a token-file device
// whose token is no longer in the file. Revoked is set by an operator
// and is final.
type Device struct {
	ID          string
	Name        string
	TokenSHA256 string
	Source      string
	Profile     string
	MachineID   string
	Created     time.Time
	Detached    time.Time
	Revoked     time.Time
}

// State is how list shows the device: active, revoked, or detached.
func (d Device) State() string {
	switch {
	case !d.Revoked.IsZero():
		return "revoked"
	case !d.Detached.IsZero():
		return "detached"
	default:
		return "active"
	}
}

// TokenEntry is one token from the token file.
type TokenEntry struct {
	Hash string
	Name string
}

// Errors the binding returns.
var (
	ErrNoDevice     = errors.New("catalog: no such device")
	ErrMachineTaken = errors.New("catalog: machine_id belongs to another device")
	ErrDeviceBound  = errors.New("catalog: device is bound to another machine_id")
)

const deviceCols = `id, name, token_sha256, source, profile, COALESCE(machine_id, ''), created_at, COALESCE(detached_at, ''), COALESCE(revoked_at, '')`

func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var d Device
	var created, detached, revoked string
	if err := row.Scan(&d.ID, &d.Name, &d.TokenSHA256, &d.Source, &d.Profile, &d.MachineID, &created, &detached, &revoked); err != nil {
		return Device{}, err
	}
	d.Created = parseStamp(created)
	d.Detached = parseStamp(detached)
	d.Revoked = parseStamp(revoked)
	return d, nil
}

func parseStamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// SyncTokenFile records the tokens the token file holds. A token not
// seen before becomes a token-file device with the name the file gives
// it, made unique. A token-file device whose token left the file is
// marked detached, and one that came back is not. A revoked device
// stays revoked. It returns the devices it created.
func (c *Catalog) SyncTokenFile(ctx context.Context, entries []TokenEntry, now time.Time) ([]Device, error) {
	return c.syncTokens(ctx, entries, DeviceFromTokenFile, now)
}

// AllowTokens records entries as allow devices, for api.Server.Allow.
// It detaches nothing.
func (c *Catalog) AllowTokens(ctx context.Context, entries []TokenEntry, now time.Time) ([]Device, error) {
	return c.syncTokens(ctx, entries, DeviceFromAllow, now)
}

func (c *Catalog) syncTokens(ctx context.Context, entries []TokenEntry, source string, now time.Time) ([]Device, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	var created []Device
	inFile := map[string]bool{}
	for i, e := range entries {
		inFile[e.Hash] = true
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM devices WHERE token_sha256=?`, e.Hash).Scan(&id)
		if err == nil {
			if _, err := tx.ExecContext(ctx, `UPDATE devices SET detached_at=NULL WHERE id=? AND source=?`, id, DeviceFromTokenFile); err != nil {
				return nil, fmt.Errorf("catalog: %w", err)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		base := DeviceName(e.Name)
		if base == "" {
			base = fmt.Sprintf("token-%d", i+1)
		}
		d := Device{Source: source, TokenSHA256: e.Hash, Created: now.UTC()}
		if d.ID, err = newDeviceID(); err != nil {
			return nil, err
		}
		if d.Name, err = freeName(ctx, tx, base); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id, name, token_sha256, source, created_at) VALUES(?,?,?,?,?)`,
			d.ID, d.Name, d.TokenSHA256, d.Source, stamp(d.Created)); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		created = append(created, d)
	}
	if source != DeviceFromTokenFile {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		return created, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, token_sha256 FROM devices WHERE source=? AND detached_at IS NULL`, DeviceFromTokenFile)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	var gone []string
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if !inFile[hash] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET detached_at=? WHERE id=?`, stamp(now), id); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return created, nil
}

// DeviceName folds s to a device name: lowercase letters, digits, '.',
// '-' and '_', at most 64 characters. Anything else becomes '-'.
func DeviceName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if b.Len() >= 64 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}

func freeName(ctx context.Context, tx *sql.Tx, base string) (string, error) {
	name := base
	for n := 2; ; n++ {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM devices WHERE name=?`, name).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return name, nil
		}
		if err != nil {
			return "", fmt.Errorf("catalog: %w", err)
		}
		name = fmt.Sprintf("%s-%d", base, n)
	}
}

var deviceIDEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func newDeviceID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("catalog: %w", err)
	}
	return "dev_" + strings.ToLower(deviceIDEncoding.EncodeToString(b)), nil
}

// DeviceByHash is the device whose token hashes to hash.
func (c *Catalog) DeviceByHash(ctx context.Context, hash string) (Device, bool, error) {
	d, err := scanDevice(c.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE token_sha256=?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, false, nil
	}
	if err != nil {
		return Device{}, false, fmt.Errorf("catalog: %w", err)
	}
	return d, true, nil
}

// DeviceByName is the device called name.
func (c *Catalog) DeviceByName(ctx context.Context, name string) (Device, error) {
	d, err := scanDevice(c.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE name=?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, fmt.Errorf("%w: %s", ErrNoDevice, name)
	}
	if err != nil {
		return Device{}, fmt.Errorf("catalog: %w", err)
	}
	return d, nil
}

// Devices lists every device by name.
func (c *Catalog) Devices(ctx context.Context) ([]Device, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT `+deviceCols+` FROM devices ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// BindMachine binds device id to machineID when it has no machine yet,
// and reports whether this call bound it. A device bound to another
// machine_id is ErrDeviceBound, and a machine_id bound to another device
// is ErrMachineTaken.
func (c *Catalog) BindMachine(ctx context.Context, id, machineID string) (bool, error) {
	if machineID == "" {
		return false, errors.New("catalog: empty machine_id")
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	var bound sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT machine_id FROM devices WHERE id=?`, id).Scan(&bound); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNoDevice
		}
		return false, fmt.Errorf("catalog: %w", err)
	}
	if bound.Valid {
		if bound.String == machineID {
			return false, nil
		}
		return false, ErrDeviceBound
	}
	var other string
	err = tx.QueryRowContext(ctx, `SELECT name FROM devices WHERE machine_id=?`, machineID).Scan(&other)
	if err == nil {
		return false, fmt.Errorf("%w: %s", ErrMachineTaken, other)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("catalog: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET machine_id=? WHERE id=?`, machineID, id); err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	return true, nil
}

// UnbindDevice clears the device's machine_id, so its next manifest
// binds again.
func (c *Catalog) UnbindDevice(ctx context.Context, name string) (Device, error) {
	d, err := c.DeviceByName(ctx, name)
	if err != nil {
		return Device{}, err
	}
	if _, err := c.db.ExecContext(ctx, `UPDATE devices SET machine_id=NULL WHERE id=?`, d.ID); err != nil {
		return Device{}, fmt.Errorf("catalog: %w", err)
	}
	return d, nil
}

// SetDeviceProfile records the profile the device's agent fetches. An
// empty profile is the default.
func (c *Catalog) SetDeviceProfile(ctx context.Context, name, profile string) (Device, error) {
	d, err := c.DeviceByName(ctx, name)
	if err != nil {
		return Device{}, err
	}
	if _, err := c.db.ExecContext(ctx, `UPDATE devices SET profile=? WHERE id=?`, profile, d.ID); err != nil {
		return Device{}, fmt.Errorf("catalog: %w", err)
	}
	d.Profile = profile
	return d, nil
}

// RevokeDevice marks the device revoked. Its token stops working on the
// next request. Revoking is final; a revoked device is not restored by
// its token reappearing in the token file.
func (c *Catalog) RevokeDevice(ctx context.Context, name string, now time.Time) (Device, error) {
	d, err := c.DeviceByName(ctx, name)
	if err != nil {
		return Device{}, err
	}
	if !d.Revoked.IsZero() {
		return d, nil
	}
	if _, err := c.db.ExecContext(ctx, `UPDATE devices SET revoked_at=? WHERE id=?`, stamp(now), d.ID); err != nil {
		return Device{}, fmt.Errorf("catalog: %w", err)
	}
	d.Revoked = now.UTC()
	return d, nil
}

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

// migrateRegistrations adds registrations, the pending codes an operator
// minted. Only the SHA-256 of a code's secret is stored. A code redeems
// once: used_at and device_id are set in the same transaction that
// creates the device. expiry_recorded_at is when the lake first saw the
// code expired and wrote that to the audit log, so it is written once.
func migrateRegistrations(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE registrations (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		secret_sha256 TEXT NOT NULL UNIQUE,
		profile TEXT NOT NULL DEFAULT '',
		key_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		used_at TEXT,
		device_id TEXT,
		revoked_at TEXT,
		expiry_recorded_at TEXT
	)`)
	return err
}

// migrateRegistrationActors records who minted and who revoked each
// code: "cli" for serve register, and the operator's identity for the
// dashboard. Codes from before it have an empty created_by.
func migrateRegistrationActors(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE registrations ADD COLUMN created_by TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	_, err := tx.Exec(`ALTER TABLE registrations ADD COLUMN revoked_by TEXT`)
	return err
}

// ActorCLI is the actor recorded for serve register.
const ActorCLI = "cli"

// Registration is one minted code.
type Registration struct {
	ID      string
	Name    string
	Profile string
	// KeyID is the lake key that signed the code. A code whose key has
	// been retired is refused.
	KeyID    string
	Created  time.Time
	Expires  time.Time
	Used     time.Time
	DeviceID string
	Revoked  time.Time
	// CreatedBy and RevokedBy name who minted and who revoked the code.
	// CreatedBy is empty for a code from before they were recorded.
	CreatedBy string
	RevokedBy string
	// Bays are the ids of the bays the device the code makes may write.
	// Empty is the default bay, as for every device before bays.
	Bays []string
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
	ErrRegistrationKey     = errors.New("catalog: the key that signed the code is retired")
	ErrNoRegistration      = errors.New("catalog: no such registration")
	ErrTokenTaken          = errors.New("catalog: this token already belongs to a device")
	ErrNameTaken           = errors.New("catalog: name is taken")
)

const registrationCols = `id, name, profile, key_id, created_at, expires_at, COALESCE(used_at, ''), COALESCE(device_id, ''), COALESCE(revoked_at, ''), created_by, COALESCE(revoked_by, ''), bays`

func scanRegistration(row interface{ Scan(...any) error }) (Registration, error) {
	var r Registration
	var created, expires, used, revoked, bays string
	if err := row.Scan(&r.ID, &r.Name, &r.Profile, &r.KeyID, &created, &expires, &used, &r.DeviceID, &revoked, &r.CreatedBy, &r.RevokedBy, &bays); err != nil {
		return Registration{}, err
	}
	if bays != "" {
		if err := json.Unmarshal([]byte(bays), &r.Bays); err != nil {
			return Registration{}, fmt.Errorf("registration %s bays: %w", r.ID, err)
		}
	}
	r.Created, r.Expires, r.Used, r.Revoked = parseStamp(created), parseStamp(expires), parseStamp(used), parseStamp(revoked)
	return r, nil
}

// Minter is who mints a code, for the bay check made in the same
// transaction that stores it (review 1415): an admin, or a command on
// the lake host, may add a device to any bay; anyone else only to the
// bays Groups hold write on, and to the default bay when no bay is
// named. The zero Minter may add a device to no bay.
type Minter struct {
	Admin  bool
	Groups []string
}

// ErrBayScope is a mint into a bay the minter may not add a device to.
var ErrBayScope = errors.New("catalog: the minter may not add a device to that bay")

// CreateRegistration records a pending code for a device called name.
// A name held by a device, or by another pending code, is ErrNameTaken.
// by names who minted it. The device it makes writes the default bay.
func (c *Catalog) CreateRegistration(ctx context.Context, name, secretSHA256, profile, keyID, by string, now, expires time.Time) (Registration, error) {
	return c.CreateRegistrationInBays(ctx, name, secretSHA256, profile, keyID, by, nil, Minter{Admin: true}, now, expires)
}

// mayAdd refuses a bay m may not add a device to. No bays is the
// default bay.
func (m Minter) mayAdd(ctx context.Context, tx *sql.Tx, bays []string) error {
	if m.Admin {
		return nil
	}
	if len(bays) == 0 {
		bays = []string{DefaultBayID}
	}
	for _, bay := range bays {
		ok := false
		for _, g := range m.Groups {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bay_grants WHERE principal_kind=? AND principal=? AND bay_id=? AND permission=?`,
				PrincipalGroup, g, bay, PermWrite).Scan(&n); err != nil {
				return fmt.Errorf("catalog: %w", err)
			}
			if n > 0 {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%w: %s", ErrBayScope, bay)
		}
	}
	return nil
}

// CreateRegistrationInBays is CreateRegistration for a device that may
// write bays, each named by id, name or alias and stored by id. No bays
// is the default bay. minter must be allowed to add a device to each,
// which is checked against the grants as they are in this transaction.
func (c *Catalog) CreateRegistrationInBays(ctx context.Context, name, secretSHA256, profile, keyID, by string, bays []string, minter Minter, now, expires time.Time) (Registration, error) {
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
	rows, err := tx.QueryContext(ctx, `SELECT `+registrationCols+` FROM registrations WHERE name=? AND used_at IS NULL AND revoked_at IS NULL`, name)
	if err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	pending := false
	for rows.Next() {
		r, err := scanRegistration(rows)
		if err != nil {
			rows.Close()
			return Registration{}, fmt.Errorf("catalog: %w", err)
		}
		// Compared as times, as in RecordExpiries.
		pending = pending || r.State(now) == "pending"
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	if pending {
		return Registration{}, fmt.Errorf("%w: a pending code is for %s; revoke it first", ErrNameTaken, name)
	}
	id, err := newDeviceID()
	if err != nil {
		return Registration{}, err
	}
	r := Registration{ID: "reg_" + strings.TrimPrefix(id, "dev_"), Name: name, Profile: profile, KeyID: keyID, Created: now.UTC(), Expires: expires.UTC(), CreatedBy: by}
	for _, ref := range bays {
		bayID, err := resolveBayID(ctx, tx, ref)
		if err != nil {
			return Registration{}, err
		}
		if !slices.Contains(r.Bays, bayID) {
			r.Bays = append(r.Bays, bayID)
		}
	}
	slices.Sort(r.Bays)
	if err := minter.mayAdd(ctx, tx, r.Bays); err != nil {
		return Registration{}, err
	}
	stored := ""
	if len(r.Bays) > 0 {
		b, err := json.Marshal(r.Bays)
		if err != nil {
			return Registration{}, err
		}
		stored = string(b)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO registrations(id, name, secret_sha256, profile, key_id, created_at, expires_at, created_by, bays) VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, secretSHA256, r.Profile, r.KeyID, stamp(r.Created), stamp(r.Expires), by, stored); err != nil {
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
// redemption in serve cannot land between them. A code that is already
// revoked is returned with ErrRegistrationRevoked, so the caller does
// not report or audit a revocation that did not happen. by names who
// revoked it in the catalog, and auditActor in the registration.revoked
// event it queues.
func (c *Catalog) RevokeRegistration(ctx context.Context, ref, by, auditActor string, now time.Time) (Registration, error) {
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
		return r, ErrRegistrationRevoked
	}
	res, err := tx.ExecContext(ctx, `UPDATE registrations SET revoked_at=?, revoked_by=? WHERE id=? AND used_at IS NULL AND revoked_at IS NULL`, stamp(now), by, r.ID)
	if err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return Registration{}, fmt.Errorf("catalog: registration %s changed while it was being revoked; run serve register --list and try again", r.ID)
	}
	if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.RegistrationRevoked, Device: r.Name, Actor: auditActor, Detail: "registration=" + r.ID}); err != nil {
		return Registration{}, err
	}
	if err := tx.Commit(); err != nil {
		return Registration{}, fmt.Errorf("catalog: %w", err)
	}
	r.Revoked, r.RevokedBy = now.UTC(), by
	return r, nil
}

// Redeem spends the code whose secret hashes to secretSHA256 and creates
// its device in one transaction: the device takes the code's name and
// profile, holds tokenSHA256, and is bound to machineID. The registration
// is returned with every refusal it can be named for, so the audit line
// can name it. keyActive says whether the key that signed the code may
// still sign; nil accepts every key.
//
// finish, when not nil, runs on the new device, the spent code, and the
// device's effective profile before the transaction commits. A profile
// that does not resolve, ErrNoProfile, is returned without running it.
// Either error rolls the redemption back, so the code stays unspent and
// no device is left behind. The audit events finish returns are queued
// in the same transaction, for FlushAudit.
func (c *Catalog) Redeem(ctx context.Context, secretSHA256, tokenSHA256, machineID string, keyActive func(string) bool, now time.Time, finish func(Device, Registration, EffectiveProfile) ([]audit.Event, error)) (Device, Registration, error) {
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
	if keyActive != nil && r.KeyID != "" && !keyActive(r.KeyID) {
		return Device{}, r, ErrRegistrationKey
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
	if err := grantCodeBays(ctx, tx, d.ID, r.Bays, now); err != nil {
		return Device{}, r, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE registrations SET used_at=?, device_id=? WHERE id=?`, stamp(now), d.ID, r.ID); err != nil {
		return Device{}, r, fmt.Errorf("catalog: %w", err)
	}
	if finish != nil {
		spent := r
		spent.Used, spent.DeviceID = now.UTC(), d.ID
		// The one connection is this transaction's, so the profile is
		// read through it.
		prof, err := resolveProfile(ctx, tx, d)
		if err != nil {
			return Device{}, r, err
		}
		events, err := finish(d, spent, prof)
		if err != nil {
			return Device{}, r, err
		}
		if err := queueAudit(ctx, tx, now, events...); err != nil {
			return Device{}, r, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Device{}, r, fmt.Errorf("catalog: %w", err)
	}
	r.Used, r.DeviceID = now.UTC(), d.ID
	return d, r, nil
}

// RecordExpiries marks each code that is expired at now and whose
// expiry was not recorded yet, queues a registration.expired event for
// each with actor, and returns them. With ids it looks only at those
// codes. A code is marked once across every caller, so the expiry has
// one audit line however often it is seen, and an open route that calls
// this queues at most one per code. The caller runs FlushAudit.
func (c *Catalog) RecordExpiries(ctx context.Context, now time.Time, actor string, ids ...string) ([]Registration, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	q := `SELECT ` + registrationCols + ` FROM registrations WHERE used_at IS NULL AND revoked_at IS NULL AND expiry_recorded_at IS NULL`
	var args []any
	if len(ids) > 0 {
		q += ` AND id IN (?` + strings.Repeat(`,?`, len(ids)-1) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := tx.QueryContext(ctx, q+` ORDER BY expires_at, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	var expired []Registration
	for rows.Next() {
		r, err := scanRegistration(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: %w", err)
		}
		// Compared as times: stamps drop trailing zeros, so their text
		// does not sort.
		if r.State(now) == "expired" {
			expired = append(expired, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	var out []Registration
	for _, r := range expired {
		res, err := tx.ExecContext(ctx, `UPDATE registrations SET expiry_recorded_at=? WHERE id=? AND expiry_recorded_at IS NULL`, stamp(now), r.ID)
		if err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		} else if n == 1 {
			out = append(out, r)
			if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.RegistrationExpired, Device: r.Name, Actor: actor,
				Detail: "registration=" + r.ID + " expires=" + r.Expires.Format(time.RFC3339)}); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
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

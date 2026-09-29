package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
)

// A bay is a named segment of the lake and an access boundary
// (TKT-01M3N8KHW5, docs/policy.md#bays). A session belongs to one or
// more bays. Membership is a row here and never copies data: the CAS
// and the derived files stay shared, and every read path joins against
// session_bays.

// DefaultBayID is the default bay's id. The default bay is the inbox:
// data from before bays is in it, and a session nothing places lands in
// it. It cannot be deleted.
const DefaultBayID = "bay_default"

// DefaultBayName is the default bay's name. It cannot be renamed; an
// alias gives it a second name.
const DefaultBayName = "default"

// Grant principals and permissions.
const (
	PrincipalGroup     = "group"
	PrincipalDevice    = "device"
	PrincipalReadToken = "read_token"

	PermRead  = "read"
	PermWrite = "write"
)

// Where a membership change came from, for the audit line.
const (
	ViaIngest = "ingest"
	ViaRule   = "rule"
	ViaCLI    = "cli"
	ViaWeb    = "web"
)

var (
	ErrNoBay       = errors.New("catalog: no such bay")
	ErrBayName     = errors.New("catalog: a bay name is 1 to 63 lowercase letters, digits and dashes, starting with a letter or digit")
	ErrBayTaken    = errors.New("catalog: that name is already a bay or an alias")
	ErrLastBay     = errors.New("catalog: a session must stay in at least one bay")
	ErrPrincipal   = errors.New("catalog: a grant names a group, device or read_token principal")
	ErrPermission  = errors.New("catalog: a grant is read or write")
	ErrNotAMember  = errors.New("catalog: the session is not in that bay")
	ErrDefaultOnly = errors.New("catalog: the default bay cannot be deleted")
	// ErrNoBayForSession refuses a new session that nothing places while
	// the default bay is turned off. The ingest rolls back, so the agent
	// keeps the session.
	ErrNoBayForSession = errors.New("catalog: the default bay is off and nothing places this session")
)

var bayNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidBayName reports whether s may name a bay or an alias. A bay id
// holds an underscore, which a name cannot, so an id is never a name.
func ValidBayName(s string) bool {
	return bayNamePattern.MatchString(s)
}

// migrateBays adds bays, their aliases, session membership, the bays
// each session asked for, and grants. Triggers keep a bay's name and
// an alias apart, so a reference resolves to one bay. Every stored session goes into
// the default bay, and every device is granted write on it, so nothing
// changes until an admin makes a second bay.
func migrateBays(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE bays (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		is_default INTEGER NOT NULL DEFAULT 0,
		disabled INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		created_by TEXT NOT NULL
	);
	CREATE TABLE bay_aliases (
		alias TEXT PRIMARY KEY,
		bay_id TEXT NOT NULL
	);
	CREATE TABLE session_bays (
		session_uid TEXT NOT NULL,
		bay_id TEXT NOT NULL,
		added_at TEXT NOT NULL,
		PRIMARY KEY (session_uid, bay_id)
	);
	CREATE INDEX session_bays_bay ON session_bays(bay_id, session_uid);
	CREATE TABLE session_bay_requests (
		session_uid TEXT NOT NULL,
		bay_ref TEXT NOT NULL,
		device_id TEXT NOT NULL DEFAULT '',
		outcome TEXT NOT NULL,
		reason TEXT NOT NULL DEFAULT '',
		first_at TEXT NOT NULL,
		last_at TEXT NOT NULL,
		PRIMARY KEY (session_uid, bay_ref)
	);
	CREATE TABLE bay_grants (
		principal_kind TEXT NOT NULL,
		principal TEXT NOT NULL,
		bay_id TEXT NOT NULL,
		permission TEXT NOT NULL,
		granted_at TEXT NOT NULL,
		granted_by TEXT NOT NULL,
		PRIMARY KEY (principal_kind, principal, bay_id, permission)
	);
	CREATE INDEX bay_grants_bay ON bay_grants(bay_id);
	CREATE TRIGGER bay_alias_not_a_name BEFORE INSERT ON bay_aliases
		WHEN EXISTS (SELECT 1 FROM bays WHERE name = NEW.alias)
		BEGIN SELECT RAISE(ABORT, 'bay name taken'); END;
	CREATE TRIGGER bay_realias_not_a_name BEFORE UPDATE OF alias ON bay_aliases
		WHEN EXISTS (SELECT 1 FROM bays WHERE name = NEW.alias)
		BEGIN SELECT RAISE(ABORT, 'bay name taken'); END;
	CREATE TRIGGER bay_name_not_an_alias BEFORE INSERT ON bays
		WHEN EXISTS (SELECT 1 FROM bay_aliases WHERE alias = NEW.name)
		BEGIN SELECT RAISE(ABORT, 'bay name taken'); END;
	CREATE TRIGGER bay_rename_not_an_alias BEFORE UPDATE OF name ON bays
		WHEN EXISTS (SELECT 1 FROM bay_aliases WHERE alias = NEW.name)
		BEGIN SELECT RAISE(ABORT, 'bay name taken'); END;`); err != nil {
		return err
	}
	now := stamp(time.Now())
	if _, err := tx.Exec(`INSERT INTO bays(id, name, is_default, created_at, created_by) VALUES(?, ?, 1, ?, 'migration')`, DefaultBayID, DefaultBayName, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO session_bays(session_uid, bay_id, added_at) SELECT session_uid, ?, ? FROM sessions`, DefaultBayID, now); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO bay_grants(principal_kind, principal, bay_id, permission, granted_at, granted_by)
		SELECT ?, id, ?, ?, ?, 'migration' FROM devices`, PrincipalDevice, DefaultBayID, PermWrite, now)
	return err
}

// Bay is one row of bays with its aliases.
type Bay struct {
	ID      string
	Name    string
	Aliases []string
	Default bool
	// Disabled is set only on the default bay, when an admin has turned
	// it off: a session nothing places is then refused.
	Disabled  bool
	Created   time.Time
	CreatedBy string
}

// Bays lists every bay, the default first, then by name.
func (c *Catalog) Bays(ctx context.Context) ([]Bay, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT id, name, is_default, disabled, created_at, created_by FROM bays ORDER BY is_default DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	var out []Bay
	for rows.Next() {
		var b Bay
		var created string
		if err := rows.Scan(&b.ID, &b.Name, &b.Default, &b.Disabled, &created, &b.CreatedBy); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: %w", err)
		}
		b.Created = parseStamp(created)
		out = append(out, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	aliases, err := c.bayAliases(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Aliases = aliases[out[i].ID]
	}
	return out, nil
}

func (c *Catalog) bayAliases(ctx context.Context) (map[string][]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT alias, bay_id FROM bay_aliases ORDER BY alias`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var alias, id string
		if err := rows.Scan(&alias, &id); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out[id] = append(out[id], alias)
	}
	return out, rows.Err()
}

// ResolveBay finds a bay by id, name or alias.
func (c *Catalog) ResolveBay(ctx context.Context, ref string) (Bay, error) {
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Bay{}, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	id, err := resolveBayID(ctx, tx, ref)
	if err != nil {
		return Bay{}, err
	}
	var b Bay
	var created string
	if err := tx.QueryRowContext(ctx, `SELECT id, name, is_default, disabled, created_at, created_by FROM bays WHERE id=?`, id).
		Scan(&b.ID, &b.Name, &b.Default, &b.Disabled, &created, &b.CreatedBy); err != nil {
		return Bay{}, fmt.Errorf("catalog: %w", err)
	}
	b.Created = parseStamp(created)
	rows, err := tx.QueryContext(ctx, `SELECT alias FROM bay_aliases WHERE bay_id=? ORDER BY alias`, id)
	if err != nil {
		return Bay{}, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return Bay{}, fmt.Errorf("catalog: %w", err)
		}
		b.Aliases = append(b.Aliases, a)
	}
	return b, rows.Err()
}

// resolveBayID maps an id, name or alias to the bay's id.
func resolveBayID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, ref string) (string, error) {
	var id string
	// The triggers migrateBays makes keep a name and an alias from
	// meeting, so at most one row matches; the order is a second guard.
	err := q.QueryRowContext(ctx, `
		SELECT id FROM (
			SELECT id, 0 AS rank FROM bays WHERE id = ?1 OR name = ?1
			UNION ALL
			SELECT bay_id, 1 FROM bay_aliases WHERE alias = ?1)
		ORDER BY rank LIMIT 1`, ref).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: %s", ErrNoBay, ref)
	}
	if err != nil {
		return "", fmt.Errorf("catalog: %w", err)
	}
	return id, nil
}

// nameFree reports whether name is neither a bay's name nor an alias.
func nameFree(ctx context.Context, tx *sql.Tx, name string) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM bays WHERE name=?1) + (SELECT count(*) FROM bay_aliases WHERE alias=?1)`, name).Scan(&n); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: %s", ErrBayTaken, name)
	}
	return nil
}

func newBayID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("catalog: %w", err)
	}
	return "bay_" + strings.ToLower(deviceIDEncoding.EncodeToString(b)), nil
}

// CreateBay makes a bay called name. actor names who made it, for the
// audit line.
func (c *Catalog) CreateBay(ctx context.Context, name, actor string, now time.Time) (Bay, error) {
	if !ValidBayName(name) {
		return Bay{}, fmt.Errorf("%w: %q", ErrBayName, name)
	}
	id, err := newBayID()
	if err != nil {
		return Bay{}, err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return Bay{}, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	if err := nameFree(ctx, tx, name); err != nil {
		return Bay{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bays(id, name, created_at, created_by) VALUES(?,?,?,?)`, id, name, stamp(now), actor); err != nil {
		return Bay{}, fmt.Errorf("catalog: %w", err)
	}
	if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.BayCreated, Actor: actor, Detail: "bay " + name + " (" + id + ")"}); err != nil {
		return Bay{}, err
	}
	if err := tx.Commit(); err != nil {
		return Bay{}, fmt.Errorf("catalog: %w", err)
	}
	return Bay{ID: id, Name: name, Created: now.UTC(), CreatedBy: actor}, nil
}

// SessionBays lists the bays a session is in, by id, sorted.
func (c *Catalog) SessionBays(ctx context.Context, sessionUID string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT bay_id FROM session_bays WHERE session_uid=? ORDER BY bay_id`, sessionUID)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Membership is one change to a session's bays: who made it, how, and
// why. The reason is free text an operator reads in the audit log.
type Membership struct {
	SessionUID string
	Bay        string // id, name or alias
	Actor      string
	Via        string
	Reason     string
}

// AddToBay puts a session in a bay. It reports false when the session
// was already there, and writes no audit line then.
func (c *Catalog) AddToBay(ctx context.Context, m Membership, now time.Time) (bool, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	added, err := addToBay(ctx, tx, m, now)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	return added, nil
}

func addToBay(ctx context.Context, tx *sql.Tx, m Membership, now time.Time) (bool, error) {
	id, err := resolveBayID(ctx, tx, m.Bay)
	if err != nil {
		return false, err
	}
	if err := sessionExists(ctx, tx, m.SessionUID); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO session_bays(session_uid, bay_id, added_at) VALUES(?,?,?) ON CONFLICT DO NOTHING`, m.SessionUID, id, stamp(now))
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	return true, queueAudit(ctx, tx, now, membershipEvent(audit.BayMemberAdded, m, id))
}

// RemoveFromBay takes a session out of a bay. A session left in no bay
// moves to the default bay, and the default bay cannot be taken from a
// session that is in no other. That holds when the default is turned
// off too: the switch refuses new sessions at ingest, and a stored
// session is never left in no bay, where no read path would reach it
// (docs/policy.md#bays).
func (c *Catalog) RemoveFromBay(ctx context.Context, m Membership, now time.Time) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	id, err := resolveBayID(ctx, tx, m.Bay)
	if err != nil {
		return err
	}
	var in, total int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(bay_id=?), 0), count(*) FROM session_bays WHERE session_uid=?`, id, m.SessionUID).Scan(&in, &total); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if in == 0 {
		return fmt.Errorf("%w: %s", ErrNotAMember, m.Bay)
	}
	if total == 1 && id == DefaultBayID {
		return ErrLastBay
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_bays WHERE session_uid=? AND bay_id=?`, m.SessionUID, id); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if err := queueAudit(ctx, tx, now, membershipEvent(audit.BayMemberRemoved, m, id)); err != nil {
		return err
	}
	if total == 1 {
		back := m
		back.Bay = DefaultBayID
		back.Reason = "left in no bay"
		if _, err := addToBay(ctx, tx, back, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

func membershipEvent(kind string, m Membership, bayID string) audit.Event {
	detail := fmt.Sprintf("session %s bay %s via %s", m.SessionUID, bayID, m.Via)
	if m.Reason != "" {
		detail += ": " + m.Reason
	}
	return audit.Event{Kind: kind, Actor: m.Actor, Detail: detail}
}

func sessionExists(ctx context.Context, tx *sql.Tx, uid string) error {
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE session_uid=?`, uid).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("catalog: no session %s", uid)
	}
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

// landInDefault puts a session that has just been stored into the
// default bay, inside the ingest transaction. It writes no audit line:
// this is where every session landed before bays, and routing, which
// is audited, replaces it. When the default is turned off it refuses
// with ErrNoBayForSession, which rolls the ingest back.
func landInDefault(ctx context.Context, tx *sql.Tx, uid string, now time.Time) error {
	var off bool
	if err := tx.QueryRowContext(ctx, `SELECT disabled FROM bays WHERE id=?`, DefaultBayID).Scan(&off); err != nil {
		return fmt.Errorf("catalog: default bay: %w", err)
	}
	if off {
		return ErrNoBayForSession
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO session_bays(session_uid, bay_id, added_at) VALUES(?,?,?) ON CONFLICT DO NOTHING`, uid, DefaultBayID, stamp(now))
	if err != nil {
		return fmt.Errorf("catalog: session bay: %w", err)
	}
	return nil
}

// Grant is one row of bay_grants.
type Grant struct {
	PrincipalKind string
	Principal     string
	BayID         string
	Permission    string
	Granted       time.Time
	GrantedBy     string
}

func checkGrant(kind, perm string) error {
	switch kind {
	case PrincipalGroup, PrincipalDevice, PrincipalReadToken:
	default:
		return fmt.Errorf("%w: %q", ErrPrincipal, kind)
	}
	if perm != PermRead && perm != PermWrite {
		return fmt.Errorf("%w: %q", ErrPermission, perm)
	}
	return nil
}

// AddGrant grants principal perm on bay. It reports false when the
// grant was already there.
func (c *Catalog) AddGrant(ctx context.Context, kind, principal, bay, perm, actor string, now time.Time) (bool, error) {
	if err := checkGrant(kind, perm); err != nil {
		return false, err
	}
	if principal == "" {
		return false, fmt.Errorf("%w: empty principal", ErrPrincipal)
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	added, err := addGrant(ctx, tx, kind, principal, bay, perm, actor, now)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	return added, nil
}

func addGrant(ctx context.Context, tx *sql.Tx, kind, principal, bay, perm, actor string, now time.Time) (bool, error) {
	id, err := resolveBayID(ctx, tx, bay)
	if err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO bay_grants(principal_kind, principal, bay_id, permission, granted_at, granted_by) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
		kind, principal, id, perm, stamp(now), actor)
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	return true, queueAudit(ctx, tx, now, audit.Event{Kind: audit.BayGrantAdded, Actor: actor, Detail: fmt.Sprintf("%s %s %s on %s", kind, principal, perm, id)})
}

// RemoveGrant takes a grant away. It reports false when there was none.
func (c *Catalog) RemoveGrant(ctx context.Context, kind, principal, bay, perm, actor string, now time.Time) (bool, error) {
	if err := checkGrant(kind, perm); err != nil {
		return false, err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	id, err := resolveBayID(ctx, tx, bay)
	if err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM bay_grants WHERE principal_kind=? AND principal=? AND bay_id=? AND permission=?`, kind, principal, id, perm)
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.BayGrantRemoved, Actor: actor, Detail: fmt.Sprintf("%s %s %s on %s", kind, principal, perm, id)}); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	return true, nil
}

// Grants lists the grants held by principal, or every grant when kind
// is empty.
func (c *Catalog) Grants(ctx context.Context, kind, principal string) ([]Grant, error) {
	q := `SELECT principal_kind, principal, bay_id, permission, granted_at, granted_by FROM bay_grants`
	var args []any
	if kind != "" {
		q += ` WHERE principal_kind=? AND principal=?`
		args = append(args, kind, principal)
	}
	rows, err := c.db.QueryContext(ctx, q+` ORDER BY principal_kind, principal, bay_id, permission`, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		var at string
		if err := rows.Scan(&g.PrincipalKind, &g.Principal, &g.BayID, &g.Permission, &at, &g.GrantedBy); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		g.Granted = parseStamp(at)
		out = append(out, g)
	}
	return out, rows.Err()
}

// grantDefaultWrite gives a new device write on the default bay, inside
// the transaction that makes it, so a device made after the upgrade
// uploads where every device did before bays. Registration with bay
// grants replaces this for a device minted into named bays.
func grantDefaultWrite(ctx context.Context, tx *sql.Tx, deviceID string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO bay_grants(principal_kind, principal, bay_id, permission, granted_at, granted_by) VALUES(?,?,?,?,?,'serve') ON CONFLICT DO NOTHING`,
		PrincipalDevice, deviceID, DefaultBayID, PermWrite, stamp(now))
	if err != nil {
		return fmt.Errorf("catalog: bay grant: %w", err)
	}
	return nil
}

// RenameBay gives a bay a new name and keeps the old one as an alias,
// so agents that request it by the old name keep reaching it. The
// default bay keeps its name; an alias gives it another.
func (c *Catalog) RenameBay(ctx context.Context, ref, name, actor string, now time.Time) error {
	if !ValidBayName(name) {
		return fmt.Errorf("%w: %q", ErrBayName, name)
	}
	return c.bayChange(ctx, now, func(tx *sql.Tx) (audit.Event, error) {
		id, err := resolveBayID(ctx, tx, ref)
		if err != nil {
			return audit.Event{}, err
		}
		if id == DefaultBayID {
			return audit.Event{}, errors.New("catalog: the default bay keeps its name; give it an alias instead")
		}
		// A bay may take back one of its own aliases, such as the name it
		// had before a rename (review 1405). The alias gives way to the
		// name in the same transaction, so the name never stops resolving.
		res, err := tx.ExecContext(ctx, `DELETE FROM bay_aliases WHERE alias=? AND bay_id=?`, name, id)
		if err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if err := nameFree(ctx, tx, name); err != nil {
				return audit.Event{}, err
			}
		}
		var old string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM bays WHERE id=?`, id).Scan(&old); err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bays SET name=? WHERE id=?`, name, id); err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bay_aliases(alias, bay_id) VALUES(?,?)`, old, id); err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		return audit.Event{Kind: audit.BayRenamed, Actor: actor, Detail: fmt.Sprintf("bay %s renamed %s to %s; %s stays as an alias", id, old, name, old)}, nil
	})
}

// AliasBay gives a bay a second name it is shown and requested under.
func (c *Catalog) AliasBay(ctx context.Context, ref, alias, actor string, now time.Time) error {
	if !ValidBayName(alias) {
		return fmt.Errorf("%w: %q", ErrBayName, alias)
	}
	return c.bayChange(ctx, now, func(tx *sql.Tx) (audit.Event, error) {
		id, err := resolveBayID(ctx, tx, ref)
		if err != nil {
			return audit.Event{}, err
		}
		if err := nameFree(ctx, tx, alias); err != nil {
			return audit.Event{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bay_aliases(alias, bay_id) VALUES(?,?)`, alias, id); err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		return audit.Event{Kind: audit.BayAliasAdded, Actor: actor, Detail: fmt.Sprintf("bay %s alias %s", id, alias)}, nil
	})
}

// UnaliasBay removes an alias. An agent that still requests the bay by
// it has the request refused as a bay that does not exist.
func (c *Catalog) UnaliasBay(ctx context.Context, alias, actor string, now time.Time) error {
	return c.bayChange(ctx, now, func(tx *sql.Tx) (audit.Event, error) {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT bay_id FROM bay_aliases WHERE alias=?`, alias).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return audit.Event{}, fmt.Errorf("%w: no alias %s", ErrNoBay, alias)
		}
		if err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM bay_aliases WHERE alias=?`, alias); err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		return audit.Event{Kind: audit.BayAliasRemoved, Actor: actor, Detail: fmt.Sprintf("bay %s alias %s", id, alias)}, nil
	})
}

// DeleteBay removes a bay, its aliases and its grants, and takes it out
// of every session's membership. A session left in no bay moves to the
// default bay, whether or not the default is turned off: that switch
// applies to ingest, and a stored session is never left in no bay. No
// data is deleted. It returns how many sessions moved to the default.
func (c *Catalog) DeleteBay(ctx context.Context, ref, actor string, now time.Time) (int, error) {
	var moved int
	err := c.bayChange(ctx, now, func(tx *sql.Tx) (audit.Event, error) {
		id, err := resolveBayID(ctx, tx, ref)
		if err != nil {
			return audit.Event{}, err
		}
		if id == DefaultBayID {
			return audit.Event{}, ErrDefaultOnly
		}
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM bays WHERE id=?`, id).Scan(&name); err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO session_bays(session_uid, bay_id, added_at)
			SELECT session_uid, ?, ? FROM session_bays m
			WHERE m.bay_id = ? AND NOT EXISTS (
				SELECT 1 FROM session_bays o WHERE o.session_uid = m.session_uid AND o.bay_id <> m.bay_id)`,
			DefaultBayID, stamp(now), id)
		if err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		n, _ := res.RowsAffected()
		moved = int(n)
		for _, q := range []string{
			`DELETE FROM session_bays WHERE bay_id=?`,
			`DELETE FROM bay_grants WHERE bay_id=?`,
			`DELETE FROM bay_aliases WHERE bay_id=?`,
			`DELETE FROM bays WHERE id=?`,
		} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return audit.Event{}, fmt.Errorf("catalog: %w", err)
			}
		}
		return audit.Event{Kind: audit.BayDeleted, Actor: actor, Detail: fmt.Sprintf("bay %s (%s) deleted; %d sessions left in no other bay moved to the default", name, id, moved)}, nil
	})
	if err != nil {
		// Rolled back: nothing moved (review 1401).
		return 0, err
	}
	return moved, nil
}

// SetDefaultEnabled turns the default bay on or off. Off applies at
// ingest: a new session nothing places is refused instead of landing
// in the inbox. The bay and what is in it stay.
func (c *Catalog) SetDefaultEnabled(ctx context.Context, on bool, actor string, now time.Time) error {
	return c.bayChange(ctx, now, func(tx *sql.Tx) (audit.Event, error) {
		if _, err := tx.ExecContext(ctx, `UPDATE bays SET disabled=? WHERE id=?`, !on, DefaultBayID); err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		state := "off"
		if on {
			state = "on"
		}
		return audit.Event{Kind: audit.BayDefault, Actor: actor, Detail: "default bay turned " + state}, nil
	})
}

// bayChange runs one audited change in a transaction.
func (c *Catalog) bayChange(ctx context.Context, now time.Time, change func(*sql.Tx) (audit.Event, error)) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	e, err := change(tx)
	if err != nil {
		return err
	}
	if err := queueAudit(ctx, tx, now, e); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

// bayRoleGrantsKey marks, in lake_meta, that the groups the web config
// mapped to a role were granted the default bay once.
const bayRoleGrantsKey = "bay_role_grants_seeded"

// SeedRoleGrants runs once per lake, the first time serve starts with a
// web config after bays: every group mapped to viewer or operator reads
// the default bay, and every operator group may mint into it, so the
// upgrade changes nothing a signed-in user sees. Admin groups need no
// grant; an admin reads every bay. No group is made admin. A group
// added to the web config later gets no grant here: an admin grants it
// bays. It returns the grants it made and whether it ran.
func (c *Catalog) SeedRoleGrants(ctx context.Context, viewers, operators []string, now time.Time) ([]Grant, bool, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	var done string
	err = tx.QueryRowContext(ctx, `SELECT value FROM lake_meta WHERE key=?`, bayRoleGrantsKey).Scan(&done)
	if err == nil {
		return nil, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("catalog: %w", err)
	}
	var made []Grant
	add := func(group, perm string) error {
		ok, err := addGrant(ctx, tx, PrincipalGroup, group, DefaultBayID, perm, "serve: upgrade to bays", now)
		if ok {
			made = append(made, Grant{PrincipalKind: PrincipalGroup, Principal: group, BayID: DefaultBayID, Permission: perm})
		}
		return err
	}
	for _, g := range viewers {
		if err := add(g, PermRead); err != nil {
			return nil, false, err
		}
	}
	for _, g := range operators {
		if err := add(g, PermRead); err != nil {
			return nil, false, err
		}
		if err := add(g, PermWrite); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO lake_meta(key, value) VALUES(?, ?)`, bayRoleGrantsKey, stamp(now)); err != nil {
		return nil, false, fmt.Errorf("catalog: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("catalog: %w", err)
	}
	return made, true, nil
}

// GroupBays is the set of bay ids any of groups holds perm on.
func (c *Catalog) GroupBays(ctx context.Context, groups []string, perm string) ([]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	q := `SELECT DISTINCT bay_id FROM bay_grants WHERE principal_kind=? AND permission=? AND principal IN (?` + strings.Repeat(",?", len(groups)-1) + `) ORDER BY bay_id`
	args := []any{PrincipalGroup, perm}
	for _, g := range groups {
		args = append(args, g)
	}
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// BaySessionCounts is how many sessions each bay holds, by bay id.
func (c *Catalog) BaySessionCounts(ctx context.Context) (map[string]int, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT bay_id, count(*) FROM session_bays GROUP BY bay_id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

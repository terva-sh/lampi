package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/config"
)

// migrateProfiles adds profiles, the base configurations agents fetch,
// and profile_revisions, every document saved under a name. A profile
// row points at its current revision. A deletion is a revision too,
// with deleted set and no document, so the history of a name reads in
// order through deletions and saves.
//
// document is the canonical JSON of the profile, the bytes its version
// hashes. note is the operator's reason for the change and may be
// empty.
func migrateProfiles(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE profiles (
		name TEXT PRIMARY KEY,
		document TEXT NOT NULL,
		version TEXT NOT NULL,
		revision INTEGER NOT NULL,
		updated_at TEXT NOT NULL,
		updated_by TEXT NOT NULL
	);
	CREATE TABLE profile_revisions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		profile TEXT NOT NULL,
		document TEXT NOT NULL DEFAULT '',
		version TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		deleted INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		created_by TEXT NOT NULL
	);
	CREATE INDEX profile_revisions_profile ON profile_revisions(profile, id);`)
	return err
}

// Profile is one row of profiles. Config is Document decoded. Revision
// is the id of the revision that saved it.
type Profile struct {
	Name      string
	Config    config.Profile
	Document  string
	Version   string
	Revision  int64
	Updated   time.Time
	UpdatedBy string
}

// ProfileRevision is one saved document, or a deletion when Deleted is
// set.
type ProfileRevision struct {
	ID        int64
	Profile   string
	Document  string
	Version   string
	Note      string
	Deleted   bool
	Created   time.Time
	CreatedBy string
}

// Errors the profile calls return.
var (
	ErrNoProfile      = errors.New("catalog: no such profile")
	ErrProfileName    = errors.New("catalog: a profile name is lowercase letters, digits, '-' and '_', at most 32 characters")
	ErrDefaultProfile = errors.New("catalog: the default profile cannot be deleted")
	ErrProfileInUse   = errors.New("catalog: profile is in use")
	ErrProfileChanged = errors.New("catalog: profile changed since it was read")
)

const profileCols = `name, document, version, revision, updated_at, updated_by`

func scanProfile(row interface{ Scan(...any) error }) (Profile, error) {
	var p Profile
	var updated string
	if err := row.Scan(&p.Name, &p.Document, &p.Version, &p.Revision, &updated, &p.UpdatedBy); err != nil {
		return Profile{}, err
	}
	p.Updated = parseStamp(updated)
	// Every write stored a document ParseProfile accepted.
	if err := json.Unmarshal([]byte(p.Document), &p.Config); err != nil {
		return Profile{}, fmt.Errorf("profile %s: %w", p.Name, err)
	}
	return p, nil
}

// Profiles lists every profile by name.
func (c *Catalog) Profiles(ctx context.Context) ([]Profile, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT `+profileCols+` FROM profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProfileByName is the profile called name, or ErrNoProfile.
func (c *Catalog) ProfileByName(ctx context.Context, name string) (Profile, error) {
	p, err := scanProfile(c.db.QueryRowContext(ctx, `SELECT `+profileCols+` FROM profiles WHERE name=?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, fmt.Errorf("%w: %s", ErrNoProfile, name)
	}
	if err != nil {
		return Profile{}, fmt.Errorf("catalog: %w", err)
	}
	return p, nil
}

// PutProfile saves raw as the profile called name, as a new revision
// by actor with the operator's note, and queues profile.put in the same
// transaction. raw passes the checks an agent applies, ParseProfile,
// or it is refused, and the canonical form of what it decoded to is
// stored. A document with the version the profile already has changes
// nothing, and PutProfile reports false.
func (c *Catalog) PutProfile(ctx context.Context, name string, raw []byte, actor, note string, now time.Time) (Profile, bool, error) {
	return c.putProfile(ctx, name, raw, actor, note, -1, now)
}

// PutProfileIf is PutProfile for an editor that read revision base: 0
// for a profile that did not exist. It refuses with ErrProfileChanged
// when another save or a delete landed since, so one operator's save
// does not silently replace another's.
func (c *Catalog) PutProfileIf(ctx context.Context, name string, raw []byte, actor, note string, base int64, now time.Time) (Profile, bool, error) {
	if base < 0 {
		return Profile{}, false, fmt.Errorf("%w: revision %d", ErrProfileChanged, base)
	}
	return c.putProfile(ctx, name, raw, actor, note, base, now)
}

// putProfile saves raw; base is the revision the caller read, or -1 to
// save whatever is there.
func (c *Catalog) putProfile(ctx context.Context, name string, raw []byte, actor, note string, base int64, now time.Time) (Profile, bool, error) {
	if !config.ValidProfileName(name) {
		return Profile{}, false, fmt.Errorf("%w: %q", ErrProfileName, name)
	}
	prof, err := config.ParseProfile(raw)
	if err != nil {
		return Profile{}, false, fmt.Errorf("profile %s: %w", name, err)
	}
	doc, err := json.Marshal(prof)
	if err != nil {
		return Profile{}, false, err
	}
	p := Profile{Name: name, Config: prof, Document: string(doc), Version: prof.Version(), Updated: now.UTC(), UpdatedBy: actor}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return Profile{}, false, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	cur, err := scanProfile(tx.QueryRowContext(ctx, `SELECT `+profileCols+` FROM profiles WHERE name=?`, name))
	existed := err == nil
	switch {
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return Profile{}, false, fmt.Errorf("catalog: %w", err)
	case base >= 0 && cur.Revision != base:
		return cur, false, fmt.Errorf("%w: %s is at revision %d, not %d", ErrProfileChanged, name, cur.Revision, base)
	case existed && cur.Version == p.Version:
		return cur, false, nil
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO profile_revisions(profile, document, version, note, created_at, created_by) VALUES(?,?,?,?,?,?)`,
		name, p.Document, p.Version, note, stamp(now), actor)
	if err != nil {
		return Profile{}, false, fmt.Errorf("catalog: %w", err)
	}
	if p.Revision, err = res.LastInsertId(); err != nil {
		return Profile{}, false, fmt.Errorf("catalog: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO profiles(`+profileCols+`) VALUES(?,?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET document=excluded.document, version=excluded.version, revision=excluded.revision, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		name, p.Document, p.Version, p.Revision, stamp(now), actor); err != nil {
		return Profile{}, false, fmt.Errorf("catalog: %w", err)
	}
	changed := "new"
	if existed {
		changed = strings.Join(ChangedProfileFields(cur.Config, prof), ",")
	}
	e := audit.Event{Kind: audit.ProfilePut, Actor: actor, Detail: fmt.Sprintf("profile=%s revision=%d version=%s changed=%s", name, p.Revision, p.Version, changed)}
	if err := queueAudit(ctx, tx, now, e); err != nil {
		return Profile{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Profile{}, false, fmt.Errorf("catalog: %w", err)
	}
	return p, true, nil
}

// DeleteProfile removes the profile called name, records the deletion
// as a revision by actor with the operator's note, and queues
// profile.delete in the same transaction. The default profile cannot be
// deleted. Nor can a profile a device that is not revoked uses: the
// error names those devices.
func (c *Catalog) DeleteProfile(ctx context.Context, name, actor, note string, now time.Time) (ProfileRevision, error) {
	return c.deleteProfile(ctx, name, actor, note, -1, now)
}

// DeleteProfileIf is DeleteProfile for an editor that read revision
// base. It refuses with ErrProfileChanged when a save landed since.
func (c *Catalog) DeleteProfileIf(ctx context.Context, name, actor, note string, base int64, now time.Time) (ProfileRevision, error) {
	if base < 1 {
		return ProfileRevision{}, fmt.Errorf("%w: revision %d", ErrProfileChanged, base)
	}
	return c.deleteProfile(ctx, name, actor, note, base, now)
}

func (c *Catalog) deleteProfile(ctx context.Context, name, actor, note string, base int64, now time.Time) (ProfileRevision, error) {
	if name == config.DefaultProfile {
		return ProfileRevision{}, ErrDefaultProfile
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return ProfileRevision{}, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	var rev int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM profiles WHERE name=?`, name).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return ProfileRevision{}, fmt.Errorf("%w: %s", ErrNoProfile, name)
	}
	if err != nil {
		return ProfileRevision{}, fmt.Errorf("catalog: %w", err)
	}
	if base >= 0 && rev != base {
		return ProfileRevision{}, fmt.Errorf("%w: %s is at revision %d, not %d", ErrProfileChanged, name, rev, base)
	}
	users, err := profileUsers(ctx, tx, name)
	if err != nil {
		return ProfileRevision{}, err
	}
	if len(users) > 0 {
		return ProfileRevision{}, fmt.Errorf("%w: %s is the profile of %s", ErrProfileInUse, name, strings.Join(users, ", "))
	}
	r := ProfileRevision{Profile: name, Note: note, Deleted: true, Created: now.UTC(), CreatedBy: actor}
	res, err := tx.ExecContext(ctx, `INSERT INTO profile_revisions(profile, note, deleted, created_at, created_by) VALUES(?,?,1,?,?)`,
		name, note, stamp(now), actor)
	if err != nil {
		return ProfileRevision{}, fmt.Errorf("catalog: %w", err)
	}
	if r.ID, err = res.LastInsertId(); err != nil {
		return ProfileRevision{}, fmt.Errorf("catalog: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM profiles WHERE name=?`, name); err != nil {
		return ProfileRevision{}, fmt.Errorf("catalog: %w", err)
	}
	e := audit.Event{Kind: audit.ProfileDelete, Actor: actor, Detail: fmt.Sprintf("profile=%s revision=%d", name, r.ID)}
	if err := queueAudit(ctx, tx, now, e); err != nil {
		return ProfileRevision{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProfileRevision{}, fmt.Errorf("catalog: %w", err)
	}
	return r, nil
}

// ChangedProfileFields names the parts of a profile that differ between
// a and b, as the audit line records them: projects.allow,
// projects.deny, harnesses, agent and redaction. The revisions hold both
// documents in full.
func ChangedProfileFields(a, b config.Profile) []string {
	var out []string
	for _, f := range []struct {
		name string
		x, y any
	}{
		{"projects.allow", a.Projects.Allow, b.Projects.Allow},
		{"projects.deny", a.Projects.Deny, b.Projects.Deny},
		{"harnesses", a.Harnesses, b.Harnesses},
		{"agent", a.Agent, b.Agent},
		{"redaction", a.Redaction, b.Redaction},
	} {
		x, _ := json.Marshal(f.x)
		y, _ := json.Marshal(f.y)
		if !bytes.Equal(x, y) {
			out = append(out, f.name)
		}
	}
	if out == nil {
		out = []string{"none"}
	}
	return out
}

// profileUsers names the devices, not revoked, whose profile is name.
func profileUsers(ctx context.Context, tx *sql.Tx, name string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM devices WHERE profile=? AND revoked_at IS NULL ORDER BY name`, name)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ProfileRevisions lists the revisions of the profile called name,
// newest first, deletions included. A name never saved has none.
func (c *Catalog) ProfileRevisions(ctx context.Context, name string) ([]ProfileRevision, error) {
	return c.RecentProfileRevisions(ctx, name, -1)
}

// ProfileRevisionByID is revision id of the profile called name, or
// ErrNoProfile when name has no such revision.
func (c *Catalog) ProfileRevisionByID(ctx context.Context, name string, id int64) (ProfileRevision, error) {
	var r ProfileRevision
	var created string
	err := c.db.QueryRowContext(ctx, `SELECT id, profile, document, version, note, deleted, created_at, created_by
		FROM profile_revisions WHERE profile=? AND id=?`, name, id).Scan(&r.ID, &r.Profile, &r.Document, &r.Version, &r.Note, &r.Deleted, &created, &r.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return ProfileRevision{}, fmt.Errorf("%w: %s revision %d", ErrNoProfile, name, id)
	}
	if err != nil {
		return ProfileRevision{}, fmt.Errorf("catalog: %w", err)
	}
	r.Created = parseStamp(created)
	return r, nil
}

// RecentProfileRevisions is ProfileRevisions cut to the newest n; a
// negative n is every revision.
func (c *Catalog) RecentProfileRevisions(ctx context.Context, name string, n int) ([]ProfileRevision, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT id, profile, document, version, note, deleted, created_at, created_by
		FROM profile_revisions WHERE profile=? ORDER BY id DESC LIMIT ?`, name, n)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []ProfileRevision
	for rows.Next() {
		var r ProfileRevision
		var created string
		if err := rows.Scan(&r.ID, &r.Profile, &r.Document, &r.Version, &r.Note, &r.Deleted, &created, &r.CreatedBy); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		r.Created = parseStamp(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

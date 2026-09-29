package catalog

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// The review queue: which projects the devices hold that the operator
// has not decided about yet. The lake records when it first saw each
// project on each device, and which projects the operator hid because
// they will not be imported. A hide is lake-wide and sends nothing to
// agents. See TKT-01M3N8F354.

// Project key kinds: a project is named by its folded git remote when it
// has one, since an allow rule names the repository in every checkout,
// and otherwise by its cwd.
const (
	KeyGitRemote = "git_remote"
	KeyCWD       = "cwd"
)

// maxProjectKey bounds a key's length. An inventory path longer than
// this is not a project anyone reviews by name.
const maxProjectKey = 4096

// sightingsSince is the lake_meta key holding when sightings began.
const sightingsSince = "sightings_since"

// ProjectKey names a project lake-wide.
type ProjectKey struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`
}

// String is the key as a note or an audit line says it.
func (k ProjectKey) String() string { return k.Kind + " " + k.Key }

// Valid reports whether k could have come from an inventory.
func (k ProjectKey) Valid() bool {
	return (k.Kind == KeyGitRemote || k.Kind == KeyCWD) && k.Key != "" && len(k.Key) <= maxProjectKey &&
		!strings.ContainsAny(k.Key, "\x00\r\n")
}

// ProjectKeyOf is the key of an inventory project, as the Allow action
// words its rule; false when the project has neither remote nor cwd.
func ProjectKeyOf(p protocol.InventoryProject) (ProjectKey, bool) {
	if r := config.NormalizeRemote(p.GitRemote); r != "" {
		k := ProjectKey{KeyGitRemote, r}
		return k, k.Valid()
	}
	if c := strings.TrimSpace(p.CWD); c != "" {
		k := ProjectKey{KeyCWD, c}
		return k, k.Valid()
	}
	return ProjectKey{}, false
}

// migrateProjectReview adds project_sightings, when the lake first and
// last saw each project on each device, and hidden_projects, the
// projects an operator reviewed and chose not to import. Sightings are
// seeded from the inventories already stored, at the time each was
// received, and lake_meta records when sightings began, so a project
// seeded here reads as seen at or before then.
func migrateProjectReview(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE project_sightings (
		device_id TEXT NOT NULL REFERENCES devices(id),
		key_kind TEXT NOT NULL,
		key TEXT NOT NULL,
		first_seen_ns INTEGER NOT NULL,
		last_seen_ns INTEGER NOT NULL,
		PRIMARY KEY (device_id, key_kind, key)
	);
	CREATE TABLE hidden_projects (
		key_kind TEXT NOT NULL,
		key TEXT NOT NULL,
		hidden_by TEXT NOT NULL,
		hidden_ns INTEGER NOT NULL,
		note TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (key_kind, key)
	)`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT device_id, received_ns, inventory FROM device_inventories`)
	if err != nil {
		return err
	}
	type stored struct {
		id  string
		ns  int64
		inv protocol.AgentInventory
	}
	var all []stored
	for rows.Next() {
		var s stored
		var raw string
		if err := rows.Scan(&s.id, &s.ns, &raw); err != nil {
			rows.Close()
			return err
		}
		// An inventory that does not parse seeds nothing; the device's
		// next one records its projects.
		if json.Unmarshal([]byte(raw), &s.inv) == nil {
			all = append(all, s)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range all {
		if err := recordSightings(context.Background(), tx, s.id, s.inv, s.ns); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`INSERT INTO lake_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO NOTHING`,
		sightingsSince, stamp(time.Now()))
	return err
}

// recordSightings marks each project inv lists as seen on device id at
// ns, keeping the first time it was seen.
func recordSightings(ctx context.Context, tx *sql.Tx, id string, inv protocol.AgentInventory, ns int64) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO project_sightings (device_id, key_kind, key, first_seen_ns, last_seen_ns)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(device_id, key_kind, key) DO UPDATE SET last_seen_ns = max(last_seen_ns, excluded.last_seen_ns),
			first_seen_ns = min(first_seen_ns, excluded.first_seen_ns)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range inv.Projects {
		k, ok := ProjectKeyOf(p)
		if !ok {
			continue
		}
		if _, err := stmt.ExecContext(ctx, id, k.Kind, k.Key, ns, ns); err != nil {
			return err
		}
	}
	return nil
}

// SightingsSince is when the lake began recording sightings: a project
// first seen at that time may have been on the device before it. Zero
// on a catalog that has not recorded it.
func (c *Catalog) SightingsSince(ctx context.Context) (time.Time, error) {
	var v string
	err := c.db.QueryRowContext(ctx, `SELECT value FROM lake_meta WHERE key=?`, sightingsSince).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("catalog: %w", err)
	}
	return parseStamp(v), nil
}

// ProjectHide is the operator's decision not to import a project.
type ProjectHide struct {
	Key  ProjectKey `json:"key"`
	By   string     `json:"hidden_by"`
	At   time.Time  `json:"hidden_at"`
	Note string     `json:"note,omitempty"`
}

// HiddenProjects lists every hidden project, the newest hide first.
func (c *Catalog) HiddenProjects(ctx context.Context) ([]ProjectHide, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT key_kind, key, hidden_by, hidden_ns, note FROM hidden_projects ORDER BY hidden_ns DESC, key_kind, key`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []ProjectHide
	for rows.Next() {
		var h ProjectHide
		var ns int64
		if err := rows.Scan(&h.Key.Kind, &h.Key.Key, &h.By, &ns, &h.Note); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		h.At = time.Unix(0, ns).UTC()
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

// ErrProjectKey refuses a key no inventory could name.
var ErrProjectKey = errors.New("catalog: not a project key")

// HideProjects hides each key for actor, with note, in one transaction,
// and queues project.hidden for each key it hid. A key already hidden
// keeps its first hide. It returns the keys it hid.
func (c *Catalog) HideProjects(ctx context.Context, keys []ProjectKey, actor, note string, now time.Time) ([]ProjectKey, error) {
	return c.changeHides(ctx, keys, func(tx *sql.Tx, k ProjectKey) (sql.Result, audit.Event, error) {
		res, err := tx.ExecContext(ctx, `INSERT INTO hidden_projects (key_kind, key, hidden_by, hidden_ns, note) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(key_kind, key) DO NOTHING`, k.Kind, k.Key, actor, now.UnixNano(), note)
		detail := k.String()
		if note != "" {
			detail += " note=" + note
		}
		return res, audit.Event{Kind: audit.ProjectHidden, Actor: actor, Detail: detail}, err
	}, now)
}

// UnhideProjects removes each key's hide for actor in one transaction,
// and queues project.unhidden for each it removed. It returns those
// keys.
func (c *Catalog) UnhideProjects(ctx context.Context, keys []ProjectKey, actor string, now time.Time) ([]ProjectKey, error) {
	return c.changeHides(ctx, keys, func(tx *sql.Tx, k ProjectKey) (sql.Result, audit.Event, error) {
		res, err := tx.ExecContext(ctx, `DELETE FROM hidden_projects WHERE key_kind=? AND key=?`, k.Kind, k.Key)
		return res, audit.Event{Kind: audit.ProjectUnhidden, Actor: actor, Detail: k.String()}, err
	}, now)
}

// changeHides runs change for each key in one transaction, auditing the
// ones that changed a row.
func (c *Catalog) changeHides(ctx context.Context, keys []ProjectKey, change func(*sql.Tx, ProjectKey) (sql.Result, audit.Event, error), now time.Time) ([]ProjectKey, error) {
	for _, k := range keys {
		if !k.Valid() {
			return nil, fmt.Errorf("%w: %q", ErrProjectKey, k.String())
		}
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	var changed []ProjectKey
	for _, k := range keys {
		res, e, err := change(tx, k)
		if err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		} else if n == 0 {
			continue
		}
		if err := queueAudit(ctx, tx, now, e); err != nil {
			return nil, err
		}
		changed = append(changed, k)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return changed, nil
}

// ReviewSighting is one device's refused copy of a project.
type ReviewSighting struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	// Profile is the profile the device fetches; empty for the default.
	Profile   string                    `json:"profile"`
	Project   protocol.InventoryProject `json:"project"`
	FirstSeen time.Time                 `json:"first_seen"`
}

// ReviewProject is one refused project across every device that holds
// it.
type ReviewProject struct {
	Key       ProjectKey       `json:"key"`
	Sightings []ReviewSighting `json:"devices"`
	Sessions  int              `json:"sessions"`
	Bytes     int64            `json:"bytes"`
	Newest    time.Time        `json:"newest,omitzero"`
	// FirstSeen is the earliest any device was seen holding it.
	FirstSeen time.Time    `json:"first_seen"`
	Hidden    *ProjectHide `json:"hidden,omitempty"`
}

// StrictRefusals is what a strict device says of its refused projects:
// how many sessions and bytes, and nothing that names them.
type StrictRefusals struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Sessions   int    `json:"sessions"`
	Bytes      int64  `json:"bytes"`
}

// ReviewQueue is every refused project in the newest inventories of
// the active devices, grouped by key, newest first seen first, with the
// strict devices' totals. It does not know the profiles' rules: the
// caller decides which projects an allow rule already covers.
type ReviewQueue struct {
	Projects       []ReviewProject  `json:"projects"`
	Strict         []StrictRefusals `json:"strict"`
	SightingsSince time.Time        `json:"sightings_since,omitzero"`
}

// ReviewQueue reads the queue.
func (c *Catalog) ReviewQueue(ctx context.Context) (ReviewQueue, error) {
	var q ReviewQueue
	var err error
	if q.SightingsSince, err = c.SightingsSince(ctx); err != nil {
		return q, err
	}
	devices, err := c.Devices(ctx)
	if err != nil {
		return q, err
	}
	hides, err := c.HiddenProjects(ctx)
	if err != nil {
		return q, err
	}
	hidden := make(map[ProjectKey]*ProjectHide, len(hides))
	for i := range hides {
		hidden[hides[i].Key] = &hides[i]
	}
	groups := map[ProjectKey]*ReviewProject{}
	for _, d := range devices {
		if !d.Revoked.IsZero() {
			continue
		}
		inv, ok, err := c.DeviceInventoryOf(ctx, d.ID)
		if err != nil {
			return q, err
		}
		if !ok {
			continue
		}
		if inv.Inventory.Mode == protocol.InventoryStrict {
			if inv.Inventory.RefusedSessions > 0 {
				q.Strict = append(q.Strict, StrictRefusals{d.ID, d.Name, inv.Inventory.RefusedSessions, inv.Inventory.RefusedBytes})
			}
			continue
		}
		first, err := c.firstSeen(ctx, d.ID)
		if err != nil {
			return q, err
		}
		for _, p := range inv.Inventory.Projects {
			if p.Allowed {
				continue
			}
			k, ok := ProjectKeyOf(p)
			if !ok {
				continue
			}
			g := groups[k]
			if g == nil {
				g = &ReviewProject{Key: k, Hidden: hidden[k]}
				groups[k] = g
			}
			seen := first[k]
			if seen.IsZero() {
				// Received before its sightings were written, as a
				// write racing this read can leave it.
				seen = inv.Received
			}
			g.Sightings = append(g.Sightings, ReviewSighting{DeviceID: d.ID, DeviceName: d.Name, Profile: d.Profile, Project: p, FirstSeen: seen})
			g.Sessions += p.Sessions
			g.Bytes += p.Bytes
			if p.Newest.After(g.Newest) {
				g.Newest = p.Newest
			}
			if g.FirstSeen.IsZero() || seen.Before(g.FirstSeen) {
				g.FirstSeen = seen
			}
		}
	}
	for _, g := range groups {
		slices.SortFunc(g.Sightings, func(a, b ReviewSighting) int { return cmp.Compare(a.DeviceName, b.DeviceName) })
		q.Projects = append(q.Projects, *g)
	}
	slices.SortFunc(q.Projects, func(a, b ReviewProject) int {
		if c := b.FirstSeen.Compare(a.FirstSeen); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(a.Key.Kind, b.Key.Kind), cmp.Compare(a.Key.Key, b.Key.Key))
	})
	return q, nil
}

// firstSeen is when each project was first seen on device id.
func (c *Catalog) firstSeen(ctx context.Context, id string) (map[ProjectKey]time.Time, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT key_kind, key, first_seen_ns FROM project_sightings WHERE device_id=?`, id)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	out := map[ProjectKey]time.Time{}
	for rows.Next() {
		var k ProjectKey
		var ns int64
		if err := rows.Scan(&k.Kind, &k.Key, &ns); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out[k] = time.Unix(0, ns).UTC()
	}
	return out, rows.Err()
}

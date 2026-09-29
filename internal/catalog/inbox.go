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

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// Keeping the default bay empty (TKT-01M3NNF2FE). The default bay is an
// inbox: a session in it is one nobody has sorted. These are what an
// admin sorts it with on the lake host.

// InboxEntry is one session that needs an admin: in the default bay,
// held or flagged, or with a request the lake refused. Reasons says why,
// one line each.
type InboxEntry struct {
	SessionUID string
	Harness    string
	NativeID   string
	CWD        string
	GitRemote  string
	Bays       []string // bay ids
	Reasons    []string
}

// The reason a session in the default bay has when nothing else
// explains it.
const ReasonNothingPlaced = "no bay asked for and no rule added one"

// Inbox lists the sessions that need an admin, oldest first.
func (c *Catalog) Inbox(ctx context.Context) ([]InboxEntry, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT s.session_uid, s.harness, s.native_session_id, s.manifest_json,
			(SELECT group_concat(bay_id, ',') FROM (SELECT bay_id FROM session_bays WHERE session_uid = s.session_uid ORDER BY bay_id))
		FROM sessions s
		WHERE EXISTS (SELECT 1 FROM session_bays m WHERE m.session_uid = s.session_uid AND m.bay_id = ?)
			OR EXISTS (SELECT 1 FROM session_holds h WHERE h.session_uid = s.session_uid AND h.state IN (?, ?))
			OR EXISTS (SELECT 1 FROM session_bay_requests r WHERE r.session_uid = s.session_uid AND r.outcome = ?)
			OR NOT EXISTS (SELECT 1 FROM session_bays m WHERE m.session_uid = s.session_uid)
		ORDER BY s.ingested_at, s.session_uid`, DefaultBayID, HoldHeld, HoldFlagged, RequestRefused)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	var out []InboxEntry
	for rows.Next() {
		var e InboxEntry
		var raw string
		var bays sql.NullString
		if err := rows.Scan(&e.SessionUID, &e.Harness, &e.NativeID, &raw, &bays); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: %w", err)
		}
		var m protocol.Manifest
		if json.Unmarshal([]byte(raw), &m) == nil {
			e.CWD, e.GitRemote = m.Project.CWD, m.Project.GitRemote
		}
		if bays.String != "" {
			e.Bays = strings.Split(bays.String, ",")
		}
		out = append(out, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	names, err := c.bayNames(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Reasons, err = c.inboxReasons(ctx, out[i], names); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (c *Catalog) bayNames(ctx context.Context) (map[string]string, error) {
	bays, err := c.Bays(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, b := range bays {
		names[b.ID] = b.Name
	}
	return names, nil
}

func (c *Catalog) inboxReasons(ctx context.Context, e InboxEntry, names map[string]string) ([]string, error) {
	var out []string
	rows, err := c.db.QueryContext(ctx, `SELECT bay_id, rule_id, state FROM session_holds WHERE session_uid=? AND state IN (?,?) ORDER BY created_at`, e.SessionUID, HoldHeld, HoldFlagged)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	for rows.Next() {
		var bay, state string
		var rule int64
		if err := rows.Scan(&bay, &rule, &state); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, fmt.Sprintf("%s by hold rule %d into bay %s", state, rule, nameOr(names, bay)))
	}
	rows.Close()
	rows, err = c.db.QueryContext(ctx, `SELECT bay_ref, reason FROM session_bay_requests WHERE session_uid=? AND outcome=? ORDER BY bay_ref`, e.SessionUID, RequestRefused)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	for rows.Next() {
		var ref, reason string
		if err := rows.Scan(&ref, &reason); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, fmt.Sprintf("asked for bay %s: refused, %s", ref, reason))
	}
	rows.Close()
	switch {
	case len(e.Bays) == 0:
		out = append(out, "in no bay, which no write leaves; run serve bays move to place it")
	case slices.Contains(e.Bays, DefaultBayID) && len(out) == 0:
		out = append(out, ReasonNothingPlaced)
	}
	return out, nil
}

func nameOr(names map[string]string, id string) string {
	if n := names[id]; n != "" {
		return n
	}
	return id
}

// SessionFilter picks stored sessions for a bulk change. Every field
// set must match. Project is the lake's project id. GitRemote and
// GitRemotePrefix are compared as a projects rule compares them, and
// CWDPrefix on a path boundary. Device is a device id: a session any of
// whose copies came from the machine that device is bound to.
type SessionFilter struct {
	Project         string
	GitRemote       string
	GitRemotePrefix string
	CWDPrefix       string
	Device          string
	Harness         string
}

func (f SessionFilter) empty() bool { return f == SessionFilter{} }

// ErrNoFilter is a bulk change with no filter: it would touch every
// session in the bay, and that is asked for by name, not by leaving a
// filter out.
var ErrNoFilter = errors.New("catalog: name at least one filter")

// Move is one bulk move: the sessions in From that f matches are added
// to To and taken out of From. Moved lists them. With DryRun nothing is
// written.
type Move struct {
	From, To string // bay id, name or alias
	Filter   SessionFilter
	Actor    string
	DryRun   bool
}

// MoveSessions runs m in one transaction, auditing each membership
// change. A session taken out of its last bay goes to the default, so
// moving out of the default is the one move that never strands one.
func (c *Catalog) MoveSessions(ctx context.Context, m Move, now time.Time) ([]string, error) {
	if m.Filter.empty() {
		return nil, ErrNoFilter
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	from, err := resolveBayID(ctx, tx, m.From)
	if err != nil {
		return nil, err
	}
	to, err := resolveBayID(ctx, tx, m.To)
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, fmt.Errorf("catalog: moving from %s to itself", m.From)
	}
	uids, err := matchSessions(ctx, tx, from, m.Filter)
	if err != nil {
		return nil, err
	}
	for _, uid := range uids {
		ms := Membership{SessionUID: uid, Bay: to, Actor: m.Actor, Via: ViaCLI, Reason: "bulk move from " + from}
		if _, err := addToBay(ctx, tx, ms, now); err != nil {
			return nil, err
		}
		ms.Bay, ms.Reason = from, "bulk move to "+to
		if err := removeFromBay(ctx, tx, ms, now); err != nil {
			return nil, err
		}
	}
	if m.DryRun {
		return uids, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return uids, nil
}

// matchSessions is the sessions in bay that f matches, oldest first.
func matchSessions(ctx context.Context, tx *sql.Tx, bay string, f SessionFilter) ([]string, error) {
	var machine string
	if f.Device != "" {
		var m sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT machine_id FROM devices WHERE id=?`, f.Device).Scan(&m)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNoDevice, f.Device)
		}
		if err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if !m.Valid || m.String == "" {
			// A device that never uploaded has no sessions.
			return nil, nil
		}
		machine = m.String
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT s.session_uid, s.harness, COALESCE(s.project_id, ''), s.manifest_json
		FROM sessions s JOIN session_bays m ON m.session_uid = s.session_uid AND m.bay_id = ?
		WHERE (? = '' OR EXISTS (SELECT 1 FROM provenance p WHERE p.session_uid = s.session_uid AND p.machine_id = ?))
		ORDER BY s.ingested_at, s.session_uid`, bay, machine, machine)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	rule := config.ProjectMatch{CWDPrefix: f.CWDPrefix, GitRemote: f.GitRemote, GitRemotePrefix: f.GitRemotePrefix}
	var out []string
	for rows.Next() {
		var uid, harness, project, raw string
		if err := rows.Scan(&uid, &harness, &project, &raw); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if f.Harness != "" && harness != f.Harness || f.Project != "" && project != f.Project {
			continue
		}
		if !rule.Empty() {
			var m protocol.Manifest
			if json.Unmarshal([]byte(raw), &m) != nil || !rule.Matches(projectOf(m)) {
				continue
			}
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

// Applied is one change ApplyRules made or would make: a session added
// to a bay by an add rule, or flagged by a hold rule.
type Applied struct {
	SessionUID string
	BayID      string
	RuleID     int64
	Action     string
}

// ApplyRules routes every stored session again by the lake's rules as
// they are now, the way a manifest routes it: add-only, a hold flags,
// and requests are not replayed. With dryRun nothing is written.
func (c *Catalog) ApplyRules(ctx context.Context, actor string, dryRun bool, now time.Time) ([]Applied, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	rules, err := loadRules(ctx, tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT session_uid, manifest_json FROM sessions ORDER BY ingested_at, session_uid`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	type stored struct {
		uid string
		m   protocol.Manifest
	}
	var all []stored
	for rows.Next() {
		var s stored
		var raw string
		if err := rows.Scan(&s.uid, &raw); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if json.Unmarshal([]byte(raw), &s.m) != nil {
			continue
		}
		all = append(all, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	var out []Applied
	for _, s := range all {
		held, err := activeHold(ctx, tx, s.uid)
		if err != nil {
			return nil, err
		}
		if held != "" {
			continue
		}
		hold, err := newHold(ctx, tx, s.uid, s.m, rules)
		if err != nil {
			return nil, err
		}
		if hold != nil {
			if err := placeHold(ctx, tx, s.uid, false, *hold, s.m, now); err != nil {
				return nil, err
			}
			out = append(out, Applied{SessionUID: s.uid, BayID: hold.BayID, RuleID: hold.ID, Action: RuleHold})
			continue
		}
		before, err := bayRows(ctx, tx, s.uid)
		if err != nil {
			return nil, err
		}
		if err := place(ctx, tx, s.uid, s.m, rules, nil, actor, now); err != nil {
			return nil, err
		}
		after, err := bayRows(ctx, tx, s.uid)
		if err != nil {
			return nil, err
		}
		for _, bay := range after {
			if !slices.Contains(before, bay) {
				out = append(out, Applied{SessionUID: s.uid, BayID: bay, RuleID: addingRule(rules, s.m, bay), Action: RuleAdd})
			}
		}
	}
	if dryRun {
		return out, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

func bayRows(ctx context.Context, tx *sql.Tx, uid string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT bay_id FROM session_bays WHERE session_uid=? ORDER BY bay_id`, uid)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func addingRule(rules []BayRule, m protocol.Manifest, bay string) int64 {
	id := projectOf(m)
	for _, r := range rules {
		if r.Action == RuleAdd && r.BayID == bay && r.matches(id, m.Harness) {
			return r.ID
		}
	}
	return 0
}

// BayProblem is a membership fact no write should leave: a session in
// no bay, or a membership, request, grant or rule naming a bay that is
// gone. serve fsck reports them.
type BayProblem struct {
	What  string
	Count int
}

// BayProblems checks the bay tables against each other.
func (c *Catalog) BayProblems(ctx context.Context) ([]BayProblem, error) {
	var out []BayProblem
	for _, q := range []struct{ what, sql string }{
		{"sessions in no bay", `SELECT count(*) FROM sessions s WHERE NOT EXISTS (SELECT 1 FROM session_bays m WHERE m.session_uid = s.session_uid)`},
		{"memberships naming a bay that is gone", `SELECT count(*) FROM session_bays m WHERE NOT EXISTS (SELECT 1 FROM bays b WHERE b.id = m.bay_id)`},
		{"memberships of a session that is gone", `SELECT count(*) FROM session_bays m WHERE NOT EXISTS (SELECT 1 FROM sessions s WHERE s.session_uid = m.session_uid)`},
		{"grants naming a bay that is gone", `SELECT count(*) FROM bay_grants g WHERE NOT EXISTS (SELECT 1 FROM bays b WHERE b.id = g.bay_id)`},
		{"rules naming a bay that is gone", `SELECT count(*) FROM bay_rules r WHERE NOT EXISTS (SELECT 1 FROM bays b WHERE b.id = r.bay_id)`},
		{"aliases naming a bay that is gone", `SELECT count(*) FROM bay_aliases a WHERE NOT EXISTS (SELECT 1 FROM bays b WHERE b.id = a.bay_id)`},
		{"lakes with no default bay", `SELECT 1 - count(*) FROM bays WHERE id = '` + DefaultBayID + `' AND is_default = 1`},
	} {
		var n int
		if err := c.db.QueryRowContext(ctx, q.sql).Scan(&n); err != nil {
			return nil, fmt.Errorf("catalog: %s: %w", q.what, err)
		}
		if n > 0 {
			out = append(out, BayProblem{What: q.what, Count: n})
		}
	}
	return out, nil
}

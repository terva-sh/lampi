package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// Routing places a session in bays as it is ingested (TKT-01M3NNF29W).
// The bays the manifest asks for, those the device may write, are
// joined by what the lake's rules add and less what they deny. A hold
// rule wins over both: a new session it matches goes only into the
// hold bay, and a stored one is flagged, until an admin releases it.
// Every manifest routes again and only ever adds; nothing here takes a
// session out of a bay. A session nothing places lands in the default
// bay, or is refused when the default is off.

// The actions a bay rule takes.
const (
	RuleHold = "hold"
	RuleAdd  = "add"
	RuleDeny = "deny"
)

// The states of a session hold.
const (
	HoldHeld     = "held"
	HoldFlagged  = "flagged"
	HoldReleased = "released"
)

// The outcomes recorded for a requested bay.
const (
	RequestAccepted = "accepted"
	RequestHeld     = "held"
	RequestRefused  = "refused"
)

// The reasons a request is refused. They are recorded on the lake and
// never sent to the agent, which would learn from them that a bay it
// may not write exists.
const (
	RefusedNoBay      = "no such bay"
	RefusedNotGranted = "not granted"
	RefusedDenied     = "denied by a rule"
)

var (
	ErrRuleAction = errors.New("catalog: a rule's action is hold, add or deny")
	ErrRuleEmpty  = errors.New("catalog: a rule must match on a project field or a harness")
	ErrNoRule     = errors.New("catalog: no such rule")
	ErrNoHold     = errors.New("catalog: the session is not held")
	ErrBayHolds   = errors.New("catalog: the bay holds sessions")
	ErrManyBays   = fmt.Errorf("catalog: a manifest may ask for at most %d bays", protocol.MaxManifestBays)
)

// migrateBayRules adds the lake's routing rules and the holds they
// place on sessions.
func migrateBayRules(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE bay_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		match_json TEXT NOT NULL,
		harness TEXT NOT NULL DEFAULT '',
		action TEXT NOT NULL,
		bay_id TEXT NOT NULL,
		created_at TEXT NOT NULL,
		created_by TEXT NOT NULL
	);
	CREATE TABLE session_holds (
		session_uid TEXT NOT NULL,
		bay_id TEXT NOT NULL,
		rule_id INTEGER NOT NULL,
		state TEXT NOT NULL,
		created_at TEXT NOT NULL,
		released_at TEXT NOT NULL DEFAULT '',
		released_by TEXT NOT NULL DEFAULT '',
		routed_json TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (session_uid, bay_id)
	);
	CREATE INDEX session_holds_state ON session_holds(state);`)
	return err
}

// BayRule is one routing rule. Match is read the way a profile's allow
// rule is, exactly: a cwd_prefix covers the folder and everything under
// it, so a rule on one folder only is a cwd_hash. Harness, when set,
// must equal the session's harness too. A rule with neither matches
// nothing and is refused.
type BayRule struct {
	ID        int64
	Match     config.ProjectMatch
	Harness   string
	Action    string
	BayID     string
	Created   time.Time
	CreatedBy string
}

func (r BayRule) matches(id config.ProjectID, harness string) bool {
	if r.Harness != "" && r.Harness != harness {
		return false
	}
	if r.Match.Empty() {
		return r.Harness != ""
	}
	return r.Match.Matches(id)
}

// BayRules lists the rules, oldest first.
func (c *Catalog) BayRules(ctx context.Context) ([]BayRule, error) {
	return loadRules(ctx, c.db)
}

type rowsQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadRules(ctx context.Context, q rowsQuerier) ([]BayRule, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, match_json, harness, action, bay_id, created_at, created_by FROM bay_rules ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []BayRule
	for rows.Next() {
		var r BayRule
		var match, created string
		if err := rows.Scan(&r.ID, &match, &r.Harness, &r.Action, &r.BayID, &created, &r.CreatedBy); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if err := json.Unmarshal([]byte(match), &r.Match); err != nil {
			return nil, fmt.Errorf("catalog: rule %d: %w", r.ID, err)
		}
		r.Created = parseStamp(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddBayRule adds r, with r.BayID naming the bay by id, name or alias.
// It applies to manifests from now on; sessions already stored are
// routed by it on their next manifest.
func (c *Catalog) AddBayRule(ctx context.Context, r BayRule, actor string, now time.Time) (BayRule, error) {
	switch r.Action {
	case RuleHold, RuleAdd, RuleDeny:
	default:
		return BayRule{}, fmt.Errorf("%w: %q", ErrRuleAction, r.Action)
	}
	if r.Match.Empty() && r.Harness == "" {
		return BayRule{}, ErrRuleEmpty
	}
	if err := (config.Projects{Allow: []config.ProjectMatch{r.Match}}).Validate(); err != nil {
		return BayRule{}, fmt.Errorf("catalog: %w", err)
	}
	match, err := json.Marshal(r.Match)
	if err != nil {
		return BayRule{}, fmt.Errorf("catalog: %w", err)
	}
	err = c.bayChange(ctx, now, func(tx *sql.Tx) (audit.Event, error) {
		id, err := resolveBayID(ctx, tx, r.BayID)
		if err != nil {
			return audit.Event{}, err
		}
		r.BayID, r.Created, r.CreatedBy = id, now.UTC(), actor
		res, err := tx.ExecContext(ctx, `INSERT INTO bay_rules(match_json, harness, action, bay_id, created_at, created_by) VALUES(?,?,?,?,?,?)`,
			string(match), r.Harness, r.Action, id, stamp(now), actor)
		if err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		if r.ID, err = res.LastInsertId(); err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		return audit.Event{Kind: audit.BayRuleAdded, Actor: actor, Detail: fmt.Sprintf("rule %d: %s bay %s when %s", r.ID, r.Action, id, ruleWhen(string(match), r.Harness))}, nil
	})
	return r, err
}

func ruleWhen(match, harness string) string {
	if harness == "" {
		return match
	}
	return match + " harness " + harness
}

// RemoveBayRule deletes a rule. A hold it placed stays until released.
func (c *Catalog) RemoveBayRule(ctx context.Context, id int64, actor string, now time.Time) error {
	return c.bayChange(ctx, now, func(tx *sql.Tx) (audit.Event, error) {
		res, err := tx.ExecContext(ctx, `DELETE FROM bay_rules WHERE id=?`, id)
		if err != nil {
			return audit.Event{}, fmt.Errorf("catalog: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return audit.Event{}, fmt.Errorf("%w: %d", ErrNoRule, id)
		}
		return audit.Event{Kind: audit.BayRuleRemoved, Actor: actor, Detail: fmt.Sprintf("rule %d", id)}, nil
	})
}

// Route is who posted a manifest, which decides the bays it may ask
// for. DeviceID empty is a post with no device row, from a tool on the
// lake host or a test: every bay that exists is accepted.
type Route struct {
	DeviceID string
}

func (r Route) actor() string {
	if r.DeviceID == "" {
		return "ingest"
	}
	return "device:" + r.DeviceID
}

// IngestRouted is IngestChanged for a manifest posted by route.
func (c *Catalog) IngestRouted(ctx context.Context, m protocol.Manifest, route Route, now time.Time, decisions []Decision, blobs BlobReader) (protocol.ManifestAck, bool, error) {
	return c.ingest(ctx, m, route, now, decisions, blobs)
}

// request is one requested bay after resolution: the bay id when it
// resolved and the device may write it.
type request struct {
	ref, bayID, reason string
}

// routeSession places uid, stored or just inserted in tx, by m and the
// rules. It returns the refs it refused, for the ack.
func routeSession(ctx context.Context, tx *sql.Tx, uid string, isNew bool, m protocol.Manifest, route Route, now time.Time) ([]string, error) {
	reqs, err := resolveRequests(ctx, tx, route.DeviceID, m.Bays)
	if err != nil {
		return nil, err
	}
	rules, err := loadRules(ctx, tx)
	if err != nil {
		return nil, err
	}
	held, err := activeHold(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	var hold *BayRule
	if held == "" {
		if hold, err = newHold(ctx, tx, uid, m, rules); err != nil {
			return nil, err
		}
	}
	holding := held != "" || hold != nil
	if held != "" {
		if err := noteRouted(ctx, tx, uid, held, m); err != nil {
			return nil, err
		}
	}
	// A deny rule refuses a request it matches. A held request waits:
	// the rules are read again when the hold is released.
	denied := deniedBays(rules, m)
	var refused []string
	for i, q := range reqs {
		if q.reason == "" && !holding && denied[q.bayID] {
			q.reason = RefusedDenied
			reqs[i] = q
		}
		outcome := RequestAccepted
		switch {
		case q.reason != "":
			outcome = RequestRefused
			refused = append(refused, q.ref)
		case holding:
			outcome = RequestHeld
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_bay_requests(session_uid, bay_ref, device_id, outcome, reason, first_at, last_at)
			VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(session_uid, bay_ref) DO UPDATE SET device_id=excluded.device_id, outcome=excluded.outcome, reason=excluded.reason, last_at=excluded.last_at`,
			uid, q.ref, route.DeviceID, outcome, q.reason, stamp(now), stamp(now)); err != nil {
			return nil, fmt.Errorf("catalog: bay request: %w", err)
		}
	}
	if hold != nil {
		if err := placeHold(ctx, tx, uid, isNew, *hold, m, now); err != nil {
			return nil, err
		}
	}
	if !holding {
		var accepted []string
		for _, q := range reqs {
			if q.reason == "" {
				accepted = append(accepted, q.bayID)
			}
		}
		if err := place(ctx, tx, uid, m, rules, accepted, route.actor(), now); err != nil {
			return nil, err
		}
	}
	if isNew {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_bays WHERE session_uid=?`, uid).Scan(&n); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if n == 0 {
			if err := landInDefault(ctx, tx, uid, now); err != nil {
				return nil, err
			}
		}
	}
	return refused, nil
}

// resolveRequests resolves each distinct ref in refs. One that names
// no bay, or a bay deviceID may not write, carries its reason.
func resolveRequests(ctx context.Context, tx *sql.Tx, deviceID string, refs []string) ([]request, error) {
	if len(refs) > protocol.MaxManifestBays {
		return nil, ErrManyBays
	}
	var out []request
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		q := request{ref: ref}
		id, err := resolveBayID(ctx, tx, ref)
		switch {
		case errors.Is(err, ErrNoBay):
			q.reason = RefusedNoBay
		case err != nil:
			return nil, err
		default:
			ok, err := mayWrite(ctx, tx, deviceID, id)
			if err != nil {
				return nil, err
			}
			if ok {
				q.bayID = id
			} else {
				q.reason = RefusedNotGranted
			}
		}
		out = append(out, q)
	}
	return out, nil
}

func mayWrite(ctx context.Context, tx *sql.Tx, deviceID, bayID string) (bool, error) {
	if deviceID == "" {
		return true, nil
	}
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bay_grants WHERE principal_kind=? AND principal=? AND bay_id=? AND permission=?`,
		PrincipalDevice, deviceID, bayID, PermWrite).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	return n > 0, nil
}

// activeHold is the bay of uid's held or flagged hold, or "".
func activeHold(ctx context.Context, tx *sql.Tx, uid string) (string, error) {
	var bay string
	err := tx.QueryRowContext(ctx, `SELECT bay_id FROM session_holds WHERE session_uid=? AND state IN (?,?) ORDER BY created_at LIMIT 1`, uid, HoldHeld, HoldFlagged).Scan(&bay)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("catalog: %w", err)
	}
	return bay, nil
}

// newHold is the oldest hold rule that matches m and has not held uid
// in its bay before. A hold released once does not return for the same
// bay: releasing it was the admin's answer.
func newHold(ctx context.Context, tx *sql.Tx, uid string, m protocol.Manifest, rules []BayRule) (*BayRule, error) {
	id := projectOf(m)
	for i, r := range rules {
		if r.Action != RuleHold || !r.matches(id, m.Harness) {
			continue
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_holds WHERE session_uid=? AND bay_id=?`, uid, r.BayID).Scan(&n); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if n == 0 {
			return &rules[i], nil
		}
	}
	return nil, nil
}

func projectOf(m protocol.Manifest) config.ProjectID {
	return config.ProjectID{CWD: m.Project.CWD, CWDHash: m.Project.CWDHash, GitRemote: m.Project.GitRemote}
}

// routedJSON is what rules match of m: its harness and project. A hold
// keeps it, so a release applies the rules to what the session was
// last routed as. The session's stored manifest moves only with its
// head, and a post can change the project and keep the head.
func routedJSON(m protocol.Manifest) string {
	b, _ := json.Marshal(protocol.Manifest{Harness: m.Harness, Project: m.Project})
	return string(b)
}

// noteRouted records m as what uid was last routed as, on its hold in
// bay.
func noteRouted(ctx context.Context, tx *sql.Tx, uid, bay string, m protocol.Manifest) error {
	if _, err := tx.ExecContext(ctx, `UPDATE session_holds SET routed_json=? WHERE session_uid=? AND bay_id=?`, routedJSON(m), uid, bay); err != nil {
		return fmt.Errorf("catalog: hold: %w", err)
	}
	return nil
}

// placeHold holds a new session in the rule's bay, or flags a stored
// one, which keeps its bays, for an admin to review.
func placeHold(ctx context.Context, tx *sql.Tx, uid string, isNew bool, r BayRule, m protocol.Manifest, now time.Time) error {
	state := HoldFlagged
	if isNew {
		state = HoldHeld
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_holds(session_uid, bay_id, rule_id, state, created_at, routed_json) VALUES(?,?,?,?,?,?)`,
		uid, r.BayID, r.ID, state, stamp(now), routedJSON(m)); err != nil {
		return fmt.Errorf("catalog: hold: %w", err)
	}
	actor := "rule:" + strconv.FormatInt(r.ID, 10)
	if isNew {
		if _, err := addToBay(ctx, tx, Membership{SessionUID: uid, Bay: r.BayID, Actor: actor, Via: ViaRule, Reason: "held"}, now); err != nil {
			return err
		}
	}
	return queueAudit(ctx, tx, now, audit.Event{Kind: audit.BayHold, Actor: actor, Detail: fmt.Sprintf("session %s %s by bay %s", uid, state, r.BayID)})
}

// deniedBays is the bays a deny rule matching m names.
func deniedBays(rules []BayRule, m protocol.Manifest) map[string]bool {
	id := projectOf(m)
	denied := map[string]bool{}
	for _, r := range rules {
		if r.Action == RuleDeny && r.matches(id, m.Harness) {
			denied[r.BayID] = true
		}
	}
	return denied
}

// place adds uid to accepted and the bays add rules name, less those
// deny rules name. It only adds.
func place(ctx context.Context, tx *sql.Tx, uid string, m protocol.Manifest, rules []BayRule, accepted []string, actor string, now time.Time) error {
	id := projectOf(m)
	denied := map[string]bool{}
	added := map[string]int64{}
	for _, r := range rules {
		if !r.matches(id, m.Harness) {
			continue
		}
		switch r.Action {
		case RuleDeny:
			denied[r.BayID] = true
		case RuleAdd:
			if _, ok := added[r.BayID]; !ok {
				added[r.BayID] = r.ID
			}
		}
	}
	for _, bay := range accepted {
		if denied[bay] {
			continue
		}
		if _, err := addToBay(ctx, tx, Membership{SessionUID: uid, Bay: bay, Actor: actor, Via: ViaIngest, Reason: "requested"}, now); err != nil {
			return err
		}
	}
	bays := make([]string, 0, len(added))
	for bay := range added {
		bays = append(bays, bay)
	}
	slices.Sort(bays)
	for _, bay := range bays {
		if denied[bay] || slices.Contains(accepted, bay) {
			continue
		}
		rule := "rule:" + strconv.FormatInt(added[bay], 10)
		if _, err := addToBay(ctx, tx, Membership{SessionUID: uid, Bay: bay, Actor: rule, Via: ViaRule, Reason: "added by " + rule}, now); err != nil {
			return err
		}
	}
	return nil
}

// Hold is one session held or flagged for review.
type Hold struct {
	SessionUID string
	BayID      string
	RuleID     int64
	State      string
	Created    time.Time
}

// Holds lists the sessions held or flagged and not yet released,
// oldest first.
func (c *Catalog) Holds(ctx context.Context) ([]Hold, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT session_uid, bay_id, rule_id, state, created_at FROM session_holds WHERE state IN (?,?) ORDER BY created_at, session_uid`, HoldHeld, HoldFlagged)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []Hold
	for rows.Next() {
		var h Hold
		var created string
		if err := rows.Scan(&h.SessionUID, &h.BayID, &h.RuleID, &h.State, &created); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		h.Created = parseStamp(created)
		out = append(out, h)
	}
	return out, rows.Err()
}

// ReleaseHold ends uid's hold in one step: the bays its manifests asked
// for while held are placed as routing would have placed them, with
// the rules as they are now, and a held session leaves the hold bay
// unless it asked for it or a rule adds it. One left in no other bay
// goes to the default, whether the default is on or off: a stored
// session is never in no bay.
func (c *Catalog) ReleaseHold(ctx context.Context, uid, actor string, now time.Time) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	var bay, state, raw string
	err = tx.QueryRowContext(ctx, `SELECT bay_id, state, routed_json FROM session_holds WHERE session_uid=? AND state IN (?,?) ORDER BY created_at LIMIT 1`, uid, HoldHeld, HoldFlagged).Scan(&bay, &state, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNoHold, uid)
	}
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	// The rules match what the session was last routed as, which the
	// hold keeps (review 1433).
	var m protocol.Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return fmt.Errorf("catalog: session %s manifest: %w", uid, err)
	}
	rules, err := loadRules(ctx, tx)
	if err != nil {
		return err
	}
	// accepted holds no denied bay, so a held session that asked for
	// its hold bay still leaves it when a deny rule names it now.
	accepted, err := releaseRequests(ctx, tx, uid, deniedBays(rules, m), now)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE session_holds SET state=?, released_at=?, released_by=? WHERE session_uid=? AND bay_id=?`,
		HoldReleased, stamp(now), actor, uid, bay); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if err := place(ctx, tx, uid, m, rules, accepted, actor, now); err != nil {
		return err
	}
	if state == HoldHeld && !slices.Contains(accepted, bay) && !ruleAdds(rules, m, bay) {
		if err := leaveHoldBay(ctx, tx, uid, bay, actor, now); err != nil {
			return err
		}
	}
	if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.BayHoldReleased, Actor: actor, Detail: fmt.Sprintf("session %s %s by bay %s released", uid, state, bay)}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

// releaseRequests resolves again each request recorded as held, with
// the device's grants as they are now and refusing a denied bay,
// records the outcome, and returns the bays accepted.
func releaseRequests(ctx context.Context, tx *sql.Tx, uid string, denied map[string]bool, now time.Time) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT bay_ref, device_id FROM session_bay_requests WHERE session_uid=? AND outcome=? ORDER BY bay_ref`, uid, RequestHeld)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	type held struct{ ref, device string }
	var pending []held
	for rows.Next() {
		var h held
		if err := rows.Scan(&h.ref, &h.device); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: %w", err)
		}
		pending = append(pending, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	var accepted []string
	for _, h := range pending {
		reqs, err := resolveRequests(ctx, tx, h.device, []string{h.ref})
		if err != nil {
			return nil, err
		}
		q, outcome := reqs[0], RequestAccepted
		if q.reason == "" && denied[q.bayID] {
			q.reason = RefusedDenied
		}
		if q.reason != "" {
			outcome = RequestRefused
		} else {
			accepted = append(accepted, q.bayID)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE session_bay_requests SET outcome=?, reason=?, last_at=? WHERE session_uid=? AND bay_ref=?`,
			outcome, q.reason, stamp(now), uid, h.ref); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
	}
	return accepted, nil
}

func ruleAdds(rules []BayRule, m protocol.Manifest, bay string) bool {
	id := projectOf(m)
	added, denied := false, false
	for _, r := range rules {
		if r.BayID != bay || !r.matches(id, m.Harness) {
			continue
		}
		added = added || r.Action == RuleAdd
		denied = denied || r.Action == RuleDeny
	}
	return added && !denied
}

// leaveHoldBay takes a released session out of the hold bay. One in
// no other bay goes to the default.
func leaveHoldBay(ctx context.Context, tx *sql.Tx, uid, bay, actor string, now time.Time) error {
	res, err := tx.ExecContext(ctx, `DELETE FROM session_bays WHERE session_uid=? AND bay_id=?`, uid, bay)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		m := Membership{SessionUID: uid, Bay: bay, Actor: actor, Via: ViaCLI, Reason: "hold released"}
		if err := queueAudit(ctx, tx, now, membershipEvent(audit.BayMemberRemoved, m, bay)); err != nil {
			return err
		}
	}
	var left int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_bays WHERE session_uid=?`, uid).Scan(&left); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if left == 0 {
		_, err := addToBay(ctx, tx, Membership{SessionUID: uid, Bay: DefaultBayID, Actor: actor, Via: ViaCLI, Reason: "left in no bay"}, now)
		return err
	}
	return nil
}

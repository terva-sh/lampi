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
)

var (
	ErrMigrationInput     = errors.New("catalog: invalid bay migration request")
	ErrMigrationStale     = errors.New("catalog: migration state changed; create and review a fresh plan")
	ErrMigrationAuthority = errors.New("catalog: applying a migration requires current administrator authority and a complete administrator preview")
	ErrMigrationBlocked   = errors.New("catalog: migration has rejected items; exclude or resolve them and preview again")
	ErrMigrationExpired   = errors.New("catalog: migration preview expired; create a fresh plan")
	ErrMigrationSize      = errors.New("catalog: migration plan exceeds 8 MiB; use a smaller explicit batch")
)

// MigrationEdit is an exact per-session edit, also used by inverse plans.
// Remove never replaces unrelated membership; additions precede removals.
type MigrationEdit struct {
	UID    string   `json:"session_uid"`
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

type BayMigrationRequest struct {
	UIDs       []string        `json:"session_uids,omitempty"`
	Exclude    []string        `json:"exclude,omitempty"`
	Add        []string        `json:"add,omitempty"`
	Remove     []string        `json:"remove,omitempty"`
	Proposed   []string        `json:"proposed_bays,omitempty"`
	Edits      []MigrationEdit `json:"edits,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	RecoveryOf string          `json:"recovery_of,omitempty"`
}

type MigrationTarget struct {
	BayReference
	Proposed bool   `json:"proposed"`
	Access   string `json:"access"`
}

type MigrationItem struct {
	UID    string         `json:"session_uid"`
	Before []BayReference `json:"before"`
	After  []BayReference `json:"after"`
	Add    []string       `json:"add"`
	Remove []string       `json:"remove"`
	Status string         `json:"status"`
	Reason string         `json:"reason"`
	State  string         `json:"state,omitempty"`
}

type BayMigrationPlan struct {
	Version             int                 `json:"version"`
	ID                  string              `json:"plan_id"`
	Created             string              `json:"created_at"`
	Expires             string              `json:"expires_at"`
	Scope               string              `json:"scope"`
	CompleteMemberships bool                `json:"complete_memberships"`
	Request             BayMigrationRequest `json:"request"`
	Destinations        []MigrationTarget   `json:"destinations"`
	Items               []MigrationItem     `json:"items"`
	PolicyState         string              `json:"policy_state"`
	Changed             int                 `json:"changed"`
	Noop                int                 `json:"noop"`
	Rejected            int                 `json:"rejected"`
	Warnings            []string            `json:"warnings"`
}

func migrateBayMigrations(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE bay_migrations (id TEXT PRIMARY KEY, plan_json TEXT NOT NULL, result_json TEXT NOT NULL, created_at TEXT NOT NULL, actor TEXT NOT NULL)`)
	return err
}

func normalizedMigration(req BayMigrationRequest) (BayMigrationRequest, []MigrationEdit, error) {
	if len(req.UIDs) > MigrationMaxSessions || len(req.Exclude) > MigrationMaxSessions || len(req.Edits) > MigrationMaxSessions {
		return req, nil, ErrSelectionLimit
	}
	if len(req.Reason) > 1024 || len(req.RecoveryOf) > 128 || len(req.Proposed) > 20 {
		return req, nil, ErrMigrationInput
	}
	if req.Reason == "" {
		req.Reason = "explicit reviewed session selection"
	}
	canonical := func(values []string) ([]string, error) {
		if len(values) > 20 {
			return nil, ErrMigrationInput
		}
		values = slices.Clone(values)
		for _, v := range values {
			if v == "" || len(v) > 128 || strings.TrimSpace(v) != v {
				return nil, ErrMigrationInput
			}
		}
		slices.Sort(values)
		return slices.Compact(values), nil
	}
	var err error
	if req.Add, err = canonical(req.Add); err != nil {
		return req, nil, err
	}
	if req.Remove, err = canonical(req.Remove); err != nil {
		return req, nil, err
	}
	if req.Proposed, err = canonical(req.Proposed); err != nil {
		return req, nil, err
	}
	for _, name := range req.Proposed {
		if !ValidBayName(name) {
			return req, nil, ErrBayName
		}
	}
	req.UIDs = slices.Clone(req.UIDs)
	slices.Sort(req.UIDs)
	req.UIDs = slices.Compact(req.UIDs)
	req.Exclude = slices.Clone(req.Exclude)
	slices.Sort(req.Exclude)
	req.Exclude = slices.Compact(req.Exclude)
	for _, uid := range req.Exclude {
		if uid == "" || len(uid) > 128 || strings.TrimSpace(uid) != uid {
			return req, nil, ErrMigrationInput
		}
	}
	edits := []MigrationEdit{}
	if len(req.Edits) > 0 {
		if len(req.UIDs) > 0 || len(req.Add) > 0 || len(req.Remove) > 0 {
			return req, nil, ErrMigrationInput
		}
		for _, edit := range req.Edits {
			if edit.Add, err = canonical(edit.Add); err != nil {
				return req, nil, err
			}
			if edit.Remove, err = canonical(edit.Remove); err != nil {
				return req, nil, err
			}
			edits = append(edits, edit)
		}
	} else {
		for _, uid := range req.UIDs {
			edits = append(edits, MigrationEdit{UID: uid, Add: req.Add, Remove: req.Remove})
		}
	}
	slices.SortFunc(edits, func(a, b MigrationEdit) int { return strings.Compare(a.UID, b.UID) })
	for i, e := range edits {
		if e.UID == "" || len(e.UID) > 128 || strings.TrimSpace(e.UID) != e.UID || i > 0 && edits[i-1].UID == e.UID || len(e.Add) == 0 && len(e.Remove) == 0 {
			return req, nil, ErrMigrationInput
		}
	}
	if len(edits) == 0 {
		return req, nil, ErrSelection
	}
	req.Edits = slices.Clone(req.Edits)
	if len(req.Edits) > 0 {
		req.Edits = slices.Clone(edits)
	}
	return req, edits, nil
}

// PlanBayMigration has no writes, including when proposing a new bay.
// The returned complete bounded artifact is portable data, not authority.
func (c *Catalog) PlanBayMigration(ctx context.Context, scope Scope, req BayMigrationRequest, now time.Time) (BayMigrationPlan, error) {
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BayMigrationPlan{}, err
	}
	defer tx.Rollback()
	return planBayMigration(ctx, tx, scope, req, now)
}

func planBayMigration(ctx context.Context, tx *sql.Tx, scope Scope, req BayMigrationRequest, now time.Time) (BayMigrationPlan, error) {
	req, edits, err := normalizedMigration(req)
	if err != nil {
		return BayMigrationPlan{}, err
	}
	out := BayMigrationPlan{Version: 1, Created: stamp(now), Expires: stamp(now.Add(24 * time.Hour)), Scope: scope.Fingerprint(), CompleteMemberships: scope.All(), Request: req, Destinations: []MigrationTarget{}, Items: []MigrationItem{}, Warnings: []string{
		"Membership controls access: destination readers can read added sessions; removing membership cannot retract content already read.",
		"Unrestricted read tokens and fixed-session tokens may retain access. New bays have no grants; grants require a separate explicit action.",
		"Historical selection is frozen. Later manifests, explicit bay requests or routing rules can re-add removed membership; future routing is a separate operation.",
	}}
	if !scope.All() {
		out.Warnings = append(out.Warnings, "Only visible memberships are shown. An administrator must regenerate a complete preview before applying this advisory plan.")
	}
	proposed := map[string]bool{}
	for _, name := range req.Proposed {
		// Scoped proposals stay hypothetical: checking name availability
		// would reveal an inaccessible bay or alias through a collision.
		if scope.All() {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM bays WHERE name=?)+(SELECT count(*) FROM bay_aliases WHERE alias=?)`, name, name).Scan(&n); err != nil {
				return out, err
			}
			if n > 0 {
				return out, ErrBaySelection
			}
		}
		proposed[name] = true
	}
	targets := map[string]MigrationTarget{}
	sources := map[string]bool{}
	accessByBay := map[string]string{}
	accessFor := func(id string) (string, error) {
		if text, ok := accessByBay[id]; ok {
			return text, nil
		}
		text, err := migrationReadAccess(ctx, tx, id)
		if err == nil {
			accessByBay[id] = text
		}
		return text, err
	}
	resolve := func(ref string, add bool) (string, error) {
		if proposed[ref] {
			if !add {
				return "", ErrMigrationInput
			}
			id := "new:" + ref
			targets[id] = MigrationTarget{BayReference: BayReference{ID: id, Name: ref}, Proposed: true, Access: "New bay: administrators and unrestricted/session-scoped read tokens may read; no grants are created."}
			return id, nil
		}
		id, err := resolveBayID(ctx, tx, ref)
		if errors.Is(err, ErrNoBay) {
			return "", ErrBaySelection
		}
		if err != nil {
			return "", err
		}
		if !scope.readsBay(id) {
			return "", ErrBaySelection
		}
		// Fixed-session scopes may not discover unrelated empty bays by ref.
		if scope.limited {
			cond, args := scope.where("m.session_uid")
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_bays m WHERE m.bay_id=? AND `+cond, append([]any{id}, args...)...).Scan(&n); err != nil {
				return "", err
			}
			if n == 0 {
				return "", ErrBaySelection
			}
		}
		if add {
			var b BayReference
			if err := tx.QueryRowContext(ctx, `SELECT id,name,is_default FROM bays WHERE id=?`, id).Scan(&b.ID, &b.Name, &b.Default); err != nil {
				return "", err
			}
			access := "Existing destination readers gain access; grant principals are unavailable to this read scope."
			if scope.All() {
				access, err = accessFor(id)
				if err != nil {
					return "", err
				}
			}
			targets[id] = MigrationTarget{BayReference: b, Access: access}
		}
		return id, nil
	}
	for _, edit := range edits {
		if slices.Contains(req.Exclude, edit.UID) {
			continue
		}
		item := MigrationItem{UID: edit.UID, Before: []BayReference{}, After: []BayReference{}, Add: []string{}, Remove: []string{}, Reason: req.Reason, Status: "change"}
		cond, args := scope.where("s.session_uid")
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions s WHERE s.session_uid=? AND `+cond, append([]any{edit.UID}, args...)...).Scan(&n); err != nil {
			return out, err
		}
		if n == 0 {
			item.Status = "unknown_or_inaccessible"
			out.Rejected++
			out.Items = append(out.Items, item)
			continue
		}
		// Resolve and deduplicate aliases before finding conflicting intent.
		adds, removes := []string{}, []string{}
		for _, ref := range edit.Add {
			id, err := resolve(ref, true)
			if err != nil {
				return out, err
			}
			adds = append(adds, id)
		}
		for _, ref := range edit.Remove {
			id, err := resolve(ref, false)
			if err != nil {
				return out, err
			}
			removes = append(removes, id)
		}
		slices.Sort(adds)
		adds = slices.Compact(adds)
		slices.Sort(removes)
		removes = slices.Compact(removes)
		for _, id := range adds {
			if slices.Contains(removes, id) {
				return out, ErrSameBay
			}
		}
		all, err := migrationMemberships(ctx, tx, edit.UID)
		if err != nil {
			return out, err
		}
		after := slices.Clone(all)
		for _, b := range all {
			if scope.readsBay(b.ID) {
				item.Before = append(item.Before, b)
			}
		}
		for _, id := range adds {
			if !slices.ContainsFunc(all, func(b BayReference) bool { return b.ID == id }) {
				item.Add = append(item.Add, id)
				after = append(after, targets[id].BayReference)
			}
		}
		for _, id := range removes {
			if slices.ContainsFunc(all, func(b BayReference) bool { return b.ID == id }) {
				item.Remove = append(item.Remove, id)
				sources[id] = true
				after = slices.DeleteFunc(after, func(b BayReference) bool { return b.ID == id })
			}
		}
		// No operation may leave a session unreachable. Removing the last
		// named bay follows the existing default fallback; default itself stays.
		if len(after) == 0 {
			if slices.Contains(item.Remove, DefaultBayID) {
				item.Status = "last_default_bay"
			} else {
				after = append(after, BayReference{ID: DefaultBayID, Name: DefaultBayName, Default: true})
				item.Add = append(item.Add, DefaultBayID)
			}
		}
		slices.SortFunc(after, func(a, b BayReference) int { return strings.Compare(a.ID, b.ID) })
		for _, b := range after {
			if scope.readsBay(b.ID) || strings.HasPrefix(b.ID, "new:") {
				item.After = append(item.After, b)
			}
		}
		var held int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_holds WHERE session_uid=? AND state IN (?,?)`, edit.UID, HoldHeld, HoldFlagged).Scan(&held); err != nil {
			return out, err
		}
		item.State, err = migrationSessionState(ctx, tx, edit.UID)
		if err != nil {
			return out, err
		}
		if held > 0 {
			item.Status = "held_or_flagged"
		}
		if item.Status != "change" {
			out.Rejected++
		} else if len(item.Add)+len(item.Remove) == 0 {
			item.Status = "noop"
			out.Noop++
		} else {
			out.Changed++
		}
		out.Items = append(out.Items, item)
	}
	// Unused proposed names are almost always an input mistake.
	for name := range proposed {
		if _, ok := targets["new:"+name]; !ok {
			return out, ErrMigrationInput
		}
	}
	for _, target := range targets {
		out.Destinations = append(out.Destinations, target)
	}
	slices.SortFunc(out.Destinations, func(a, b MigrationTarget) int { return strings.Compare(a.ID, b.ID) })
	if scope.All() {
		ids := []string{}
		for id := range sources {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			access, err := accessFor(id)
			if err != nil {
				return out, err
			}
			out.Warnings = append(out.Warnings, "Removing source "+id+" can remove its readers' reach unless another membership or token scope retains access. Source readership: "+access)
		}
	}
	if scope.All() {
		out.PolicyState, err = migrationPolicyState(ctx, tx)
		if err != nil {
			return out, err
		}
	} else {
		out.PolicyState = "unavailable_in_scoped_preview"
	}
	out.ID = migrationPlanID(out)
	encoded, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if len(encoded) > 8<<20 {
		return BayMigrationPlan{}, ErrMigrationSize
	}
	return out, nil
}

func migrationMemberships(ctx context.Context, tx *sql.Tx, uid string) ([]BayReference, error) {
	rows, err := tx.QueryContext(ctx, `SELECT b.id,b.name,b.is_default FROM session_bays m JOIN bays b ON b.id=m.bay_id WHERE m.session_uid=? ORDER BY b.id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BayReference{}
	for rows.Next() {
		var b BayReference
		if err := rows.Scan(&b.ID, &b.Name, &b.Default); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func migrationPlanID(p BayMigrationPlan) string { p.ID = ""; return migrationDigest(p) }

// Principal identifiers describe access policy, never token values. Only a
// complete host/admin scope calls this; scoped previews retain generic text.
func migrationReadAccess(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bay_grants WHERE bay_id=? AND permission='read'`, id).Scan(&total); err != nil {
		return "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT principal_kind,substr(principal,1,512),length(principal)>512 FROM bay_grants WHERE bay_id=? AND permission='read' ORDER BY principal_kind,principal LIMIT 100`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	principals := []string{}
	clipped := false
	for rows.Next() {
		var kind, name string
		var shortened bool
		if err := rows.Scan(&kind, &name, &shortened); err != nil {
			return "", err
		}
		clipped = clipped || shortened
		principals = append(principals, kind+":"+name)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	text := fmt.Sprintf("%d explicit read grants", total)
	if len(principals) > 0 {
		text += " (" + strings.Join(principals, ", ") + ")"
	}
	if total > len(principals) || clipped {
		text += "; principal display clipped: inspect serve bays grants for the complete policy"
	}
	return text + ", plus administrators and unrestricted/session-scoped read tokens. Removing membership cannot retract content already read.", nil
}

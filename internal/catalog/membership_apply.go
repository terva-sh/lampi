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

type MigrationReceiptItem struct {
	UID          string         `json:"session_uid"`
	Added        []string       `json:"added"`
	Removed      []string       `json:"removed"`
	After        []BayReference `json:"after"`
	TouchedState string         `json:"touched_state"`
}

type BayMigrationReceipt struct {
	ID             string                 `json:"plan_id"`
	Actor          string                 `json:"actor"`
	Applied        string                 `json:"applied_at"`
	AlreadyApplied bool                   `json:"already_applied"`
	Complete       bool                   `json:"complete"`
	Items          []MigrationReceiptItem `json:"items"`
	CreatedBays    []BayReference         `json:"created_bays"`
}

// ApplyBayMigration requires independently established current admin
// authority. A read token or valid artifact alone is never sufficient.
// Every effective change and the receipt commit atomically, max 500 UIDs.
func (c *Catalog) ApplyBayMigration(ctx context.Context, scope Scope, admin bool, plan BayMigrationPlan, createProposed bool, actor, via string, now time.Time) (BayMigrationReceipt, error) {
	empty := BayMigrationReceipt{}
	if !admin || !scope.All() || !plan.CompleteMemberships || plan.Scope != scope.Fingerprint() || actor == "" {
		return empty, ErrMigrationAuthority
	}
	if plan.Version != 1 || plan.ID != migrationPlanID(plan) || len(plan.Items) > MigrationMaxSessions {
		return empty, ErrMigrationInput
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	// Check the durable receipt before expiry/staleness. A retry never
	// replays the mutation, even after unrelated state has changed.
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT result_json FROM bay_migrations WHERE id=?`, plan.ID).Scan(&raw)
	if err == nil {
		var receipt BayMigrationReceipt
		if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
			return empty, err
		}
		receipt.AlreadyApplied = true
		return receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	created, err := time.Parse(time.RFC3339Nano, plan.Created)
	if err != nil {
		return empty, ErrMigrationInput
	}
	expires, err := time.Parse(time.RFC3339Nano, plan.Expires)
	if err != nil || !expires.Equal(created.Add(24*time.Hour)) || now.Before(created.Add(-time.Minute)) {
		return empty, ErrMigrationInput
	}
	if !now.Before(expires) {
		return empty, ErrMigrationExpired
	}
	current, err := planBayMigration(ctx, tx, scope, admin, plan.Request, created)
	if err != nil {
		return empty, fmt.Errorf("%w: %v", ErrMigrationStale, err)
	}
	if current.ID != plan.ID {
		return empty, ErrMigrationStale
	}
	if plan.Rejected > 0 {
		return empty, ErrMigrationBlocked
	}
	if len(plan.Request.Proposed) > 0 && !createProposed {
		return empty, fmt.Errorf("%w: explicitly enable creation of proposed bays", ErrMigrationInput)
	}
	result := BayMigrationReceipt{ID: plan.ID, Actor: actor, Applied: stamp(now), Complete: true, Items: []MigrationReceiptItem{}, CreatedBays: []BayReference{}}
	replacements := map[string]string{}
	for _, target := range plan.Destinations {
		if !target.Proposed {
			continue
		}
		id, err := newBayID()
		if err != nil {
			return empty, err
		}
		if err := nameFree(ctx, tx, target.Name); err != nil {
			return empty, ErrMigrationStale
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bays(id,name,created_at,created_by) VALUES(?,?,?,?)`, id, target.Name, stamp(now), actor); err != nil {
			return empty, err
		}
		if err := queueAudit(ctx, tx, now, audit.Event{Kind: audit.BayCreated, Actor: actor, Detail: "bay " + target.Name + " (" + id + ") migration=" + plan.ID}); err != nil {
			return empty, err
		}
		replacements[target.ID] = id
		result.CreatedBays = append(result.CreatedBays, BayReference{ID: id, Name: target.Name})
	}
	for _, item := range plan.Items {
		entry := MigrationReceiptItem{UID: item.UID, Added: []string{}, Removed: []string{}}
		for _, id := range item.Add {
			if replacement := replacements[id]; replacement != "" {
				id = replacement
			}
			changed, err := addToBay(ctx, tx, Membership{SessionUID: item.UID, Bay: id, Actor: actor, Via: via, Reason: "migration " + plan.ID}, now)
			if err != nil {
				return empty, err
			}
			if changed {
				entry.Added = append(entry.Added, id)
			}
		}
		for _, id := range item.Remove {
			if err := removeFromBay(ctx, tx, Membership{SessionUID: item.UID, Bay: id, Actor: actor, Via: via, Reason: "migration " + plan.ID}, now); err != nil {
				return empty, err
			}
			entry.Removed = append(entry.Removed, id)
		}
		entry.After, err = migrationMemberships(ctx, tx, item.UID)
		if err != nil {
			return empty, err
		}
		entry.TouchedState, err = migrationTouchedState(ctx, tx, item.UID, touchedIDs(entry))
		if err != nil {
			return empty, err
		}
		result.Items = append(result.Items, entry)
	}
	if err := queueAudit(ctx, tx, now, audit.Event{Kind: "bay.migration.applied", Actor: actor, Detail: fmt.Sprintf("plan=%s sessions=%d changed=%d noop=%d", plan.ID, len(plan.Items), plan.Changed, plan.Noop)}); err != nil {
		return empty, err
	}
	pJSON, err := json.Marshal(plan)
	if err != nil {
		return empty, err
	}
	rJSON, err := json.Marshal(result)
	if err != nil {
		return empty, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bay_migrations(id,plan_json,result_json,created_at,actor) VALUES(?,?,?,?,?)`, plan.ID, string(pJSON), string(rJSON), stamp(now), actor); err != nil {
		return empty, err
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return result, nil
}

func touchedIDs(item MigrationReceiptItem) []string {
	ids := append(slices.Clone(item.Added), item.Removed...)
	slices.Sort(ids)
	return slices.Compact(ids)
}

type MigrationVerification struct {
	ID      string                      `json:"plan_id"`
	Matches bool                        `json:"matches_receipt"`
	Items   []MigrationVerificationItem `json:"items"`
}

type MigrationVerificationItem struct {
	UID         string         `json:"session_uid"`
	Matches     bool           `json:"matches_receipt"`
	Memberships []BayReference `json:"memberships"`
}

func (c *Catalog) VerifyBayMigration(ctx context.Context, scope Scope, admin bool, id string) (MigrationVerification, error) {
	out := MigrationVerification{ID: id, Matches: true, Items: []MigrationVerificationItem{}}
	// Receipts include complete membership. Only a complete admin scope
	// with independent administrator authority can retrieve them, regardless
	// of knowledge of a plan ID.
	if !admin || !scope.All() {
		return out, ErrMigrationAuthority
	}
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	receipt, err := migrationReceipt(ctx, tx, id)
	if err != nil {
		return out, err
	}
	for _, entry := range receipt.Items {
		current, err := migrationMemberships(ctx, tx, entry.UID)
		if err != nil {
			return out, err
		}
		state, err := migrationTouchedState(ctx, tx, entry.UID, touchedIDs(entry))
		if err != nil {
			return out, err
		}
		matches := migrationDigest(current) == migrationDigest(entry.After) && state == entry.TouchedState
		if !matches {
			out.Matches = false
		}
		out.Items = append(out.Items, MigrationVerificationItem{UID: entry.UID, Matches: matches, Memberships: current})
	}
	return out, nil
}

// RecoverBayMigration previews the inverse effective edits using current
// state, retaining unrelated memberships. It refuses touched memberships
// edited since apply (including remove/re-add), and never restores grants.
func (c *Catalog) RecoverBayMigration(ctx context.Context, scope Scope, admin bool, id string, now time.Time) (BayMigrationPlan, error) {
	if !admin || !scope.All() {
		return BayMigrationPlan{}, ErrMigrationAuthority
	}
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BayMigrationPlan{}, err
	}
	defer tx.Rollback()
	receipt, err := migrationReceipt(ctx, tx, id)
	if err != nil {
		return BayMigrationPlan{}, err
	}
	req := BayMigrationRequest{RecoveryOf: id, Reason: "inverse of effective changes from migration " + id}
	for _, entry := range receipt.Items {
		state, err := migrationTouchedState(ctx, tx, entry.UID, touchedIDs(entry))
		if err != nil {
			return BayMigrationPlan{}, err
		}
		if state != entry.TouchedState {
			return BayMigrationPlan{}, ErrMigrationStale
		}
		if len(entry.Added)+len(entry.Removed) > 0 {
			req.Edits = append(req.Edits, MigrationEdit{UID: entry.UID, Add: slices.Clone(entry.Removed), Remove: slices.Clone(entry.Added)})
		}
	}
	if len(req.Edits) == 0 {
		return BayMigrationPlan{}, fmt.Errorf("%w: receipt has no effective edits to reverse", ErrMigrationInput)
	}
	return planBayMigration(ctx, tx, scope, admin, req, now)
}

func migrationReceipt(ctx context.Context, tx *sql.Tx, id string) (BayMigrationReceipt, error) {
	var out BayMigrationReceipt
	if len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
		return out, ErrMigrationInput
	}
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT result_json FROM bay_migrations WHERE id=?`, id).Scan(&raw); err != nil {
		return out, err
	}
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}

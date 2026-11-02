package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

type BayRulePreviewRequest struct {
	Match    config.ProjectMatch `json:"match"`
	Harness  string              `json:"harness,omitempty"`
	Action   string              `json:"action"`
	Bay      string              `json:"bay"`
	Proposed bool                `json:"proposed_bay,omitempty"`
}

type BayRulePreview struct {
	Request  BayRulePreviewRequest `json:"request"`
	UIDs     []string              `json:"session_uids"`
	Complete bool                  `json:"complete"`
	Scope    string                `json:"scope"`
	AsOf     string                `json:"as_of"`
	Warnings []string              `json:"warnings"`
}

// PreviewBayRule evaluates a hypothetical metadata predicate without
// installing it. Its catchment is independent of historical selections.
func (c *Catalog) PreviewBayRule(ctx context.Context, scope Scope, req BayRulePreviewRequest, now time.Time) (BayRulePreview, error) {
	out := BayRulePreview{Request: req, UIDs: []string{}, Scope: scope.Fingerprint(), AsOf: stamp(now), Warnings: []string{
		"This is a hypothetical rule, not an exact historical migration or a persistent classifier. It has not been installed.",
		"Catchment includes every reachable stored metadata match, including development sessions and unlinked companions. Future sessions can also match.",
		"Once installed, add rules add membership on later manifests of existing sessions; deny rules do not remove existing membership; hold rules quarantine new sessions and flag existing ones.",
	}}
	if req.Match.Empty() && req.Harness == "" {
		return out, ErrRuleEmpty
	}
	if req.Action != RuleAdd && req.Action != RuleDeny && req.Action != RuleHold || !validHarness(req.Harness) {
		return out, ErrMigrationInput
	}
	if err := (config.Projects{Allow: []config.ProjectMatch{req.Match}}).Validate(); err != nil {
		return out, err
	}
	if req.Proposed {
		if !ValidBayName(req.Bay) {
			return out, ErrBayName
		}
	} else {
		if _, err := c.SelectReadBays(ctx, scope, req.Bay); err != nil {
			return out, err
		}
	}
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	cond, args := scope.where("s.session_uid")
	rows, err := tx.QueryContext(ctx, `SELECT s.session_uid,s.harness,s.manifest_json FROM sessions s WHERE `+cond+` ORDER BY s.session_uid`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	rule := BayRule{Match: req.Match, Harness: req.Harness}
	for rows.Next() {
		var uid, harness, raw string
		if err := rows.Scan(&uid, &harness, &raw); err != nil {
			return out, err
		}
		var m protocol.Manifest
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return out, err
		}
		if rule.matches(projectOf(m), harness) {
			out.UIDs = append(out.UIDs, uid)
			if len(out.UIDs) > MigrationMaxSessions {
				return BayRulePreview{}, ErrSelectionLimit
			}
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.Complete = true
	return out, nil
}

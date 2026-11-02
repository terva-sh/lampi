package catalog

import (
	"context"
	"errors"
	"slices"
	"strings"
)

const MigrationMaxSessions = 500

var (
	ErrSelection      = errors.New("catalog: select exact session IDs or at least one metadata filter")
	ErrSelectionLimit = errors.New("catalog: selection exceeds 500 sessions; narrow the filters or use explicit batches")
)

type SessionSelectionRequest struct {
	UIDs    []string          `json:"session_uids,omitempty"`
	Exclude []string          `json:"exclude,omitempty"`
	Filter  *DiscoveryRequest `json:"filter,omitempty"`
}

type SessionSelection struct {
	UIDs     []string                `json:"session_uids"`
	Excluded []string                `json:"excluded"`
	Complete bool                    `json:"complete"`
	Reason   string                  `json:"reason"`
	Scope    string                  `json:"scope"`
	Selector SessionSelectionRequest `json:"selector"`
}

// SelectSessions freezes complete bounded metadata matches in one SQL snapshot. A
// practical cap refuses the whole selection, never returns a silent sample.
// Explicit IDs are retained for the plan to report unknown/inaccessible ones.
func (c *Catalog) SelectSessions(ctx context.Context, scope Scope, req SessionSelectionRequest) (SessionSelection, error) {
	out := SessionSelection{UIDs: []string{}, Excluded: []string{}, Scope: scope.Fingerprint(), Reason: "explicit session IDs", Selector: req}
	if len(req.UIDs) > MigrationMaxSessions || len(req.Exclude) > MigrationMaxSessions {
		return out, ErrSelectionLimit
	}
	if req.Filter != nil && len(req.UIDs) > 0 {
		return out, ErrSelection
	}
	ids := slices.Clone(req.UIDs)
	if req.Filter != nil {
		r := *req.Filter
		if r.Cursor != "" || r.Limit != 0 || r.Harness == "" && r.Project == "" && !r.Unlinked && r.CWDPrefix == "" && r.CWDHash == "" && r.Machine == "" && !scope.selected {
			return out, ErrSelection
		}
		if _, err := r.validate("selection", scope); err != nil {
			return out, err
		}
		out.Reason = "all matching stored session metadata; AND filters"
		where, args := discoveryFilters(scope, r)
		args = append(args, MigrationMaxSessions+1)
		rows, err := c.db.QueryContext(ctx, `SELECT s.session_uid FROM sessions s WHERE `+strings.Join(where, " AND ")+` ORDER BY s.session_uid LIMIT ?`, args...)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var uid string
			if err := rows.Scan(&uid); err != nil {
				rows.Close()
				return out, err
			}
			ids = append(ids, uid)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if len(ids) > MigrationMaxSessions {
			return out, ErrSelectionLimit
		}
	}
	if req.Filter == nil && len(ids) == 0 {
		return out, ErrSelection
	}
	for _, uid := range append(slices.Clone(ids), req.Exclude...) {
		if uid == "" || len(uid) > 128 || strings.TrimSpace(uid) != uid {
			return out, ErrSelection
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for _, uid := range ids {
		if slices.Contains(req.Exclude, uid) {
			out.Excluded = append(out.Excluded, uid)
		} else {
			out.UIDs = append(out.UIDs, uid)
		}
	}
	out.Complete = true
	return out, nil
}

package catalog

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

// ErrBaySelection deliberately conflates inaccessible and unknown refs.
var ErrBaySelection = errors.New("catalog: unknown or inaccessible bay selector")

// SelectReadBays resolves up to 20 comma-separated IDs, names or aliases.
// It narrows session membership; it grants no access to a co-membership.
func (c *Catalog) SelectReadBays(ctx context.Context, scope Scope, refs string) (Scope, error) {
	if refs == "" || len(refs) > 4096 {
		return Scope{}, ErrBaySelection
	}
	parts := strings.Split(refs, ",")
	if len(parts) > 20 {
		return Scope{}, ErrBaySelection
	}
	var ids []string
	for _, ref := range parts {
		if ref == "" || strings.TrimSpace(ref) != ref {
			return Scope{}, ErrBaySelection
		}
		b, err := c.ResolveBay(ctx, ref)
		if errors.Is(err, ErrNoBay) || errors.Is(err, sql.ErrNoRows) {
			return Scope{}, ErrBaySelection
		}
		if err != nil {
			return Scope{}, err
		}
		if !scope.readsBay(b.ID) {
			return Scope{}, ErrBaySelection
		}
		if scope.limited {
			cond, args := scope.where("m.session_uid")
			var count int
			if err := c.db.QueryRowContext(ctx, `SELECT count(*) FROM session_bays m WHERE m.bay_id=? AND `+cond, append([]any{b.ID}, args...)...).Scan(&count); err != nil {
				return Scope{}, err
			}
			if count == 0 {
				return Scope{}, ErrBaySelection
			}
		}
		ids = append(ids, b.ID)
	}
	slices.Sort(ids)
	return scope.SelectBays(slices.Compact(ids)), nil
}

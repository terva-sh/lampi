package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Scope is which bays a caller reads (TKT-01M3N8KHW5). Every catalog
// function that returns sessions, or anything counted or derived from
// them, takes one, and TestEverySessionReadTakesAScope keeps it so.
// The zero Scope reads nothing, so a caller that forgets to build one
// shows an empty lake rather than the whole of it.
type Scope struct {
	all  bool
	bays []string
	// sessions, when limited, narrows the bays to these session uids: a
	// read token minted for named sessions (TKT-01M3FPWCFS).
	limited  bool
	sessions []string
}

// AllBays reads every bay: an admin, or a command on the lake host.
func AllBays() Scope { return Scope{all: true} }

// InBays reads the sessions in any of the bays, by id. No bays reads
// nothing.
func InBays(ids []string) Scope {
	return Scope{bays: slices.Clone(ids)}
}

// OnlySessions narrows s to the sessions named by uid. No uids reads
// nothing.
func (s Scope) OnlySessions(uids []string) Scope {
	s.limited, s.sessions = true, slices.Clone(uids)
	return s
}

// All reports whether the scope reads every bay and every session in
// them.
func (s Scope) All() bool { return s.all && !s.limited }

// Bays is the bay ids a limited scope reads, or nil for AllBays.
func (s Scope) Bays() []string { return slices.Clone(s.bays) }

// Reads reports whether a session in bays is inside the scope. A scope
// narrowed to named sessions cannot tell from bays alone, so it answers
// false.
func (s Scope) Reads(bays []string) bool {
	if s.limited {
		return false
	}
	if s.all {
		return true
	}
	for _, b := range bays {
		if slices.Contains(s.bays, b) {
			return true
		}
	}
	return false
}

// where is a condition that holds for a session uid column inside the
// scope, with its arguments.
func (s Scope) where(uidColumn string) (string, []any) {
	cond, args := s.bayWhere(uidColumn)
	if !s.limited || cond == "0=1" {
		return cond, args
	}
	if len(s.sessions) == 0 {
		return "0=1", nil
	}
	in := uidColumn + " IN (?" + strings.Repeat(",?", len(s.sessions)-1) + ")"
	for _, uid := range s.sessions {
		args = append(args, uid)
	}
	if s.all {
		return in, args
	}
	return "(" + cond + " AND " + in + ")", args
}

func (s Scope) bayWhere(uidColumn string) (string, []any) {
	if s.all {
		return "1=1", nil
	}
	if len(s.bays) == 0 {
		return "0=1", nil
	}
	args := make([]any, len(s.bays))
	for i, b := range s.bays {
		args[i] = b
	}
	return "EXISTS (SELECT 1 FROM session_bays sb WHERE sb.session_uid = " + uidColumn + " AND sb.bay_id IN (?" + strings.Repeat(",?", len(s.bays)-1) + "))", args
}

// ScopeFor is what a signed-in user reads: every bay for an admin, and
// otherwise the bays its groups hold read on.
func (c *Catalog) ScopeFor(ctx context.Context, admin bool, groups []string) (Scope, error) {
	if admin {
		return AllBays(), nil
	}
	bays, err := c.GroupBays(ctx, groups, PermRead)
	if err != nil {
		return Scope{}, err
	}
	return InBays(bays), nil
}

// DeviceScope is what a device reads through /v1: the bays it may
// write, which are the bays its sessions can be in by its own request.
func (c *Catalog) DeviceScope(ctx context.Context, deviceID string) (Scope, error) {
	grants, err := c.Grants(ctx, PrincipalDevice, deviceID)
	if err != nil {
		return Scope{}, err
	}
	var bays []string
	for _, g := range grants {
		if g.Permission == PermWrite {
			bays = append(bays, g.BayID)
		}
	}
	return InBays(bays), nil
}

// SessionInScope reports whether a stored session is inside the scope.
// A session that is not stored is not.
func (c *Catalog) SessionInScope(ctx context.Context, scope Scope, uid string) (bool, error) {
	cond, args := scope.where("s.session_uid")
	var one int
	err := c.db.QueryRowContext(ctx, `SELECT 1 FROM sessions s WHERE s.session_uid = ? AND `+cond, append([]any{uid}, args...)...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("catalog: %w", err)
	}
	return true, nil
}

// SessionBayNames names the bays uid is in that scope reads, sorted. A
// reader is not told the name of a bay it does not read, since a bay
// name can name a client.
func (c *Catalog) SessionBayNames(ctx context.Context, scope Scope, uid string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT b.id, b.name FROM session_bays m JOIN bays b ON b.id = m.bay_id WHERE m.session_uid = ? ORDER BY b.name`, uid)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if scope.all || slices.Contains(scope.bays, id) {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}

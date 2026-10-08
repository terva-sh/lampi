package catalog

import (
	"context"
	"strings"
)

// PageNavigation names each page's starting cursor. The first is empty.
// Current is a zero-based index. Only records in the reader's scope count.
type PageNavigation struct {
	Cursors []string
	Current int
}

// DashboardNavigation reads only ordering keys, never display projections,
// to find page boundaries for sessions or a metadata collection. The normal
// page read still validates the request and bounds the displayed records.
func (c *Catalog) DashboardNavigation(ctx context.Context, scope Scope, uid, kind string, r PageRequest) (PageNavigation, error) {
	validationKind := kind
	if kind != "sessions" {
		validationKind += ":" + uid
	}
	cur, err := r.validate(validationKind)
	if err != nil {
		return PageNavigation{}, err
	}
	r.Cursor = ""
	var query string
	var args []any
	switch kind {
	case "sessions":
		query, args = sessionSQL(scope, r, pageCursor{}, "")
		query = "SELECT s.session_uid,s.web_updated_ns" + query[strings.Index(query, " FROM sessions s WHERE "):]
	case "provenance", "artifacts", "conflicts":
		query, args = recordSQL(scope, uid, kind, r, pageCursor{})
		if kind == "provenance" {
			query = "SELECT CAST(p.rowid AS TEXT),p.rowid" + query[strings.Index(query, " FROM provenance p WHERE "):]
		} else {
			query = "SELECT a.artifact_id,0" + query[strings.Index(query, " FROM artifacts a JOIN sessions s "):]
		}
	default:
		return PageNavigation{}, ErrPage
	}
	query = strings.TrimSuffix(query, " LIMIT ?")
	args = args[:len(args)-1]
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return PageNavigation{}, err
	}
	defer rows.Close()
	nav := PageNavigation{Cursors: []string{""}}
	count, skipped := 0, 0
	previousID := ""
	var previousWhen int64
	for rows.Next() {
		var id string
		var when int64
		if err := rows.Scan(&id, &when); err != nil {
			return PageNavigation{}, err
		}
		if count > 0 && count%r.Limit == 0 {
			nav.Cursors = append(nav.Cursors, cur.encode(previousID, previousWhen))
		}
		before := false
		if cur.After != "" {
			switch kind {
			case "sessions":
				before = when > cur.When || when == cur.When && id >= cur.After
			case "provenance":
				before = when <= cur.When
			default:
				before = id <= cur.After
			}
		}
		if before {
			skipped++
		}
		previousID, previousWhen = id, when
		count++
	}
	nav.Current = min(skipped/r.Limit, len(nav.Cursors)-1)
	return nav, rows.Err()
}

package catalog

import (
	"context"
	"fmt"
)

// ReferencedDigests is every digest a catalog row names: artifact
// versions and what each grew from, session heads, provenance, and
// head update history. Each session's last manifest also names blobs;
// ListSessions returns those. serve compact keeps all of them.
func (c *Catalog) ReferencedDigests(ctx context.Context) (map[string]bool, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT sha256 FROM artifacts
		UNION SELECT grown_from FROM artifacts WHERE grown_from <> ''
		UNION SELECT head_sha256 FROM sessions
		UNION SELECT sha256 FROM provenance
		UNION SELECT old_sha256 FROM head_updates WHERE old_sha256 <> ''
		UNION SELECT new_sha256 FROM head_updates`)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		out[d] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

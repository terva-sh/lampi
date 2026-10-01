package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migrateMachineActivity indexes head_updates by machine, so the
// operations page reads each machine's newest update from the index
// rather than scanning every update the lake has recorded.
func migrateMachineActivity(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE INDEX head_updates_machine ON head_updates(machine_id, received_ns)`)
	return err
}

// MachineActivity is what the catalog knows of one machine's uploads.
// Both times are of new data: provenance records the first time a
// machine posts a digest, and head_updates a change of a session head.
// A machine that is running but has nothing new to send keeps its
// older times.
type MachineActivity struct {
	MachineID string `json:"machine_id"`
	// LastUpload is the newest artifact the machine was first to post,
	// or the newest it posted after another machine; zero for none.
	LastUpload time.Time `json:"last_upload"`
	// Sessions is how many sessions the machine has posted to.
	Sessions int64 `json:"sessions"`
	// LastUpdate is the newest head update the machine made; zero
	// for none since recording began.
	LastUpdate time.Time `json:"last_update"`
	// RecentUpdates counts its head updates at or after the since
	// argument.
	RecentUpdates int64 `json:"recent_updates"`
}

// SchemaVersion is the catalog file's schema version.
func (c *Catalog) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	if err := c.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("catalog: %w", err)
	}
	return v, nil
}

// MachinesActivity returns every machine that has posted to the lake or
// is bound to a device, ordered by machine id. RecentUpdates counts
// head updates at or after since. Uploads, session counts and updates
// count only sessions in scope; a machine is listed either way, since
// the machine list is device metadata, not session data.
func (c *Catalog) MachinesActivity(ctx context.Context, scope Scope, since time.Time) ([]MachineActivity, error) {
	inP, pArgs := scope.where("p.session_uid")
	inH, hArgs := scope.where("h.session_uid")
	args := append(append(append([]any{}, pArgs...), since.UnixNano()), hArgs...)
	rows, err := c.db.QueryContext(ctx, `
		WITH prov AS (
			SELECT machine_id, MAX(ingested_at) AS last_upload, COUNT(DISTINCT session_uid) AS sessions
			FROM provenance p WHERE `+inP+` GROUP BY machine_id
		), upd AS (
			SELECT machine_id, MAX(received_ns) AS last_ns, SUM(received_ns >= ?) AS recent
			FROM head_updates h WHERE `+inH+` GROUP BY machine_id
		), machines AS (
			SELECT machine_id FROM prov
			UNION SELECT machine_id FROM upd
			UNION SELECT machine_id FROM devices WHERE machine_id IS NOT NULL
		)
		SELECT m.machine_id, COALESCE(prov.last_upload, ''), COALESCE(prov.sessions, 0),
			COALESCE(upd.last_ns, 0), COALESCE(upd.recent, 0)
		FROM machines m
		LEFT JOIN prov ON prov.machine_id = m.machine_id
		LEFT JOIN upd ON upd.machine_id = m.machine_id
		ORDER BY m.machine_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []MachineActivity
	for rows.Next() {
		var a MachineActivity
		var upload string
		var lastNS int64
		if err := rows.Scan(&a.MachineID, &upload, &a.Sessions, &lastNS, &a.RecentUpdates); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		a.LastUpload = parseStamp(upload)
		if lastNS > 0 {
			a.LastUpdate = time.Unix(0, lastNS).UTC()
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

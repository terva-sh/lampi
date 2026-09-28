package catalog

import (
	"database/sql"
	"fmt"
	"time"
)

// migrateSubagentHeads repairs what TKT-01M3M5VEQ left in a catalog.
// Before it, a manifest's head was its first transcript, and a Claude
// session lists its subagents directory before its own transcript. So:
//
//   - A session head could be a subagent transcript. Normalization reads
//     the head's directory, so the session's own transcript was left
//     out of its events and search. The head moves back to the current
//     artifact of its kind that it is a companion of.
//   - A manifest carrying only a new subagent file related that file to
//     the session head as if the transcript had moved, and stored it as
//     a divergent_copy that is not current. The newest such copy that is
//     a companion of the session head, at a relpath with no current row,
//     becomes the current artifact at its path.
//
// Each session it changes is queued for normalization, as an ingest
// that moved the head would be. Any other copy is a transcript that
// really diverged or moved, and stays as it is.
func migrateSubagentHeads(tx *sql.Tx) error {
	type head struct {
		uid, sha, rel, kind string
	}
	rows, err := tx.Query(`
		SELECT s.session_uid, s.head_sha256, a.relpath, a.kind
		FROM sessions s JOIN artifacts a
		  ON a.session_uid = s.session_uid AND a.sha256 = s.head_sha256 AND a.current = 1`)
	if err != nil {
		return err
	}
	heads := map[string]head{}
	for rows.Next() {
		var h head
		if err := rows.Scan(&h.uid, &h.sha, &h.rel, &h.kind); err != nil {
			rows.Close()
			return err
		}
		// A digest current at two paths: the one outside the other's
		// directory is the head.
		if prev, ok := heads[h.uid]; !ok || companion(prev.rel, h.rel) {
			heads[h.uid] = h
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	changed := map[string]bool{}
	for uid, h := range heads {
		rows, err := tx.Query(`
			SELECT sha256, relpath FROM artifacts
			WHERE session_uid = ? AND kind = ? AND current = 1 ORDER BY relpath`, uid, h.kind)
		if err != nil {
			return err
		}
		var sha, rel string
		for rows.Next() {
			var s, r string
			if err := rows.Scan(&s, &r); err != nil {
				rows.Close()
				return err
			}
			if sha == "" && companion(h.rel, r) {
				sha, rel = s, r
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if sha != "" {
			if _, err := tx.Exec(`UPDATE sessions SET head_sha256 = ? WHERE session_uid = ?`, sha, uid); err != nil {
				return err
			}
			h.sha, h.rel = sha, rel
			heads[uid] = h
			changed[uid] = true
		}
	}

	type copyRow struct{ id, uid, rel string }
	rows, err = tx.Query(`
		SELECT a.artifact_id, a.session_uid, a.relpath FROM artifacts a
		WHERE a.relation = 'divergent_copy' AND a.current = 0
		  AND NOT EXISTS (SELECT 1 FROM artifacts c
		                  WHERE c.session_uid = a.session_uid AND c.relpath = a.relpath AND c.current = 1)
		  AND a.artifact_id = (SELECT MAX(b.artifact_id) FROM artifacts b
		                       WHERE b.session_uid = a.session_uid AND b.relpath = a.relpath)`)
	if err != nil {
		return err
	}
	var copies []copyRow
	for rows.Next() {
		var c copyRow
		if err := rows.Scan(&c.id, &c.uid, &c.rel); err != nil {
			rows.Close()
			return err
		}
		copies = append(copies, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range copies {
		h, ok := heads[c.uid]
		if !ok || !companion(c.rel, h.rel) {
			continue
		}
		if _, err := tx.Exec(`UPDATE artifacts SET current = 1, relation = 'head' WHERE artifact_id = ?`, c.id); err != nil {
			return err
		}
		changed[c.uid] = true
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for uid := range changed {
		if _, err := tx.Exec(`UPDATE sessions SET normalize_gen = normalize_gen + 1 WHERE session_uid = ?`, uid); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO normalize_jobs (session_uid, gen, enqueued_at)
			SELECT session_uid, normalize_gen, ? FROM sessions WHERE session_uid = ?
			ON CONFLICT(session_uid) DO UPDATE
			SET gen = excluded.gen, enqueued_at = excluded.enqueued_at`, now, uid); err != nil {
			return fmt.Errorf("queue %s: %w", uid, err)
		}
	}
	return nil
}

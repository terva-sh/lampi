package catalog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"terva.sh/lampi/internal/protocol"
)

// Project columns (TKT-01M6F7JMM85DPVQ88YSC24TENC). A session's git
// remote and folder live in manifest_json, which no index reaches. The
// dashboard's project filter and pickers need both, so each session
// keeps them in indexed columns:
//
//   - project_remote is the remote as protocol.NormalizeRemote writes
//     it, the form allow rules and project ids compare, such as
//     github.com/acme/app, or "" for none;
//   - project_cwd is the folder as the manifest names it, without a
//     trailing slash, or "".
//
// Ingest writes both whenever it writes manifest_json. Provenance
// gains an index that leads with machine_id, for a filter by machine.

// projectColumns is what a manifest's project puts in the columns.
func projectColumns(p protocol.Project) (remote, cwd string) {
	if p.GitRemote != "" {
		remote = protocol.NormalizeRemote(p.GitRemote)
	}
	cwd = p.CWD
	if len(cwd) > 1 {
		cwd = strings.TrimRight(cwd, "/")
	}
	return remote, cwd
}

// migrateProjectColumns adds the columns and their indexes, fills them
// from every stored manifest, and indexes provenance by machine. A
// manifest that cannot be read leaves its session's columns empty, as
// a session with no project has them.
func migrateProjectColumns(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE sessions ADD COLUMN project_remote TEXT NOT NULL DEFAULT '';
	ALTER TABLE sessions ADD COLUMN project_cwd TEXT NOT NULL DEFAULT '';`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT session_uid, manifest_json FROM sessions`)
	if err != nil {
		return err
	}
	type fill struct{ uid, remote, cwd string }
	var fills []fill
	for rows.Next() {
		var uid, raw string
		if err := rows.Scan(&uid, &raw); err != nil {
			rows.Close()
			return err
		}
		var m struct {
			Project protocol.Project `json:"project"`
		}
		if json.Unmarshal([]byte(raw), &m) != nil {
			continue
		}
		if r, c := projectColumns(m.Project); r != "" || c != "" {
			fills = append(fills, fill{uid, r, c})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`UPDATE sessions SET project_remote = ?, project_cwd = ? WHERE session_uid = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, f := range fills {
		if _, err := stmt.Exec(f.remote, f.cwd, f.uid); err != nil {
			return fmt.Errorf("catalog: project columns for %s: %w", f.uid, err)
		}
	}
	_, err = tx.Exec(`CREATE INDEX sessions_by_remote ON sessions(project_remote, web_updated_ns);
	CREATE INDEX sessions_by_cwd ON sessions(project_cwd, web_updated_ns);
	CREATE INDEX provenance_by_machine ON provenance(machine_id, session_uid);`)
	return err
}

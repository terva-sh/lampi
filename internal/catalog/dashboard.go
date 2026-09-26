package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrPage = errors.New("invalid filters or cursor")

type PageRequest struct {
	Harness  string
	Project  string
	Unlinked bool
	State    string
	Limit    int
	Cursor   string
	Current  bool
}
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
	AsOf       string `json:"as_of"`
}
type SessionSummary struct {
	UID          string   `json:"session_uid"`
	NativeID     string   `json:"native_session_id"`
	Harness      string   `json:"harness"`
	ProjectID    string   `json:"project_id"`
	ProjectLabel string   `json:"project_label"`
	HeadSHA256   string   `json:"head_sha256"`
	UpdatedAt    string   `json:"last_head_update"`
	State        string   `json:"normalization_state"`
	Machines     []string `json:"machines"`
	MachineCount int      `json:"machine_count"`
	updatedNS    int64
}
type Overview struct {
	Sessions      int64            `json:"sessions"`
	Artifacts     int64            `json:"artifacts"`
	Machines      int64            `json:"machines"`
	Conflicts     int64            `json:"conflicts"`
	Harnesses     map[string]int64 `json:"harnesses"`
	Normalization map[string]int64 `json:"normalization"`
	AsOf          string           `json:"as_of"`
}

// Record is a bounded metadata projection, never a raw manifest or error body.
// Paths/native labels are display previews capped at 512 characters in SQL.
type Record struct {
	ID         string `json:"id"`
	SessionUID string `json:"session_uid"`
	Kind       string `json:"kind,omitempty"`
	RelPath    string `json:"relpath"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Relation   string `json:"relation,omitempty"`
	Current    bool   `json:"current"`
	MachineID  string `json:"machine_id,omitempty"`
	HeadSHA256 string `json:"head_sha256,omitempty"`
	row        int64
}
type pageCursor struct {
	Version int    `json:"v"`
	Kind    string `json:"k"`
	Filter  string `json:"f"`
	After   string `json:"a"`
	When    int64  `json:"t"`
}

func (r PageRequest) fingerprint() string {
	r.Cursor = ""
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func (r *PageRequest) validate(kind string) (pageCursor, error) {
	if r.Limit == 0 {
		r.Limit = 50
	}
	if r.Limit < 1 || r.Limit > 200 || len(r.Project) > 4096 || len(r.Cursor) > 8192 || r.Unlinked && r.Project != "" {
		return pageCursor{}, ErrPage
	}
	switch r.Harness {
	case "", "terva", "claude", "codex", "opencode", "cursor", "cursor-cli":
	default:
		return pageCursor{}, ErrPage
	}
	switch r.State {
	case "", "pending", "failed", "ready", "unknown":
	default:
		return pageCursor{}, ErrPage
	}
	cur := pageCursor{Version: 1, Kind: kind, Filter: r.fingerprint()}
	if r.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(r.Cursor)
		if err != nil {
			return cur, ErrPage
		}
		var got pageCursor
		d := json.NewDecoder(strings.NewReader(string(b)))
		d.DisallowUnknownFields()
		if d.Decode(&got) != nil || got.Version != 1 || got.Kind != kind || got.Filter != cur.Filter || len(got.After) > 128 {
			return cur, ErrPage
		}
		cur = got
	}
	return cur, nil
}
func (cur pageCursor) encode(after string, when int64) string {
	cur.After = after
	cur.When = when
	b, _ := json.Marshal(cur)
	return base64.RawURLEncoding.EncodeToString(b)
}
func emptyPage[T any]() Page[T] {
	return Page[T]{Items: []T{}, AsOf: time.Now().UTC().Format(time.RFC3339Nano)}
}

func migrateDashboard(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE sessions ADD COLUMN web_updated_ns INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT session_uid,ingested_at FROM sessions`)
	if err != nil {
		return err
	}
	type stamp struct {
		uid string
		ns  int64
	}
	var stamps []stamp
	for rows.Next() {
		var uid, raw string
		if err := rows.Scan(&uid, &raw); err != nil {
			rows.Close()
			return err
		}
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			rows.Close()
			return errors.New("catalog: invalid existing ingest timestamp")
		}
		stamps = append(stamps, stamp{uid, t.UnixNano()})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, s := range stamps {
		if _, err := tx.Exec(`UPDATE sessions SET web_updated_ns=? WHERE session_uid=?`, s.ns, s.uid); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`CREATE INDEX web_sessions_time ON sessions(web_updated_ns DESC,session_uid DESC);
 CREATE INDEX web_sessions_harness ON sessions(harness,web_updated_ns DESC,session_uid DESC);
 CREATE INDEX web_sessions_project ON sessions(project_id,web_updated_ns DESC,session_uid DESC);
 CREATE INDEX web_sessions_project_harness ON sessions(project_id,harness,web_updated_ns DESC,session_uid DESC);
 CREATE INDEX web_artifacts ON artifacts(session_uid,artifact_id);
 CREATE INDEX web_conflicts ON artifacts(relation,artifact_id);
 CREATE INDEX web_session_conflicts ON artifacts(session_uid,relation,artifact_id);
 CREATE INDEX web_provenance ON provenance(session_uid);`)
	return err
}

func (c *Catalog) DashboardOverview(ctx context.Context) (Overview, error) {
	out := Overview{Harnesses: map[string]int64{}, Normalization: map[string]int64{"pending": 0, "failed": 0, "ready": 0, "unknown": 0}, AsOf: time.Now().UTC().Format(time.RFC3339Nano)}
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sessions),(SELECT COUNT(*) FROM artifacts),(SELECT COUNT(DISTINCT machine_id) FROM provenance),(SELECT COUNT(*) FROM artifacts WHERE relation='divergent_copy')`).Scan(&out.Sessions, &out.Artifacts, &out.Machines, &out.Conflicts)
	if err != nil {
		return out, err
	}
	for _, q := range []struct {
		sql  string
		dest map[string]int64
	}{{`SELECT harness,COUNT(*) FROM sessions GROUP BY harness`, out.Harnesses}, {`SELECT ` + normalizationStateSQL + `,COUNT(*) FROM sessions s GROUP BY 1`, out.Normalization}} {
		rows, err := tx.QueryContext(ctx, q.sql)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var k string
			var n int64
			if err := rows.Scan(&k, &n); err != nil {
				rows.Close()
				return out, err
			}
			q.dest[k] = n
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
	}
	return out, tx.Commit()
}

const sessionColumns = `s.session_uid,substr(s.native_session_id,1,512),s.harness,substr(s.project_id,1,4096),
 substr(COALESCE(NULLIF(json_extract(s.manifest_json,'$.project.git_remote'),''),NULLIF(json_extract(s.manifest_json,'$.project.cwd'),''),'Unknown project'),1,512),
 s.head_sha256,s.web_updated_ns,` + normalizationStateSQL + `,
 (SELECT COUNT(DISTINCT p.machine_id) FROM provenance p WHERE p.session_uid=s.session_uid),
 (SELECT json_group_array(machine) FROM (SELECT DISTINCT substr(p.machine_id,1,128) machine FROM provenance p WHERE p.session_uid=s.session_uid ORDER BY machine LIMIT 5))`

func sessionFilters(r PageRequest) ([]string, []any) {
	where := []string{"1=1"}
	args := []any{}
	if r.Harness != "" {
		where = append(where, "s.harness=?")
		args = append(args, r.Harness)
	}
	if r.Project != "" {
		where = append(where, "s.project_id=?")
		args = append(args, r.Project)
	} else if r.Unlinked {
		where = append(where, "s.project_id=''")
	}
	if r.State != "" {
		where = append(where, "("+normalizationStateSQL+")=?")
		args = append(args, r.State)
	}
	return where, args
}
func sessionSQL(r PageRequest, cur pageCursor, uid string) (string, []any) {
	where, args := sessionFilters(r)
	if uid != "" {
		where = append(where, "s.session_uid=?")
		args = append(args, uid)
	}
	if r.Cursor != "" {
		where = append(where, "(s.web_updated_ns,s.session_uid)<(?,?)")
		args = append(args, cur.When, cur.After)
	}
	args = append(args, r.Limit+1)
	return `SELECT ` + sessionColumns + ` FROM sessions s WHERE ` + strings.Join(where, " AND ") + ` ORDER BY s.web_updated_ns DESC,s.session_uid DESC LIMIT ?`, args
}
func (c *Catalog) DashboardSessions(ctx context.Context, r PageRequest) (Page[SessionSummary], error) {
	return c.dashboardSessions(ctx, r, "")
}
func (c *Catalog) dashboardSessions(ctx context.Context, r PageRequest, uid string) (Page[SessionSummary], error) {
	out := emptyPage[SessionSummary]()
	cur, err := r.validate("sessions")
	if err != nil {
		return out, err
	}
	query, args := sessionSQL(r, cur, uid)
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var s SessionSummary
		var machines string
		if err := rows.Scan(&s.UID, &s.NativeID, &s.Harness, &s.ProjectID, &s.ProjectLabel, &s.HeadSHA256, &s.updatedNS, &s.State, &s.MachineCount, &machines); err != nil {
			return out, err
		}
		s.UpdatedAt = time.Unix(0, s.updatedNS).UTC().Format(time.RFC3339Nano)
		if err := json.Unmarshal([]byte(machines), &s.Machines); err != nil {
			return out, err
		}
		out.Items = append(out.Items, s)
	}
	if len(out.Items) > r.Limit {
		out.Items = out.Items[:r.Limit]
		last := out.Items[len(out.Items)-1]
		out.NextCursor = cur.encode(last.UID, last.updatedNS)
	}
	return out, rows.Err()
}
func (c *Catalog) DashboardSession(ctx context.Context, uid string) (SessionSummary, error) {
	if uid == "" || len(uid) > 128 {
		return SessionSummary{}, ErrPage
	}
	p, err := c.dashboardSessions(ctx, PageRequest{Limit: 1}, uid)
	if err != nil {
		return SessionSummary{}, err
	}
	if len(p.Items) == 0 {
		return SessionSummary{}, sql.ErrNoRows
	}
	return p.Items[0], nil
}

// DashboardRecords paginates every child collection as well as the global
// conflict list. Session identity is bound into the cursor, not trusted from it.
func (c *Catalog) DashboardRecords(ctx context.Context, uid, kind string, r PageRequest) (Page[Record], error) {
	out := emptyPage[Record]()
	if len(uid) > 128 {
		return out, ErrPage
	}
	if kind != "artifacts" && kind != "provenance" && kind != "conflicts" {
		return out, ErrPage
	}
	if uid == "" && kind != "conflicts" {
		return out, ErrPage
	}
	cur, err := r.validate(kind + ":" + uid)
	if err != nil {
		return out, err
	}
	if r.Harness != "" || r.Project != "" || r.State != "" || r.Unlinked {
		return out, ErrPage
	}
	var query string
	var args []any
	if kind == "provenance" {
		query = `SELECT p.rowid,p.session_uid,substr(p.machine_id,1,128),p.sha256,substr(p.relpath,1,512) FROM provenance p WHERE p.session_uid=? AND p.rowid>? ORDER BY p.rowid LIMIT ?`
		args = []any{uid, cur.When, r.Limit + 1}
	} else {
		where := []string{"a.artifact_id>?"}
		args = []any{cur.After}
		if uid != "" {
			where = append(where, "a.session_uid=?")
			args = append(args, uid)
		}
		if kind == "conflicts" {
			where = append(where, "a.relation='divergent_copy'")
		}
		if r.Current {
			where = append(where, "a.current=1")
		}
		query = `SELECT a.artifact_id,a.session_uid,a.kind,substr(a.relpath,1,512),a.sha256,a.size,a.relation,a.current,s.head_sha256 FROM artifacts a JOIN sessions s ON s.session_uid=a.session_uid WHERE ` + strings.Join(where, " AND ") + ` ORDER BY a.artifact_id LIMIT ?`
		args = append(args, r.Limit+1)
	}
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Record
		if kind == "provenance" {
			err = rows.Scan(&a.row, &a.SessionUID, &a.MachineID, &a.SHA256, &a.RelPath)
			a.ID = fmt.Sprint(a.row)
		} else {
			err = rows.Scan(&a.ID, &a.SessionUID, &a.Kind, &a.RelPath, &a.SHA256, &a.Size, &a.Relation, &a.Current, &a.HeadSHA256)
		}
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, a)
	}
	if len(out.Items) > r.Limit {
		out.Items = out.Items[:r.Limit]
		last := out.Items[len(out.Items)-1]
		out.NextCursor = cur.encode(last.ID, last.row)
	}
	return out, rows.Err()
}

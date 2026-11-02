package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// DiscoveryRequest selects metadata, independently of transcript/index
// availability. Filters are ANDed; pagination is bound to the scope.
type DiscoveryRequest struct {
	Harness   string `json:"harness,omitempty"`
	Project   string `json:"project,omitempty"`
	Unlinked  bool   `json:"unlinked,omitempty"`
	CWDPrefix string `json:"cwd_prefix,omitempty"`
	CWDHash   string `json:"cwd_hash,omitempty"`
	Machine   string `json:"machine,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
}

// DiscoveredBay discloses no creator or grant principals. Sessions counts
// only reachable sessions, not every session that shares this bay.
type BayReference struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

type DiscoveredBay struct {
	BayReference
	Sessions int `json:"sessions"`
}

type DiscoveredProject struct {
	ID          string `json:"project_id"`
	IDTruncated bool   `json:"id_truncated"`
	pageKey     string
	Label       string `json:"label"`
	Sessions    int    `json:"sessions"`
}

// SessionMetadata contains only the selected session's routing evidence.
// Workflow is unknown until a capture source explicitly records it; an
// SDK origin or a display label is not invented workflow provenance.
type SessionMetadata struct {
	SessionSummary
	CWD               string         `json:"cwd"`
	CWDHash           string         `json:"cwd_hash"`
	GitRemote         string         `json:"git_remote"`
	Workflow          *string        `json:"workflow"`
	Bays              []BayReference `json:"bays"`
	MetadataTruncated bool           `json:"metadata_truncated"`
	MachinesTruncated bool           `json:"machines_truncated"`
}

func (r *DiscoveryRequest) validate(kind string, scope Scope) (pageCursor, error) {
	if r.Limit == 0 {
		r.Limit = 50
	}
	if _, err := (&PageRequest{Harness: r.Harness, Project: r.Project, Unlinked: r.Unlinked, Limit: r.Limit}).validate(kind); err != nil {
		return pageCursor{}, err
	}
	if len(r.CWDPrefix) > 4096 || r.CWDPrefix != "" && !strings.HasPrefix(r.CWDPrefix, "/") || len(r.CWDHash) > 128 || len(r.Machine) > 128 || len(r.Cursor) > 16384 {
		return pageCursor{}, ErrPage
	}
	filter := *r
	filter.Cursor = ""
	b, _ := json.Marshal([]any{filter, scope.Fingerprint()})
	hash := sha256.Sum256(b)
	cur := pageCursor{Version: 1, Kind: kind, Filter: base64.RawURLEncoding.EncodeToString(hash[:])}
	if r.Cursor == "" {
		return cur, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(r.Cursor)
	if err != nil {
		return cur, ErrPage
	}
	var got pageCursor
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if d.Decode(&got) != nil || got.Version != cur.Version || got.Kind != kind || got.Filter != cur.Filter || len(got.After) > 4096 {
		return cur, ErrPage
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return cur, ErrPage
	}
	return got, nil
}

func discoveryFilters(scope Scope, r DiscoveryRequest) ([]string, []any) {
	where, args := sessionFilters(scope, PageRequest{Harness: r.Harness, Project: r.Project, Unlinked: r.Unlinked})
	if r.CWDPrefix != "" {
		prefix := strings.TrimRight(r.CWDPrefix, "/")
		where = append(where, `(json_extract(s.manifest_json,'$.project.cwd')=? OR substr(json_extract(s.manifest_json,'$.project.cwd'),1,?)=?)`)
		args = append(args, prefix, utf8.RuneCountInString(prefix)+1, prefix+"/")
	}
	if r.CWDHash != "" {
		where = append(where, `json_extract(s.manifest_json,'$.project.cwd_hash')=?`)
		args = append(args, r.CWDHash)
	}
	if r.Machine != "" {
		where = append(where, `EXISTS (SELECT 1 FROM provenance p WHERE p.session_uid=s.session_uid AND p.machine_id=?)`)
		args = append(args, r.Machine)
	}
	return where, args
}

// DiscoverSessions pages stored sessions even when normalization failed or
// no search event matches. Rows and every child projection stay in scope.
func (c *Catalog) DiscoverSessions(ctx context.Context, scope Scope, r DiscoveryRequest) (Page[SessionMetadata], error) {
	out := emptyPage[SessionMetadata]()
	cur, err := r.validate("discover_sessions", scope)
	if err != nil {
		return out, err
	}
	where, args := discoveryFilters(scope, r)
	if r.Cursor != "" {
		where = append(where, `(s.web_updated_ns,s.session_uid)<(?,?)`)
		args = append(args, cur.When, cur.After)
	}
	args = append(args, r.Limit+1)
	rows, err := c.db.QueryContext(ctx, `SELECT `+sessionColumns+`,
		substr(COALESCE(json_extract(s.manifest_json,'$.project.cwd'),''),1,4096),
		substr(COALESCE(json_extract(s.manifest_json,'$.project.cwd_hash'),''),1,128),
		substr(COALESCE(json_extract(s.manifest_json,'$.project.git_remote'),''),1,4096),
		(length(s.native_session_id)>512 OR length(s.project_id)>4096 OR length(COALESCE(NULLIF(json_extract(s.manifest_json,'$.project.git_remote'),''),json_extract(s.manifest_json,'$.project.cwd'),''))>512 OR length(COALESCE(json_extract(s.manifest_json,'$.project.cwd'),''))>4096 OR length(COALESCE(json_extract(s.manifest_json,'$.project.git_remote'),''))>4096 OR length(COALESCE(json_extract(s.manifest_json,'$.project.cwd_hash'),''))>128)
		FROM sessions s WHERE `+strings.Join(where, " AND ")+` ORDER BY s.web_updated_ns DESC,s.session_uid DESC LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item SessionMetadata
		var machines string
		if err := rows.Scan(&item.UID, &item.NativeID, &item.Harness, &item.ProjectID, &item.ProjectLabel, &item.HeadSHA256, &item.updatedNS, &item.State, &item.MachineCount, &machines, &item.CWD, &item.CWDHash, &item.GitRemote, &item.MetadataTruncated); err != nil {
			return out, err
		}
		item.UpdatedAt = time.Unix(0, item.updatedNS).UTC().Format(time.RFC3339Nano)
		if err := json.Unmarshal([]byte(machines), &item.Machines); err != nil {
			return out, err
		}
		item.MachinesTruncated = item.MachineCount > len(item.Machines)
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	// The catalog has one connection. Close before the bounded child reads.
	rows.Close()
	if len(out.Items) > r.Limit {
		out.Items = out.Items[:r.Limit]
		last := out.Items[len(out.Items)-1]
		out.NextCursor = cur.encode(last.UID, last.updatedNS)
	}
	for i := range out.Items {
		out.Items[i].Bays, err = c.discoveredMemberships(ctx, scope, out.Items[i].UID)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// DiscoverSession deliberately treats an inaccessible ID like an unknown ID.
func (c *Catalog) DiscoverSession(ctx context.Context, scope Scope, uid string) (SessionMetadata, error) {
	if uid == "" || len(uid) > 128 {
		return SessionMetadata{}, ErrPage
	}
	// Narrowing keeps the same bay restrictions and yields at most one row.
	if !scope.names(uid) {
		return SessionMetadata{}, sql.ErrNoRows
	}
	page, err := c.DiscoverSessions(ctx, scope.OnlySessions([]string{uid}), DiscoveryRequest{Limit: 1})
	if err != nil {
		return SessionMetadata{}, err
	}
	if len(page.Items) == 0 {
		return SessionMetadata{}, sql.ErrNoRows
	}
	return page.Items[0], nil
}

func (c *Catalog) discoveredMemberships(ctx context.Context, scope Scope, uid string) ([]BayReference, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT b.id,b.name,b.is_default FROM bays b JOIN session_bays m ON m.bay_id=b.id WHERE m.session_uid=? ORDER BY b.name`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BayReference{}
	for rows.Next() {
		var b BayReference
		if err := rows.Scan(&b.ID, &b.Name, &b.Default); err != nil {
			return nil, err
		}
		if scope.readsBay(b.ID) {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

// DiscoverBays returns permitted bays and counts only reachable sessions.
// Session-limited callers learn only bays containing their named sessions.
func (c *Catalog) DiscoverBays(ctx context.Context, scope Scope, r DiscoveryRequest) (Page[DiscoveredBay], error) {
	out := emptyPage[DiscoveredBay]()
	cur, err := r.validate("discover_bays", scope)
	if err != nil || r.Harness != "" || r.Project != "" || r.Unlinked || r.CWDPrefix != "" || r.CWDHash != "" || r.Machine != "" {
		return out, ErrPage
	}
	cond, args := scope.where("s.session_uid")
	bayCond := "1=1"
	args = append(args, cur.After)
	if !scope.all {
		bayCond = "0=1"
		if len(scope.bays) > 0 {
			bayCond = "b.id IN (?" + strings.Repeat(",?", len(scope.bays)-1) + ")"
			for _, id := range scope.bays {
				args = append(args, id)
			}
		}
	}
	query := `SELECT * FROM (SELECT b.id,b.name,b.is_default,(SELECT count(*) FROM session_bays m JOIN sessions s ON s.session_uid=m.session_uid WHERE m.bay_id=b.id AND ` + cond + `) AS sessions FROM bays b WHERE b.id>? AND ` + bayCond + `)`
	if scope.limited {
		query += ` WHERE sessions>0`
	}
	query += ` ORDER BY id LIMIT ?`
	args = append(args, r.Limit+1)
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var b DiscoveredBay
		if err := rows.Scan(&b.ID, &b.Name, &b.Default, &b.Sessions); err != nil {
			return out, err
		}
		out.Items = append(out.Items, b)
		if len(out.Items) > r.Limit {
			out.Items = out.Items[:r.Limit]
			out.NextCursor = cur.encode(out.Items[len(out.Items)-1].ID, 0)
			break
		}
	}
	return out, rows.Err()
}

// DiscoverProjects groups only stored, reachable sessions. Unlinked
// sessions remain available through DiscoverSessions(Unlinked: true).
func (c *Catalog) DiscoverProjects(ctx context.Context, scope Scope, r DiscoveryRequest) (Page[DiscoveredProject], error) {
	out := emptyPage[DiscoveredProject]()
	cur, err := r.validate("discover_projects", scope)
	if err != nil || r.Unlinked {
		return out, ErrPage
	}
	where, args := discoveryFilters(scope, r)
	where = append(where, `s.project_id<>''`)
	args = append(args, cur.After, r.Limit+1)
	rows, err := c.db.QueryContext(ctx, `SELECT CASE WHEN length(CAST(s.project_id AS BLOB))>4096 THEN substr(s.project_id,1,512) ELSE s.project_id END, length(CAST(s.project_id AS BLOB))>4096,MIN(s.session_uid),
		MIN(substr(COALESCE(NULLIF(json_extract(s.manifest_json,'$.project.git_remote'),''),NULLIF(json_extract(s.manifest_json,'$.project.cwd'),''),'Unknown project'),1,512)),count(*)
		FROM sessions s WHERE `+strings.Join(where, " AND ")+` GROUP BY s.project_id HAVING MIN(s.session_uid)>? ORDER BY MIN(s.session_uid) LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p DiscoveredProject
		if err := rows.Scan(&p.ID, &p.IDTruncated, &p.pageKey, &p.Label, &p.Sessions); err != nil {
			return out, err
		}
		out.Items = append(out.Items, p)
	}
	if len(out.Items) > r.Limit {
		out.Items = out.Items[:r.Limit]
		out.NextCursor = cur.encode(out.Items[len(out.Items)-1].pageKey, 0)
	}
	return out, rows.Err()
}

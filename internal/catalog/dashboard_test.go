package catalog

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedDashboard(t *testing.T, n int) *Catalog {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < n; i++ {
		uid := fmt.Sprintf("session-%06d", i)
		h := "codex"
		project := "repo"
		if i%2 == 0 {
			h = "terva"
			project = ""
		}
		ns := time.Date(2026, 9, 26, 1, 0, 0, i%3, time.UTC).UnixNano()
		_, err = tx.Exec(`INSERT INTO sessions(session_uid,harness,native_session_id,head_sha256,manifest_json,ingested_at,project_id,web_updated_ns) VALUES(?,?,?,'head','{"project":{"cwd":"<script>synthetic</script>"}}',?,?,?)`, uid, h, uid, time.Unix(0, ns).UTC().Format(time.RFC3339Nano), project, ns)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(`INSERT INTO artifacts(artifact_id,session_uid,kind,relpath,sha256,size,relation,current) VALUES(?,?,'transcript_jsonl','session.jsonl','head',10,'head',1)`, fmt.Sprintf("artifact-%06d", i), uid)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(`INSERT INTO provenance(session_uid,machine_id,sha256,relpath,ingested_at) VALUES(?,'machine-a','head','session.jsonl','2026-09-26T00:00:00Z')`, uid)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestDashboardPaginationAndFilters(t *testing.T) {
	c := seedDashboard(t, 123)
	seen := map[string]bool{}
	r := PageRequest{Limit: 7}
	var prev int64 = 1<<63 - 1
	for {
		p, err := c.DashboardSessions(t.Context(), r)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range p.Items {
			if seen[s.UID] || s.updatedNS > prev {
				t.Fatal("duplicate or chronological ordering failure")
			}
			seen[s.UID] = true
			prev = s.updatedNS
		}
		if p.NextCursor == "" {
			break
		}
		r.Cursor = p.NextCursor
	}
	if len(seen) != 123 {
		t.Fatal(len(seen))
	}
	p, err := c.DashboardSessions(t.Context(), PageRequest{Harness: "codex", Project: "repo", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.NextCursor == "" {
		t.Fatal("filter paging")
	}
	if _, err := c.DashboardSessions(t.Context(), PageRequest{Harness: "terva", Limit: 2, Cursor: p.NextCursor}); err != ErrPage {
		t.Fatal("cross-filter cursor accepted")
	}
	for _, r := range []PageRequest{{Limit: 201}, {Limit: -1}, {Harness: "unknown"}, {State: "complete"}, {Cursor: "bad!"}, {Project: "repo", Unlinked: true}} {
		if _, err := c.DashboardSessions(t.Context(), r); err != ErrPage {
			t.Fatal("bad input accepted")
		}
	}
	p, err = c.DashboardSessions(t.Context(), PageRequest{Unlinked: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Items {
		if s.ProjectID != "" {
			t.Fatal("unlinked filter")
		}
	}
}
func TestDashboardCountsStatesAndBoundedChildren(t *testing.T) {
	c := seedDashboard(t, 5)
	ctx := t.Context()
	_, _ = c.db.Exec(`UPDATE sessions SET published_gen=normalize_gen,published_head=head_sha256 WHERE session_uid='session-000000'`)
	_ = c.SetNormalizeError(ctx, "session-000001", "sensitive detailed error")
	_, _ = c.EnqueueNormalize(ctx, "session-000002", time.Now())
	for i := 0; i < 12; i++ {
		_, err := c.db.Exec(`INSERT INTO provenance(session_uid,machine_id,sha256,relpath,ingested_at) VALUES('session-000000',?,'head','other','2026-09-26T00:00:00Z')`, fmt.Sprintf("machine-%02d", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	o, err := c.DashboardOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if o.Sessions != 5 || o.Artifacts != 5 || o.Machines != 13 || o.Normalization["ready"] != 1 || o.Normalization["failed"] != 1 || o.Normalization["pending"] != 1 || o.Normalization["unknown"] != 2 {
		t.Fatalf("counts %+v", o)
	}
	s, err := c.DashboardSession(ctx, "session-000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Machines) != 5 || s.MachineCount != 13 {
		t.Fatal("machine summary not bounded")
	}
	p, err := c.DashboardRecords(ctx, s.UID, "provenance", PageRequest{Limit: 4})
	if err != nil || len(p.Items) != 4 || p.NextCursor == "" {
		t.Fatalf("provenance %v %+v", err, p)
	}
	if _, err := c.DashboardRecords(ctx, "session-000001", "provenance", PageRequest{Limit: 4, Cursor: p.NextCursor}); err != ErrPage {
		t.Fatal("cross-session cursor")
	}
	b, _ := json.Marshal(o)
	if strings.Contains(string(b), "sensitive") {
		t.Fatal("error details exposed")
	}
}
func TestDashboard20KIndexedPages(t *testing.T) {
	c := seedDashboard(t, 20000)
	ctx := t.Context()
	start := time.Now()
	r := PageRequest{Harness: "codex", Project: "repo", Limit: 50}
	cur, err := r.validate("sessions")
	if err != nil {
		t.Fatal(err)
	}
	q, args := sessionSQL(r, cur, "")
	rows, err := c.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for rows.Next() {
		var a, b, c int
		var detail string
		if err := rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	rows.Close()
	if !strings.Contains(strings.Join(details, "\n"), "web_sessions_project_harness") {
		t.Fatalf("missing composite index: %v", details)
	}
	p, err := c.DashboardSessions(ctx, r)
	if err != nil || len(p.Items) != 50 || p.NextCursor == "" {
		t.Fatalf("page %d err %v", len(p.Items), err)
	}
	b, _ := json.Marshal(p)
	if len(b) > 512<<10 {
		t.Fatal("unbounded response")
	}
	r.Cursor = p.NextCursor
	if _, err := c.DashboardSessions(ctx, r); err != nil {
		t.Fatal(err)
	}
	o, err := c.DashboardOverview(ctx)
	if err != nil || o.Sessions != 20000 {
		t.Fatal(err)
	}
	t.Logf("20k catalog: two filtered pages + overview %s; first page %d bytes; plan %v", time.Since(start), len(b), details)
}

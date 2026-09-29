package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// conflictLake posts one Claude session, native "S", artifact by
// artifact with the decision the lake made for each.
type conflictLake struct {
	t   *testing.T
	c   *Catalog
	now time.Time
	n   int
}

func (l *conflictLake) post(rel string, d Decision) string {
	l.t.Helper()
	l.n++
	m := claudeSession("S")
	m.Artifacts = []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: rel, Size: int64(l.n), SHA256: digestHex([]byte(rel + string(rune('a'+l.n))))}}
	l.now = l.now.Add(time.Second)
	if _, err := l.c.Ingest(context.Background(), m, l.now, []Decision{d}, nil); err != nil {
		l.t.Fatal(err)
	}
	return m.Artifacts[0].SHA256
}

func (l *conflictLake) artifact(sha string) string {
	l.t.Helper()
	var id string
	if err := l.c.db.QueryRow(`SELECT artifact_id FROM artifacts WHERE sha256 = ?`, sha).Scan(&id); err != nil {
		l.t.Fatal(err)
	}
	return id
}

var (
	sessionHead       = Decision{Relation: protocol.RelationHead, Record: true, Head: true}
	companionDecision = Decision{Relation: protocol.RelationHead, Record: true}
	divergentDecision = Decision{Relation: protocol.RelationDivergentCopy, Record: true}
)

func openConflicts(t *testing.T, c *Catalog, resolved bool) map[string]*Resolution {
	t.Helper()
	rows, err := c.DivergentCopies(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*Resolution{}
	for _, r := range rows {
		out[r.SHA256] = r.Resolution
	}
	return out
}

// The migration resolves the copies TKT-01M3M5VEQ stored for a
// subagent file that had nothing at its own path, and leaves a copy
// that diverged from a row at its own path, or that is not a companion
// of the head, open.
func TestMigrateConflictResolutionsResolvesOnlySubagentLeftovers(t *testing.T) {
	c, _ := openTemp(t)
	l := &conflictLake{t: t, c: c, now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	l.post("projects/p/S.jsonl", sessionHead)

	// Before the fix: each growth of agent-1 was a copy of S.jsonl, and
	// migrateSubagentHeads made the newest current.
	sub := "projects/p/S/subagents/agent-1.jsonl"
	bug1 := l.post(sub, divergentDecision)
	bug2 := l.post(sub, divergentDecision)
	made := l.post(sub, divergentDecision)
	if _, err := c.db.Exec(`UPDATE artifacts SET current = 1, relation = 'head' WHERE sha256 = ?`, made); err != nil {
		t.Fatal(err)
	}
	// After the fix: a real divergence of agent-1 from its own row, and
	// of agent-2, which arrived as its own current artifact.
	real1 := l.post(sub, divergentDecision)
	l.post("projects/p/S/subagents/agent-2.jsonl", companionDecision)
	real2 := l.post("projects/p/S/subagents/agent-2.jsonl", divergentDecision)
	// The session's transcript posted from another cwd: a move that
	// diverged, not a companion.
	moved := l.post("projects/q/S.jsonl", divergentDecision)

	if _, err := c.db.Exec(`DROP TABLE conflict_resolutions; DELETE FROM audit_outbox`); err != nil {
		t.Fatal(err)
	}
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateConflictResolutions(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	open := openConflicts(t, c, false)
	for name, sha := range map[string]string{"real1": real1, "real2": real2, "moved": moved} {
		if _, ok := open[sha]; !ok {
			t.Errorf("%s was resolved; open %v", name, open)
		}
	}
	if len(open) != 3 {
		t.Errorf("open conflicts %d, want 3", len(open))
	}
	all := openConflicts(t, c, true)
	for name, sha := range map[string]string{"bug1": bug1, "bug2": bug2} {
		r := all[sha]
		if r == nil || r.Resolution != ResolutionNotAConflict || r.By != migrationActor || r.At.IsZero() {
			t.Errorf("%s resolution %+v", name, r)
		}
	}
	events := queuedEvents(t, c, "conflict.resolved")
	if len(events) != 2 || !strings.Contains(events[0], l.artifact(bug1)) || !strings.Contains(events[1], "resolution=not_a_conflict") {
		t.Errorf("audit events %v", events)
	}
	ov, err := c.DashboardOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ov.Conflicts != 3 {
		t.Errorf("overview conflicts %d, want 3", ov.Conflicts)
	}
}

func TestResolveAndReopenConflict(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	l := &conflictLake{t: t, c: c, now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	head := l.post("projects/p/S.jsonl", sessionHead)
	cp := l.post("projects/q/S.jsonl", divergentDecision)
	id := l.artifact(cp)
	when := l.now.Add(time.Minute)

	if err := c.ResolveConflict(ctx, l.artifact(head), ResolutionKeptHead, "op", "", when); !errors.Is(err, ErrNoConflict) {
		t.Errorf("resolving the head: %v, want ErrNoConflict", err)
	}
	if err := c.ResolveConflict(ctx, id, "merged", "op", "", when); err == nil {
		t.Error("an unknown resolution was recorded")
	}
	if err := c.ReopenConflict(ctx, id, "op", when); !errors.Is(err, ErrConflictOpen) {
		t.Errorf("reopening an open conflict: %v", err)
	}
	if err := c.ResolveConflict(ctx, id, ResolutionKeptHead, "user:ada", "checked", when); err != nil {
		t.Fatal(err)
	}
	if err := c.ResolveConflict(ctx, id, ResolutionMadeHead, "user:bob", "", when); !errors.Is(err, ErrConflictResolved) {
		t.Errorf("resolving twice: %v", err)
	}
	if len(openConflicts(t, c, false)) != 0 {
		t.Error("a resolved conflict is still listed")
	}
	r := openConflicts(t, c, true)[cp]
	if r == nil || r.Resolution != ResolutionKeptHead || r.By != "user:ada" || r.Note != "checked" || !r.At.Equal(when) {
		t.Errorf("resolution %+v", r)
	}
	page, err := c.DashboardRecords(ctx, "", "conflicts", PageRequest{})
	if err != nil || len(page.Items) != 0 {
		t.Errorf("dashboard open conflicts %v %v", page.Items, err)
	}
	page, err = c.DashboardRecords(ctx, "", "conflicts", PageRequest{Resolved: true})
	if err != nil || len(page.Items) != 1 || page.Items[0].Resolution != ResolutionKeptHead {
		t.Errorf("dashboard with resolved %v %v", page.Items, err)
	}
	if _, err := c.DashboardRecords(ctx, page.Items[0].SessionUID, "artifacts", PageRequest{Resolved: true}); !errors.Is(err, ErrPage) {
		t.Errorf("artifacts took resolved: %v", err)
	}

	if err := c.ReopenConflict(ctx, id, "user:bob", when); err != nil {
		t.Fatal(err)
	}
	if _, ok := openConflicts(t, c, false)[cp]; !ok {
		t.Error("a reopened conflict is not listed")
	}
	if got := queuedEvents(t, c, "conflict.resolved"); len(got) != 1 || !strings.Contains(got[0], `"actor":"user:ada"`) || !strings.Contains(got[0], "resolution=kept_head") {
		t.Errorf("resolved events %v", got)
	}
	if got := queuedEvents(t, c, "conflict.reopened"); len(got) != 1 || !strings.Contains(got[0], `"actor":"user:bob"`) || !strings.Contains(got[0], "was=kept_head") {
		t.Errorf("reopened events %v", got)
	}

	// Purging the session takes its resolutions with it.
	if err := c.ResolveConflict(ctx, id, ResolutionKeptHead, "op", "", when); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeleteSession(ctx, page.Items[0].SessionUID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM conflict_resolutions`).Scan(&n); err != nil || n != 0 {
		t.Errorf("resolutions after purge %d %v", n, err)
	}
}

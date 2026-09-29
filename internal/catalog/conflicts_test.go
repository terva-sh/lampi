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
	// agent-3's first post stayed a current divergent copy, and a later
	// post diverged from it: a real fork, whatever the earlier row's
	// relation says.
	sub3 := "projects/p/S/subagents/agent-3.jsonl"
	cur3 := l.post(sub3, divergentDecision)
	if _, err := c.db.Exec(`UPDATE artifacts SET current = 1 WHERE sha256 = ?`, cur3); err != nil {
		t.Fatal(err)
	}
	real3 := l.post(sub3, divergentDecision)
	// A companion that is not under subagents/: not the shape the bug
	// left, so a real move into that directory stays open.
	nested := l.post("projects/p/S/moved/S.jsonl", divergentDecision)

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
	for name, sha := range map[string]string{"real1": real1, "real2": real2, "moved": moved, "real3": real3, "nested": nested} {
		if _, ok := open[sha]; !ok {
			t.Errorf("%s was resolved; open %v", name, open)
		}
	}
	// cur3 is a leftover like bug1: it had nothing at its path.
	if len(open) != 5 {
		t.Errorf("open conflicts %d, want 5", len(open))
	}
	all := openConflicts(t, c, true)
	for name, sha := range map[string]string{"bug1": bug1, "bug2": bug2} {
		r := all[sha]
		if r == nil || r.Resolution != ResolutionNotAConflict || r.By != migrationActor || r.At.IsZero() {
			t.Errorf("%s resolution %+v", name, r)
		}
	}
	events := queuedEvents(t, c, "conflict.resolved")
	if len(events) != 3 || !strings.Contains(events[0], l.artifact(bug1)) || !strings.Contains(events[1], "resolution=not_a_conflict") {
		t.Errorf("audit events %v", events)
	}
	ov, err := c.DashboardOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ov.Conflicts != 5 {
		t.Errorf("overview conflicts %d, want 5", ov.Conflicts)
	}
}

// Only Claude sessions are touched: a terva session with the same
// shape keeps its conflict.
func TestMigrateConflictResolutionsOnlyTouchesClaude(t *testing.T) {
	c, _ := openTemp(t)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	post := func(rel string, d Decision) {
		m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "m", Harness: protocol.HarnessTerva, NativeSessionID: "T",
			Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: rel, Size: 1, SHA256: digestHex([]byte(rel + d.Relation))}}}
		if _, err := c.Ingest(context.Background(), m, now, []Decision{d}, nil); err != nil {
			t.Fatal(err)
		}
	}
	post("sessions/x/T.jsonl", sessionHead)
	post("sessions/x/T/subagents/a.jsonl", divergentDecision)
	if _, err := c.db.Exec(`DROP TABLE conflict_resolutions`); err != nil {
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
	if open := openConflicts(t, c, false); len(open) != 1 {
		t.Errorf("open %v", open)
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
	// made_head and superseded say the head moved, which resolving alone
	// does not do.
	for _, r := range []string{"merged", ResolutionMadeHead, ResolutionSuperseded} {
		if err := c.ResolveConflict(ctx, id, r, "op", "", when); err == nil {
			t.Errorf("%s was recorded without moving the head", r)
		}
	}
	if err := c.ReopenConflict(ctx, id, "op", when); !errors.Is(err, ErrConflictOpen) {
		t.Errorf("reopening an open conflict: %v", err)
	}
	if err := c.ResolveConflict(ctx, id, ResolutionKeptHead, "user:ada", "checked", when); err != nil {
		t.Fatal(err)
	}
	if err := c.ResolveConflict(ctx, id, ResolutionNotAConflict, "user:bob", "", when); !errors.Is(err, ErrConflictResolved) {
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

func currentAt(t *testing.T, c *Catalog, uid string) map[string]string {
	t.Helper()
	rows, err := c.db.Query(`SELECT relpath, sha256 FROM artifacts WHERE session_uid = ? AND current = 1`, uid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var rel, sha string
		if err := rows.Scan(&rel, &sha); err != nil {
			t.Fatal(err)
		}
		out[rel] = sha
	}
	return out
}

func artifactOf(t *testing.T, c *Catalog, sha string) string {
	t.Helper()
	var id string
	if err := c.db.QueryRow(`SELECT artifact_id FROM artifacts WHERE sha256 = ?`, sha).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Making a copy the head moves the head and the current row, supersedes
// the older copies it extends, records the update, queues normalization,
// and lets the next post that extends it move the head (TKT-01M3PTMWM9).
func TestMakeConflictHead(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	rel := "sessions/aaaa/sess-1.jsonl"
	base := []byte("{\"n\":1}\n{\"n\":2}\n")
	// The harness rewrote the file and kept appending to it.
	f1 := []byte("{\"summary\":1}\n")
	f2 := append(append([]byte{}, f1...), "{\"n\":3}\n"...)
	other := []byte("{\"other\":1}\n")
	first := p.mustPost("machine-a", rel, base)
	uid := first.SessionUID
	for _, b := range [][]byte{f1, f2, other} {
		if ack := p.mustPost("machine-a", rel, b); ack.HeadSHA256 != digestHex(base) {
			t.Fatalf("a fork moved the head: %+v", ack)
		}
	}
	gen := func() int64 {
		var g int64
		if err := c.db.QueryRow(`SELECT normalize_gen FROM sessions WHERE session_uid = ?`, uid).Scan(&g); err != nil {
			t.Fatal(err)
		}
		return g
	}
	before := gen()
	id2 := artifactOf(t, c, digestHex(f2))
	when := p.now.Add(time.Minute)

	if _, err := c.MakeConflictHead(ctx, p.blobs, id2, digestHex(f1), "op", "", when); !errors.Is(err, ErrHeadMoved) {
		t.Fatalf("a stale head: %v", err)
	}
	made, err := c.MakeConflictHead(ctx, p.blobs, id2, digestHex(base), "user:ada", "rewritten after compaction", when)
	if err != nil {
		t.Fatal(err)
	}
	if made.SessionUID != uid || made.OldHead != digestHex(base) || len(made.Superseded) != 1 || made.Superseded[0] != artifactOf(t, c, digestHex(f1)) {
		t.Errorf("made %+v", made)
	}
	if cur := currentAt(t, c, uid); len(cur) != 1 || cur[rel] != digestHex(f2) {
		t.Errorf("current rows %v", cur)
	}
	v, _, err := c.Head(ctx, protocol.HarnessTerva, "sess-1")
	if err != nil || v.HeadSHA256 != digestHex(f2) {
		t.Fatalf("head %s %v", v.HeadSHA256, err)
	}
	all := openConflicts(t, c, true)
	if r := all[digestHex(f2)]; r == nil || r.Resolution != ResolutionMadeHead || r.Note != "rewritten after compaction" {
		t.Errorf("f2 resolution %+v", r)
	}
	if r := all[digestHex(f1)]; r == nil || r.Resolution != ResolutionSuperseded {
		t.Errorf("f1 resolution %+v", r)
	}
	if r := all[digestHex(other)]; r != nil {
		t.Errorf("an unrelated copy was resolved: %+v", r)
	}
	ups := headUpdateRows(t, c)
	last := ups[len(ups)-1]
	if last.Relation != ResolutionMadeHead || last.OldSHA != digestHex(base) || last.NewSHA != digestHex(f2) || last.Machine != "machine-a" || last.NewSize != int64(len(f2)) || last.OldSize != int64(len(base)) {
		t.Errorf("head update %+v", last)
	}
	var job int64
	if err := c.db.QueryRow(`SELECT gen FROM normalize_jobs WHERE session_uid = ?`, uid).Scan(&job); err != nil || gen() != before+1 || job != before+1 {
		t.Errorf("normalization gen %d job %d (%v), before %d", gen(), job, err, before)
	}
	if got := queuedEvents(t, c, "conflict.head_changed"); len(got) != 1 || !strings.Contains(got[0], "old_head="+digestHex(base)) || !strings.Contains(got[0], `"actor":"user:ada"`) {
		t.Errorf("head_changed events %v", got)
	}
	if got := queuedEvents(t, c, "conflict.resolved"); len(got) != 2 {
		t.Errorf("resolved events %v", got)
	}

	if err := c.ReopenConflict(ctx, id2, "op", when); !errors.Is(err, ErrConflictIsHead) {
		t.Errorf("reopening the head: %v", err)
	}
	if _, err := c.MakeConflictHead(ctx, p.blobs, id2, digestHex(f2), "op", "", when); !errors.Is(err, ErrConflictResolved) {
		t.Errorf("making it the head twice: %v", err)
	}
	// The machine goes on appending: the next post extends the new head.
	f3 := append(append([]byte{}, f2...), "{\"n\":4}\n"...)
	if ack := p.mustPost("machine-a", rel, f3); ack.HeadSHA256 != digestHex(f3) || ack.Relation != protocol.RelationGrownFrom {
		t.Errorf("after make-head, the next append: %+v", ack)
	}
}

// A copy under another path, as from a second machine's cwd, becomes
// the head and the old path stops being current. A companion of the
// head, such as a subagent transcript, is refused.
func TestMakeConflictHeadAcrossPathsAndRefusals(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	relA, relB := "sessions/aaaa/sess-1.jsonl", "sessions/bbbb/sess-1.jsonl"
	base := []byte("{\"n\":1}\n")
	moved := []byte("{\"m\":1}\n")
	uid := p.mustPost("machine-a", relA, base).SessionUID
	p.mustPost("machine-b", relB, moved)
	id := artifactOf(t, c, digestHex(moved))
	if _, err := c.MakeConflictHead(ctx, p.blobs, id, digestHex(base), "op", "", p.now); err != nil {
		t.Fatal(err)
	}
	if cur := currentAt(t, c, uid); len(cur) != 1 || cur[relB] != digestHex(moved) {
		t.Errorf("current rows %v", cur)
	}
	if ups := headUpdateRows(t, c); ups[len(ups)-1].Machine != "machine-b" {
		t.Errorf("attributed to %q", ups[len(ups)-1].Machine)
	}

	sub := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-b", Harness: protocol.HarnessTerva, NativeSessionID: "sess-1",
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/bbbb/sess-1/sub.jsonl", Size: 3, SHA256: digestHex([]byte("sub"))}}}
	ack, err := c.Ingest(ctx, sub, p.now, []Decision{{Relation: protocol.RelationDivergentCopy, Record: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.MakeConflictHead(ctx, p.blobs, ack.ArtifactIDs[0], digestHex(moved), "op", "", p.now); !errors.Is(err, ErrNotHeadCandidate) {
		t.Errorf("a companion: %v", err)
	}
	if _, err := c.MakeConflictHead(ctx, p.blobs, artifactOf(t, c, digestHex(moved)), digestHex(moved), "op", "", p.now); !errors.Is(err, ErrConflictResolved) {
		t.Errorf("the head itself: %v", err)
	}
	if _, err := c.MakeConflictHead(ctx, p.blobs, "nope", digestHex(moved), "op", "", p.now); !errors.Is(err, ErrNoConflict) {
		t.Errorf("no such conflict: %v", err)
	}
}

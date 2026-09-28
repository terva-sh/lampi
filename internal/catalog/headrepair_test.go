package catalog

import (
	"context"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func claudeSession(native string, rels ...string) protocol.Manifest {
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "m", Harness: protocol.HarnessClaude, NativeSessionID: native}
	for _, rel := range rels {
		m.Artifacts = append(m.Artifacts, protocol.Artifact{
			Kind: protocol.KindTranscriptJSONL, RelPath: rel, Size: 1, SHA256: digestHex([]byte(rel)),
		})
	}
	return m
}

// The repair moves a subagent head back to the session's transcript,
// whether that transcript is current or was kept as a divergent copy,
// makes a subagent file stored as a divergent copy current, leaves a
// transcript that really moved alone, and queues each changed session
// for normalization (TKT-01M3M5VEQ).
func TestMigrateSubagentHeadsRepairsBothShapes(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	// The decisions the lake made before the fix: the first transcript
	// listed is the head.
	head := func(i int) Decision {
		return Decision{Relation: protocol.RelationHead, Record: true, Head: i == 0}
	}

	// A: the subagent took the head.
	a := claudeSession("A", "projects/p/A/subagents/agent-1.jsonl", "projects/p/A.jsonl")
	ackA, err := c.Ingest(ctx, a, now, []Decision{head(0), head(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// B: the subagent was kept as a divergent copy of the transcript,
	// and a copy at the transcript's own depth is a real move.
	b := claudeSession("B", "projects/p/B.jsonl")
	ackB, err := c.Ingest(ctx, b, now, []Decision{head(0)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sub := claudeSession("B", "projects/p/B/subagents/agent-1.jsonl")
	moved := claudeSession("B", "projects/q/B.jsonl")
	for _, m := range []protocol.Manifest{sub, moved} {
		if _, err := c.Ingest(ctx, m, now, []Decision{{Relation: protocol.RelationDivergentCopy, Record: true}}, nil); err != nil {
			t.Fatal(err)
		}
	}

	// C: a subagent arrived first and took the head, then the session's
	// transcript was taken for a move of it and kept as a divergent copy.
	cSub := claudeSession("C", "projects/p/C/subagents/agent-1.jsonl")
	ackC, err := c.Ingest(ctx, cSub, now, []Decision{head(0)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cMain := claudeSession("C", "projects/p/C.jsonl")
	if _, err := c.Ingest(ctx, cMain, now, []Decision{{Relation: protocol.RelationDivergentCopy, Record: true}}, nil); err != nil {
		t.Fatal(err)
	}

	var genA, genB, genC int64
	for uid, gen := range map[string]*int64{ackA.SessionUID: &genA, ackB.SessionUID: &genB, ackC.SessionUID: &genC} {
		if err := c.db.QueryRow(`SELECT normalize_gen FROM sessions WHERE session_uid = ?`, uid).Scan(gen); err != nil {
			t.Fatal(err)
		}
	}

	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateSubagentHeads(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	v, _, err := c.Head(ctx, protocol.HarnessClaude, "A")
	if err != nil {
		t.Fatal(err)
	}
	if v.HeadSHA256 != a.Artifacts[1].SHA256 {
		t.Fatalf("session A head %s, want its transcript", v.HeadSHA256)
	}
	v, _, err = c.Head(ctx, protocol.HarnessClaude, "B")
	if err != nil {
		t.Fatal(err)
	}
	var current []string
	for _, r := range v.Current {
		current = append(current, r.RelPath)
	}
	if strings.Join(current, " ") != "projects/p/B.jsonl projects/p/B/subagents/agent-1.jsonl" {
		t.Fatalf("session B current %v", current)
	}
	v, _, err = c.Head(ctx, protocol.HarnessClaude, "C")
	if err != nil {
		t.Fatal(err)
	}
	current = current[:0]
	for _, r := range v.Current {
		current = append(current, r.RelPath)
	}
	if v.HeadSHA256 != cMain.Artifacts[0].SHA256 || strings.Join(current, " ") != "projects/p/C.jsonl projects/p/C/subagents/agent-1.jsonl" {
		t.Fatalf("session C head %s, current %v", v.HeadSHA256, current)
	}
	for uid, before := range map[string]int64{ackA.SessionUID: genA, ackB.SessionUID: genB, ackC.SessionUID: genC} {
		var gen, job int64
		if err := c.db.QueryRow(`SELECT s.normalize_gen, j.gen FROM sessions s JOIN normalize_jobs j USING (session_uid) WHERE s.session_uid = ?`, uid).Scan(&gen, &job); err != nil {
			t.Fatalf("%s not queued: %v", uid, err)
		}
		if gen != before+1 || job != gen {
			t.Fatalf("%s gen %d job %d, was %d", uid, gen, job, before)
		}
	}

	// A second run finds nothing to change.
	tx, err = c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := migrateSubagentHeads(tx); err != nil {
		t.Fatal(err)
	}
	var gen int64
	if err := tx.QueryRow(`SELECT normalize_gen FROM sessions WHERE session_uid = ?`, ackA.SessionUID).Scan(&gen); err != nil || gen != genA+1 {
		t.Fatalf("second run: gen %d %v", gen, err)
	}
}

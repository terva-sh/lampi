package catalog

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

type updateRow struct {
	UID, Machine, Harness string
	Received              int64
	OldSHA, NewSHA        string
	OldSize, NewSize      int64
	Relation              string
}

func headUpdateRows(t *testing.T, c *Catalog) []updateRow {
	t.Helper()
	rows, err := c.db.Query(`SELECT session_uid, machine_id, harness, received_ns, old_sha256, new_sha256, old_size, new_size, relation FROM head_updates ORDER BY update_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []updateRow
	for rows.Next() {
		var r updateRow
		if err := rows.Scan(&r.UID, &r.Machine, &r.Harness, &r.Received, &r.OldSHA, &r.NewSHA, &r.OldSize, &r.NewSize, &r.Relation); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func openTemp(t *testing.T) (*Catalog, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.db")
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, path
}

// transcriptPoster posts one terva transcript per call, with the CAS
// relating it to the stored head.
type transcriptPoster struct {
	t     *testing.T
	c     *Catalog
	blobs memBlobs
	now   time.Time
}

func (p *transcriptPoster) post(machine, rel string, body []byte) (protocol.ManifestAck, error) {
	p.t.Helper()
	p.blobs[digestHex(body)] = body
	p.now = p.now.Add(time.Minute)
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       machine,
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sess-1",
		Artifacts: []protocol.Artifact{{
			Kind: protocol.KindTranscriptJSONL, RelPath: rel, Size: int64(len(body)), SHA256: digestHex(body),
		}},
	}
	return p.c.Ingest(context.Background(), m, p.now, []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, p.blobs)
}

func (p *transcriptPoster) mustPost(machine, rel string, body []byte) protocol.ManifestAck {
	p.t.Helper()
	ack, err := p.post(machine, rel, body)
	if err != nil {
		p.t.Fatal(err)
	}
	return ack
}

func TestHeadUpdatesRecordOnlyHeadChanges(t *testing.T) {
	c, _ := openTemp(t)
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	base := []byte("{\"n\":1}\n")
	grown := append(append([]byte{}, base...), "{\"n\":2}\n"...)
	fork := append(append([]byte{}, base...), "{\"n\":3}\n"...)
	relA := "sessions/aaaa/sess-1.jsonl"
	relB := "sessions/bbbb/sess-1.jsonl"

	first := p.mustPost("machine-a", relA, base)
	firstAt := p.now
	// A retry of the same bytes, and a second machine with the same bytes
	// under its own path, add provenance at most.
	p.mustPost("machine-a", relA, base)
	p.mustPost("machine-b", relB, base)
	if got := headUpdateRows(t, c); len(got) != 1 {
		t.Fatalf("retries recorded updates: %+v", got)
	}

	p.mustPost("machine-b", relB, grown)
	grownAt := p.now
	// The older bytes again are stale; bytes that fork from the head are
	// a divergent copy. Neither moves the head.
	if ack := p.mustPost("machine-a", relA, base); ack.Relation != protocol.RelationStale && ack.Relation != protocol.RelationUnchanged {
		t.Fatalf("old bytes: %+v", ack)
	}
	if ack := p.mustPost("machine-a", relA, fork); ack.Relation != protocol.RelationDivergentCopy {
		t.Fatalf("fork: %+v", ack)
	}

	want := []updateRow{
		{UID: first.SessionUID, Machine: "machine-a", Harness: protocol.HarnessTerva, Received: firstAt.UnixNano(),
			NewSHA: digestHex(base), NewSize: int64(len(base)), Relation: protocol.RelationHead},
		{UID: first.SessionUID, Machine: "machine-b", Harness: protocol.HarnessTerva, Received: grownAt.UnixNano(),
			OldSHA: digestHex(base), NewSHA: digestHex(grown), OldSize: int64(len(base)), NewSize: int64(len(grown)), Relation: protocol.RelationGrownFrom},
	}
	got := headUpdateRows(t, c)
	if len(got) != len(want) {
		t.Fatalf("updates %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("update %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHeadUpdatesRecordSnapshotRewriteBack(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	long := []byte(`{"info":{"id":"ses_1"},"messages":[{"n":1},{"n":2}]}`)
	short := []byte(`{"info":{"id":"ses_1"},"messages":[{"n":1}]}`)
	blobs := memBlobs{digestHex(long): long, digestHex(short): short}
	post := func(body []byte) {
		t.Helper()
		now = now.Add(time.Minute)
		m := protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       "machine-a",
			Harness:         protocol.HarnessOpenCode,
			NativeSessionID: "ses_1",
			Artifacts: []protocol.Artifact{{
				Kind: protocol.KindOpenCodeExportJSON, RelPath: "export/ses_1.json", Size: int64(len(body)), SHA256: digestHex(body),
			}},
		}
		if _, err := c.Ingest(ctx, m, now, []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, blobs); err != nil {
			t.Fatal(err)
		}
	}
	// An export rewritten to other bytes and back is two head changes,
	// and the second shrinks the logical head.
	post(long)
	post(short)
	post(long)
	post(long)
	got := headUpdateRows(t, c)
	if len(got) != 3 {
		t.Fatalf("updates %+v", got)
	}
	last := got[2]
	if last.OldSHA != digestHex(short) || last.NewSHA != digestHex(long) || last.Harness != protocol.HarnessOpenCode {
		t.Fatalf("rewrite back: %+v", last)
	}
	if d := got[1].NewSize - got[1].OldSize; d != int64(len(short)-len(long)) || d >= 0 {
		t.Fatalf("shrinking rewrite net %d", d)
	}
}

func TestHeadUpdateFailureRollsBackTheHead(t *testing.T) {
	c, _ := openTemp(t)
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	base := []byte("{\"n\":1}\n")
	grown := append(append([]byte{}, base...), "{\"n\":2}\n"...)
	rel := "sessions/aaaa/sess-1.jsonl"
	p.mustPost("machine-a", rel, base)

	if _, err := c.db.Exec(`CREATE TRIGGER refuse_update BEFORE INSERT ON head_updates BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.post("machine-a", rel, grown); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("post with a failing history insert: %v", err)
	}
	head, ok, err := c.Head(context.Background(), protocol.HarnessTerva, "sess-1")
	if err != nil || !ok || head.HeadSHA256 != digestHex(base) {
		t.Fatalf("head after failed post: %+v ok=%v err=%v", head, ok, err)
	}
	if got := headUpdateRows(t, c); len(got) != 1 {
		t.Fatalf("updates after failed post: %+v", got)
	}

	if _, err := c.db.Exec(`DROP TRIGGER refuse_update`); err != nil {
		t.Fatal(err)
	}
	p.mustPost("machine-a", rel, grown)
	if got := headUpdateRows(t, c); len(got) != 2 {
		t.Fatalf("updates after retry: %+v", got)
	}
}

func TestPurgeRemovesHeadUpdates(t *testing.T) {
	c, _ := openTemp(t)
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	ack := p.mustPost("machine-a", "sessions/aaaa/sess-1.jsonl", []byte("{\"n\":1}\n"))
	if ok, err := c.DeleteSession(context.Background(), ack.SessionUID); err != nil || !ok {
		t.Fatalf("delete ok=%v err=%v", ok, err)
	}
	if got := headUpdateRows(t, c); len(got) != 0 {
		t.Fatalf("updates after purge: %+v", got)
	}
	if _, ok, err := c.HeadUpdatesSince(context.Background()); err != nil || !ok {
		t.Fatalf("purge dropped the coverage marker: ok=%v err=%v", ok, err)
	}
}

func TestHeadUpdatesMigrationInventsNothing(t *testing.T) {
	c, path := openTemp(t)
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	p.mustPost("machine-a", "sessions/aaaa/sess-1.jsonl", []byte("{\"n\":1}\n"))
	// Turn the file back into schema 7: a lake with a session and no
	// history table.
	if _, err := c.db.Exec(`DROP TABLE head_updates; DROP TABLE audit_outbox; DROP TABLE storage_samples; DROP TABLE device_reports; DROP TABLE profiles; DROP TABLE profile_revisions; DROP TABLE device_inventories; DROP TABLE project_sightings; DROP TABLE hidden_projects; DROP TABLE read_tokens; DROP TABLE conflict_resolutions; DELETE FROM lake_meta WHERE key IN ('head_updates_since', 'sightings_since'); PRAGMA user_version = 7`); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC().Add(-time.Second)
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if got := headUpdateRows(t, c); len(got) != 0 {
		t.Fatalf("migration invented updates: %+v", got)
	}
	since, ok, err := c.HeadUpdatesSince(context.Background())
	if err != nil || !ok || since.Before(before) || since.After(time.Now().Add(time.Second)) {
		t.Fatalf("coverage since %v ok=%v err=%v, want about now", since, ok, err)
	}
	n, err := c.Counts(context.Background())
	if err != nil || n.Sessions != 1 {
		t.Fatalf("sessions after migration: %+v %v", n, err)
	}
}

func TestHeadUpdatesSurviveVacuumInto(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	p.mustPost("machine-a", "sessions/aaaa/sess-1.jsonl", []byte("{\"n\":1}\n"))
	since, _, err := c.HeadUpdatesSince(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := c.VacuumInto(ctx, dest); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restored.Close() })
	if got, want := headUpdateRows(t, restored), headUpdateRows(t, c); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("restored updates %+v, want %+v", got, want)
	}
	got, ok, err := restored.HeadUpdatesSince(ctx)
	if err != nil || !ok || !got.Equal(since) {
		t.Fatalf("restored coverage %v ok=%v err=%v, want %v", got, ok, err, since)
	}
}

func TestHeadUpdatesSinceOnOldReadOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (session_uid TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	c, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if _, ok, err := c.HeadUpdatesSince(context.Background()); err != nil || ok {
		t.Fatalf("old file: ok=%v err=%v", ok, err)
	}
}

// The bucketed reads the activity API makes are range scans over the
// time indexes, reading no table rows.
func TestHeadUpdateRangeScansUseIndexes(t *testing.T) {
	c, _ := openTemp(t)
	for _, q := range []struct {
		sql, index string
		args       []any
	}{
		{`SELECT received_ns / 3600000000000, COUNT(*), SUM(new_size - old_size) FROM head_updates
			WHERE received_ns >= ? AND received_ns < ? GROUP BY 1`, "head_updates_time", []any{0, 1}},
		{`SELECT received_ns / 86400000000000, COUNT(*), SUM(new_size - old_size) FROM head_updates
			WHERE harness = ? AND received_ns >= ? AND received_ns < ? GROUP BY 1`, "head_updates_harness_time", []any{"terva", 0, 1}},
	} {
		rows, err := c.db.Query(`EXPLAIN QUERY PLAN `+q.sql, q.args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		joined := strings.Join(plan, "; ")
		if !strings.Contains(joined, "COVERING INDEX "+q.index) {
			t.Fatalf("plan %q does not scan covering index %s", joined, q.index)
		}
	}
}

package recall

import (
	"fmt"
	"testing"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

// rowIDs is uid's row id by position.
func rowIDs(t *testing.T, x *Index, uid string) map[int64]int64 {
	t.Helper()
	sigs, err := x.rowSigs(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]int64{}
	for pos, r := range sigs {
		out[pos] = r.id
	}
	return out
}

// A new generation rewrites only the rows that changed: a session that
// grew keeps its earlier rows and gains the new ones, a changed event
// is replaced, and a shorter generation drops the rows past its end
// (TKT-01M3KC2DD).
func TestNewGenerationWritesOnlyChangedRows(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "incremental")
	x := openIndex(t, s)
	text := func(i int) string { return fmt.Sprint("incremental event ", i) }
	publish(t, s, uid, events(100, text))
	pass(t, x)
	before := rowIDs(t, x, uid)

	// Grown by five events.
	grown := publish(t, s, uid, events(105, text))
	pass(t, x)
	after := rowIDs(t, x, uid)
	if len(after) != 105 {
		t.Fatalf("%d rows after growth", len(after))
	}
	for pos, id := range before {
		if after[pos] != id {
			t.Fatalf("row %d was rewritten although it did not change", pos)
		}
	}
	if x.deleted {
		t.Fatal("a pass that only added rows asked for a forced merge")
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "incremental event 104"}); len(p.Items) != 1 {
		t.Fatal("the new event is not searchable", len(p.Items))
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "incremental event 3"}); len(p.Items) == 0 || p.Items[0].Generation != grown {
		t.Fatal("a kept row does not report the current generation", p.Items)
	}

	// Event 7 changed, and the session is shorter.
	publish(t, s, uid, events(50, func(i int) string {
		if i == 7 {
			return "rewritten seven"
		}
		return text(i)
	}))
	pass(t, x)
	last := rowIDs(t, x, uid)
	if len(last) != 50 || last[7] == after[7] || last[8] != after[8] {
		t.Fatalf("rows %d, 7: %d->%d, 8: %d->%d", len(last), after[7], last[7], after[8], last[8])
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "rewritten seven"}); len(p.Items) != 1 {
		t.Fatal("the changed event is not searchable")
	}
	// Event 7's old text and events 70 to 79 are gone.
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "incremental event 7"}); len(p.Items) != 0 {
		t.Fatal("stale text still searchable", len(p.Items))
	}
}

// An event whose source had no time carries the projection time as
// recorded_at, the same as ingested_at, and a new one at every
// generation. Its row is kept across generations and reports no time
// (TKT-01M3NENNN8).
func TestUntimedEventsAreNotRewrittenEachGeneration(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "untimed")
	x := openIndex(t, s)
	gen := func(n int, stamp string) []normalize.Event {
		ev := events(n, func(i int) string { return fmt.Sprint("untimed event ", i) })
		for i := range ev {
			ev[i].IngestedAt = stamp
			if i%2 == 0 {
				ev[i].RecordedAt = stamp
			}
		}
		return ev
	}
	publish(t, s, uid, gen(20, "2026-09-29T01:00:00.123456789Z"))
	pass(t, x)
	before := rowIDs(t, x, uid)
	publish(t, s, uid, gen(22, "2026-09-29T02:00:00.987654321Z"))
	pass(t, x)
	after := rowIDs(t, x, uid)
	for pos, id := range before {
		if after[pos] != id {
			t.Fatalf("row %d was rewritten although only its projection time changed", pos)
		}
	}
	p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "untimed event 4"})
	if len(p.Items) != 1 || p.Items[0].RecordedAt != nil {
		t.Fatalf("an untimed event: %+v", p.Items)
	}
	p = search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "untimed event 5"})
	if len(p.Items) != 1 || p.Items[0].RecordedAt == nil {
		t.Fatalf("a timed event lost its time: %+v", p.Items)
	}
}

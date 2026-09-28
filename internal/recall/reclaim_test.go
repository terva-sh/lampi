package recall

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// indexBytes is the index file and its WAL after a checkpoint.
func indexBytes(t *testing.T, x *Index, path string) int64 {
	t.Helper()
	if _, err := x.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	var n int64
	for _, p := range []string{path, path + "-wal"} {
		if st, err := os.Stat(p); err == nil {
			n += st.Size()
		}
	}
	return n
}

// A session that grows is re-indexed whole at every sync. The rows the
// index deletes stay in its full-text segments until they merge, and
// the file used to grow to about four times the session's live size
// (TKT-01M3KC2DD). A bounded merge and an incremental vacuum after each
// pass keep it near that size.
func TestReindexingKeepsTheIndexNearItsLiveSize(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "growing")
	path := filepath.Join(t.TempDir(), IndexFile)
	x, err := OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close() })

	words := strings.Fields("lake index merge segment vacuum trigram session event tool result assistant user message page row delete insert")
	rng := rand.New(rand.NewPCG(1, 2))
	text := make([]string, 2000)
	for i := range text {
		var b strings.Builder
		for w := 0; w < 60; w++ {
			fmt.Fprintf(&b, "%s%d ", words[rng.IntN(len(words))], rng.IntN(100000))
		}
		text[i] = b.String()
	}
	const first = 1900
	var live int64
	for g := 0; g < 8; g++ {
		publish(t, s, uid, events(first+g*10, func(i int) string { return text[i] }))
		pass(t, x)
		if g == 0 {
			live = indexBytes(t, x, path)
		}
	}
	if got := indexBytes(t, x, path); got > 2*live {
		t.Fatalf("index is %d bytes after 8 generations, %d after the first", got, live)
	}
	if p := search(t, x, SearchRequest{Query: text[first+69][:20]}); len(p.Items) == 0 {
		t.Fatal("the newest events are not searchable")
	}
}

// A pass that writes nothing keeps merging while the last merge did
// work, and stops once a merge finds nothing to do. A reclaim that
// fails stays pending.
func TestIdlePassesFinishTheMerge(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "merge")
	x := openIndex(t, s)
	for g := 0; g < 6; g++ {
		publish(t, s, uid, events(300+g, func(i int) string { return fmt.Sprintf("merge text %d of generation %d", i, g) }))
		pass(t, x)
	}
	for i := 0; x.merging; i++ {
		if i > 50 {
			t.Fatal("idle passes never stopped merging")
		}
		pass(t, x)
	}
	if more, err := x.reclaim(t.Context()); err != nil || more {
		t.Fatalf("a merge after the index settled did work: %v %v", more, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if more, err := x.reclaim(ctx); err == nil || !more {
		t.Fatalf("a failed reclaim is not pending: %v %v", more, err)
	}
}

// A pass stopped after it wrote leaves the reclaim pending for the next.
func TestAStoppedPassLeavesTheReclaimPending(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "stopped")
	x := openIndex(t, s)
	publish(t, s, uid, events(5, func(i int) string { return fmt.Sprint("stopped text ", i) }))
	pass(t, x)
	for x.merging {
		pass(t, x)
	}
	publish(t, s, uid, events(6, func(i int) string { return fmt.Sprint("stopped text ", i) }))
	ctx, cancel := context.WithCancel(t.Context())
	x.beforeReclaim = cancel
	if err := x.Pass(ctx); err == nil {
		t.Fatal("the stopped pass reported no error")
	}
	x.beforeReclaim = nil
	if !x.merging {
		t.Fatal("a pass stopped after writing left no reclaim pending")
	}
}

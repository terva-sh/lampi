package recall

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
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

// A generation whose events all changed replaces every row. The
// replaced rows stay in the full-text segments until they merge, and
// the file grew to about four times the session's live size
// (TKT-01M3KC2DD). A forced merge and an incremental vacuum after each
// pass that deleted rows keep it near that size. Four generations are
// enough to tell: with no merge the file ends at over three times its
// first size, and with FTS5's ordinary merge in place of the forced one
// at nearly three times. More generations make the test slow under
// -race without telling more (TKT-01M3MD3C).
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
	text := make([]string, 250)
	for i := range text {
		var b strings.Builder
		for w := 0; w < 20; w++ {
			fmt.Fprintf(&b, "%s%d ", words[rng.IntN(len(words))], rng.IntN(100000))
		}
		text[i] = b.String()
	}
	const first, gens = 200, 4
	var live int64
	for g := 0; g < gens; g++ {
		// Every event's text changes, so every row is replaced.
		publish(t, s, uid, events(first+g*10, func(i int) string { return fmt.Sprint(text[i], " g", g) }))
		pass(t, x)
		for i := 0; x.merging; i++ {
			if i > 50 {
				t.Fatal("replacement merge did not finish")
			}
			pass(t, x)
		}
		if g == 0 {
			live = indexBytes(t, x, path)
		}
	}
	if got := indexBytes(t, x, path); got > 2*live {
		t.Fatalf("index is %d bytes after %d generations, %d after the first", got, gens, live)
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: text[first+(gens-1)*10-1][:20]}); len(p.Items) == 0 {
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
	if more, err := x.reclaim(t.Context(), true); err != nil || more {
		t.Fatalf("a merge after the index settled did work: %v %v", more, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if more, err := x.reclaim(ctx, true); err == nil || !more {
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

// A pass that reclaims leaves the WAL empty. The WAL kept the size of
// its largest stretch between resets, and on the dev lake it held
// 845 MiB after the rebuild (TKT-01M3NPFNJA).
func TestAReclaimTruncatesTheWAL(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "wal")
	path := filepath.Join(t.TempDir(), IndexFile)
	x, err := OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close() })
	var limit int64
	if err := x.db.QueryRow(`PRAGMA journal_size_limit`).Scan(&limit); err != nil {
		t.Fatal(err)
	}
	if limit != walLimit {
		t.Fatalf("journal_size_limit is %d, want %d", limit, walLimit)
	}
	publish(t, s, uid, events(300, func(i int) string { return fmt.Sprintf("wal text %d %s", i, strings.Repeat("x", 500)) }))
	pass(t, x)
	st, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		t.Fatalf("the WAL is %d bytes after a pass that reclaimed", st.Size())
	}
}

// A reader still on the WAL keeps the checkpoint from truncating it,
// which leaves the reclaim pending until a pass truncates it.
func TestAReaderOnTheWALLeavesTheReclaimPending(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "busy")
	path := filepath.Join(t.TempDir(), IndexFile)
	x, err := OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close() })
	// On one connection with no busy timeout, the checkpoint reports busy
	// at once rather than after the DSN's five seconds.
	x.db.SetMaxOpenConns(1)
	if _, err := x.db.Exec(`PRAGMA busy_timeout=0`); err != nil {
		t.Fatal(err)
	}
	// indexDSN begins every transaction immediate, which takes the write
	// lock, so the reader opens the file with a deferred one.
	r, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	var tx *sql.Tx
	x.beforeReclaim = func() {
		// The pass has written, so the reader's snapshot is on the WAL.
		if tx, err = r.Begin(); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM docs`).Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	publish(t, s, uid, events(20, func(i int) string { return fmt.Sprint("busy text ", i) }))
	pass(t, x)
	x.beforeReclaim = nil
	if !x.merging {
		t.Fatal("a reclaim a reader kept from truncating the WAL is not pending")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for i := 0; x.merging; i++ {
		if i > 50 {
			t.Fatal("passes never finished the reclaim")
		}
		pass(t, x)
	}
	if st, err := os.Stat(path + "-wal"); err != nil || st.Size() != 0 {
		t.Fatalf("the WAL is not empty once the reader left: %v %v", st, err)
	}
}

// freePages is the index's freelist length.
func freePages(t *testing.T, path string) int64 {
	t.Helper()
	db, err := openIndexDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int64
	if err := db.QueryRow(`PRAGMA freelist_count`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ftsBytes is the payload of the index's full-text segments.
func ftsBytes(t *testing.T, path string) int64 {
	t.Helper()
	db, err := openIndexDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int64
	if err := db.QueryRow(`SELECT coalesce(sum(length(block)),0) FROM fts_data`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Removing a session leaves its entries in the full-text segments until a
// merge meets them, and no pass merges after a purge. OptimizeIndex frees
// them and returns the pages to the filesystem (TKT-01M3NPFNMH).
func TestOptimizeFreesTheEntriesOfRemovedRows(t *testing.T) {
	s := lake(t)
	keep := ingest(t, s, "keep")
	gone := ingest(t, s, "gone")
	publish(t, s, keep, events(20, func(i int) string { return fmt.Sprint("keepword text ", i) }))
	publish(t, s, gone, events(2000, func(i int) string { return fmt.Sprintf("goneword %d %s", i, strings.Repeat("y", 400)) }))
	path := filepath.Join(t.TempDir(), IndexFile)
	x, err := OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	pass(t, x)
	for x.merging {
		pass(t, x)
	}
	x.Close()
	if err := RemoveFromIndex(t.Context(), path, gone); err != nil {
		t.Fatal(err)
	}
	removed := ftsBytes(t, path)

	before, after, err := OptimizeIndex(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := ftsBytes(t, path); got*4 > removed {
		t.Fatalf("full-text segments hold %d bytes after optimize, %d before", got, removed)
	}
	if after >= before {
		t.Fatalf("the index is %d bytes after optimize, %d before", after, before)
	}
	// incremental_vacuum frees one page per step, so a vacuum not run to
	// the end leaves pages on the freelist.
	if n := freePages(t, path); n != 0 {
		t.Fatalf("%d pages are still on the freelist after optimize", n)
	}
	if st, err := os.Stat(path + "-wal"); err == nil && st.Size() != 0 {
		t.Fatalf("the WAL is %d bytes after optimize", st.Size())
	}
	y, err := OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	defer y.Close()
	if p := search(t, y, SearchRequest{Scope: catalog.AllBays(), Query: "keepword"}); len(p.Items) == 0 {
		t.Fatal("the kept session is not searchable after optimize")
	}
	if p := search(t, y, SearchRequest{Scope: catalog.AllBays(), Query: "goneword"}); len(p.Items) != 0 {
		t.Fatal("the removed session is searchable after optimize")
	}

	// A reader that did not take lake.lock keeps the WAL from being
	// truncated, and optimize says so. The wait is the DSN's busy_timeout.
	r, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	tx, err := r.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM docs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OptimizeIndex(t.Context(), path); err == nil || !strings.Contains(err.Error(), "not truncated") {
		t.Fatalf("optimize beside a reader: %v", err)
	}
	tx.Rollback()

	if before, after, err := OptimizeIndex(t.Context(), filepath.Join(t.TempDir(), "absent.db")); err != nil || before != 0 || after != 0 {
		t.Fatalf("no index to optimize: %d %d %v", before, after, err)
	}
}

func TestOnlineOptimizeWaitsForIndexingAndKeepsSearchReadable(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "online-optimize")
	publish(t, s, uid, events(30, func(i int) string { return fmt.Sprint("keepword ", i) }))
	x := openIndex(t, s)
	entered, release := make(chan struct{}), make(chan struct{})
	x.beforeReclaim = func() {
		close(entered)
		<-release
	}
	passDone := make(chan error, 1)
	go func() { passDone <- x.Pass(t.Context()) }()
	<-entered
	optimized := make(chan error, 1)
	go func() { _, _, err := x.Optimize(t.Context()); optimized <- err }()
	select {
	case err := <-optimized:
		close(release)
		t.Fatalf("optimize ran during a pass: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-passDone; err != nil {
		t.Fatal(err)
	}
	if err := <-optimized; err != nil {
		t.Fatal(err)
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "keepword"}); len(p.Items) != 30 {
		t.Fatal("live compaction changed search results", len(p.Items))
	}
}

// Small FTS pages and a one-page budget make a merge span many steps,
// without needing the multi-GiB index that exposed the failure.
func seedLongMerge(t *testing.T, x *Index) {
	t.Helper()
	for _, q := range []string{`INSERT INTO fts(fts,rank) VALUES('pgsz',64)`, `INSERT INTO fts(fts,rank) VALUES('automerge',0)`} {
		if _, err := x.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rng := rand.New(rand.NewPCG(2, 3))
	for batch := 0; batch < 4; batch++ {
		tx, err := x.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 20; i++ {
			text := make([]byte, 4000)
			for j := range text {
				text[j] = byte('a' + rng.IntN(26))
			}
			if _, err := tx.Exec(`INSERT INTO docs(session_uid,pos,sig,harness,project_id,event_type,actor,raw_type,content) VALUES('fixture',?,0,'codex','','message','user','message',?)`, batch*20+i, string(text)); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

func addMergeRow(t *testing.T, x *Index, i int) {
	t.Helper()
	if _, err := x.db.Exec(`INSERT INTO docs(session_uid,pos,sig,harness,project_id,event_type,actor,raw_type,content) VALUES('fixture',?,0,'codex','','message','user','message',?)`, 100+i, fmt.Sprint("new batch ", i)); err != nil {
		t.Fatal(err)
	}
}

func segmentCount(t *testing.T, x *Index) int {
	t.Helper()
	var n int
	if err := x.db.QueryRow(`SELECT count(DISTINCT segid) FROM fts_idx`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestForcedMergeContinuesWhileNewSegmentsArrive(t *testing.T) {
	x := openIndex(t, lake(t))
	seedLongMerge(t, x)
	x.forcing = false
	// New deletions may request another forced merge, but must not
	// restart an unfinished one when uploads add new segments.
	for i := 0; i < 100; i++ {
		if i == 50 {
			// A restart must resume the persistent partial merge, too.
			path, reader := x.path, x.reader
			x.Close()
			var err error
			x, err = OpenIndex(path, reader)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { x.Close() })
		}
		addMergeRow(t, x, i)
		x.deleted = true
		if _, err := x.reclaimPages(t.Context(), x.deleted, 1); err != nil {
			t.Fatal(err)
		}
	}
	if n := segmentCount(t, x); n > 20 {
		t.Fatalf("restarted merges accumulated %d segments", n)
	}
	// Pending deletions are not lost, and finishing the current merge
	// eventually allows another forced merge to consume that request.
	for i := 0; x.forcing || x.deleted; i++ {
		if i > 200 {
			t.Fatal("pending forced merge did not finish")
		}
		if _, err := x.reclaim(t.Context(), x.deleted); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := x.db.Exec(`INSERT INTO fts(fts,rank) VALUES('integrity-check',1)`); err != nil {
		t.Fatal("merge changed indexed content", err)
	}
}

func TestOptimizeRecoversSaturatedSegmentsAndRetriesFailedSessions(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "saturated")
	publish(t, s, uid, events(3, func(i int) string { return fmt.Sprint("keepword ", i) }))
	x := openIndex(t, s)
	pass(t, x)
	seedLongMerge(t, x)
	// Reach the limit when an upload allocates the last ID, leaving no
	// ID for the forced merge's output (the production failure).
	if segmentCount(t, x)%2 == 0 {
		addMergeRow(t, x, 5000)
	}
	// Reproduce the old reclaim: every step starts a negative-budget
	// merge while a new upload adds a segment. It exhausts FTS5 IDs.
	for i := 0; segmentCount(t, x) < 2000; i++ {
		if i > 2000 {
			t.Fatal("fixture did not saturate")
		}
		addMergeRow(t, x, i)
		if _, err := x.db.Exec(`INSERT INTO fts(fts,rank) VALUES('merge',-1)`); err != nil && segmentCount(t, x) < 2000 {
			t.Fatal(err)
		}
	}
	if _, err := x.db.Exec(`INSERT INTO fts(fts,rank) VALUES('merge',-1)`); err == nil {
		t.Fatal("saturated fixture still starts a forced merge")
	}
	if _, err := x.db.Exec(`INSERT INTO fts(fts) VALUES('optimize')`); err == nil || !strings.Contains(err.Error(), "full") {
		t.Fatal("saturated fixture did not fail ordinary optimize", err)
	}
	// A canceled attempt must leave the old searchable content intact.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := x.Optimize(ctx); err == nil {
		t.Fatal("canceled recovery succeeded")
	}
	if n := segmentCount(t, x); n != 2000 {
		t.Fatal("canceled recovery changed segments", n)
	}
	// Restrict the one connection's row size so rebuild fails when it
	// reads the 4,000-byte fixture content, after clearing FTS tables.
	// The statement must roll back, preserving all old segments/hits.
	x.db.SetMaxOpenConns(1)
	conn, err := x.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	oldLimit, err := sqlite.Limit(conn, sqlite3.SQLITE_LIMIT_LENGTH, 3000)
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := x.Optimize(t.Context()); err == nil || !strings.Contains(err.Error(), "rebuild saturated index") {
		t.Fatal("recovery did not propagate the limited-row failure", err)
	}
	conn, err = x.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlite.Limit(conn, sqlite3.SQLITE_LIMIT_LENGTH, oldLimit)
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if n := segmentCount(t, x); n != 2000 {
		t.Fatal("failed rebuild did not roll back segments", n)
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "keepword"}); len(p.Items) != 3 {
		t.Fatal("failed rebuild lost existing search hits", len(p.Items))
	}
	pending := ingest(t, s, "failed-at-saturation")
	publish(t, s, pending, events(1, func(int) string { return "retryword" }))
	pass(t, x)
	if x.Coverage().Failed == 0 {
		t.Fatal("saturated indexing did not record a failed session")
	}
	if _, _, err := x.Optimize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := segmentCount(t, x); n != 1 {
		t.Fatal("recovery did not compact segments", n)
	}
	if _, err := x.db.Exec(`INSERT INTO fts(fts,rank) VALUES('integrity-check',1)`); err != nil {
		t.Fatal("recovery changed indexed content", err)
	}
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "keepword"}); len(p.Items) != 3 {
		t.Fatal("recovery lost existing search hits", len(p.Items))
	}
	pass(t, x)
	if p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: "retryword"}); len(p.Items) != 1 {
		t.Fatal("recovery did not retry failed indexing", len(p.Items))
	}
}

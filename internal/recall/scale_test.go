//go:build !race

// The race detector slows this measurement tenfold without testing any
// more concurrency than the other tests do.

package recall

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
)

// TestIndexScale indexes one long session and many short ones and
// records timings. Heap growth while indexing stays near the batch
// bounds, not the size of the transcript.
func TestIndexScale(t *testing.T) {
	if testing.Short() {
		t.Skip("scale")
	}
	s := lake(t)
	big := ingest(t, s, "big")
	filler := strings.Repeat("synthetic tool output line with some words. ", 40)
	publish(t, s, big, events(30000, func(i int) string {
		if i == 29999 {
			return "the rare token zqxjv lives here"
		}
		return fmt.Sprint(i, " ", filler)
	}))
	for i := range 400 {
		uid := ingest(t, s, fmt.Sprint("small-", i))
		publish(t, s, uid, events(20, func(j int) string { return fmt.Sprintf("small %d event %d needle", i, j) }))
	}
	x := openIndex(t, s)
	var before, peak runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	stop := make(chan struct{})
	sampled := make(chan uint64)
	go func() {
		var max uint64
		for {
			select {
			case <-stop:
				sampled <- max
				return
			case <-time.After(5 * time.Millisecond):
				runtime.ReadMemStats(&peak)
				if peak.HeapAlloc > max {
					max = peak.HeapAlloc
				}
			}
		}
	}()
	start := time.Now()
	pass(t, x)
	took := time.Since(start)
	close(stop)
	maxHeap := <-sampled
	growth := int64(maxHeap) - int64(before.HeapAlloc)
	t.Logf("indexed 38,000 events (%d MiB of text) in %s; peak heap growth %d MiB", 30000*len(filler)>>20, took.Round(time.Millisecond), growth>>20)
	var pages, pageSize int64
	x.db.QueryRow(`PRAGMA page_count`).Scan(&pages)
	x.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	t.Logf("search.db holds %d MiB", pages*pageSize>>20)
	if growth > 256<<20 {
		t.Fatalf("heap grew %d MiB while indexing", growth>>20)
	}
	if cov := x.Coverage(); cov.Indexed != 401 || cov.Behind != 0 {
		t.Fatal("coverage", cov)
	}
	for _, q := range []string{"zqxjv", "needle", "synthetic tool output"} {
		start := time.Now()
		p := search(t, x, SearchRequest{Scope: catalog.AllBays(), Query: q, Limit: 200})
		t.Logf("query %q: %d hits on the first page in %s", q, len(p.Items), time.Since(start).Round(time.Microsecond))
		if len(p.Items) == 0 {
			t.Fatal("no hits", q)
		}
	}
}

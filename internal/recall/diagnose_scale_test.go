//go:build !race

package recall

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

// TestDiagnoseScale measures one large session's diagnosis and the
// lake-wide walk, for docs/performance.md. It runs only when
// LAMPI_DIAGNOSE_SCALE names the large session's event count:
//
//	LAMPI_DIAGNOSE_SCALE=200000 go test -run TestDiagnoseScale -v ./internal/recall/
//
// Every third event is a tool call and the next its result, of 1 KiB
// each, with an error in every tenth result. LAMPI_DIAGNOSE_SESSIONS
// (default 200) sessions of 2,000 such events are then walked with the
// default limit.
func TestDiagnoseScale(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("LAMPI_DIAGNOSE_SCALE"))
	if n <= 0 {
		t.Skip("set LAMPI_DIAGNOSE_SCALE to the large session's event count")
	}
	sessions := DiagnoseLakeLimit
	if v, _ := strconv.Atoi(os.Getenv("LAMPI_DIAGNOSE_SESSIONS")); v > 0 {
		sessions = v
	}
	s := lake(t)
	r := NewReader(s.Catalog, s.Normalized)
	body := strings.Repeat("y", 1024)
	session := func(n int) []normalize.Event {
		evs := events(n, func(i int) string { return body })
		for i := 0; i+2 < n; i += 3 {
			id, name := fmt.Sprint("c", i), []string{"Read", "Bash", "Grep"}[i%3]
			evs[i+1].EventType, evs[i+1].Tool = normalize.EventToolCall, normalize.Tool{Name: &name, CallID: &id}
			evs[i+2].EventType, evs[i+2].Tool = normalize.EventToolResult, normalize.Tool{Name: &name, CallID: &id, IsError: ptr(i%30 == 0)}
		}
		return evs
	}
	big := ingest(t, s, "big")
	publish(t, s, big, session(n))
	start := time.Now()
	rep, err := r.Diagnose(t.Context(), catalog.AllBays(), big)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("one session: %d events, %d findings in %v", rep.Events, len(rep.Findings), time.Since(start).Round(time.Millisecond))

	for i := range sessions {
		publish(t, s, ingest(t, s, fmt.Sprint("walk-", i)), session(2000))
	}
	start = time.Now()
	g, err := r.DiagnoseLake(t.Context(), DiagnoseLakeRequest{Scope: catalog.AllBays(), Limit: DiagnoseLakeLimit, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("lake-wide: %d sessions of 2,000 events (limit %d, truncated %v), %d groups in %v", g.Sessions, g.Limit, g.Truncated, len(g.Groups), time.Since(start).Round(time.Millisecond))
}

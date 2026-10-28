//go:build !race

// The race detector slows a million-line pass tenfold without testing
// any concurrency.

package diagnose

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// syntheticLine is line i of a long session in the shape of the
// performance workloads (cmd/lampi-perf), with tool traffic added in
// the patterns that grow a naive pass's state: calls never answered,
// results for unknown calls, a new file read every time, a new tool
// and MCP server name every time, usage, compactions, and errors.
func syntheticLine(i int, filler string) []byte {
	ts := fmt.Sprintf("2026-10-17T%02d:%02d:%02dZ", i/3600%24, i/60%60, i%60)
	head := `{"schema_version":1,"harness":"claude","recorded_at":"` + ts + `","parent_session_id":null,`
	switch i % 10 {
	case 0, 1:
		return []byte(fmt.Sprintf(`%s"event_type":"message","actor":"assistant","content_text":%q,"extra":{}}`, head, filler))
	case 2:
		return []byte(fmt.Sprintf(`%s"event_type":"tool_call","content_text":"{\"file_path\":\"/f/%d\"}","tool":{"name":"Read","call_id":"r%d"}}`, head, i, i))
	case 3:
		return []byte(fmt.Sprintf(`%s"event_type":"tool_result","content_text":%q,"tool":{"name":"Read","call_id":"r%d","is_error":false}}`, head, filler, i-1))
	case 4:
		// Never answered.
		return []byte(fmt.Sprintf(`%s"event_type":"tool_call","content_text":"{}","tool":{"name":"mcp__srv%d__t%d","call_id":"lost%d"}}`, head, i, i, i))
	case 5:
		// Answers no call.
		return []byte(fmt.Sprintf(`%s"event_type":"tool_result","content_text":"x","tool":{"name":"mcp__srv%d__t","call_id":"ghost%d","is_error":true}}`, head, i, i))
	case 6:
		return []byte(fmt.Sprintf(`%s"event_type":"usage","usage":{"input":%d,"output":5,"cache_read":%d,"cache_write":10}}`, head, i%997, (i*7919)%90000))
	case 7:
		return []byte(fmt.Sprintf(`%s"event_type":"tool_call","content_text":"{\"command\":\"ls\"}","tool":{"name":"Bash","call_id":"b%d"}}`, head, i))
	case 8:
		return []byte(fmt.Sprintf(`%s"event_type":"tool_result","content_text":"same","tool":{"name":"Bash","call_id":"b%d","is_error":true}}`, head, i-1))
	default:
		return []byte(head + `"event_type":"compaction","extra":{}}`)
	}
}

func heapAfterGC() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// The pass holds the same state after a million events as after a
// hundred thousand: every table it keeps has a cap.
func TestMemoryBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("large transcript")
	}
	filler := strings.Repeat("synthetic tool output line with some words. ", 20)
	a := New(nil)
	const warm, total = 100_000, 1_000_000
	var at uint64
	for i := range total {
		if i == warm {
			at = heapAfterGC()
		}
		a.Line(syntheticLine(i, filler))
	}
	end := heapAfterGC()
	growth := int64(end) - int64(at)
	t.Logf("heap %d KiB after %d events, %d KiB after %d", at>>10, warm, end>>10, total)
	if growth > 4<<20 {
		t.Fatalf("heap grew %d KiB between %d and %d events", growth>>10, warm, total)
	}
	rep := a.Report()
	if rep.Events != total || len(rep.Findings) == 0 {
		t.Fatalf("%+v", rep)
	}
	for _, f := range rep.Findings {
		if len(f.Positions) > MaxPositions {
			t.Fatalf("%s: %d positions", f.Code, len(f.Positions))
		}
	}
	runtime.KeepAlive(a)
}

// Identical calls whose results never come, or whose first result
// never comes while the rest do, keep a bounded number of retry links
// (review 2913 on #222).
func TestMemoryBoundedRepeats(t *testing.T) {
	if testing.Short() {
		t.Skip("large transcript")
	}
	head := `{"schema_version":1,"harness":"claude","recorded_at":"2026-10-17T00:00:00Z","parent_session_id":null,`
	call := func(id string) []byte {
		return []byte(head + `"event_type":"tool_call","content_text":"{\"pattern\":\"x\"}","tool":{"name":"Grep","call_id":"` + id + `"}}`)
	}
	result := func(id string) []byte {
		return []byte(head + `"event_type":"tool_result","content_text":"no match","tool":{"name":"Grep","call_id":"` + id + `","is_error":false}}`)
	}
	for name, line := range map[string]func(i int) [][]byte{
		"never answered": func(i int) [][]byte { return [][]byte{call(fmt.Sprint("n", i))} },
		"first never answered": func(i int) [][]byte {
			id := fmt.Sprint("a", i)
			if i == 0 {
				return [][]byte{call(id)}
			}
			return [][]byte{call(id), result(id)}
		},
	} {
		a := New(nil)
		const warm, total = 50_000, 500_000
		var at uint64
		for i := range total {
			if i == warm {
				at = heapAfterGC()
			}
			for _, l := range line(i) {
				a.Line(l)
			}
		}
		end := heapAfterGC()
		if growth := int64(end) - int64(at); growth > 4<<20 {
			t.Fatalf("%s: heap grew %d KiB between %d and %d calls", name, growth>>10, warm, total)
		}
		t.Logf("%s: heap %d KiB after %d calls, %d KiB after %d", name, at>>10, warm, end>>10, total)
		runtime.KeepAlive(a)
	}
}

// Steady-state allocation per line does not grow with the session.
func TestAllocsFlat(t *testing.T) {
	filler := strings.Repeat("w", 512)
	measure := func(skip int) float64 {
		a := New(nil)
		for i := range skip {
			a.Line(syntheticLine(i, filler))
		}
		i := skip
		return testing.AllocsPerRun(2000, func() {
			a.Line(syntheticLine(i, filler))
			i++
		})
	}
	early, late := measure(10_000), measure(200_000)
	t.Logf("allocs per line: %.1f early, %.1f late", early, late)
	if late > early*1.5+2 {
		t.Fatalf("allocs per line grew from %.1f to %.1f", early, late)
	}
}

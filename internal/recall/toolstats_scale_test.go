//go:build !race

package recall

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/toolorigin"
)

// TestToolStatsScale measures ToolStats on a synthetic lake, for
// docs/performance.md. It runs only when LAMPI_TOOLSTATS_SCALE names
// the session count, since building the lake takes minutes:
//
//	LAMPI_TOOLSTATS_SCALE=2000 go test -run TestToolStatsScale -v ./internal/recall/
//
// Each session holds LAMPI_TOOLSTATS_PAIRS call and result pairs
// (default 250) over 30 days, written straight into docs with no
// content, since ToolStats reads none: 70% Claude Code sessions and 30%
// Codex code-mode sessions whose cells hold inner calls.
func TestToolStatsScale(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("LAMPI_TOOLSTATS_SCALE"))
	if n <= 0 {
		t.Skip("set LAMPI_TOOLSTATS_SCALE to the session count")
	}
	pairs := 250
	if p, _ := strconv.Atoi(os.Getenv("LAMPI_TOOLSTATS_PAIRS")); p > 0 {
		pairs = p
	}
	s := lake(t)
	ctx := t.Context()
	start := time.Now()
	uids := make([]string, n)
	harness := make([]string, n)
	// Each session starts in the 29 days before toolBase, and the lake
	// accepts it an hour after its last possible row.
	starts := make([]int64, n)
	month := toolBase.Add(-30 * 24 * time.Hour).UnixNano()
	span := int64(time.Duration(pairs)*20*time.Second + time.Hour)
	order := rand.New(rand.NewPCG(3, 4))
	for i := range n {
		harness[i] = "claude"
		if i%10 >= 7 {
			harness[i] = "codex"
		}
		starts[i] = month + order.Int64N(int64(29*24*time.Hour))
		uids[i] = ingestAt(t, s, harness[i], fmt.Sprint("scale-", i), time.Unix(0, starts[i]+span))
		publish(t, s, uids[i], toolSession(harness[i], usage(0, 1000, 20000+i, 500)))
	}
	if _, err := s.Catalog.CreateBay(ctx, "half", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i += 2 {
		if _, err := s.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: uids[i], Bay: "half", Actor: "admin", Via: catalog.ViaCLI}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	x := openIndex(t, s)
	pass(t, x)
	t.Logf("catalog and index of %d sessions in %s", n, time.Since(start).Round(time.Second))

	start = time.Now()
	rng := rand.New(rand.NewPCG(1, 2))
	size := func(median float64) int64 { return int64(math.Exp(math.Log(median) + 1.2*rng.NormFloat64())) }
	claudeTools := []string{"Bash", "Bash", "Bash", "Bash", "Bash", "Bash", "Bash", "Read", "Read", "Edit", "Write", "mcp__docs__search", "mcp__tracker__list_issues", "AskUserQuestion", "Agent", "WebFetch", "mcp__plugin_kit_files__read", "Grep", "Glob", "TodoWrite"}
	innerTools := []string{"exec_command", "exec_command", "exec_command", "apply_patch", "mcp__docs__search", "mcp__tracker__list_issues"}
	cols := "session_uid,pos,sig,harness,project_id,event_type,actor,tool_name,tool_error,raw_type,recorded_ns,content_bytes,call_id,model_id"
	const per = 14
	tx, err := x.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var args []any
	flush := func() {
		if len(args) == 0 {
			return
		}
		v := "(" + strings.TrimSuffix(strings.Repeat("?,", per), ",") + ")"
		if _, err := tx.Exec(`INSERT INTO docs(`+cols+`) VALUES`+v+strings.Repeat(","+v, len(args)/per-1), args...); err != nil {
			t.Fatal(err)
		}
		args = args[:0]
	}
	rows := 0
	for i, uid := range uids {
		at := starts[i]
		pos := int64(1)
		add := func(typ, tool, raw string, ns int64, bytes int64, failed any, callID, model any) {
			actor := "assistant"
			if typ == "tool_result" {
				actor = "tool"
			}
			args = append(args, uid, pos, rng.Int64(), harness[i], "", typ, actor, tool, failed, raw, ns, bytes, callID, model)
			pos++
			rows++
			if len(args) >= 200*per {
				flush()
			}
		}
		for p := range pairs {
			id := fmt.Sprint("call-", p)
			at += int64(time.Duration(rng.IntN(20000)) * time.Millisecond)
			lat := int64(time.Duration(size(80)) * time.Millisecond)
			if harness[i] == "claude" {
				tool := claudeTools[rng.IntN(len(claudeTools))]
				var failed any
				if tool == "Bash" || rng.IntN(30) == 0 {
					failed = rng.IntN(30) == 0
				}
				add("tool_call", tool, "assistant", at, size(400), nil, id, "model-a")
				add("tool_result", tool, "user", at+lat, min(size(700), 80000), failed, id, nil)
				continue
			}
			add("tool_call", "exec", "custom_tool_call", at, size(600), nil, id, "model-b")
			for k := range rng.IntN(3) {
				inner := fmt.Sprint(id, "-", k)
				tool := innerTools[rng.IntN(len(innerTools))]
				add("tool_call", tool, codexInnerRawType, at+lat/2, size(270), nil, inner, nil)
				add("tool_result", tool, codexInnerRawType, at+lat/2+lat/4, size(600), rng.IntN(10) == 0, inner, nil)
			}
			add("tool_result", "exec", "custom_tool_call_output", at+lat, min(size(1500), 40000), nil, id, nil)
		}
	}
	flush()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var pages, pageSize int64
	x.db.QueryRow(`PRAGMA page_count`).Scan(&pages)
	x.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	t.Logf("wrote %d tool rows in %s; search.db holds %d MiB", rows, time.Since(start).Round(time.Second), pages*pageSize>>20)

	since := toolBase.Add(-3 * 24 * time.Hour)
	bay := catalog.InBays(nil)
	if b, err := s.Catalog.ResolveBay(ctx, "half"); err == nil {
		bay = catalog.InBays([]string{b.ID})
	} else {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		req  ToolStatsRequest
	}{
		{"all bays, by tool", ToolStatsRequest{Scope: catalog.AllBays()}},
		{"all bays, by origin, split by harness", ToolStatsRequest{Scope: catalog.AllBays(), GroupBy: ToolGroupOrigin, Split: ToolSplitHarness}},
		{"all bays, last 3 days", ToolStatsRequest{Scope: catalog.AllBays(), Since: &since}},
		{"all bays, 30 days by day, 100 largest", ToolStatsRequest{Scope: catalog.AllBays(), Bucket: catalog.BucketDay, Since: new(toolBase.Add(-30 * 24 * time.Hour)), Largest: 100}},
		{"all bays, MCP only, split by model", ToolStatsRequest{Scope: catalog.AllBays(), OriginKind: toolorigin.MCP, Split: ToolSplitModel}},
		{"all bays, with context", ToolStatsRequest{Scope: catalog.AllBays(), GroupBy: ToolGroupOrigin, Context: true}},
		{"half the sessions by bay", ToolStatsRequest{Scope: bay}},
		{"one named session", ToolStatsRequest{Scope: catalog.AllBays().OnlySessions(uids[:1])}},
	} {
		c.req.Now = toolBase
		var took []time.Duration
		var st ToolStats
		for range 3 {
			start := time.Now()
			st, err = x.ToolStats(ctx, c.req)
			if err != nil {
				t.Fatal(c.name, err)
			}
			took = append(took, time.Since(start).Round(time.Millisecond))
		}
		t.Logf("%s: %v; %d groups, %d calls, %d sessions", c.name, took, len(st.Groups), st.Totals.Calls, st.Totals.Sessions)
	}
}

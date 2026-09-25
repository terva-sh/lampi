package upload

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/config"
)

// writeClaudeCorpus plants n Claude Code sessions of about size bytes
// each. The lines look like a transcript: JSON text with escaped line
// breaks and quotes, code, and words that share a prefix with a rule
// (task-, desk-, -----) without being a key.
func writeClaudeCorpus(tb testing.TB, home string, n int, size int) {
	tb.Helper()
	words := strings.Fields(`the a to of and in is it for on with as this that func return err nil if else
		task-list desk-top sk-ip -----\n secret_sauce aws_region github hooks slack AIzaNope xoxo
		package import struct type string int64 byte slice map chan go defer select case
		{"path":"/work/app/main.go"} \"quoted\" \\n\\t fmt.Println(\"hi\") SG.no npm_run hf_x`)
	rng := rand.New(rand.NewPCG(1, 2))
	dir := filepath.Join(home, "projects", "-work-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		tb.Fatal(err)
	}
	var line strings.Builder
	for i := range n {
		id := fmt.Sprintf("bench-%04d", i)
		var b strings.Builder
		for b.Len() < size {
			line.Reset()
			for line.Len() < 600+rng.IntN(1800) {
				line.WriteString(words[rng.IntN(len(words))])
				if rng.IntN(9) == 0 {
					line.WriteString(`\n`)
				} else {
					line.WriteByte(' ')
				}
			}
			fmt.Fprintf(&b, `{"type":"assistant","sessionId":%q,"cwd":"/work/app","message":{"content":[{"type":"text","text":"%s"}]}}`+"\n", id, line.String())
		}
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(b.String()), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
}

// BenchmarkSyncUnchanged is a second sync of 120 Claude sessions of
// 1 MiB that did not change: with a memo, as the agent runs it, and
// without, as a one-shot sync runs it. The first sync is outside the
// timer. Run it with -bench SyncUnchanged -benchtime 3x.
func BenchmarkSyncUnchanged(b *testing.B) {
	for _, memo := range []bool{true, false} {
		name := "memo"
		if !memo {
			name = "no-memo"
		}
		b.Run(name, func(b *testing.B) { benchUnchanged(b, memo) })
	}
}

func benchUnchanged(b *testing.B, memo bool) {
	lake, _ := openLake(b)
	srv := httptest.NewServer(lake.Handler())
	b.Cleanup(srv.Close)
	home := b.TempDir()
	writeClaudeCorpus(b, home, 120, 1<<20)
	opt := Options{
		ServerURL:  srv.URL,
		Token:      "tok",
		ClaudeHome: home,
		MachineID:  "machine-1",
		StateDir:   b.TempDir(),
		Client:     srv.Client(),
		Projects:   config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}
	if memo {
		opt.Memo = NewMemo()
	}
	ctx := context.Background()
	first, err := Sync(ctx, opt)
	if err != nil {
		b.Fatal(err)
	}
	if first.Manifests != 120 {
		b.Fatalf("first: %+v", first)
	}
	if err := lake.WaitNormalized(ctx); err != nil {
		b.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ReportAllocs()
	b.ResetTimer()
	var res Result
	start := time.Now()
	for range b.N {
		res, err = Sync(ctx, opt)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(time.Since(start).Milliseconds())/float64(b.N), "ms/sync")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/float64(b.N)/(1<<20), "MiB-alloc/sync")
	b.ReportMetric(float64(res.Manifests), "manifests")
}

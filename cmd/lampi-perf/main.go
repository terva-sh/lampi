// lampi-perf measures synthetic workloads without reading an agent config.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/upload"
)

type options struct {
	out, workload                         string
	sizeMiB, sessions, appends, appendKiB int
	unchanged                             int
	profiles, keepData, worker            bool
}

type stage struct {
	Name        string         `json:"name"`
	Seconds     float64        `json:"seconds"`
	Allocated   uint64         `json:"allocated_bytes"`
	GC          uint32         `json:"gc_cycles"`
	HeapAfter   uint64         `json:"heap_after_bytes"`
	SampledHeap uint64         `json:"sampled_peak_heap_bytes"`
	SampledRSS  uint64         `json:"sampled_peak_rss_bytes,omitempty"`
	Events      int            `json:"events,omitempty"`
	Sync        *upload.Result `json:"sync,omitempty"`
}

type result struct {
	Schema           int               `json:"schema_version"`
	Workload         string            `json:"workload"`
	Scope            string            `json:"scope"`
	GoVersion        string            `json:"go_version"`
	GOOS             string            `json:"goos"`
	GOARCH           string            `json:"goarch"`
	Build            *debug.BuildInfo  `json:"build"`
	Runtime          map[string]string `json:"runtime"`
	Sessions         int               `json:"sessions"`
	InputBytes       int64             `json:"input_bytes"`
	OutputBytes      map[string]int64  `json:"output_bytes"`
	SetupSeconds     float64           `json:"setup_seconds"`
	ProcessSeconds   float64           `json:"process_seconds"`
	ProcessUserCPU   float64           `json:"process_user_cpu_seconds"`
	ProcessSystemCPU float64           `json:"process_system_cpu_seconds"`
	PeakRSS          uint64            `json:"process_peak_rss_bytes,omitempty"`
	Profiles         bool              `json:"profiles"`
	Stages           []stage           `json:"stages"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "lampi-perf:", err)
		os.Exit(1)
	}
}

func parse(args []string) (options, error) {
	var o options
	f := flag.NewFlagSet("lampi-perf", flag.ContinueOnError)
	f.StringVar(&o.out, "out", "", "required NEW private output directory")
	f.StringVar(&o.workload, "workload", "projection", "projection or sync (combined agent/lake)")
	f.IntVar(&o.sizeMiB, "size-mib", 32, "approximate MiB per synthetic transcript")
	f.IntVar(&o.sessions, "sessions", 1, "number of synthetic sessions")
	f.IntVar(&o.appends, "append-passes", 2, "sync passes appending to every transcript")
	f.IntVar(&o.appendKiB, "append-kib", 64, "approximate KiB appended per session per pass")
	f.IntVar(&o.unchanged, "unchanged-passes", 1, "sync passes with unchanged transcripts")
	f.BoolVar(&o.profiles, "profiles", false, "capture CPU and final heap/allocation profiles")
	f.BoolVar(&o.keepData, "keep-data", false, "keep synthetic input and derived data")
	f.BoolVar(&o.worker, "worker", false, "internal subprocess mode")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() != 0 || o.out == "" || o.sizeMiB < 1 || o.sizeMiB > 4096 || o.sessions < 1 || o.appends < 0 || o.appendKiB < 1 || o.unchanged < 0 {
		return o, fmt.Errorf("require --out, 1..4096 --size-mib, positive sessions/append-kib and nonnegative pass counts")
	}
	if o.workload != "projection" && o.workload != "sync" {
		return o, fmt.Errorf("unknown workload %q", o.workload)
	}
	var err error
	o.out, err = filepath.Abs(o.out)
	return o, err
}

func run(args []string, stdout io.Writer) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	if o.worker {
		r, err := work(o)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(r)
	}
	// Never reuse an existing directory, even an empty one. Every file we
	// remove later is under the data directory this invocation created.
	if err := os.Mkdir(o.out, 0o700); err != nil {
		return fmt.Errorf("create NEW output directory: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, append(args, "--worker")...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, os.Stderr
	start := time.Now()
	if err := cmd.Run(); err != nil {
		return err
	}
	var r result
	if err := json.Unmarshal(output.Bytes(), &r); err != nil {
		return err
	}
	r.ProcessSeconds = time.Since(start).Seconds()
	r.ProcessUserCPU = cmd.ProcessState.UserTime().Seconds()
	r.ProcessSystemCPU = cmd.ProcessState.SystemTime().Seconds()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.WriteFile(filepath.Join(o.out, "results.json"), b, 0o600); err != nil {
		return err
	}
	_, err = stdout.Write(b)
	return err
}

func work(o options) (result, error) {
	start := time.Now()
	r := result{Schema: 1, Workload: o.workload, Scope: "projection", GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Sessions: o.sessions, Profiles: o.profiles}
	r.Build, _ = debug.ReadBuildInfo()
	r.Runtime = map[string]string{"GOMAXPROCS": strconv.Itoa(runtime.GOMAXPROCS(0))}
	for _, k := range []string{"GOMEMLIMIT", "GOGC", "GOEXPERIMENT"} {
		r.Runtime[k] = os.Getenv(k)
	}
	data := filepath.Join(o.out, "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		return r, err
	}
	if !o.keepData {
		defer os.RemoveAll(data)
	}
	home := filepath.Join(data, "claude")
	paths, err := corpus(home, o.sessions, int64(o.sizeMiB)<<20)
	if err != nil {
		return r, err
	}
	for _, path := range paths {
		st, err := os.Stat(path)
		if err != nil {
			return r, err
		}
		r.InputBytes += st.Size()
	}
	r.SetupSeconds = time.Since(start).Seconds()
	runtime.GC()
	if o.profiles {
		runtime.MemProfileRate = 64 << 10
		f, err := os.OpenFile(filepath.Join(o.out, "cpu.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return r, err
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			return r, err
		}
		defer func() { pprof.StopCPUProfile(); f.Close() }()
	}
	if o.workload == "sync" {
		r.Scope = "combined agent and lake; includes normalization, excludes search"
		err = syncWork(o, data, home, paths, &r)
	} else {
		err = projectionWork(data, paths, &r)
	}
	if err != nil {
		return r, err
	}
	r.OutputBytes, err = sizes(data)
	if err != nil {
		return r, err
	}
	if o.profiles {
		// Make the final allocation profile current. This explicit collection
		// is outside stage timings, and is included in process CPU/time.
		runtime.GC()
		f, err := os.OpenFile(filepath.Join(o.out, "heap.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return r, err
		}
		err = pprof.WriteHeapProfile(f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return r, err
		}
	}
	_, r.PeakRSS = resident()
	return r, nil
}

func projectionWork(data string, paths []string, r *result) error {
	for i, path := range paths {
		var raw []byte
		var events []normalize.Event
		uid := fmt.Sprintf("synthetic-%04d", i)
		for _, task := range []struct {
			name string
			fn   func() error
		}{
			{"read", func() (err error) { raw, err = os.ReadFile(path); return err }},
			{"normalize", func() (err error) {
				events, err = (normalize.Claude{Now: time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), NativeID: uid}).Normalize(context.Background(), raw)
				raw = nil
				return err
			}},
			{"events", func() error { return normalize.WriteFile(filepath.Join(data, "normalized"), uid, events) }},
			{"parquet", func() error { return normalize.WriteParquet(filepath.Join(data, "parquet"), uid, events) }},
		} {
			s, err := measure(fmt.Sprintf("session-%d/%s", i, task.name), task.fn)
			s.Events = len(events)
			r.Stages = append(r.Stages, s)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func syncWork(o options, data, home string, paths []string, r *result) error {
	lake, err := api.Open(filepath.Join(data, "lake"))
	if err != nil {
		return err
	}
	defer lake.Close()
	srv := httptest.NewServer(lake.Handler())
	defer srv.Close()
	opt := upload.Options{ServerURL: srv.URL, Client: srv.Client(), MachineID: "synthetic-perf", ClaudeHome: home, StateDir: filepath.Join(data, "agent"), Memo: upload.NewMemo(), Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/synthetic/lampi"}}}}
	sync := func(name string) error {
		var res upload.Result
		s, err := measure(name, func() (err error) {
			res, err = upload.Sync(context.Background(), opt)
			if err == nil {
				err = lake.WaitNormalized(context.Background())
			}
			return err
		})
		s.Sync = &res
		r.Stages = append(r.Stages, s)
		return err
	}
	if err := sync("initial-sync"); err != nil {
		return err
	}
	for i := 0; i < o.unchanged; i++ {
		if err := sync(fmt.Sprintf("unchanged-%d", i)); err != nil {
			return err
		}
	}
	for i := 0; i < o.appends; i++ {
		for j, path := range paths {
			if err := writeTranscript(path, fmt.Sprintf("synthetic-%04d", j), int64(o.appendKiB)<<10, uint64(i+100), true); err != nil {
				return err
			}
		}
		if err := sync(fmt.Sprintf("append-%d", i)); err != nil {
			return err
		}
	}
	return nil
}

func corpus(home string, sessions int, size int64) ([]string, error) {
	dir := filepath.Join(home, "projects", "synthetic-lampi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var paths []string
	for i := 0; i < sessions; i++ {
		uid := fmt.Sprintf("synthetic-%04d", i)
		path := filepath.Join(dir, uid+".jsonl")
		if err := writeTranscript(path, uid, size, uint64(i+1), false); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func writeTranscript(path, uid string, size int64, seed uint64, appendFile bool) error {
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	if appendFile {
		flags = os.O_APPEND | os.O_WRONLY
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	rng := rand.New(rand.NewPCG(seed, 7))
	words := strings.Fields("function return value sample query table pond lake message context file input output branch commit memory cpu text array buffer error loop variable")
	for written := int64(0); written < size; {
		var b strings.Builder
		for b.Len() < 3800 {
			b.WriteString(words[rng.IntN(len(words))])
			b.WriteByte(' ')
		}
		// Quoting and escaped line breaks exercise actual JSON string work.
		text, _ := json.Marshal(fmt.Sprintf("record %d: %s\nvalue = \"sample\"", written, b.String()))
		line := fmt.Sprintf("{\"type\":\"assistant\",\"sessionId\":%q,\"cwd\":\"/synthetic/lampi\",\"timestamp\":\"2026-10-17T00:00:00Z\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":%s}]}}\n", uid, text)
		n, err := io.WriteString(f, line)
		if err != nil {
			return err
		}
		written += int64(n)
	}
	return f.Close()
}

func measure(name string, fn func() error) (stage, error) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	stop, done := make(chan struct{}), make(chan [2]uint64)
	go func() {
		var peaks [2]uint64
		samples := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
		sample := func() {
			metrics.Read(samples)
			peaks[0] = max(peaks[0], samples[0].Value.Uint64())
			rss, _ := resident()
			peaks[1] = max(peaks[1], rss)
		}
		sample()
		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				sample()
				done <- peaks
				return
			case <-t.C:
				sample()
			}
		}
	}()
	start := time.Now()
	err := fn()
	elapsed := time.Since(start).Seconds()
	close(stop)
	peaks := <-done
	runtime.ReadMemStats(&after)
	return stage{Name: name, Seconds: elapsed, Allocated: after.TotalAlloc - before.TotalAlloc, GC: after.NumGC - before.NumGC, HeapAfter: after.HeapAlloc, SampledHeap: max(peaks[0], before.HeapAlloc, after.HeapAlloc), SampledRSS: peaks[1]}, err
}

// Linux offers a true lifetime RSS high-water mark. Other systems still
// report portable Go heap metrics and process CPU/time, and omit RSS.
func resident() (rss, peak uint64) {
	if runtime.GOOS != "linux" {
		return 0, 0
	}
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, 0
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || (f[0] != "VmRSS:" && f[0] != "VmHWM:") {
			continue
		}
		n, _ := strconv.ParseUint(f[1], 10, 64)
		if f[0] == "VmRSS:" {
			rss = n << 10
		} else {
			peak = n << 10
		}
	}
	return rss, peak
}

func sizes(root string) (map[string]int64, error) {
	out := map[string]int64{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		component := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		out[component] += st.Size()
		return nil
	})
	return out, err
}

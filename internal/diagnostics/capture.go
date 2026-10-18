// Package diagnostics implements explicit, bounded, file-only runtime captures.
// It never reads application state, environment, credentials or heap contents.
package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

const DefaultDuration = 30 * time.Second
const MaxDuration = 5 * time.Minute

type Options struct {
	Dir      string
	Duration time.Duration
}

func (o Options) Validate() error {
	if o.Duration < time.Second || o.Duration > MaxDuration {
		return fmt.Errorf("diagnostic duration must be 1s..5m")
	}
	return nil
}

// Snapshot contains only aggregate runtime/process counters. RSS is optional
// and is distinct from Go heap and cgroup filesystem cache.
type Snapshot struct {
	At             time.Time `json:"at"`
	HeapBytes      uint64    `json:"heap_bytes"`
	AllocatedBytes uint64    `json:"allocated_bytes"`
	GC             uint32    `json:"gc_cycles"`
	GCPauseSeconds float64   `json:"gc_pause_seconds"`
	GoMemoryBytes  uint64    `json:"go_memory_bytes"`
	RSSBytes       uint64    `json:"rss_bytes,omitempty"`
	CPUSeconds     float64   `json:"process_cpu_seconds,omitempty"`
	CPUAvailable   bool      `json:"process_cpu_available"`
}

func Read() Snapshot {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	cpu, ok := processCPU()
	return Snapshot{At: time.Now().UTC(), HeapBytes: m.HeapAlloc, AllocatedBytes: m.TotalAlloc, GC: m.NumGC, GCPauseSeconds: float64(m.PauseTotalNs) / 1e9, GoMemoryBytes: m.Sys - m.HeapReleased, RSSBytes: resident(), CPUSeconds: cpu, CPUAvailable: ok}
}

type Capture struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
	err  error
}

// Start is off when Dir is empty. Otherwise Dir must be a NEW directory; a
// pre-existing directory or symlink is refused. Profiles expose allocation
// stacks and executable symbols, never a raw heap or goroutine/command dump.
func Start(o Options) (*Capture, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if o.Dir == "" {
		return nil, nil
	}
	if err := os.Mkdir(o.Dir, 0700); err != nil {
		return nil, fmt.Errorf("create new diagnostic directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(o.Dir, "cpu.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err = pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return nil, err
	}
	c := &Capture{stop: make(chan struct{}), done: make(chan struct{})}
	first := Read()
	go func() {
		defer close(c.done)
		timer := time.NewTimer(o.Duration)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-c.stop:
		}
		pprof.StopCPUProfile()
		c.err = f.Close()
		heap, err := os.OpenFile(filepath.Join(o.Dir, "heap.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			err = pprof.WriteHeapProfile(heap)
			err = errors.Join(err, heap.Close())
		}
		c.err = errors.Join(c.err, err)
		stats, err := os.OpenFile(filepath.Join(o.Dir, "runtime.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			err = json.NewEncoder(stats).Encode(struct {
				Start, End       Snapshot
				RequestedSeconds float64
			}{first, Read(), o.Duration.Seconds()})
			err = errors.Join(err, stats.Close())
		}
		c.err = errors.Join(c.err, err)
	}()
	return c, nil
}

// Close stops early on shutdown, flushes profiles and waits for owned files.
// It is safe after automatic completion and for a nil (disabled) capture.
func (c *Capture) Close() error {
	if c == nil {
		return nil
	}
	c.once.Do(func() { close(c.stop) })
	<-c.done
	return c.err
}

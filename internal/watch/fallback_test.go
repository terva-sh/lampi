package watch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// runWatch starts w and fails the test if Run returns before cleanup.
func runWatch(t *testing.T, w *Watcher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("run did not return")
		}
	})
	rctx, cancelReady := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelReady()
	if err := w.WaitReady(rctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		t.Fatalf("run returned %v", err)
	default:
	}
}

type fallbacks struct {
	mu   sync.Mutex
	errs []error
}

func (f *fallbacks) add(err error) {
	f.mu.Lock()
	f.errs = append(f.errs, err)
	f.mu.Unlock()
}

func (f *fallbacks) wait(t *testing.T, want string) error {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, err := range f.errs {
			if strings.Contains(err.Error(), want) {
				f.mu.Unlock()
				return err
			}
		}
		f.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no fallback naming %q: %v", want, f.errs)
	return nil
}

func waitMode(t *testing.T, w *Watcher, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if w.Mode() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("mode %q, want %q", w.Mode(), want)
}

func waitOp(t *testing.T, ch <-chan Change, rel, op string) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case c := <-ch:
			if c.RelPath == rel && c.Op == op {
				return
			}
		case <-timeout:
			t.Fatalf("no %s for %s", op, rel)
		}
	}
}

func newWatcher(root string, ch chan Change, fb *fallbacks) *Watcher {
	return &Watcher{
		Root:         root,
		Debounce:     20 * time.Millisecond,
		PollInterval: 20 * time.Millisecond,
		OnChange:     func(c Change) { ch <- c },
		OnFallback:   fb.add,
	}
}

// A terva home that does not exist yet is not an error. The watcher
// polls until it appears, sees the first file, and then moves to
// fsnotify.
func TestMissingRootPollsUntilItExists(t *testing.T) {
	root := filepath.Join(t.TempDir(), "terva")
	ch := make(chan Change, 16)
	fb := &fallbacks{}
	w := newWatcher(root, ch, fb)
	runWatch(t, w)
	if w.Mode() != "poll" {
		t.Fatalf("mode %q before the root exists", w.Mode())
	}
	path := filepath.Join(root, "sessions", "aaaa", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitOp(t, ch, "sessions/aaaa/s.jsonl", OpCreate)
	waitMode(t, w, "fsnotify")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	waitOp(t, ch, "sessions/aaaa/s.jsonl", OpAppend)
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if len(fb.errs) != 0 {
		t.Fatalf("a missing root is not a fallback: %v", fb.errs)
	}
}

// A watch that cannot be added, such as the inotify limit, moves that
// watcher to polling and names the path. Run keeps going.
func TestAddFailureFallsBackToPolling(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sessions", "aaaa")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := addWatch
	t.Cleanup(func() { addWatch = orig })
	addWatch = func(fsw *fsnotify.Watcher, path string) error {
		if path == sub {
			return syscall.ENOSPC
		}
		return orig(fsw, path)
	}
	ch := make(chan Change, 16)
	fb := &fallbacks{}
	w := newWatcher(root, ch, fb)
	runWatch(t, w)
	err := fb.wait(t, sub)
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("fallback %v", err)
	}
	if w.Mode() != "poll" {
		t.Fatalf("mode %q", w.Mode())
	}
	path := filepath.Join(sub, "s.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitOp(t, ch, "sessions/aaaa/s.jsonl", OpCreate)
}

// An error from fsnotify, such as a queue overflow, moves the watcher
// to polling. A write after that is still reported.
func TestFsnotifyErrorFallsBackToPolling(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	opened := make(chan *fsnotify.Watcher, 1)
	t.Cleanup(func() { testOpened = nil })
	testOpened = func(fsw *fsnotify.Watcher) { opened <- fsw }
	ch := make(chan Change, 16)
	fb := &fallbacks{}
	w := newWatcher(root, ch, fb)
	runWatch(t, w)
	fsw := <-opened
	if w.Mode() != "fsnotify" {
		t.Fatalf("mode %q", w.Mode())
	}
	fsw.Errors <- fsnotify.ErrEventOverflow
	if err := fb.wait(t, root); !errors.Is(err, fsnotify.ErrEventOverflow) {
		t.Fatalf("fallback %v", err)
	}
	waitMode(t, w, "poll")
	path := filepath.Join(root, "sessions", "s.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitOp(t, ch, "sessions/s.jsonl", OpCreate)
}

func TestPollByDefault(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "LAMPI_WATCH" {
				return v
			}
			return ""
		}
	}
	if poll, err := PollByDefault(env("poll")); err != nil || !poll {
		t.Fatalf("poll: %v %v", poll, err)
	}
	if poll, err := PollByDefault(env("fsnotify")); err != nil || poll {
		t.Fatalf("fsnotify: %v %v", poll, err)
	}
	if _, err := PollByDefault(env("kqueue")); err == nil {
		t.Fatal("an unknown backend was accepted")
	}
}

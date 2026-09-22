package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"terva.sh/lampi/internal/discover"
)

func TestAppendReportsPriorOffset(t *testing.T) {
	for _, poll := range []bool{false, true} {
		name := "fsnotify"
		if poll {
			name = "poll"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			path := sessionFile(t, home, "abcd", "s.jsonl")
			const prefix = "0123456789abcdefghij" // 20 bytes
			if err := os.WriteFile(path, []byte(prefix), 0o644); err != nil {
				t.Fatal(err)
			}
			w, ch := startWatch(t, home, poll)
			waitTracked(t, w, "sessions/abcd/s.jsonl")

			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString("TAILDATA!!"); err != nil {
				t.Fatal(err)
			}
			f.Close()

			c := waitChange(t, ch, "sessions/abcd/s.jsonl")
			if c.Op != OpAppend || c.Offset != int64(len(prefix)) || c.Size != int64(len(prefix)+len("TAILDATA!!")) {
				t.Fatalf("change: %+v", c)
			}
			if c.Kind != discover.KindTranscript {
				t.Fatalf("kind %s", c.Kind)
			}
			// The event carries the cursor. Reading [Offset:Size] is the
			// tail; the watcher never had to open the prefix.
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got[c.Offset:c.Size]) != "TAILDATA!!" {
				t.Fatalf("tail %q", got[c.Offset:c.Size])
			}
		})
	}
}

func TestAtomicReplaceAndTruncate(t *testing.T) {
	for _, poll := range []bool{false, true} {
		name := "fsnotify"
		if poll {
			name = "poll"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, "sessions", "abcd")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "s.jsonl")
			if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, ch := startWatch(t, home, poll)
			rel := "sessions/abcd/s.jsonl"

			// Editor-style replace: write a temp file and rename it over
			// the destination. The inode changes. The new body is not an
			// append from the old size, including when the length matches.
			replaceAtomic(t, path, "HELLO\n")
			c := waitChange(t, ch, rel)
			if c.Op != OpReplace || c.Offset != 0 || c.Size != int64(len("HELLO\n")) {
				t.Fatalf("same-size replace: %+v", c)
			}

			replaceAtomic(t, path, "hello\nworld\n")
			c = waitChange(t, ch, rel)
			if c.Op != OpReplace || c.Offset != 0 || c.Size != int64(len("hello\nworld\n")) {
				t.Fatalf("larger replace: %+v", c)
			}

			if err := os.Truncate(path, 2); err != nil {
				t.Fatal(err)
			}
			c = waitChange(t, ch, rel)
			if c.Op != OpTruncate || c.Offset != 0 || c.Size != 2 {
				t.Fatalf("truncate: %+v", c)
			}
		})
	}
}

func TestDiscoverSwarmAndErrors(t *testing.T) {
	for _, poll := range []bool{false, true} {
		name := "fsnotify"
		if poll {
			name = "poll"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			mustWrite(t, filepath.Join(home, "sessions", "abcd", "parent.jsonl"), "{}\n")
			mustWrite(t, filepath.Join(home, "sessions", "abcd", "swarm", "worker.jsonl"), "{}\n")
			mustWrite(t, filepath.Join(home, "sessions", "abcd", "subagents", "child", "session.jsonl"), "{}\n")
			mustWrite(t, filepath.Join(home, "sessions", "abcd", "parent.errors.jsonl"), "{}\n")
			mustWrite(t, filepath.Join(home, "sessions", "abcd", "notes.txt"), "no")

			w, ch := startWatch(t, home, poll)
			waitTracked(t, w, "sessions/abcd/swarm/worker.jsonl")
			got := map[string]bool{}
			for _, rel := range w.Tracked() {
				got[rel] = true
			}
			for _, rel := range []string{
				"sessions/abcd/parent.jsonl",
				"sessions/abcd/swarm/worker.jsonl",
				"sessions/abcd/subagents/child/session.jsonl",
				"sessions/abcd/parent.errors.jsonl",
			} {
				if !got[rel] {
					t.Fatalf("missing %s in %v", rel, w.Tracked())
				}
			}
			if got["sessions/abcd/notes.txt"] {
				t.Fatal("tracked a non-jsonl file")
			}

			// A swarm file created after the walk, in a new directory.
			mustWrite(t, filepath.Join(home, "sessions", "abcd", "swarm", "extra", "late.jsonl"), "{}\n")
			c := waitChange(t, ch, "sessions/abcd/swarm/extra/late.jsonl")
			if c.Op != OpCreate || c.Offset != 0 {
				t.Fatalf("late swarm file: %+v", c)
			}
		})
	}
}

func TestSkipErrors(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "sessions", "abcd", "s.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(home, "sessions", "abcd", "s.errors.jsonl"), "{}\n")
	ch := make(chan Change, 8)
	w := &Watcher{
		Root:         home,
		ForcePoll:    true,
		PollInterval: 15 * time.Millisecond,
		SkipErrors:   true,
		OnChange: func(c Change) {
			ch <- c
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = w.Run(ctx) }()
	waitTracked(t, w, "sessions/abcd/s.jsonl")
	for _, rel := range w.Tracked() {
		if rel == "sessions/abcd/s.errors.jsonl" {
			t.Fatal("errors sidecar was tracked")
		}
	}
	mustWrite(t, filepath.Join(home, "sessions", "abcd", "s.errors.jsonl"), "{\"e\":1}\n")
	select {
	case c := <-ch:
		if c.RelPath == "sessions/abcd/s.errors.jsonl" {
			t.Fatalf("emitted errors file: %+v", c)
		}
	case <-time.After(80 * time.Millisecond):
	}
}

func TestDebounceCoalescesAppend(t *testing.T) {
	home := t.TempDir()
	path := sessionFile(t, home, "abcd", "s.jsonl")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Change, 8)
	w := &Watcher{
		Root:     home,
		Debounce: 80 * time.Millisecond,
		OnChange: func(c Change) { ch <- c },
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = w.Run(ctx) }()
	waitTracked(t, w, "sessions/abcd/s.jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"A", "B", "C"} {
		if _, err := f.WriteString(b); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	c := waitChange(t, ch, "sessions/abcd/s.jsonl")
	if c.Op != OpAppend || c.Offset != 5 || c.Size != 8 {
		t.Fatalf("debounced change: %+v", c)
	}
	select {
	case extra := <-ch:
		t.Fatalf("second event %s %+v", extra.RelPath, extra)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPollCoalescesBurst(t *testing.T) {
	home := t.TempDir()
	path := sessionFile(t, home, "abcd", "s.jsonl")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Change, 8)
	w := &Watcher{
		Root:         home,
		ForcePoll:    true,
		PollInterval: 200 * time.Millisecond,
		OnChange:     func(c Change) { ch <- c },
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = w.Run(ctx) }()
	waitTracked(t, w, "sessions/abcd/s.jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("ABC"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	c := waitChange(t, ch, "sessions/abcd/s.jsonl")
	if c.Op != OpAppend || c.Offset != 5 || c.Size != 8 {
		t.Fatalf("poll burst: %+v", c)
	}
	select {
	case extra := <-ch:
		t.Fatalf("second event %+v", extra)
	case <-time.After(350 * time.Millisecond):
	}
}

func TestWaitReadyDoesNotSucceedWhenRunFails(t *testing.T) {
	w := &Watcher{}
	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(context.Background()) }()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("empty root should fail")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := w.WaitReady(ctx); err == nil {
		t.Fatal("WaitReady succeeded after Run failed")
	}

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "sessions"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = &Watcher{Root: home, ForcePoll: true}
	errCh = make(chan error, 1)
	go func() { errCh <- w.Run(context.Background()) }()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("sessions file should fail the seed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := w.WaitReady(ctx); err == nil {
		t.Fatal("WaitReady succeeded after seed failed")
	}
}

func startWatch(t *testing.T, home string, poll bool) (*Watcher, <-chan Change) {
	t.Helper()
	ch := make(chan Change, 16)
	w := &Watcher{
		Root:         home,
		ForcePoll:    poll,
		Debounce:     30 * time.Millisecond,
		PollInterval: 20 * time.Millisecond,
		OnChange: func(c Change) {
			ch <- c
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
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
	return w, ch
}

func waitTracked(t *testing.T, w *Watcher, rel string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, got := range w.Tracked() {
			if got == rel {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never tracked %s; have %v", rel, w.Tracked())
}

func waitChange(t *testing.T, ch <-chan Change, rel string) Change {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case c := <-ch:
			if c.RelPath == rel {
				return c
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %s", rel)
			return Change{}
		}
	}
}

func sessionFile(t *testing.T, home, bucket, name string) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceAtomic(t *testing.T, path, body string) {
	t.Helper()
	tmp := path + ".tmp-replace"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

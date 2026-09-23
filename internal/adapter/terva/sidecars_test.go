package terva

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"terva.sh/lampi/internal/discover"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watch"
)

func TestKindsMatchWire(t *testing.T) {
	if discover.KindRaati != protocol.KindRaatiJSON || discover.KindTasks != protocol.KindTasksJSON {
		t.Fatalf("sidecar kinds drifted: %s %s", discover.KindRaati, discover.KindTasks)
	}
}

func TestManifestsWithoutSidecarDirs(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\"type\":\"meta\",\"meta\":{\"id\":\"sess-1\",\"cwd\":\"/work/app\"}}\n"
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Manifests(home, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 || len(b.Manifests[0].Artifacts) != 1 {
		t.Fatalf("manifests: %+v", b.Manifests)
	}
	if b.Manifests[0].Artifacts[0].Kind != protocol.KindTranscriptJSONL {
		t.Fatalf("kind %s", b.Manifests[0].Artifacts[0].Kind)
	}
}

func TestManifestsAttachSidecars(t *testing.T) {
	home := t.TempDir()
	sessionID := "11111111-2222-3333-4444-555555555555"
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sessions/abcd/s.jsonl", "{\"type\":\"meta\",\"meta\":{\"id\":\""+sessionID+"\",\"cwd\":\"/work/app\"}}\n")
	write("sessions/eeee/other.jsonl", "{\"type\":\"meta\",\"meta\":{\"id\":\"other-sess\",\"cwd\":\"/work/else\"}}\n")
	write("tasks/tasks-"+sessionID+".json", `{"tasks":[],"generations":[{"seq":1}]}`)
	write("tasks/tasks-nobody.json", `{"tasks":[{"title":"orphan"}]}`)
	write("ext-data/tasks/tasks-"+sessionID+".json", `{"tasks":[{"title":"legacy"}]}`)
	sum := sha256.Sum256([]byte("unsafe id"))
	hashed := hex.EncodeToString(sum[:8])
	write("sessions/ffff/unsafe.jsonl", "{\"type\":\"meta\",\"meta\":{\"id\":\"unsafe id\",\"cwd\":\"/work/app\"}}\n")
	write("tasks/tasks-"+hashed+".json", `{"tasks":[{"title":"hashed"}]}`)
	write("raati/raati-1700000000000000001.json", `{"question":"ship?","units":[{"agent_id":"yata-1","blind_agent_id":"../escape"}]}`)
	write("swarm/agents/yata-1/meta.json", `{"session_id":"`+sessionID+`","origin":"/work/else","dir":"/tmp/lease"}`)
	write("raati/raati-1700000000000000002.json", `{"question":"later?","units":[{"agent_id":"kusanagi-2"}]}`)
	write("swarm/archive/kusanagi-2/meta.json", `{"origin":"/work/else","dir":"/tmp/lease"}`)
	write("raati/raati-1700000000000000003.json", `{"question":"partial"`)
	write("raati/raati-1700000000000000004.json", `{"question":"unlinked","units":[{"agent_id":"missing"}]}`)

	b, err := Manifests(home, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]protocol.Manifest{}
	for _, m := range b.Manifests {
		byID[m.NativeSessionID] = m
	}
	if len(byID) != 3 {
		t.Fatalf("manifests: %d %+v", len(byID), byID)
	}
	main := kindsOf(t, byID[sessionID])
	if main[protocol.KindTranscriptJSONL] != 1 || main[protocol.KindTasksJSON] != 2 || main[protocol.KindRaatiJSON] != 1 {
		t.Fatalf("main session kinds: %+v", main)
	}
	if kindsOf(t, byID["other-sess"])[protocol.KindRaatiJSON] != 1 {
		t.Fatalf("origin-linked raati missing: %+v", byID["other-sess"].Artifacts)
	}
	if len(byID["other-sess"].Artifacts) != 2 {
		t.Fatalf("other session should be transcript plus one raati: %+v", byID["other-sess"].Artifacts)
	}
	unsafe := kindsOf(t, byID["unsafe id"])
	if unsafe[protocol.KindTasksJSON] != 1 || unsafe[protocol.KindTranscriptJSONL] != 1 {
		t.Fatalf("hashed tasks: %+v", unsafe)
	}
	for _, m := range b.Manifests {
		for _, a := range m.Artifacts {
			if a.RelPath == "tasks/tasks-nobody.json" || a.RelPath == "raati/raati-1700000000000000003.json" || a.RelPath == "raati/raati-1700000000000000004.json" {
				t.Fatalf("unlinked sidecar on %s: %s", m.NativeSessionID, a.RelPath)
			}
		}
	}
}

func TestDiscoverListsSidecars(t *testing.T) {
	home := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sessions/abcd/s.jsonl", "{}\n")
	write("raati/raati-1.json", "{}")
	write("tasks/tasks-abc.json", "{}")
	write("tasks/nope.txt", "no")
	refs, err := (Adapter{}).Discover(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range refs {
		got[r.RelPath] = r.Kind
	}
	for _, rel := range []string{"sessions/abcd/s.jsonl", "raati/raati-1.json", "tasks/tasks-abc.json"} {
		if got[rel] == "" {
			t.Fatalf("missing %s in %+v", rel, got)
		}
	}
	if _, ok := got["tasks/nope.txt"]; ok {
		t.Fatal("junk file discovered")
	}
}

func TestWatchSidecarDirs(t *testing.T) {
	for _, poll := range []bool{false, true} {
		name := "fsnotify"
		if poll {
			name = "poll"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			a := Adapter{}
			tasks := filepath.Join(home, "tasks", "tasks-sess.json")
			if err := os.MkdirAll(filepath.Dir(tasks), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tasks, []byte(`{"tasks":[]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "tasks", "notes.txt"), []byte("no"), 0o644); err != nil {
				t.Fatal(err)
			}
			var tasksDir string
			for _, dir := range a.WatchDirs() {
				if dir == "tasks" {
					tasksDir = dir
				}
			}
			if tasksDir == "" {
				t.Fatal("tasks is not a watch dir")
			}
			w, ch := startSidecarWatch(t, home, tasksDir, a.Match, poll)
			waitSidecarTracked(t, w, "tasks/tasks-sess.json")
			for _, got := range w.Tracked() {
				if got == "tasks/notes.txt" {
					t.Fatalf("tracked %s", got)
				}
			}
			raati := filepath.Join(home, "raati", "raati-9.json")
			if err := os.MkdirAll(filepath.Dir(raati), 0o755); err != nil {
				t.Fatal(err)
			}
			rw, _ := startSidecarWatch(t, home, "raati", a.Match, poll)
			if err := os.WriteFile(raati, []byte(`{"units":[]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			waitSidecarTracked(t, rw, "raati/raati-9.json")
			f, err := os.OpenFile(tasks, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString("\n"); err != nil {
				t.Fatal(err)
			}
			f.Close()
			c := waitSidecarChange(t, ch, "tasks/tasks-sess.json")
			if c.Kind != discover.KindTasks || c.Op == "" {
				t.Fatalf("change %+v", c)
			}
		})
	}
}

func kindsOf(t *testing.T, m protocol.Manifest) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, a := range m.Artifacts {
		out[a.Kind]++
		if a.SHA256 == "" || a.TailSHA256 != a.SHA256 {
			t.Fatalf("artifact %+v", a)
		}
	}
	return out
}

func startSidecarWatch(t *testing.T, root, dir string, match func(string) (string, bool), poll bool) (*watch.Watcher, <-chan watch.Change) {
	t.Helper()
	ch := make(chan watch.Change, 16)
	w := &watch.Watcher{
		Root:         root,
		ForcePoll:    poll,
		Debounce:     30 * time.Millisecond,
		PollInterval: 20 * time.Millisecond,
		Layout:       watch.Layout{Dir: dir, Match: match},
		OnChange:     func(c watch.Change) { ch <- c },
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

func waitSidecarTracked(t *testing.T, w *watch.Watcher, rel string) {
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

func waitSidecarChange(t *testing.T, ch <-chan watch.Change, rel string) watch.Change {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case c := <-ch:
			if c.RelPath == rel {
				return c
			}
		case <-deadline:
			t.Fatalf("no change for %s", rel)
		}
	}
}

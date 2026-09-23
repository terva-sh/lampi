package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watch"
)

func TestHome(t *testing.T) {
	got, err := Home(func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return "/tmp/claude-alt"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/claude-alt" {
		t.Fatalf("override %s", got)
	}

	got, err = Home(func(k string) string {
		switch k {
		case "HOME":
			return "/home/drew"
		case "XDG_CONFIG_HOME":
			return "/xdg"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/home/drew", ".claude") {
		t.Fatalf("default %s", got)
	}

	got, err = Home(func(k string) string {
		if k == "USERPROFILE" {
			return `C:\Users\drew`
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(`C:\Users\drew`, ".claude") {
		t.Fatalf("userprofile %s", got)
	}

	if _, err := Home(func(string) string { return "" }); err == nil {
		t.Fatal("missing home should fail")
	}
}

func TestDiscoverProjectsGlob(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "projects", "-home-drew-src-foo")
	sub := filepath.Join(proj, "sess-1", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(proj, "sess-1.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(sub, "agent-a.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(proj, "notes.txt"), "no")
	mustWrite(t, filepath.Join(proj, ".hidden.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(root, "history.jsonl"), "{}\n")

	refs, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range refs {
		got[r.RelPath] = true
		if r.Kind != protocol.KindTranscriptJSONL {
			t.Fatalf("kind %s", r.Kind)
		}
	}
	for _, rel := range []string{
		"projects/-home-drew-src-foo/sess-1.jsonl",
		"projects/-home-drew-src-foo/sess-1/subagents/agent-a.jsonl",
	} {
		if !got[rel] {
			t.Fatalf("missing %s in %v", rel, got)
		}
	}
	if got["projects/-home-drew-src-foo/notes.txt"] || got["projects/-home-drew-src-foo/.hidden.jsonl"] || got["history.jsonl"] {
		t.Fatalf("discovered a non-session: %v", got)
	}

	empty, err := Adapter{}.Discover(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("missing projects: %d", len(empty))
	}
}

func TestParseKeepsUnknownFields(t *testing.T) {
	line := []byte(`{"type":"user","sessionId":"sid-1","cwd":"/work/app","version":"2.1.1","future_field":{"n":1},"message":{"role":"user"}}`)
	rec, err := ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if rec.SessionID != "sid-1" || rec.CWD != "/work/app" || rec.Type != "user" {
		t.Fatalf("record %+v", rec)
	}
	if string(rec.Extra["future_field"]) != `{"n":1}` {
		t.Fatalf("future_field %s", rec.Extra["future_field"])
	}
	if string(rec.Extra["version"]) != `"2.1.1"` {
		t.Fatalf("cli version was interpreted: %s", rec.Extra["version"])
	}
	if _, ok := rec.Extra["sessionId"]; ok {
		t.Fatal("sessionId was copied into extra")
	}
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var again map[string]any
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	future, _ := again["future_field"].(map[string]any)
	if future["n"] != float64(1) {
		t.Fatalf("round trip dropped future_field: %s", out)
	}
	if again["version"] != "2.1.1" || again["sessionId"] != "sid-1" {
		t.Fatalf("round trip %s", out)
	}
}

func TestManifestPinsAdapterVersion(t *testing.T) {
	if Version != "1" {
		t.Fatalf("adapter version %q", Version)
	}
	root := t.TempDir()
	body := `{"type":"user","sessionId":"sid-1","cwd":"/work/app","version":"2.1.1","future_field":true}` + "\n" +
		`{"type":"assistant","sessionId":"sid-1","extra_line":{"keep":true}}` + "\n"
	dir := filepath.Join(root, "projects", "-work-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sid-1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "sid-1", "subagents")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	sub := `{"type":"user","sessionId":"sid-1","cwd":"/work/app"}` + "\n"
	if err := os.WriteFile(filepath.Join(other, "agent-a.jsonl"), []byte(sub), 0o644); err != nil {
		t.Fatal(err)
	}
	solo := filepath.Join(root, "projects", "-other")
	if err := os.MkdirAll(solo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(solo, "no-id.jsonl"), []byte("not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 2 {
		t.Fatalf("manifests %d", len(b.Manifests))
	}
	var grouped protocol.Manifest
	for _, m := range b.Manifests {
		if m.Harness != protocol.HarnessClaude || m.HarnessVersion != Version || m.CaptureProtocol != 1 {
			t.Fatalf("header %+v", m)
		}
		if m.HarnessVersion == "2.1.1" {
			t.Fatal("cli version was used as the adapter version")
		}
		if m.NativeSessionID == "sid-1" {
			grouped = m
		}
	}
	if len(grouped.Artifacts) != 2 {
		t.Fatalf("grouped %+v", grouped.Artifacts)
	}
	if grouped.Project.CWD != "/work/app" || grouped.Project.CWDHash == "" {
		t.Fatalf("project %+v", grouped.Project)
	}
}

func TestWatchProjects(t *testing.T) {
	for _, poll := range []bool{false, true} {
		name := "fsnotify"
		if poll {
			name = "poll"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			rel := "projects/-work-app/sess-1.jsonl"
			path := filepath.Join(root, "projects", "-work-app", "sess-1.jsonl")
			mustWrite(t, path, "{}\n")
			mustWrite(t, filepath.Join(root, "projects", "-work-app", "notes.txt"), "no")
			a := Adapter{}
			w, ch := startLayout(t, root, a.WatchDir(), a.Match, poll)
			waitTracked(t, w, rel)
			for _, got := range w.Tracked() {
				if got == "projects/-work-app/notes.txt" {
					t.Fatal("tracked a non-jsonl file")
				}
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(`{"type":"user"}` + "\n"); err != nil {
				t.Fatal(err)
			}
			f.Close()
			c := waitChange(t, ch, rel)
			if c.Op != watch.OpAppend || c.Offset == 0 {
				t.Fatalf("append %+v", c)
			}
		})
	}
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

func startLayout(t *testing.T, root, dir string, match func(string) (string, bool), poll bool) (*watch.Watcher, <-chan watch.Change) {
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

func waitTracked(t *testing.T, w *watch.Watcher, rel string) {
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

func waitChange(t *testing.T, ch <-chan watch.Change, rel string) watch.Change {
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
			return watch.Change{}
		}
	}
}

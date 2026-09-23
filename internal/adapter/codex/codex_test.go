package codex

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
		if k == "CODEX_HOME" {
			return "/tmp/codex-alt"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/codex-alt" {
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
	if got != filepath.Join("/home/drew", ".codex") {
		t.Fatalf("default %s", got)
	}
	if _, err := Home(func(string) string { return "" }); err == nil {
		t.Fatal("missing home should fail")
	}
}

func TestDiscoverSkipsHistory(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "sessions", "2026", "09", "23")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	roll := "sessions/2026/09/23/rollout-2026-09-23T12-00-00-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl"
	mustWrite(t, filepath.Join(root, filepath.FromSlash(roll)), "{}\n")
	mustWrite(t, filepath.Join(root, "history.jsonl"), "{\"session_id\":\"no\"}\n")
	mustWrite(t, filepath.Join(root, "sessions", "history.jsonl"), "{\"session_id\":\"no\"}\n")
	mustWrite(t, filepath.Join(day, "history.jsonl"), "{\"session_id\":\"no\"}\n")
	mustWrite(t, filepath.Join(day, "session.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(root, "archived_sessions", "2026", "09", "23", "rollout-old.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(day, "rollout-old.jsonl.zst"), "compressed")

	refs, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].RelPath != roll {
		t.Fatalf("refs %+v", refs)
	}
	if refs[0].Kind != protocol.KindTranscriptJSONL {
		t.Fatalf("kind %s", refs[0].Kind)
	}

	empty, err := Adapter{}.Discover(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("missing sessions: %d", len(empty))
	}
}

func TestParseKeepsUnknownFields(t *testing.T) {
	line := []byte(`{"timestamp":"2026-09-23T12:00:00Z","type":"session_meta","payload":{"id":"thread-1","cwd":"/work/app","cli_version":"0.9.0","future":true},"custom_top":{"a":1}}`)
	rec, err := ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if rec.SessionID() != "thread-1" || rec.CWD() != "/work/app" {
		t.Fatalf("record %+v", rec)
	}
	if string(rec.Payload["future"]) != "true" {
		t.Fatalf("payload future %s", rec.Payload["future"])
	}
	if string(rec.Payload["cli_version"]) != `"0.9.0"` {
		t.Fatalf("cli version dropped: %s", rec.Payload["cli_version"])
	}
	if string(rec.Extra["custom_top"]) != `{"a":1}` {
		t.Fatalf("custom_top %s", rec.Extra["custom_top"])
	}
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var again map[string]any
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	payload, _ := again["payload"].(map[string]any)
	if payload["future"] != true || payload["id"] != "thread-1" {
		t.Fatalf("round trip dropped payload: %s", out)
	}
	top, _ := again["custom_top"].(map[string]any)
	if top["a"] != float64(1) {
		t.Fatalf("round trip dropped custom_top: %s", out)
	}
}

func TestManifestPinsAdapterVersion(t *testing.T) {
	if Version != "1" {
		t.Fatalf("adapter version %q", Version)
	}
	root := t.TempDir()
	day := filepath.Join(root, "sessions", "2026", "09", "23")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"timestamp":"2026-09-23T12:00:00Z","type":"session_meta","payload":{"id":"thread-1","cwd":"/work/app","cli_version":"0.9.0","future":{"n":1}}}` + "\n" +
		`{"timestamp":"2026-09-23T12:00:01Z","type":"event_msg","payload":{"kind":"user"}}` + "\n"
	name := "rollout-2026-09-23T12-00-00-thread-1.jsonl"
	if err := os.WriteFile(filepath.Join(day, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(day, "history.jsonl"), []byte(`{"type":"session_meta","payload":{"id":"not-a-rollout","cwd":"/secret"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "history.jsonl"), []byte(`{"type":"session_meta","payload":{"id":"home-history","cwd":"/secret"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 {
		t.Fatalf("manifests %+v", b.Manifests)
	}
	m := b.Manifests[0]
	if m.Harness != protocol.HarnessCodex || m.HarnessVersion != Version {
		t.Fatalf("header %+v", m)
	}
	if m.HarnessVersion == "0.9.0" {
		t.Fatal("cli version was used as the adapter version")
	}
	if m.NativeSessionID != "thread-1" || m.Project.CWD != "/work/app" {
		t.Fatalf("identity %+v", m)
	}
	if m.NativeSessionID == "not-a-rollout" || m.NativeSessionID == "home-history" {
		t.Fatal("history.jsonl was ingested as a rollout")
	}
}

func TestWatchSkipsHistory(t *testing.T) {
	for _, poll := range []bool{false, true} {
		name := "fsnotify"
		if poll {
			name = "poll"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			day := filepath.Join(root, "sessions", "2026", "09", "23")
			rollRel := "sessions/2026/09/23/rollout-2026-09-23T12-00-00-thread-1.jsonl"
			roll := filepath.Join(root, filepath.FromSlash(rollRel))
			mustWrite(t, roll, "{}\n")
			mustWrite(t, filepath.Join(day, "history.jsonl"), "{}\n")
			mustWrite(t, filepath.Join(root, "sessions", "history.jsonl"), "{}\n")
			mustWrite(t, filepath.Join(root, "history.jsonl"), "{}\n")

			a := Adapter{}
			w, ch := startLayout(t, root, a.WatchDir(), a.Match, poll)
			waitTracked(t, w, rollRel)
			for _, rel := range w.Tracked() {
				if filepath.Base(rel) == "history.jsonl" {
					t.Fatalf("tracked history %s in %v", rel, w.Tracked())
				}
			}

			hist := filepath.Join(day, "history.jsonl")
			f, err := os.OpenFile(hist, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString("{\"n\":1}\n"); err != nil {
				t.Fatal(err)
			}
			f.Close()
			quiet := time.After(150 * time.Millisecond)
		drain:
			for {
				select {
				case c := <-ch:
					if filepath.Base(c.RelPath) == "history.jsonl" {
						t.Fatalf("history event %+v", c)
					}
				case <-quiet:
					break drain
				}
			}

			f, err = os.OpenFile(roll, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString("{\"type\":\"event_msg\"}\n"); err != nil {
				t.Fatal(err)
			}
			f.Close()
			c := waitChange(t, ch, rollRel)
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

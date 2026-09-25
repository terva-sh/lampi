package opencode

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
		if k == "XDG_DATA_HOME" {
			return "/tmp/xdg-data"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/tmp/xdg-data", "opencode") {
		t.Fatalf("xdg %s", got)
	}

	got, err = Home(func(k string) string {
		switch k {
		case "HOME":
			return "/home/drew"
		case "XDG_CONFIG_HOME":
			return "/xdg-config"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/home/drew", ".local", "share", "opencode") {
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
	if got != filepath.Join(`C:\Users\drew`, ".local", "share", "opencode") {
		t.Fatalf("userprofile %s", got)
	}

	if _, err := Home(func(string) string { return "" }); err == nil {
		t.Fatal("missing home should fail")
	}
}

func TestDiscoverPrefersExport(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "export", "ses_1.json"), "{}\n")
	mustWrite(t, filepath.Join(root, "export", "nested", "ses_2.json"), "{}\n")
	mustWrite(t, filepath.Join(root, "export", "notes.txt"), "no")
	mustWrite(t, filepath.Join(root, "export", ".hidden.json"), "{}\n")
	mustWrite(t, filepath.Join(root, "opencode.db"), "db")
	mustWrite(t, filepath.Join(root, "opencode.db-wal"), "wal")
	mustWrite(t, filepath.Join(root, "opencode.db-shm"), "shm")
	mustWrite(t, filepath.Join(root, "opencode-stable.db"), "channel")

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
	for _, rel := range []string{"export/ses_1.json", "export/nested/ses_2.json"} {
		if !got[rel] {
			t.Fatalf("missing %s in %v", rel, got)
		}
	}
	for _, rel := range []string{
		"export/notes.txt",
		"export/.hidden.json",
		"opencode.db",
		"opencode.db-wal",
		"opencode.db-shm",
		"opencode-stable.db",
	} {
		if got[rel] {
			t.Fatalf("export present, still discovered %s", rel)
		}
	}
}

func TestDiscoverDatabaseWhenNoExport(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "opencode.db"), "db")
	mustWrite(t, filepath.Join(root, "opencode.db-wal"), "wal")
	mustWrite(t, filepath.Join(root, "opencode.db-shm"), "shm")
	mustWrite(t, filepath.Join(root, "opencode.db-journal"), "journal")
	mustWrite(t, filepath.Join(root, "opencode-stable.db"), "channel")
	mustWrite(t, filepath.Join(root, ".opencode.db"), "hidden")
	mustWrite(t, filepath.Join(root, "log", "debug.db"), "nested")
	mustWrite(t, filepath.Join(root, "export", "notes.txt"), "not json")

	refs, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range refs {
		got[r.RelPath] = true
	}
	if !got["opencode.db"] || !got["opencode-stable.db"] {
		t.Fatalf("database files: %v", got)
	}
	for _, rel := range []string{
		"opencode.db-wal",
		"opencode.db-shm",
		"opencode.db-journal",
		".opencode.db",
		"log/debug.db",
		"export/notes.txt",
	} {
		if got[rel] {
			t.Fatalf("discovered %s", rel)
		}
	}

	empty, err := Adapter{}.Discover(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("missing data dir: %d", len(empty))
	}
}

func TestParseKeepsUnknownFields(t *testing.T) {
	raw := []byte(`{"info":{"id":"ses_1","directory":"/work/app","version":"1.2.3","future_field":{"n":1}},"messages":[{"keep":true}],"extra_top":1}`)
	rec, err := ParseExport(raw)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Info.ID != "ses_1" || rec.Info.Directory != "/work/app" {
		t.Fatalf("info %+v", rec.Info)
	}
	if string(rec.Info.Extra["future_field"]) != `{"n":1}` {
		t.Fatalf("future_field %s", rec.Info.Extra["future_field"])
	}
	if string(rec.Info.Extra["version"]) != `"1.2.3"` {
		t.Fatalf("cli version was interpreted: %s", rec.Info.Extra["version"])
	}
	if _, ok := rec.Info.Extra["id"]; ok {
		t.Fatal("id was copied into extra")
	}
	if string(rec.Extra["messages"]) != `[{"keep":true}]` {
		t.Fatalf("messages %s", rec.Extra["messages"])
	}
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var again map[string]any
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	info, _ := again["info"].(map[string]any)
	future, _ := info["future_field"].(map[string]any)
	if future["n"] != float64(1) || info["version"] != "1.2.3" || info["id"] != "ses_1" {
		t.Fatalf("round trip %s", out)
	}
	if again["extra_top"] != float64(1) {
		t.Fatalf("round trip dropped extra_top: %s", out)
	}
}

func TestManifestPinsAdapterVersion(t *testing.T) {
	if Version != "1" {
		t.Fatalf("adapter version %q", Version)
	}
	root := t.TempDir()
	body := `{"info":{"id":"ses_1","directory":"/work/app","version":"1.2.3","future_field":true},"messages":[]}` + "\n"
	mustWrite(t, filepath.Join(root, "export", "ses_1.json"), body)
	mustWrite(t, filepath.Join(root, "export", "copy", "again.json"), `{"info":{"id":"ses_1","directory":"/work/app"}}`+"\n")
	mustWrite(t, filepath.Join(root, "export", "no-id.json"), "not-json\n")
	mustWrite(t, filepath.Join(root, "opencode.db"), "db")
	mustWrite(t, filepath.Join(root, "opencode.db-wal"), "wal")

	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 2 {
		t.Fatalf("manifests %d", len(b.Manifests))
	}
	var grouped protocol.Manifest
	for _, m := range b.Manifests {
		if m.Harness != protocol.HarnessOpenCode || m.HarnessVersion != Version || m.CaptureProtocol != 1 {
			t.Fatalf("header %+v", m)
		}
		if m.HarnessVersion == "1.2.3" {
			t.Fatal("cli version was used as the adapter version")
		}
		if m.NativeSessionID == "ses_1" {
			grouped = m
		}
		if stringsContainsDB(m) {
			t.Fatal("database was a manifest while export JSON exists")
		}
	}
	if len(grouped.Artifacts) != 2 {
		t.Fatalf("grouped %+v", grouped.Artifacts)
	}
	if grouped.Project.CWD != "/work/app" || grouped.Project.CWDHash == "" {
		t.Fatalf("project %+v", grouped.Project)
	}
}

func TestManifestDatabaseFallback(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "opencode.db"), "sqlite-bytes")
	mustWrite(t, filepath.Join(root, "opencode.db-wal"), "wal-bytes")

	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 {
		t.Fatalf("manifests %d", len(b.Manifests))
	}
	m := b.Manifests[0]
	if m.Harness != protocol.HarnessOpenCode || m.NativeSessionID != "opencode.db" {
		t.Fatalf("header %+v", m)
	}
	if len(m.Artifacts) != 1 || m.Artifacts[0].RelPath != "opencode.db" {
		t.Fatalf("artifacts %+v", m.Artifacts)
	}
	if m.Project.CWD != "" || m.Project.CWDHash != "" {
		t.Fatalf("database project should be empty: %+v", m.Project)
	}
	if _, ok := b.Paths[m.Artifacts[0].RelPath]; !ok {
		t.Fatal("database bytes were not a put path")
	}
}

func TestWatchExport(t *testing.T) {
	for _, poll := range []bool{false, true} {
		name := "fsnotify"
		if poll {
			name = "poll"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			rel := "export/ses_1.json"
			path := filepath.Join(root, "export", "ses_1.json")
			mustWrite(t, path, "{}\n")
			mustWrite(t, filepath.Join(root, "opencode.db"), "db")
			mustWrite(t, filepath.Join(root, "opencode.db-wal"), "wal")
			a := Adapter{}
			w, ch := startLayout(t, root, a.WatchDir(), a.Match, poll)
			waitTracked(t, w, rel)
			for _, got := range w.Tracked() {
				if got == "opencode.db" || got == "opencode.db-wal" {
					t.Fatalf("tracked %s", got)
				}
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(`{"info":{"id":"ses_1"}}` + "\n"); err != nil {
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

func stringsContainsDB(m protocol.Manifest) bool {
	for _, a := range m.Artifacts {
		if a.RelPath == "opencode.db" || a.RelPath == "opencode.db-wal" {
			return true
		}
	}
	return false
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

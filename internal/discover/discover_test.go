package discover

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSessions(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260922-161000-abcd1234.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260922-161000-abcd1234.errors.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := Sessions(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files: %+v", files)
	}
	kinds := map[string]string{}
	for _, f := range files {
		kinds[filepath.Base(f.AbsPath)] = f.Kind
		if !stringsHasSlash(f.RelPath) {
			t.Fatalf("relpath %q", f.RelPath)
		}
	}
	if kinds["20260922-161000-abcd1234.jsonl"] != KindTranscript {
		t.Fatalf("transcript kind %q", kinds["20260922-161000-abcd1234.jsonl"])
	}
	if kinds["20260922-161000-abcd1234.errors.jsonl"] != KindErrors {
		t.Fatalf("errors kind %q", kinds["20260922-161000-abcd1234.errors.jsonl"])
	}

	empty, err := Sessions(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("missing home returned %d", len(empty))
	}
}

func TestSidecarsOptional(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "raati"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "ext-data", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("raati/raati-1700000000000000000.json", `{"question":"ship?"}`)
	write("raati/notes.txt", "no")
	write("raati/raati-12a.json", "no")
	write("tasks/tasks-11111111-2222-3333-4444-555555555555.json", `{"tasks":[]}`)
	write("tasks/tasks-not safe.json", "no")
	write("tasks/board.json", "no")
	write("ext-data/tasks/tasks-legacy-board.json", `{"generations":[]}`)
	write("ext-data/tasks/tasks-bad.json.corrupt-1", "no")

	files, err := Sidecars(home)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.RelPath] = f.Kind
		if !stringsHasSlash(f.RelPath) {
			t.Fatalf("relpath %q", f.RelPath)
		}
	}
	want := map[string]string{
		"raati/raati-1700000000000000000.json":                  KindRaati,
		"tasks/tasks-11111111-2222-3333-4444-555555555555.json": KindTasks,
		"ext-data/tasks/tasks-legacy-board.json":                KindTasks,
	}
	if len(got) != len(want) {
		t.Fatalf("files: %+v", got)
	}
	for rel, kind := range want {
		if got[rel] != kind {
			t.Fatalf("%s kind %q", rel, got[rel])
		}
	}

	empty, err := Sidecars(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("missing home returned %d", len(empty))
	}

	sessions, err := Sessions(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions walked sidecars: %+v", sessions)
	}
}

func TestClassifySessions(t *testing.T) {
	kind, ok := Classify("sessions/abcd/s.jsonl")
	if !ok || kind != KindTranscript {
		t.Fatalf("transcript %q %v", kind, ok)
	}
	kind, ok = Classify("sessions/abcd/s.errors.jsonl")
	if !ok || kind != KindErrors {
		t.Fatalf("errors %q %v", kind, ok)
	}
	if _, ok := Classify("sessions/abcd/notes.txt"); ok {
		t.Fatal("notes.txt classified")
	}
	if _, ok := Classify("sessions/.hidden.jsonl"); ok {
		t.Fatal("dotfile classified")
	}
}

func stringsHasSlash(s string) bool {
	for _, c := range s {
		if c == '\\' {
			return false
		}
	}
	return len(s) > 0
}

func TestTervaHomeEnv(t *testing.T) {
	got, err := TervaHome(func(k string) string {
		if k == "TERVA_HOME" {
			return "/tmp/custom"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/custom" {
		t.Fatalf("got %s", got)
	}

	got, err = TervaHome(func(k string) string {
		switch k {
		case "HOME":
			return "/home/drew"
		case "XDG_STATE_HOME":
			return "/var/state"
		case "LOCALAPPDATA":
			return `C:\Users\drew\AppData\Local`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = filepath.Join("/home/drew", "Library", "Application Support", "terva")
	case "windows":
		want = filepath.Join(`C:\Users\drew\AppData\Local`, "terva")
	default:
		want = filepath.Join("/var/state", "terva")
	}
	if got != want {
		t.Fatalf("default home %s, want %s", got, want)
	}
}

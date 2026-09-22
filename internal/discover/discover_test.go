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

package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir", "token")
	tok, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Fatalf("token length %d", len(tok))
	}
	if err := Write(path, tok); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != tok {
		t.Fatalf("read %q", got)
	}
	if !Match("Bearer "+tok, tok) {
		t.Fatal("expected match")
	}
	if Match("Bearer nope-not-the-token-value-pad", tok) || Match(tok, tok) || Match("Bearer "+tok, "") {
		t.Fatal("expected rejection")
	}
}

func TestWriteReplacesWholeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	first := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	second := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := Write(path, first); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, second); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != second {
		t.Fatalf("read %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "token" {
		t.Fatalf("temp file left behind: %v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

func TestWriteRejectsEmpty(t *testing.T) {
	if err := Write(filepath.Join(t.TempDir(), "t"), "  "); err == nil {
		t.Fatal("expected empty token to fail")
	}
}

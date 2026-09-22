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

func TestWriteRejectsEmpty(t *testing.T) {
	if err := Write(filepath.Join(t.TempDir(), "t"), "  "); err == nil {
		t.Fatal("expected empty token to fail")
	}
}

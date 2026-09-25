package auth

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDevicesHashedAtRest(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two devices were given the same token")
	}
	dir := t.TempDir()
	client := filepath.Join(dir, "client-token")
	if err := Write(client, a); err != nil {
		t.Fatal(err)
	}
	lake := filepath.Join(dir, "lake-tokens")
	if err := os.WriteFile(lake, []byte(a+"\n"+b+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	devices, err := LoadDevices(lake)
	if err != nil {
		t.Fatal(err)
	}
	if devices.Len() != 2 {
		t.Fatalf("devices %d", devices.Len())
	}
	if !devices.Match("Bearer "+a) || !devices.Match("Bearer "+b) {
		t.Fatal("enrolled device was rejected")
	}
	if devices.Match("Bearer "+a+"no") || devices.Match("Bearer "+HashToken(a)) || devices.Match(a) {
		t.Fatal("expected rejection")
	}
	stored, err := os.ReadFile(lake)
	if err != nil {
		t.Fatal(err)
	}
	text := string(stored)
	if strings.Contains(text, a) || strings.Contains(text, b) {
		t.Fatalf("plaintext token left on disk:\n%s", text)
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if !strings.HasPrefix(line, "sha256:") {
			t.Fatalf("line %q is not a hash", line)
		}
	}
	// The client's own file is still the secret. The lake copy is the hash.
	got, err := Read(client)
	if err != nil {
		t.Fatal(err)
	}
	if got != a {
		t.Fatalf("client token changed: %q", got)
	}
	again, err := LoadDevices(lake)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Match("Bearer "+a) || !again.Match("Bearer "+b) {
		t.Fatal("reloading hashes failed")
	}

	perDevice := filepath.Join(dir, "devices")
	if err := os.MkdirAll(perDevice, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(perDevice, "laptop.token"), []byte(a+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(perDevice, "desktop.token"), []byte(b+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	many, err := LoadDevices(perDevice)
	if err != nil {
		t.Fatal(err)
	}
	if many.Len() != 2 || !many.Match("Bearer "+a) || !many.Match("Bearer "+b) {
		t.Fatalf("directory devices %d", many.Len())
	}
	laptop, err := os.ReadFile(filepath.Join(perDevice, "laptop.token"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(laptop), a) {
		t.Fatalf("device file kept the token: %s", laptop)
	}
	st, err := os.Stat(lake)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("lake token mode %o", st.Mode().Perm())
	}
}

func TestWriteRejectsEmpty(t *testing.T) {
	if err := Write(filepath.Join(t.TempDir(), "t"), "  "); err == nil {
		t.Fatal("expected empty token to fail")
	}
}

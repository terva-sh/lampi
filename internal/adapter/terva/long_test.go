package terva

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter"
)

// A first line past 1 MiB would carry the meta. That transcript is left
// out and named; the other session in the home is still a manifest.
func TestManifestsSkipsAnOverlongFirstLine(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	huge := `{"type":"meta","pad":"` + strings.Repeat("x", maxMetaLine) + "\"}\n"
	if err := os.WriteFile(filepath.Join(dir, "big.jsonl"), []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}
	ok := `{"type":"meta","meta":{"id":"small","cwd":"/work/app"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "small.jsonl"), []byte(ok), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Manifests(home, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 || b.Manifests[0].NativeSessionID != "small" {
		t.Fatalf("manifests %+v", b.Manifests)
	}
	var long *adapter.LongLineError
	if len(b.Skipped) != 1 || !errors.As(b.Skipped[0], &long) || !strings.Contains(b.Skipped[0].Error(), "sessions/abcd/big.jsonl") {
		t.Fatalf("skipped %v", b.Skipped)
	}
}

// A sessions directory that is a symlink is walked through the link.
// filepath.WalkDir alone does not follow a symlinked root and found 0.
func TestManifestsFollowSymlinkedSessions(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "abcd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "abcd", "s.jsonl"), []byte(`{"type":"meta","meta":{"id":"s","cwd":"/w"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Symlink(target, filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}
	b, err := Manifests(home, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 || b.Paths["sessions/abcd/s.jsonl"] != filepath.Join(home, "sessions", "abcd", "s.jsonl") {
		t.Fatalf("manifests %+v paths %v", b.Manifests, b.Paths)
	}
}

package claude

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter"
)

// A line past the cap is read past. Identity after it still counts.
// A file whose identity is still missing after such a line is left out
// and named, and the other file in the tree stays in the bundle.
func TestManifestsSkipsALongLineBeforeIdentity(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "-work-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	huge := `{"type":"user","blob":"` + strings.Repeat("x", maxLine) + "\"}\n"
	later := huge + `{"type":"user","sessionId":"sid-later","cwd":"/work/app"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "later.jsonl"), []byte(later), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lost.jsonl"), []byte(huge+`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 || b.Manifests[0].NativeSessionID != "sid-later" || b.Manifests[0].Project.CWD != "/work/app" {
		t.Fatalf("manifests %+v", b.Manifests)
	}
	if len(b.Skipped) != 1 || !strings.Contains(b.Skipped[0].Error(), "projects/-work-app/lost.jsonl") {
		t.Fatalf("skipped %v", b.Skipped)
	}
	var long *adapter.LongLineError
	if !errors.As(b.Skipped[0], &long) {
		t.Fatalf("skip is not a long line: %v", b.Skipped[0])
	}
}

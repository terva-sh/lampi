package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/lakelock"
	"terva.sh/lampi/internal/recall"
)

func TestCompactDryRunsBesideServeAndOtherwiseNeedsTheLock(t *testing.T) {
	dir, lake, _, _ := liveLake(t)
	env := Env{Stdout: ioDiscard(), Stderr: ioDiscard()}
	if err := Run([]string{"serve", "compact", "--data", dir}, env); !errors.Is(err, lakelock.ErrHeld) {
		t.Fatalf("compact beside serve: %v", err)
	}
	var out bytes.Buffer
	if err := Run([]string{"serve", "compact", "--data", dir, "--dry-run"}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "folded into a prefix record: 0") || !strings.Contains(out.String(), "raw objects to compress: 0") || !strings.Contains(out.String(), "dry run") {
		t.Fatalf("dry run:\n%s", out.String())
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}
	releaseLive(t, dir)
	out.Reset()
	if err := Run([]string{"serve", "compact", "--data", dir}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "dry run") || !strings.Contains(out.String(), "object bytes reclaimed: 0") || !strings.Contains(out.String(), "raw objects compressed: 0") {
		t.Fatalf("compact:\n%s", out.String())
	}
	// A lake without a web config has no search index, and compact says
	// nothing about one. With an index, compact optimizes it.
	if strings.Contains(out.String(), "search index") {
		t.Fatalf("compact reported a search index the lake does not have:\n%s", out.String())
	}
	x, err := recall.OpenIndex(filepath.Join(dir, recall.IndexFile), nil)
	if err != nil {
		t.Fatal(err)
	}
	x.Close()
	out.Reset()
	if err := Run([]string{"serve", "compact", "--data", dir, "--dry-run"}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil || !strings.Contains(out.String(), "search index to optimize: ") {
		t.Fatalf("dry run with an index: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := Run([]string{"serve", "compact", "--data", dir}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil || !strings.Contains(out.String(), "search index optimized: ") {
		t.Fatalf("compact with an index: %v\n%s", err, out.String())
	}
	if err := Run([]string{"serve", "compact", "--data", dir, "--min-age", "-1s"}, env); err == nil {
		t.Fatal("a negative --min-age was accepted")
	}
}

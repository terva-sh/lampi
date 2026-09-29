package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"terva.sh/lampi/internal/lakelock"
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
	if err := Run([]string{"serve", "compact", "--data", dir, "--min-age", "-1s"}, env); err == nil {
		t.Fatal("a negative --min-age was accepted")
	}
}

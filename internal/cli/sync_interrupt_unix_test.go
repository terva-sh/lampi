//go:build unix

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"terva.sh/lampi/internal/upload"
)

func TestAnInterruptedSyncFailsNamingTheLakesItDidNotReach(t *testing.T) {
	f := newTwoLakeFixture(t)
	// Allow the session for both lakes, so the default lake's push
	// succeeds on its own.
	conf := filepath.Join(f.cfg, "terva-lampi", "config.json")
	raw, _ := os.ReadFile(conf)
	if err := os.WriteFile(conf, []byte(strings.Replace(string(raw), "/home/app", "/work/app", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	// The interrupt lands just after the default lake's push succeeds.
	var calls int
	syncLake = func(ctx context.Context, opt upload.Options) (upload.Result, error) {
		calls++
		res, err := upload.Sync(ctx, opt)
		if err != nil {
			t.Errorf("push %d: %v", calls, err)
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		<-ctx.Done()
		return res, err
	}
	t.Cleanup(func() { syncLake = upload.Sync })

	err := f.run("sync")
	if err == nil || !strings.Contains(err.Error(), "sync failed for 1 of 2 lakes: work") {
		t.Fatalf("interrupted sync: %v\n%s\n%s", err, f.stdout, f.stderr)
	}
	if calls != 1 {
		t.Fatalf("%d lakes pushed after the interrupt", calls)
	}
	if !strings.Contains(f.stderr.String(), "lake work: not attempted: context canceled") {
		t.Fatalf("the skipped lake is not named:\n%s", f.stderr)
	}
	if n, _ := f.work.Catalog.Counts(t.Context()); n.Sessions != 0 {
		t.Fatalf("work sessions %d", n.Sessions)
	}
}

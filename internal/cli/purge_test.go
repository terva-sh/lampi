package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/lakelock"
)

func TestPurgeIsADryRunUntilYesAndRefusesBesideServe(t *testing.T) {
	dir, lake, uid, sum := liveLake(t)
	env := Env{Stdout: ioDiscard(), Stderr: ioDiscard()}
	if err := Run([]string{"serve", "purge", "--data", dir, "--session", uid, "--yes"}, env); !errors.Is(err, lakelock.ErrHeld) {
		t.Fatalf("purge beside serve: %v", err)
	}
	// Stop the stand-in serve and give up its lock.
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}
	releaseLive(t, dir)

	var out bytes.Buffer
	if err := Run([]string{"serve", "purge", "--data", dir, "--session", uid}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "objects: 1\n  "+sum) || !strings.Contains(out.String(), "dry run") {
		t.Fatalf("dry run:\n%s", out.String())
	}
	check, err := api.OpenIdle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := check.CAS.Has(sum); !ok {
		t.Fatal("dry run removed the blob")
	}
	check.Close()

	out.Reset()
	if err := Run([]string{"serve", "purge", "--data", dir, "--session", uid, "--yes"}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	check, err = api.OpenIdle(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	if ok, _ := check.CAS.Has(sum); ok {
		t.Fatal("purge kept the blob")
	}
	if _, ok, _ := check.Catalog.Session(t.Context(), uid); ok {
		t.Fatal("purge kept the session")
	}
	if err := Run([]string{"serve", "purge", "--data", dir, "--session", uid}, env); err == nil || !strings.Contains(err.Error(), "no session") {
		t.Fatalf("purge of a purged session: %v", err)
	}
}

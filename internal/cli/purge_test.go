package cli

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/lakelock"
	"terva.sh/lampi/internal/recall"
)

func TestPurgeIsADryRunUntilYesAndRefusesBesideServe(t *testing.T) {
	dir, lake, uid, sum := liveLake(t)
	env := Env{Stdout: ioDiscard(), Stderr: ioDiscard()}
	if err := Run([]string{"serve", "purge", "--data", dir, "--session", uid, "--yes"}, env); !errors.Is(err, lakelock.ErrHeld) {
		t.Fatalf("purge beside serve: %v", err)
	}
	// Stop the stand-in serve and give up its lock. Index the session
	// first, as a lake with the web interface would have.
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	index, err := recall.OpenIndex(filepath.Join(dir, recall.IndexFile), recall.NewReader(lake.Catalog, lake.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	index.Close()
	indexRows := func() int {
		db, err := sql.Open("sqlite", filepath.Join(dir, recall.IndexFile))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM docs WHERE session_uid=?`, uid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if indexRows() == 0 {
		t.Fatal("fixture session not indexed")
	}
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
	if n := indexRows(); n != 0 {
		t.Fatal("purge kept search rows", n)
	}
	if err := Run([]string{"serve", "purge", "--data", dir, "--session", uid}, env); err == nil || !strings.Contains(err.Error(), "no session") {
		t.Fatalf("purge of a purged session: %v", err)
	}
}

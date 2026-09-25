package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/lakelock"
)

// liveLake stands in for a running serve: it holds lake.lock and has
// one normalized session.
func liveLake(t *testing.T) (dir string, lake *api.Server, uid, sum string) {
	t.Helper()
	dir = t.TempDir()
	lock, err := lakelock.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Release() })
	lake, err = api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Allow("sekret")
	h := lake.Handler()
	body := fixturePrompt()
	sum, _, err = cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, sum, body)
	ack := postManifest(t, h, manifest("sid-prompt", "sessions/x/sid-prompt.jsonl", sum, int64(len(body))))
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	return dir, lake, ack.SessionUID, sum
}

func TestServeRefusesALakeInUse(t *testing.T) {
	dir, _, _, _ := liveLake(t)
	err := Run([]string{"serve", "--data", dir}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if !errors.Is(err, lakelock.ErrHeld) || !strings.Contains(err.Error(), "in use by pid") {
		t.Fatalf("second serve: %v", err)
	}
}

// Export beside a running serve opens the catalog read-only. A session
// with a queued job and no derived file is named, not projected: a
// worker in this process would publish over serve's.
func TestExportBesideServeStartsNoWorker(t *testing.T) {
	dir, lake, uid, _ := liveLake(t)
	ctx := t.Context()
	if _, err := lake.Catalog.EnqueueNormalize(ctx, uid, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Stop the stand-in's workers so only export could project.
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(dir, "normalized", uid+".jsonl")
	if err := os.Remove(jsonl); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := Run([]string{"export", "--data", dir}, Env{Stdout: &stdout, Stderr: &stderr}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), uid+" not yet normalized") || stdout.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(jsonl); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("export beside serve wrote the derived file: %v", err)
	}
	cat, err := catalog.OpenReadOnly(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cat.Close()
	jobs, err := cat.ListNormalizeJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].SessionUID != uid {
		t.Fatalf("export beside serve ran the queued job: %+v", jobs)
	}
}

func TestBackupWhileServeRuns(t *testing.T) {
	dir, lake, uid, sum := liveLake(t)
	tokens := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokens, []byte("sha256:"+strings.Repeat("ab", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "backup")
	var stdout bytes.Buffer
	if err := Run([]string{"serve", "backup", "--data", dir, "--out", out, "--token-file", tokens}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "catalog.db: 1 sessions") || !strings.Contains(stdout.String(), "cas: 1 new entries copied") {
		t.Fatalf("backup output:\n%s", stdout.String())
	}

	cat, err := catalog.OpenReadOnly(filepath.Join(out, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cat.Close()
	if _, ok, err := cat.Session(t.Context(), uid); err != nil || !ok {
		t.Fatalf("session in the backup ok=%v err=%v", ok, err)
	}
	got, err := (&cas.Store{Root: filepath.Join(out, "cas")}).Read(sum)
	if err != nil || !bytes.Equal(got, fixturePrompt()) {
		t.Fatalf("blob in the backup: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "tokens")); err != nil || !strings.HasPrefix(string(b), "sha256:") {
		t.Fatalf("token file in the backup: %q %v", b, err)
	}

	// Serve keeps writing; a second backup into the same directory
	// replaces the catalog and copies only the new object.
	body := []byte(`{"type":"meta","meta":{"id":"sid-two","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}` + "\n")
	two, _, _ := cas.Hash(bytes.NewReader(body))
	putBlob(t, lake.Handler(), two, body)
	postManifest(t, lake.Handler(), manifest("sid-two", "sessions/x/sid-two.jsonl", two, int64(len(body))))
	stdout.Reset()
	if err := Run([]string{"serve", "backup", "--data", dir, "--out", out}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "catalog.db: 2 sessions") || !strings.Contains(stdout.String(), "cas: 1 new entries copied") {
		t.Fatalf("second backup output:\n%s", stdout.String())
	}

	if err := Run([]string{"serve", "backup", "--data", dir, "--out", filepath.Join(dir, "bk")}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err == nil {
		t.Fatal("backup into the lake was accepted")
	}

	// A stopped lake has no -wal or -shm file. The read-only open
	// still reads it.
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"serve", "backup", "--data", dir, "--out", out}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatalf("backup of a stopped lake: %v", err)
	}
}

func TestFsckNamesBadObjectsAndRefusesRepairBesideServe(t *testing.T) {
	dir, lake, _, sum := liveLake(t)
	p, err := lake.CAS.Path(sum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("rotted"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err = Run([]string{"serve", "fsck", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()})
	if err == nil || !strings.Contains(stdout.String(), "bad object "+sum) || !strings.Contains(stdout.String(), "checked 1 entries, 1 bad") {
		t.Fatalf("fsck: %v\n%s", err, stdout.String())
	}
	if err := Run([]string{"serve", "fsck", "--data", dir, "--repair"}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); !errors.Is(err, lakelock.ErrHeld) {
		t.Fatalf("repair beside serve: %v", err)
	}
	if ok, _ := lake.CAS.Has(sum); !ok {
		t.Fatal("refused repair removed the object")
	}
}

func TestFsckRepairRemovesBadObject(t *testing.T) {
	dir := t.TempDir()
	store, err := cas.Open(filepath.Join(dir, "cas"))
	if err != nil {
		t.Fatal(err)
	}
	good, bad := []byte("good"), []byte("bad")
	for _, b := range [][]byte{good, bad} {
		sum, _, _ := cas.Hash(bytes.NewReader(b))
		if _, err := store.Put(sum, bytes.NewReader(b), 0); err != nil {
			t.Fatal(err)
		}
	}
	badSum, _, _ := cas.Hash(bytes.NewReader(bad))
	p, _ := store.Path(badSum)
	if err := os.WriteFile(p, []byte("bod"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err = Run([]string{"serve", "fsck", "--data", dir, "--repair"}, Env{Stdout: &stdout, Stderr: ioDiscard()})
	if err == nil || !strings.Contains(stdout.String(), "removed "+badSum) {
		t.Fatalf("repair: %v\n%s", err, stdout.String())
	}
	if ok, _ := store.Has(badSum); ok {
		t.Fatal("bad object still present")
	}
	stdout.Reset()
	if err := Run([]string{"serve", "fsck", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatalf("fsck after repair: %v\n%s", err, stdout.String())
	}
}

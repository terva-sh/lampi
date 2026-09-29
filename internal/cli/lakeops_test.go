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

// liveLocks lets a test stop the stand-in serve: releaseLive drops
// the lock liveLake took. Tests in this package do not run in parallel.
var liveLocks = map[string]*lakelock.Lock{}

func releaseLive(t *testing.T, dir string) {
	t.Helper()
	if err := liveLocks[dir].Release(); err != nil {
		t.Fatal(err)
	}
	delete(liveLocks, dir)
}

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
	liveLocks[dir] = lock
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

// backupTwoSessions is a lake with two sessions and a backup of it.
// The lake is stopped when it returns.
func backupTwoSessions(t *testing.T) (dir, out, uid, sum, two string) {
	t.Helper()
	dir, lake, uid, sum := liveLake(t)
	body := []byte(`{"type":"meta","meta":{"id":"sid-two","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}` + "\n")
	two, _, _ = cas.Hash(bytes.NewReader(body))
	putBlob(t, lake.Handler(), two, body)
	postManifest(t, lake.Handler(), manifest("sid-two", "sessions/x/sid-two.jsonl", two, int64(len(body))))
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	out = filepath.Join(t.TempDir(), "backup")
	if err := Run([]string{"serve", "backup", "--data", dir, "--out", out}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}
	releaseLive(t, dir)
	return dir, out, uid, sum, two
}

func backupHas(t *testing.T, out, digest string) bool {
	t.Helper()
	object, logical, _, err := (&cas.Store{Root: filepath.Join(out, "cas")}).Stored(digest)
	if err != nil {
		t.Fatal(err)
	}
	return object || logical
}

// A purged session stays in a backup until --prune, which removes it
// and nothing the backup's catalog still names.
func TestBackupPruneRemovesWhatThePurgeRemoved(t *testing.T) {
	dir, out, uid, sum, two := backupTwoSessions(t)
	if err := Run([]string{"serve", "purge", "--data", dir, "--session", uid, "--yes"}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}

	if err := Run([]string{"serve", "backup", "--data", dir, "--out", out}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !backupHas(t, out, sum) {
		t.Fatal("a backup without --prune removed the purged blob")
	}

	var stdout, stderr bytes.Buffer
	if err := Run([]string{"serve", "backup", "--data", dir, "--out", out, "--prune"}, Env{Stdout: &stdout, Stderr: &stderr}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "prune: removed 1 objects") || stderr.Len() != 0 {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if backupHas(t, out, sum) {
		t.Fatal("prune kept the purged blob")
	}
	got, err := (&cas.Store{Root: filepath.Join(out, "cas")}).Read(two)
	if err != nil || !strings.Contains(string(got), "sid-two") {
		t.Fatalf("prune removed the live blob: %v", err)
	}

	// Nothing is left to remove.
	stdout.Reset()
	if err := Run([]string{"serve", "backup", "--data", dir, "--out", out, "--prune"}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "prune: removed 0 objects") {
		t.Fatalf("second prune:\n%s", stdout.String())
	}
}

// A logical entry that does not parse hides what it reads from, so
// prune stops before removing anything.
func TestBackupPruneStopsOnAnUnreadableLogicalEntry(t *testing.T) {
	dir, out, uid, sum, two := backupTwoSessions(t)
	if err := Run([]string{"serve", "purge", "--data", dir, "--session", uid, "--yes"}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	lp := filepath.Join(dir, "cas", "logical", two[:2], two[2:])
	if err := os.MkdirAll(filepath.Dir(lp), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lp, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Run([]string{"serve", "backup", "--data", dir, "--out", out, "--prune"}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("prune past an unreadable logical entry: %v", err)
	}
	if !backupHas(t, out, sum) {
		t.Fatal("a prune that failed removed the purged blob")
	}
}

// A digest the backup's catalog names and its CAS lacks is reported.
// Pruning cannot make it worse, so it does not stop the prune.
func TestBackupPruneReportsAMissingDigest(t *testing.T) {
	dir, out, uid, sum, two := backupTwoSessions(t)
	if err := Run([]string{"serve", "purge", "--data", dir, "--session", uid, "--yes"}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	// Lost from the lake, so the next backup cannot copy it either.
	for _, root := range []string{dir, out} {
		p, _, err := (&cas.Store{Root: filepath.Join(root, "cas")}).ObjectPath(two)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	var stderr bytes.Buffer
	if err := Run([]string{"serve", "backup", "--data", dir, "--out", out, "--prune"}, Env{Stdout: ioDiscard(), Stderr: &stderr}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "names 1 digests its CAS lacks, first "+two) {
		t.Fatalf("stderr:\n%s", stderr.String())
	}
	if backupHas(t, out, sum) {
		t.Fatal("prune kept the purged blob")
	}
}

func TestFsckNamesBadObjectsAndRefusesRepairBesideServe(t *testing.T) {
	dir, lake, _, sum := liveLake(t)
	p, _, err := lake.CAS.ObjectPath(sum)
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
	p, _, _ := store.ObjectPath(badSum)
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

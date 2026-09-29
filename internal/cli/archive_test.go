package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"filippo.io/age"

	"terva.sh/lampi/internal/cas"
)

// testKeys writes a generated identity file and returns it with its
// public key. The key exists only for the test.
func testKeys(t *testing.T) (identityFile, recipient string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	identityFile = filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(identityFile, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return identityFile, id.Recipient().String()
}

// leftovers lists what a backup could leave behind: temp archives
// beside the archive and catalog snapshots in the lake.
func leftovers(t *testing.T, archiveDir, lake string) []string {
	t.Helper()
	a, _ := filepath.Glob(filepath.Join(archiveDir, ".*.tmp-*"))
	b, _ := filepath.Glob(filepath.Join(lake, ".backup-catalog-*"))
	return append(a, b...)
}

// exportEvents is dir's events export without event_id and
// ingested_at: a restored lake normalizes its sessions again, and both
// are stamped then.
func exportEvents(t *testing.T, dir string) string {
	t.Helper()
	var out bytes.Buffer
	if err := Run([]string{"export", "--data", dir}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("export line %q: %v", line, err)
		}
		delete(ev, "event_id")
		delete(ev, "ingested_at")
		b, _ := json.Marshal(ev)
		lines = append(lines, string(b))
	}
	if len(lines) < 2 {
		t.Fatalf("export has %d events", len(lines))
	}
	return strings.Join(lines, "\n")
}

// An archive restored into a fresh directory holds the same catalog,
// CAS and export as the lake it was taken from, with private modes,
// and takes nothing but public keys to write (TKT-01M3FBQX).
func TestArchiveBackupRestoresIntoAFreshDirectory(t *testing.T) {
	dir, _, _, sum, two := backupTwoSessions(t)
	tokens := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokens, []byte("sha256:"+strings.Repeat("ab", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	idFile, recipient := testKeys(t)
	otherID, otherRecipient := testKeys(t)
	rfile := filepath.Join(t.TempDir(), "recipients.txt")
	if err := os.WriteFile(rfile, []byte("# escrow\n"+otherRecipient+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	archiveDir := t.TempDir()
	file := filepath.Join(archiveDir, "lake.age")
	var stdout bytes.Buffer
	if err := Run([]string{"serve", "backup", "--data", dir, "--archive", file, "--recipient", recipient, "--recipients-file", rfile, "--token-file", tokens}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "catalog.db: 2 sessions") || !strings.Contains(stdout.String(), "2 recipients") {
		t.Fatalf("backup output:\n%s", stdout.String())
	}
	if st, err := os.Stat(file); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("archive: %v", err)
	}
	if l := leftovers(t, archiveDir, dir); len(l) > 0 {
		t.Fatalf("left behind: %v", l)
	}

	for _, key := range []string{idFile, otherID} {
		restored := filepath.Join(t.TempDir(), "restored")
		stdout.Reset()
		if err := Run([]string{"serve", "restore", "--archive", file, "--identity-file", key, "--data", restored}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
			t.Fatalf("restore: %v\n%s", err, stdout.String())
		}
		if !strings.Contains(stdout.String(), "catalog.db: 2 sessions") || !strings.Contains(stdout.String(), " 0 bad") {
			t.Fatalf("restore output:\n%s", stdout.String())
		}
		store := &cas.Store{Root: filepath.Join(restored, "cas")}
		for _, d := range []string{sum, two} {
			a, err := (&cas.Store{Root: filepath.Join(dir, "cas")}).Read(d)
			if err != nil {
				t.Fatal(err)
			}
			if b, err := store.Read(d); err != nil || !bytes.Equal(a, b) {
				t.Fatalf("object %s: %v", d, err)
			}
		}
		for name, src := range map[string]string{"identity.json": filepath.Join(dir, "identity.json"), "tokens": tokens} {
			a, aerr := os.ReadFile(src)
			b, berr := os.ReadFile(filepath.Join(restored, name))
			if (aerr == nil) != (berr == nil) || !bytes.Equal(a, b) {
				t.Fatalf("%s differs: source %v, restored %v", name, aerr, berr)
			}
		}
		if _, err := os.Stat(filepath.Join(restored, "lampi-backup.json")); !os.IsNotExist(err) {
			t.Fatalf("manifest left in the lake: %v", err)
		}
		if st, _ := os.Stat(restored); st.Mode().Perm() != 0o700 {
			t.Fatalf("restored directory is %o", st.Mode().Perm())
		}
		if got, want := exportEvents(t, restored), exportEvents(t, dir); got != want {
			t.Fatalf("export differs after restore:\n%s\nwant\n%s", got, want)
		}
	}
}

// A backup that fails part-way, is interrupted by a signal, or cannot
// write its destination leaves no archive, no temp file and no catalog
// snapshot, and an archive already at the path is kept as it was.
func TestArchiveBackupFailuresPublishNothing(t *testing.T) {
	dir, _, _, _, _ := backupTwoSessions(t)
	_, recipient := testKeys(t)
	archiveDir := t.TempDir()
	file := filepath.Join(archiveDir, "lake.age")
	if err := os.WriteFile(file, []byte("the last good archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func() error {
		return Run([]string{"serve", "backup", "--data", dir, "--archive", file, "--recipient", recipient}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	}
	check := func(what string) {
		t.Helper()
		if l := leftovers(t, archiveDir, dir); len(l) > 0 {
			t.Fatalf("%s: left behind %v", what, l)
		}
		if b, _ := os.ReadFile(file); string(b) != "the last good archive" {
			t.Fatalf("%s: the existing archive changed", what)
		}
	}

	archiveEntryHook = func(name string) error {
		if strings.HasPrefix(name, "cas/") {
			return errors.New("disk went away")
		}
		return nil
	}
	t.Cleanup(func() { archiveEntryHook = nil })
	if err := run(); err == nil || !strings.Contains(err.Error(), "disk went away") {
		t.Fatalf("failing backup: %v", err)
	}
	check("a failure part-way")

	archiveEntryHook = func(name string) error {
		if strings.HasPrefix(name, "cas/") {
			syscall.Kill(os.Getpid(), syscall.SIGTERM)
			time.Sleep(200 * time.Millisecond)
		}
		return nil
	}
	if err := run(); err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("interrupted backup: %v", err)
	}
	check("SIGTERM")
	archiveEntryHook = nil

	if os.Getuid() != 0 {
		if err := os.Chmod(archiveDir, 0o500); err != nil {
			t.Fatal(err)
		}
		err := run()
		os.Chmod(archiveDir, 0o700)
		if err == nil {
			t.Fatal("a backup into a read-only directory succeeded")
		}
		check("a read-only destination")
	}

	if err := Run([]string{"serve", "backup", "--data", dir, "--archive", file}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err == nil {
		t.Fatal("an archive with no recipient was accepted")
	}
	if err := Run([]string{"serve", "backup", "--data", dir, "--archive", filepath.Join(dir, "in-lake.age"), "--recipient", recipient}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err == nil {
		t.Fatal("an archive inside the lake was accepted")
	}
}

// A wrong key or a damaged archive stops the restore and removes what
// it wrote, the directory too when the restore made it, and a
// directory that holds anything is refused.
func TestRestoreRefusesAndCleansUp(t *testing.T) {
	dir, _, _, _, _ := backupTwoSessions(t)
	rightKey, recipient := testKeys(t)
	wrongKey, _ := testKeys(t)
	file := filepath.Join(t.TempDir(), "lake.age")
	if err := Run([]string{"serve", "backup", "--data", dir, "--archive", file, "--recipient", recipient}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	restore := func(archive, key, data string) error {
		return Run([]string{"serve", "restore", "--archive", archive, "--identity-file", key, "--data", data}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	}

	fresh := filepath.Join(t.TempDir(), "fresh")
	if err := restore(file, wrongKey, fresh); err == nil {
		t.Fatal("a wrong identity restored")
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatalf("the directory the failed restore made is still there: %v", err)
	}

	b, _ := os.ReadFile(file)
	cut := filepath.Join(t.TempDir(), "cut.age")
	os.WriteFile(cut, b[:len(b)*2/3], 0o600)
	empty := t.TempDir()
	if err := restore(cut, rightKey, empty); err == nil {
		t.Fatal("a cut archive restored")
	}
	if entries, _ := os.ReadDir(empty); len(entries) != 0 {
		t.Fatalf("a failed restore into an empty directory left %v", entries)
	}

	used := t.TempDir()
	os.WriteFile(filepath.Join(used, "catalog.db"), []byte("someone's lake"), 0o600)
	if err := restore(file, wrongKey, used); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("restore into a used directory: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(used, "catalog.db")); string(b) != "someone's lake" {
		t.Fatal("restore touched a directory in use")
	}
}

// A lake with an identity archives it, the manifest names the lake,
// and the restored lake opens as that lake. The identity's private
// keys are inside the encryption only.
func TestArchiveCarriesTheIdentity(t *testing.T) {
	dir, lake, _, _ := liveLake(t)
	if _, err := lake.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	lakeID := lake.Identity().LakeID
	key, recipient := testKeys(t)
	file := filepath.Join(t.TempDir(), "lake.age")
	if err := Run([]string{"serve", "backup", "--data", dir, "--archive", file, "--recipient", recipient}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); bytes.Contains(b, []byte(lakeID)) || bytes.Contains(b, []byte("private")) {
		t.Fatal("identity material readable in the archive")
	}
	restored := filepath.Join(t.TempDir(), "restored")
	var stdout bytes.Buffer
	if err := Run([]string{"serve", "restore", "--archive", file, "--identity-file", key, "--data", restored}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatalf("%v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "from lake "+lakeID) {
		t.Fatalf("restore output:\n%s", stdout.String())
	}
	a, _ := os.ReadFile(filepath.Join(dir, "identity.json"))
	if b, err := os.ReadFile(filepath.Join(restored, "identity.json")); err != nil || !bytes.Equal(a, b) {
		t.Fatalf("identity.json after restore: %v", err)
	}
}

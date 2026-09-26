package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/identity"
)

func TestServeIdentityPrintsTheLakeAndFingerprints(t *testing.T) {
	dir, lake, _, _ := liveLake(t)
	var stdout bytes.Buffer
	err := Run([]string{"serve", "identity", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), "no identity yet") {
		t.Fatalf("before an identity: %v", err)
	}
	if _, err := lake.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"serve", "identity", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	k := lake.Identity.Keys[0]
	want := "lake_id " + lake.Identity.LakeID + "\nkey " + k.ID + " active " + identity.Fingerprint(k.Pub) + " created "
	if !strings.HasPrefix(stdout.String(), want) {
		t.Fatalf("identity output:\n%s\nwant prefix:\n%s", stdout.String(), want)
	}
	if strings.Contains(stdout.String(), "seed") {
		t.Fatal("identity output names the private seed")
	}
}

func TestBackupCopiesTheIdentityAndARestoreKeepsIt(t *testing.T) {
	dir, lake, _, _ := liveLake(t)
	if _, err := lake.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir() + "/backup"
	var stdout bytes.Buffer
	if err := Run([]string{"serve", "backup", "--data", dir, "--out", out}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "identity: "+identity.Path(out)) {
		t.Fatalf("backup output:\n%s", stdout.String())
	}
	st, err := os.Stat(identity.Path(out))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("identity in the backup: %v %v", st, err)
	}

	// The backup directory is a restore: it opens as the same lake, and
	// the catalog it holds already names that lake.
	restored, err := api.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if created, err := restored.EnsureIdentity(out); err != nil || created || restored.Identity.LakeID != lake.Identity.LakeID {
		t.Fatalf("restore created=%v err=%v", created, err)
	}

	// A restore that left identity.json behind refuses to start.
	if err := os.Remove(identity.Path(out)); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.EnsureIdentity(out); err == nil {
		t.Fatal("a restore without identity.json made a new identity")
	}
}

func TestFsckChecksTheIdentity(t *testing.T) {
	dir, lake, _, _ := liveLake(t)
	var stdout bytes.Buffer
	if err := Run([]string{"serve", "fsck", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "identity: none") {
		t.Fatalf("fsck without identity:\n%s", stdout.String())
	}
	if _, err := lake.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := Run([]string{"serve", "fsck", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "identity: "+lake.Identity.LakeID+", 1 keys") {
		t.Fatalf("fsck with identity:\n%s", stdout.String())
	}
	if err := os.WriteFile(identity.Path(dir), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	err := Run([]string{"serve", "fsck", "--data", dir}, Env{Stdout: &stdout, Stderr: ioDiscard()})
	if err == nil || !strings.Contains(stdout.String(), "bad identity") {
		t.Fatalf("fsck with a broken identity: %v\n%s", err, stdout.String())
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatal("broken file read as missing")
	}
}

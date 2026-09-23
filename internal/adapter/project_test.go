package adapter

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

func TestProjectAtLinksTwoCWDsWithoutCWDHash(t *testing.T) {
	root := strings.Repeat("a", 40)
	child := strings.Repeat("b", 40)
	left := fakeCheckout(t, "git@github.com:Terva-sh/Lampi.git", child, map[string][]string{
		root:  nil,
		child: {root},
	})
	right := fakeCheckout(t, "https://github.com/terva-sh/lampi", root, map[string][]string{
		root: nil,
	})
	sub := filepath.Join(left, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	a := ProjectAt(sub)
	b := ProjectAt(right)
	want := protocol.ProjectLinkID("https://github.com/terva-sh/lampi.git", root)
	if a.ProjectID == "" || a.ProjectID != want || b.ProjectID != want {
		t.Fatalf("ids left %q right %q want %q", a.ProjectID, b.ProjectID, want)
	}
	if a.CWD == b.CWD || a.CWDHash == "" || a.CWDHash == b.CWDHash {
		t.Fatalf("paths did not differ: %+v %+v", a, b)
	}
	if a.ProjectID == a.CWDHash || a.ProjectID == b.CWDHash || b.ProjectID == a.CWDHash {
		t.Fatalf("project id is a cwd hash: %s", a.ProjectID)
	}
	if a.GitCommit == b.GitCommit {
		t.Fatal("HEAD was the same, so the test did not separate it from the root")
	}
	if a.GitRoot != root || b.GitRoot != root {
		t.Fatalf("roots %s %s", a.GitRoot, b.GitRoot)
	}
	if strings.Contains(a.ProjectID, a.CWD) || strings.Contains(a.ProjectID, b.CWD) || strings.Contains(a.ProjectID, a.CWDHash) {
		t.Fatalf("project id contains a path input: %s", a.ProjectID)
	}

	otherRoot := strings.Repeat("d", 40)
	other := fakeCheckout(t, "https://github.com/terva-sh/lampi", otherRoot, map[string][]string{
		otherRoot: nil,
	})
	if got := ProjectAt(other).ProjectID; got == want || got == "" {
		t.Fatalf("different root: %s", got)
	}
	fork := fakeCheckout(t, "git@github.com:someone/lampi.git", root, map[string][]string{
		root: nil,
	})
	if got := ProjectAt(fork).ProjectID; got == want || got == "" {
		t.Fatalf("different remote: %s", got)
	}

	plain := t.TempDir()
	bare := ProjectAt(plain)
	if bare.ProjectID != "" {
		t.Fatalf("no git: %s", bare.ProjectID)
	}
	if bare.CWDHash == "" || bare.ProjectID == bare.CWDHash {
		t.Fatalf("cwd hash %q project %q", bare.CWDHash, bare.ProjectID)
	}

	shallow := fakeCheckout(t, "https://github.com/terva-sh/lampi", root, map[string][]string{
		root: nil,
	})
	if err := os.WriteFile(filepath.Join(shallow, ".git", "shallow"), []byte(root+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh := ProjectAt(shallow)
	if sh.ProjectID != "" || sh.CWDHash == "" {
		t.Fatalf("shallow id %q hash %q", sh.ProjectID, sh.CWDHash)
	}
}

func TestProjectAtRealGitClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	left := t.TempDir()
	gitRun(t, left, "init", "-b", "main")
	gitRun(t, left, "config", "user.email", "a@example.com")
	gitRun(t, left, "config", "user.name", "Lampi Test")
	gitRun(t, left, "commit", "--allow-empty", "-m", "root")
	gitRun(t, left, "remote", "add", "origin", "https://github.com/terva-sh/lampi.git")

	right := t.TempDir()
	gitRun(t, "", "clone", left, right)
	gitRun(t, right, "remote", "set-url", "origin", "git@github.com:terva-sh/lampi.git")
	gitRun(t, left, "commit", "--allow-empty", "-m", "later")
	gitRun(t, left, "gc", "--prune=now")
	gitRun(t, right, "gc", "--prune=now")

	sub := filepath.Join(left, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	a := ProjectAt(sub)
	b := ProjectAt(right)
	if a.ProjectID == "" || a.ProjectID != b.ProjectID {
		t.Fatalf("clone ids %q %q roots %q %q", a.ProjectID, b.ProjectID, a.GitRoot, b.GitRoot)
	}
	if a.CWDHash == b.CWDHash || a.ProjectID == a.CWDHash || a.ProjectID == b.CWDHash {
		t.Fatalf("cwd hash leaked: %s %s %s", a.ProjectID, a.CWDHash, b.CWDHash)
	}
	if a.GitCommit == b.GitCommit {
		t.Fatalf("HEAD matched, root link was not isolated: %s", a.GitCommit)
	}
	if a.GitRoot == "" || a.GitRoot != b.GitRoot || a.GitRoot == a.GitCommit {
		t.Fatalf("root %s head %s other root %s", a.GitRoot, a.GitCommit, b.GitRoot)
	}
	if a.ProjectID != protocol.ProjectLinkID(a.GitRemote, a.GitRoot) {
		t.Fatalf("id %s remote %s root %s", a.ProjectID, a.GitRemote, a.GitRoot)
	}
}

func fakeCheckout(t *testing.T, remote, head string, commits map[string][]string) string {
	t.Helper()
	repo := t.TempDir()
	git := filepath.Join(repo, ".git")
	if err := os.MkdirAll(filepath.Join(git, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[remote \"origin\"]\n\turl = " + remote + "\n"
	if err := os.WriteFile(filepath.Join(git, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(git, "refs", "heads", "main"), []byte(head+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for hash, parents := range commits {
		writeLooseCommit(t, git, hash, parents)
	}
	return repo
}

func writeLooseCommit(t *testing.T, gitDir, hash string, parents []string) {
	t.Helper()
	var body bytes.Buffer
	fmt.Fprintf(&body, "tree %s\n", strings.Repeat("c", 40))
	for _, parent := range parents {
		fmt.Fprintf(&body, "parent %s\n", parent)
	}
	body.WriteString("author A <a@example.com> 1 +0000\ncommitter A <a@example.com> 1 +0000\n\nmsg\n")
	payload := body.Bytes()
	var raw bytes.Buffer
	fmt.Fprintf(&raw, "commit %d\x00", len(payload))
	raw.Write(payload)
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	if _, err := zw.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(gitDir, "objects", hash[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, hash[2:]), z.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

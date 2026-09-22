package terva

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectGitOriginAndHead(t *testing.T) {
	repo := t.TempDir()
	git := filepath.Join(repo, ".git")
	if err := os.MkdirAll(filepath.Join(git, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	commit := "0123456789abcdef0123456789abcdef01234567"
	cfg := "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = git@github.com:terva-sh/lampi.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(filepath.Join(git, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(git, "refs", "heads", "main"), []byte(commit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	remote, got := projectGit(sub)
	if remote != "git@github.com:terva-sh/lampi.git" || got != commit {
		t.Fatalf("remote %q commit %q", remote, got)
	}
	if r, c := projectGit(""); r != "" || c != "" {
		t.Fatalf("empty cwd: %q %q", r, c)
	}
	if r, c := projectGit(filepath.Join(t.TempDir(), "missing")); r != "" || c != "" {
		t.Fatalf("missing cwd: %q %q", r, c)
	}
}

func TestProjectGitWorktree(t *testing.T) {
	main := t.TempDir()
	git := filepath.Join(main, ".git")
	wtGit := filepath.Join(git, "worktrees", "wt")
	if err := os.MkdirAll(wtGit, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[remote \"upstream\"]\n\turl = https://example.com/other.git\n[remote \"origin\"]\n\turl = ssh://git@github.com/terva-sh/lampi.git\n"
	if err := os.WriteFile(filepath.Join(git, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	if err := os.WriteFile(filepath.Join(git, "packed-refs"), []byte("# pack-refs\n"+commit+" refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGit, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wt, wtGit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.ToSlash(rel)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote, got := projectGit(wt)
	if remote != "ssh://git@github.com/terva-sh/lampi.git" || got != commit {
		t.Fatalf("remote %q commit %q", remote, got)
	}
}

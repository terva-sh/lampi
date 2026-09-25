package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ProjectAt reads a checkout once per cwd and reuses it while HEAD, the
// ref it names, packed-refs, and config keep their size and mtime. A
// rewrite that keeps both is not read again, which shows the cache
// answered; a commit, a branch switch, or a new origin is.
func TestProjectAtCachesUntilTheCheckoutMoves(t *testing.T) {
	root := strings.Repeat("a", 40)
	child := strings.Repeat("b", 40)
	repo := fakeCheckout(t, "https://github.com/terva-sh/lampi", child, map[string][]string{
		root:  nil,
		child: {root},
	})
	dotGit := filepath.Join(repo, ".git")
	first := ProjectAt(repo)
	if first.GitCommit != child || first.GitRoot != root || first.GitRemote == "" {
		t.Fatalf("first %+v", first)
	}
	later := func(path string) {
		t.Helper()
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		at := st.ModTime().Add(time.Second)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}

	cfgPath := filepath.Join(dotGit, "config")
	st, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	same := "[remote \"origin\"]\n\turl = https://github.com/terva-sh/lampX\n"
	if err := os.WriteFile(cfgPath, []byte(same), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(cfgPath, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := ProjectAt(repo); got != first {
		t.Fatalf("same size and mtime was read again: %+v", got)
	}
	later(cfgPath)
	if got := ProjectAt(repo); got.GitRemote != "https://github.com/terva-sh/lampX" {
		t.Fatalf("new origin: %+v", got)
	}

	next := strings.Repeat("e", 40)
	writeLooseCommit(t, dotGit, next, []string{child})
	ref := filepath.Join(dotGit, "refs", "heads", "main")
	if err := os.WriteFile(ref, []byte(next+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later(ref)
	if got := ProjectAt(repo); got.GitCommit != next || got.GitRoot != root {
		t.Fatalf("commit: %+v", got)
	}

	if err := os.WriteFile(filepath.Join(dotGit, "refs", "heads", "dev"), []byte(child+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(dotGit, "HEAD")
	if err := os.WriteFile(head, []byte("ref: refs/heads/dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later(head)
	if got := ProjectAt(repo); got.GitCommit != child {
		t.Fatalf("switch: %+v", got)
	}
}

package adapter

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

// TestProjectAtReadsDeltifiedPacks walks a history whose commits are
// stored as offset deltas and then as ref deltas, without running git.
func TestProjectAtReadsDeltifiedPacks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "config", "user.email", "a@example.com")
	gitRun(t, repo, "config", "user.name", "Lampi Test")
	gitRun(t, repo, "remote", "add", "origin", "https://github.com/terva-sh/lampi.git")
	body := strings.Repeat("a long commit message that repeats so the packer deltifies commits\n", 40)
	for i := 0; i < 30; i++ {
		gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", fmt.Sprintf("change %d\n\n%s", i, body))
	}
	out, err := exec.Command("git", "-C", repo, "rev-list", "--max-parents=0", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(out))
	common := filepath.Join(repo, ".git")
	packDir := filepath.Join(common, "objects", "pack")

	gitRun(t, repo, "repack", "-q", "-adf", "--window=250", "--depth=50")
	requireDeltaCommits(t, repo, packDir)
	if got := rootWalk(common, headOf(t, repo)); got != want {
		t.Fatalf("offset deltas: root %q want %q", got, want)
	}

	// pack-objects without --delta-base-offset names each base by id.
	scratch := t.TempDir()
	objs, err := exec.Command("git", "-C", repo, "rev-list", "--objects", "--all").Output()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "pack-objects", "-q", "--window=250", "--depth=50", filepath.Join(scratch, "pack"))
	cmd.Stdin = bytes.NewReader(objs)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pack-objects: %v\n%s", err, out)
	}
	old, _ := filepath.Glob(filepath.Join(packDir, "pack-*"))
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	fresh, _ := filepath.Glob(filepath.Join(scratch, "pack-*"))
	for _, f := range fresh {
		if err := os.Rename(f, filepath.Join(packDir, filepath.Base(f))); err != nil {
			t.Fatal(err)
		}
	}
	requireDeltaCommits(t, repo, packDir)
	p := ProjectAt(repo)
	if p.GitRoot != want || p.ProjectID == "" {
		t.Fatalf("ref deltas: %+v want root %s", p, want)
	}
}

// TestRootWalkSurvivesCorruptPacks flips bytes across a pack and its
// index. A hostile checkout can hand the reader anything, and the
// reader has to miss rather than panic or loop.
func TestRootWalkSurvivesCorruptPacks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "config", "user.email", "a@example.com")
	gitRun(t, repo, "config", "user.name", "Lampi Test")
	body := strings.Repeat("repeated text so commits deltify\n", 20)
	for i := 0; i < 8; i++ {
		gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", fmt.Sprintf("c%d\n\n%s", i, body))
	}
	gitRun(t, repo, "repack", "-q", "-adf", "--window=50")
	head := headOf(t, repo)
	common := filepath.Join(repo, ".git")
	files, _ := filepath.Glob(filepath.Join(common, "objects", "pack", "pack-*"))
	for _, f := range files {
		if !strings.HasSuffix(f, ".pack") && !strings.HasSuffix(f, ".idx") {
			continue
		}
		orig, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// git writes pack and idx as 0444; WriteFile cannot replace them otherwise.
		if err := os.Chmod(f, 0o644); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(orig); i += 1 + len(orig)/200 {
			bad := append([]byte(nil), orig...)
			bad[i] ^= 0xff
			if err := os.WriteFile(f, bad, 0o644); err != nil {
				t.Fatal(err)
			}
			rootWalk(common, head)
		}
		for _, n := range []int{0, 12, len(orig) / 2} {
			if err := os.WriteFile(f, orig[:n], 0o644); err != nil {
				t.Fatal(err)
			}
			rootWalk(common, head)
		}
		if err := os.WriteFile(f, orig, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if rootWalk(common, head) == "" {
		t.Fatal("restored pack did not resolve")
	}
}

func TestResolveRootAsksGitWhenTheReaderCannot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "config", "user.email", "a@example.com")
	gitRun(t, repo, "config", "user.name", "Lampi Test")
	gitRun(t, repo, "remote", "add", "origin", "https://github.com/terva-sh/lampi.git")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "root")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "next")
	want := ProjectAt(repo)
	if want.GitRoot == "" {
		t.Fatal("in-process reader found no root")
	}
	p := want
	p.GitRoot, p.ProjectID = "", ""
	rootCache.Lock()
	rootCache.m = map[string]string{}
	rootCache.Unlock()
	if got := ResolveRoot(p); got != want {
		t.Fatalf("git fallback %+v want %+v", got, want)
	}
	if got := ResolveRoot(want); got != want {
		t.Fatalf("a known root changed: %+v", got)
	}
}

func headOf(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// requireDeltaCommits fails unless the pack stores a commit as a
// delta, so the test exercises the path it names.
func requireDeltaCommits(t *testing.T, repo, packDir string) {
	t.Helper()
	idxs, _ := filepath.Glob(filepath.Join(packDir, "pack-*.idx"))
	if len(idxs) != 1 {
		t.Fatalf("packs: %v", idxs)
	}
	out, err := exec.Command("git", "-C", repo, "verify-pack", "-v", idxs[0]).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 7 && f[1] == "commit" {
			return
		}
	}
	t.Fatal("no commit was stored as a delta")
}

func TestOriginURLDropsCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:ghp_secret@github.com/org/repo.git": "https://github.com/org/repo.git",
		"https://ghp_token@github.com/org/repo":           "https://github.com/org/repo",
		"ssh://git:pw@host.example:22/org/repo.git":       "ssh://host.example:22/org/repo.git",
		"ssh://git@host.example/org/repo.git":             "ssh://git@host.example/org/repo.git",
		"HTTPS://tok@host.example/org/repo.git":           "HTTPS://host.example/org/repo.git",
		"https://github.com/org/a@b.git":                  "https://github.com/org/a@b.git",
		"git@github.com:org/repo.git":                     "git@github.com:org/repo.git",
		"file:///srv/repo.git":                            "file:///srv/repo.git",
		"/srv/repo.git":                                   "/srv/repo.git",
	} {
		path := filepath.Join(t.TempDir(), "config")
		if err := os.WriteFile(path, []byte("[remote \"origin\"]\n\turl = "+in+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := originURL(path); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
	root := strings.Repeat("a", 40)
	repo := fakeCheckout(t, "https://user:ghp_secret@github.com/terva-sh/lampi.git", root, map[string][]string{root: nil})
	p := ProjectAt(repo)
	if p.GitRemote != "https://github.com/terva-sh/lampi.git" {
		t.Fatalf("remote kept userinfo: %q", p.GitRemote)
	}
	if p.ProjectID != protocol.ProjectLinkID("https://github.com/terva-sh/lampi", root) {
		t.Fatalf("project id changed: %q", p.ProjectID)
	}
	if remote, _ := ProjectGit(repo); remote != p.GitRemote {
		t.Fatalf("ProjectGit %q", remote)
	}
}

func TestOutsideCheckout(t *testing.T) {
	plain := t.TempDir()
	repo := fakeCheckout(t, "https://github.com/terva-sh/lampi", strings.Repeat("a", 40), nil)
	sub := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if OutsideCheckout(plain) == underCheckout(plain) {
		t.Fatalf("plain dir %s", plain)
	}
	for _, cwd := range []string{"", "relative", filepath.Join(plain, "gone"), repo, sub} {
		if OutsideCheckout(cwd) {
			t.Errorf("%q counted as outside a checkout", cwd)
		}
	}
}

// underCheckout is true when the temp dir itself sits inside a
// repository, which makes the plain case a checkout too.
func underCheckout(dir string) bool {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

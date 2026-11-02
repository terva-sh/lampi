package profiledoctor

import (
	"slices"
	"testing"

	"terva.sh/lampi/internal/config"
)

func TestOwnerFolds(t *testing.T) {
	remote := func(s string) config.ProjectMatch { return config.ProjectMatch{GitRemote: s} }
	for _, c := range []struct {
		name  string
		rules []config.ProjectMatch
		want  []OwnerFold
	}{
		{"three under one owner", []config.ProjectMatch{remote("h/o/a"), remote("git@h:o/b.git"), remote("https://h/o/c")}, []OwnerFold{{"h/o", 3}}},
		{"two is not enough", []config.ProjectMatch{remote("h/o/a"), remote("h/o/b")}, nil},
		{"never the bare host", []config.ProjectMatch{remote("h/a"), remote("h/b"), remote("h/c")}, nil},
		{"a rule with another field is not exact", []config.ProjectMatch{remote("h/o/a"), remote("h/o/b"), {GitRemote: "h/o/c", CWDPrefix: "/w"}}, nil},
		{"not when a prefix already covers the owner", []config.ProjectMatch{remote("h/o/a"), remote("h/o/b"), remote("h/o/c"), {GitRemotePrefix: "h"}}, nil},
		{"nested groups fold to the group", []config.ProjectMatch{remote("h/o/g/a"), remote("h/o/g/b"), remote("h/o/g/c"), remote("h/o/d")}, []OwnerFold{{"h/o/g", 3}}},
	} {
		if got := OwnerFolds(c.rules); !slices.Equal(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestWideFolder(t *testing.T) {
	for dir, want := range map[string]bool{
		"/home/me/.t3/worktrees": true,
		"/var/folders/b1/x/T":    true,
		"/mnt/h":                 true,
		"/tmp":                   false,
		"/":                      false,
		"/home":                  false,
		"/home/me":               false,
		"/Users/me":              false,
		"/var/home/me":           false,
		"/var/root":              false,
		"/var/rooted":            true,
		"/mnt/c/Users":           false,
		"/mnt/c/Users/me":        false,
		"relative/path":          false,
	} {
		if got := WideFolder(dir); got != want {
			t.Errorf("WideFolder(%q) = %v, want %v", dir, got, want)
		}
	}
}

func TestParentOf(t *testing.T) {
	for _, c := range []struct{ remote, cwd, want string }{
		{"", "/home/me/.local/state/vuoro/worktrees/20261102-a", "/home/me/.local/state/vuoro/worktrees"},
		{"", "/home/me/.t3/worktrees/lampi/t3-1/", "/home/me/.t3/worktrees/lampi"},
		{"git.example/team/app", "/work/app/x", ""},
		{"", "/home/me/scratch", ""},
		{"", "/tmp/x", ""},
		{"", "/var/root/project", ""},
		{"", "/", ""},
		{"", "", ""},
		{"", `C:\work\x`, ""},
	} {
		if got := ParentOf(c.remote, c.cwd); got != c.want {
			t.Errorf("ParentOf(%q, %q) = %q, want %q", c.remote, c.cwd, got, c.want)
		}
	}
}

func TestFolderFolds(t *testing.T) {
	cwd := func(s string) config.ProjectMatch { return config.ProjectMatch{CWDPrefix: s} }
	wt := "/home/me/.t3/worktrees"
	for _, c := range []struct {
		name  string
		rules []config.ProjectMatch
		want  []FolderFold
	}{
		{"three siblings", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2"), cwd(wt + "/a/t3/")}, []FolderFold{{wt + "/a", 3}}},
		{"two is not enough", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2")}, nil},
		{"three under one child is one subfolder", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2"), cwd(wt + "/a/t3"), cwd(wt + "/b/t1"), cwd(wt + "/c/t1")},
			[]FolderFold{{wt, 5}, {wt + "/a", 3}}},
		{"never root's home", []config.ProjectMatch{cwd("/var/root/a"), cwd("/var/root/b"), cwd("/var/root/c")}, nil},
		{"never a home or a top-level folder", []config.ProjectMatch{cwd("/home/me/a"), cwd("/home/me/b"), cwd("/home/you/c"), cwd("/tmp/x"), cwd("/tmp/y"), cwd("/tmp/z")}, nil},
		{"not when a rule already covers the folder", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2"), cwd(wt + "/a/t3"), cwd(wt)}, nil},
		{"a rule with another field is not a folder rule", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2"), {CWDPrefix: wt + "/a/t3", GitRemote: "h/o/r"}}, nil},
	} {
		if got := FolderFolds(c.rules); !slices.Equal(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	rules := []config.ProjectMatch{{GitRemote: "h/o/r"}, cwd(wt + "/a/t1"), cwd("/other"), cwd(wt + "/b/t1/"), {CWDPrefix: wt + "/c", GitRemote: "h/o/s"}, cwd(wt + "/a/t2")}
	got, n := FoldFolder(rules, wt)
	want := []config.ProjectMatch{{GitRemote: "h/o/r"}, cwd(wt), cwd("/other"), {CWDPrefix: wt + "/c", GitRemote: "h/o/s"}}
	if n != 3 || !slices.Equal(got, want) {
		t.Errorf("foldFolder: %d %+v, want 3 %+v", n, got, want)
	}
}

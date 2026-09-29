package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TKT-01M3NM01S: a cwd_glob matches a directory layout, and what it
// names and everything under it.
func TestGlobHasPrefix(t *testing.T) {
	for _, c := range []struct {
		pattern, cwd string
		want         bool
	}{
		{"/home/*/notes", "/home/me/notes", true},
		{"/home/*/notes", "/home/me/notes/2026/q3", true},
		{"/home/*/notes", "/home/me/notes-old", false},
		{"/home/*/notes", "/home/notes", false},
		{"/home/*/notes", "/home/a/b/notes", false},
		{"/home/**/notes", "/home/a/b/notes", true},
		{"/home/**/notes", "/home/notes", true},
		{"/home/*/.t3/worktrees/**", "/home/me/.t3/worktrees/lampi/abc", true},
		{"/home/*/.t3/worktrees/**", "/home/me/.t3/worktrees", true},
		{"/srv/proj-*", "/srv/proj-a/sub", true},
		{"/srv/proj-*", "/srv/proj/sub", false},
		{"/srv/*-ci-*", "/srv/lampi-ci-7", true},
		{"/srv/*-ci-*", "/srv/lampi-ci", false},
		{"/srv/a*a", "/srv/a", false},
		{"/srv/a*a", "/srv/aa", true},
		{"/srv/[x]?", "/srv/[x]?", true},
		{"/srv/[x]?", "/srv/xy", false},
		{"/srv/app/", "/srv/app/x", true},
		{"/srv/app", "/srv/../srv/app", true},
		{"/Srv/app", "/srv/app", false},
		{"/srv/app", "", false},
		{"/srv/app", "relative/srv/app", false},
		{"/**", "/srv", false},
		{"relative/*", "/relative/x", false},
	} {
		if got := globHasPrefix(c.cwd, c.pattern, false); got != c.want {
			t.Errorf("globHasPrefix(%q, %q) = %v, want %v", c.cwd, c.pattern, got, c.want)
		}
	}
	if !globHasPrefix("/SRV/App/x", "/srv/*/x", true) {
		t.Error("fold does not ignore case")
	}
}

func TestCheckGlob(t *testing.T) {
	for _, ok := range []string{"/home/*/notes", "/home/**/notes/", "/a/**/b/**/c", "/srv"} {
		if err := checkGlob(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "home/*", "/", "/**", "/*/**", "/*", "/a//b", "/a/./b", "/a/../b", "/a/b**", "/a/**/**/**/**/**"} {
		if checkGlob(bad) == nil {
			t.Errorf("%q is accepted", bad)
		}
	}
}

// An allow cwd_glob compares exactly; a deny one ignores case, follows
// symlinks on the cwd, and reads a pattern it cannot use as a match.
func TestCWDGlobAllowAndDeny(t *testing.T) {
	id := ProjectID{CWD: "/home/me/Notes/today", NoRepo: true}
	if !(Projects{Allow: []ProjectMatch{{CWDGlob: "/home/*/Notes"}}}).Permitted(id) {
		t.Error("allow does not match")
	}
	if (Projects{Allow: []ProjectMatch{{CWDGlob: "/home/*/notes"}}}).Permitted(id) {
		t.Error("allow ignores case")
	}
	allowAll := []ProjectMatch{{CWDPrefix: "/home"}}
	if (Projects{Allow: allowAll, Deny: []ProjectMatch{{CWDGlob: "/HOME/*/notes"}}}).Permitted(id) {
		t.Error("deny does not ignore case")
	}
	if (Projects{Allow: allowAll, Deny: []ProjectMatch{{CWDGlob: "/**"}}}).Permitted(id) {
		t.Error("a deny pattern that cannot be used does not read as a match")
	}
	if (Projects{Allow: []ProjectMatch{{CWDGlob: "/**"}}}).Permitted(id) {
		t.Error("an allow pattern that cannot be used matches")
	}

	dir := t.TempDir()
	real := filepath.Join(dir, "real", "private")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "real"), link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	via := ProjectID{CWD: filepath.Join(link, "private"), NoRepo: true}
	deny := []ProjectMatch{{CWDGlob: filepath.Dir(resolved) + "/*vate"}}
	if (Projects{Allow: []ProjectMatch{{CWDPrefix: dir}}, Deny: deny}).Permitted(via) {
		t.Error("deny does not follow a symlink on the cwd")
	}
}

func TestCWDGlobIsValidatedWhereRulesLoad(t *testing.T) {
	if _, err := ParseProfile([]byte(`{"projects":{"deny":[{"cwd_glob":"/*/**"}]}}`)); err == nil || !strings.Contains(err.Error(), "projects.deny[0].cwd_glob") {
		t.Errorf("profile: %v", err)
	}
	if _, err := ParseProfile([]byte(`{"projects":{"allow":[{"cwd_glob":"/home/*/notes"}]}}`)); err != nil {
		t.Errorf("profile: %v", err)
	}
	dir := t.TempDir()
	getenv := func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return dir
		}
		return ""
	}
	path, err := ConfigPath(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for body, want := range map[string]string{
		`{"projects":{"deny":[{"cwd_glob":"notes"}]}}`:                                        "projects.deny[0].cwd_glob",
		`{"lakes":{"b":{"server":"http://x","projects":{"allow":[{"cwd_glob":"/a/../b"}]}}}}`: "lakes.b.projects.allow[0].cwd_glob",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFile(getenv); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", body, err)
		}
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"

	"terva.sh/lampi/internal/adapter"
)

func TestDenyIgnoresCase(t *testing.T) {
	p := Projects{
		Allow: []ProjectMatch{{CWDPrefix: "/work"}},
		Deny:  []ProjectMatch{{CWDPrefix: "/work/Secret"}},
	}
	for _, cwd := range []string{"/work/Secret", "/work/secret", "/WORK/SECRET/sub"} {
		if p.Permitted(ProjectID{CWD: cwd}) {
			t.Errorf("deny missed case variant %s", cwd)
		}
	}
	if !p.Permitted(ProjectID{CWD: "/work/secrets"}) {
		t.Fatal("case folding broke the path boundary")
	}
	// Allow stays exact, so a case variant is refused.
	if (Projects{Allow: []ProjectMatch{{CWDPrefix: "/work/App"}}}).Permitted(ProjectID{CWD: "/work/app"}) {
		t.Fatal("allow folded case")
	}
}

func TestDenyFollowsSymlinks(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "secret", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	viaLink := filepath.Join(link, "secret", "sub")
	realSecret, err := filepath.EvalSymlinks(filepath.Join(real, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	allow := []ProjectMatch{{CWDPrefix: base}, {CWDPrefix: realSecret}}

	// The session cwd goes through a symlink the rule does not name.
	p := Projects{Allow: allow, Deny: []ProjectMatch{{CWDPrefix: realSecret}}}
	if p.Permitted(ProjectID{CWD: viaLink}) {
		t.Fatal("deny missed a cwd reached through a symlink")
	}
	// The rule names the symlink and the session records the target.
	p = Projects{Allow: allow, Deny: []ProjectMatch{{CWDPrefix: filepath.Join(link, "secret")}}}
	if p.Permitted(ProjectID{CWD: filepath.Join(realSecret, "sub")}) {
		t.Fatal("deny missed a symlinked prefix")
	}
	// A cwd hash deny also covers the resolved path.
	p = Projects{Allow: allow, Deny: []ProjectMatch{{CWDHash: adapter.CWDHash(filepath.Join(realSecret, "sub"))}}}
	if p.Permitted(ProjectID{CWD: viaLink, CWDHash: adapter.CWDHash(viaLink)}) {
		t.Fatal("cwd hash deny missed the resolved cwd")
	}
	if !p.Permitted(ProjectID{CWD: base, CWDHash: adapter.CWDHash(base)}) {
		t.Fatal("unrelated cwd was denied")
	}
}

func TestDenyRemoteRefusesUnknownRemote(t *testing.T) {
	const secret = "https://github.com/org/secret.git"
	allow := []ProjectMatch{{CWDPrefix: "/work"}}
	only := Projects{Allow: allow, Deny: []ProjectMatch{{GitRemote: secret}}}
	cases := []struct {
		name string
		id   ProjectID
		want bool
	}{
		{"denied remote", ProjectID{CWD: "/work/a", GitRemote: "https://github.com/Org/secret"}, false},
		{"other remote", ProjectID{CWD: "/work/a", GitRemote: "https://github.com/org/open"}, true},
		{"unknown remote", ProjectID{CWD: "/work/a"}, false},
		{"no repository", ProjectID{CWD: "/work/a", NoRepo: true}, true},
	}
	for _, c := range cases {
		if got := only.Permitted(c.id); got != c.want {
			t.Errorf("%s: permitted %v, want %v", c.name, got, c.want)
		}
	}
	// With a cwd prefix on the rule, an unknown remote is refused only
	// under that prefix.
	scoped := Projects{Allow: allow, Deny: []ProjectMatch{{CWDPrefix: "/work/corp", GitRemote: secret}}}
	if scoped.Permitted(ProjectID{CWD: "/work/corp/x"}) {
		t.Fatal("scoped deny missed an unknown remote under its prefix")
	}
	if !scoped.Permitted(ProjectID{CWD: "/work/home/x"}) {
		t.Fatal("scoped deny refused a cwd outside its prefix")
	}
	// Allow does not read an unknown remote as a match.
	if (Projects{Allow: []ProjectMatch{{GitRemote: secret}}}).Permitted(ProjectID{CWD: "/work/a"}) {
		t.Fatal("allow matched an unknown remote")
	}
}

func TestCWDHashMatchesAdapter(t *testing.T) {
	for _, cwd := range []string{"", "/work/app", `C:\work`} {
		if cwdHash(cwd) != adapter.CWDHash(cwd) {
			t.Fatalf("%q: %s != %s", cwd, cwdHash(cwd), adapter.CWDHash(cwd))
		}
	}
}

func TestProjectsDefaultDeny(t *testing.T) {
	id := ProjectID{CWD: "/work/app", CWDHash: "abcd", GitRemote: "git@github.com:org/app.git"}
	if (Projects{}).Permitted(id) {
		t.Fatal("empty allow list permitted a project")
	}
	// An empty rule object is not an allow-all.
	open := Projects{Allow: []ProjectMatch{{}}}
	if open.Permitted(id) {
		t.Fatal("empty rule permitted a project")
	}
}

func TestProjectsMatchAndDeny(t *testing.T) {
	id := ProjectID{
		CWD:       "/work/app",
		CWDHash:   "abcdabcdabcdabcd",
		GitRemote: "git@github.com:Org/App.git",
	}
	allow := Projects{Allow: []ProjectMatch{{CWDPrefix: "/work/app"}}}
	if !allow.Permitted(id) {
		t.Fatal("cwd prefix should allow")
	}
	if allow.Permitted(ProjectID{CWD: "/work/app-other"}) {
		t.Fatal("prefix matched a sibling path")
	}
	child := ProjectID{CWD: "/work/app/pkg"}
	if !allow.Permitted(child) {
		t.Fatal("prefix should allow a child directory")
	}

	byHash := Projects{Allow: []ProjectMatch{{CWDHash: "ABCDABCDABCDABCD"}}}
	if !byHash.Permitted(id) {
		t.Fatal("cwd hash should allow, ignoring case")
	}

	byRemote := Projects{Allow: []ProjectMatch{{GitRemote: "https://github.com/org/app"}}}
	if !byRemote.Permitted(id) {
		t.Fatal("normalized git remote should allow")
	}
	if byRemote.Permitted(ProjectID{CWD: "/work/app"}) {
		t.Fatal("remote rule matched a project with no remote")
	}

	both := Projects{Allow: []ProjectMatch{{
		CWDPrefix: "/work/app",
		CWDHash:   "nope",
	}}}
	if both.Permitted(id) {
		t.Fatal("a rule with a mismatched field should not allow")
	}

	denied := Projects{
		Allow: []ProjectMatch{{CWDPrefix: "/work"}},
		Deny:  []ProjectMatch{{CWDPrefix: "/work/app"}},
	}
	if denied.Permitted(id) {
		t.Fatal("deny should win over allow")
	}
	if !denied.Permitted(ProjectID{CWD: "/work/other"}) {
		t.Fatal("a sibling outside the deny prefix should stay allowed")
	}
}

func TestNormalizeRemote(t *testing.T) {
	want := "github.com/terva-sh/lampi"
	for _, in := range []string{
		"git@github.com:terva-sh/lampi.git",
		"https://github.com/terva-sh/lampi.git",
		"https://github.com/terva-sh/lampi",
		"ssh://git@github.com/terva-sh/lampi.git",
		"https://github.com/Terva-sh/Lampi/",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Fatalf("%s: %s", in, got)
		}
	}
	if NormalizeRemote("") != "" {
		t.Fatal("empty remote should stay empty")
	}
}

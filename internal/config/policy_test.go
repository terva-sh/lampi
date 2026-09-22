package config

import "testing"

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

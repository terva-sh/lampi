package profiledoctor

import (
	"slices"
	"testing"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

// TKT-01M6F4MD2QAGQNJ9D6XSYWY7J2: a temporary folder's deny rule names
// its temp root or .cache folder; a folder temporary only by name has
// none.
func TestTemporaryDeny(t *testing.T) {
	for dir, want := range map[string]string{
		"/tmp/hs-exp": "/tmp",
		"/tmp":        "/tmp",
		"/var/folders/b1/8b_cq/T/TestX1234567/001": "/var/folders",
		"/private/var/folders/b1/x/T":              "/private/var/folders",
		"/home/me/.cache/agent-scratch/x.1":        "/home/me/.cache",
		"/home/me/.cache":                          "/home/me/.cache",
		"/home/me/src/TestFoo1234567/001":          "",
		"/home/me/src/app":                         "",
	} {
		r, ok := TemporaryDeny(dir)
		if ok != (want != "") || ok && r != cwd(want) {
			t.Errorf("TemporaryDeny(%q) = %+v, %v; want %q", dir, r, ok, want)
		}
	}
}

// ApplyFixes removes, narrows and remove-and-hides together, whatever
// order the findings come in, and skips a finding whose rule moved.
func TestApplyFixes(t *testing.T) {
	p := config.Projects{Allow: []config.ProjectMatch{cwd("/home/me"), cwd("/tmp/a"), remote("x/y"), cwd("/tmp/b"), cwd("/srv/gone")}}
	orig := slices.Clone(p.Allow)
	a := catalog.ProjectKey{Kind: catalog.KeyCWD, Key: "/tmp/a"}
	b := catalog.ProjectKey{Kind: catalog.KeyCWD, Key: "/tmp/b"}
	rule := func(i int) *config.ProjectMatch { r := p.Allow[i]; return &r }
	fs := []Finding{
		{Code: CodeTemporary, Fix: FixNarrow, Index: 1, Rule: rule(1), Hide: []catalog.ProjectKey{a}},
		{Code: CodeTemporary, Fix: FixNarrow, Index: 3, Rule: rule(3), Hide: []catalog.ProjectKey{b, a}},
		{Code: CodeHomeFolder, Fix: FixNarrow, Index: 0, Rule: rule(0), Replace: []config.ProjectMatch{cwd("/home/me/one"), cwd("/home/me/two")}},
		{Code: CodeUnmatched, Fix: FixRemove, Index: 4, Rule: rule(4)},
		{Code: CodeCovered, Fix: FixSafe, Index: 2, Rule: rule(4)}, // not the rule at 2
		{Code: CodeOwnerFold, Fix: FixWiden, Index: -1},
	}
	got, hide, n := ApplyFixes(p, fs)
	if want := []config.ProjectMatch{cwd("/home/me/one"), cwd("/home/me/two"), remote("x/y")}; !slices.Equal(got.Allow, want) {
		t.Errorf("allow %+v, want %+v", got.Allow, want)
	}
	if n != 4 || len(hide) != 2 || !slices.Contains(hide, a) || !slices.Contains(hide, b) {
		t.Errorf("applied %d, hide %v", n, hide)
	}
	if !slices.Equal(p.Allow, orig) {
		t.Error("ApplyFixes changed its argument's rules")
	}
}

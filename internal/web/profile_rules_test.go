package web

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/profiledoctor"
	"terva.sh/lampi/internal/protocol"
)

// TKT-01M6ETZV536S: a rule posts as conditions, a kind and a value each;
// the field names the editor used to post still read.
func TestReadRule(t *testing.T) {
	for _, tc := range []struct {
		name string
		form url.Values
		want config.ProjectMatch
		err  string
	}{
		{"one condition", url.Values{"r.c.0.kind": {"git_remote"}, "r.c.0.value": {" git.example/a/b "}}, config.ProjectMatch{GitRemote: "git.example/a/b"}, ""},
		{"two conditions", url.Values{"r.c.0.kind": {"cwd_prefix"}, "r.c.0.value": {"/home/me"}, "r.c.1.kind": {"cwd_hash"}, "r.c.1.value": {"0123456789abcdef"}},
			config.ProjectMatch{CWDPrefix: "/home/me", CWDHash: "0123456789abcdef"}, ""},
		{"empty condition", url.Values{"r.c.0.kind": {"cwd_prefix"}, "r.c.0.value": {" "}, "r.c.1.kind": {"cwd_glob"}, "r.c.1.value": {"/x/*"}}, config.ProjectMatch{CWDGlob: "/x/*"}, ""},
		{"old names", url.Values{"r.cwd_prefix": {"/work"}, "r.git_remote_prefix": {"git.example/a"}}, config.ProjectMatch{CWDPrefix: "/work", GitRemotePrefix: "git.example/a"}, ""},
		{"the same value twice", url.Values{"r.cwd_prefix": {"/work"}, "r.c.0.kind": {"cwd_prefix"}, "r.c.0.value": {"/work"}}, config.ProjectMatch{CWDPrefix: "/work"}, ""},
		{"a field twice", url.Values{"r.c.0.kind": {"cwd_prefix"}, "r.c.0.value": {"/a"}, "r.c.1.kind": {"cwd_prefix"}, "r.c.1.value": {"/b"}}, config.ProjectMatch{}, "sets cwd_prefix twice"},
		{"an unknown kind", url.Values{"r.c.0.kind": {"cwd"}, "r.c.0.value": {"/a"}}, config.ProjectMatch{}, "choose what"},
	} {
		got, _, err := readRule(tc.form, "r.")
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: error %v, want %q", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: %+v %v, want %+v", tc.name, got, err, tc.want)
		}
	}
}

// TKT-01M6ETZV536S: runs of rules under one folder or owner get a
// header, and each allow rule carries its use and its findings.
func TestRuleRows(t *testing.T) {
	rules := []config.ProjectMatch{
		{GitRemote: "git.example/team/a"}, {GitRemote: "git.example/team/b"},
		{CWDPrefix: "/wt/x"},
		{CWDPrefix: "/home/me/wt/1"}, {CWDPrefix: "/home/me/wt/2"}, {CWDPrefix: "/home/me/wt/3"},
		{},
	}
	h := &healthView{Report: profiledoctor.Report{Uses: []profiledoctor.RuleUse{{Projects: 2, Sessions: 5}, {}, {}, {}, {}, {}}}}
	h.Items = []healthItem{
		{Finding: profiledoctor.Finding{Code: profiledoctor.CodeCovered, Index: 1, Rule: &rules[1]}},
		{Finding: profiledoctor.Finding{Code: profiledoctor.CodeUnmatched, Index: 2, Rule: &rules[2]}},
	}
	rows := ruleRows("allow", rules, nil, h)
	if rows[0].Group != "git.example/team" || rows[0].GroupCount != 2 || rows[1].Group != "" {
		t.Errorf("owner header: %+v %+v", rows[0], rows[1])
	}
	if rows[2].Group != "" || rows[3].Group != "/home/me/wt" || rows[3].GroupCount != 3 {
		t.Errorf("folder header: %+v %+v", rows[2], rows[3])
	}
	if !rows[6].Blank || len(rows[6].Conds) != 1 || rows[6].Group != "" {
		t.Errorf("blank row %+v", rows[6])
	}
	if rows[0].Use == nil || rows[0].Use.Sessions != 5 || len(rows[1].Findings) != 1 || len(rows[2].Findings) != 0 {
		t.Errorf("use and findings: %+v %+v %+v", rows[0].Use, rows[1].Findings, rows[2].Findings)
	}
	if deny := ruleRows("deny", rules, nil, h); deny[0].Use != nil || deny[1].Findings != nil {
		t.Error("deny rules carry the allow rules' use")
	}

	// The doctor reads the rules with empty rows dropped; a row after an
	// empty one still gets its own use and findings.
	gap := []config.ProjectMatch{{GitRemote: "git.example/team/a"}, {}, {GitRemote: "git.example/team/b"}}
	h = &healthView{Report: profiledoctor.Report{Uses: []profiledoctor.RuleUse{{Projects: 1}, {Projects: 7}}}}
	h.Items = []healthItem{{Finding: profiledoctor.Finding{Code: profiledoctor.CodeCovered, Index: 1, Rule: &gap[2]}}}
	rows = ruleRows("allow", gap, nil, h)
	if rows[1].Use != nil || rows[2].Use == nil || rows[2].Use.Projects != 7 || len(rows[2].Findings) != 1 || len(rows[1].Findings) != 0 {
		t.Errorf("after an empty row: %+v %+v", rows[1], rows[2])
	}
}

// TKT-01M6ETZV536S: a rule the form cannot read comes back with every
// condition as it was sent.
func TestRuleSentAsIs(t *testing.T) {
	form := url.Values{"r.c.0.kind": {"cwd_prefix"}, "r.c.0.value": {"/a"}, "r.c.1.kind": {"cwd_prefix"}, "r.c.1.value": {"/b"},
		"r.c.2.kind": {"cwd_glob"}, "r.c.2.value": {"/c/*"}}
	_, sent, err := readRule(form, "r.")
	want := []ruleCond{{"cwd_prefix", "/a"}, {"cwd_prefix", "/b"}, {"cwd_glob", "/c/*"}}
	if err == nil || !slices.Equal(sent, want) {
		t.Fatalf("sent %+v %v, want %+v", sent, err, want)
	}
	rows := ruleRows("allow", []config.ProjectMatch{{CWDPrefix: "/a"}}, map[int][]ruleCond{0: sent}, nil)
	if !slices.Equal(rows[0].Conds, want) {
		t.Errorf("shown %+v", rows[0].Conds)
	}
}

// TKT-01M6ETZV536S: the editor lists one line per rule with what it
// admits, previews conditions and Remove, and keeps what was typed when
// a rule cannot be read.
func TestEditorRuleLines(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
		{GitRemote: "git.example/team/app", CWD: "/src/app", CWDs: 1, Sessions: 4, Allowed: true},
	}}
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[0].ID, inv, now); err != nil {
		t.Fatal(err)
	}
	doc := `{"projects":{"allow":[{"git_remote":"git.example/team/app"},{"cwd_prefix":"/work/a"},{"cwd_prefix":"/work/b"}]}}`
	p, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, []byte(doc), "test", "", now)
	if err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)

	page := get(h, "/profiles/default/edit", cookie).Body.String()
	for _, want := range []string{
		`name="allow.0.c.0.value" value="git.example/team/app"`, `<option value="git_remote" selected>Repository</option>`,
		`<span class="mono">/work/</span> <span class="sub">2 rules</span>`, `name="remove" value="allow.1"`, "data-rule-filter",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("editor lacks %q", want)
		}
	}
	// Enter in a value box submits with the form's first submit button,
	// which must preview, not Remove a rule or apply a fix.
	editor := page[strings.Index(page, `action="/profiles/default/preview"`):]
	if first := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`).FindString(editor); first == "" || strings.Contains(first, "name=") {
		t.Errorf("the editor's first submit button is %s", first)
	}
	for _, want := range []string{"Admits 1 project · 4 sessions", "Admits nothing on a listed device"} {
		if !strings.Contains(squash(page), want) {
			t.Errorf("editor lacks %q", want)
		}
	}

	base := strconvI(p.Revision)
	form := url.Values{"csrf": {csrf}, "base": {base}, "allow_rows": {"3"}, "deny_rows": {"0"},
		"allow.0.c.0.kind": {"git_remote"}, "allow.0.c.0.value": {"git.example/team/app"},
		"allow.1.c.0.kind": {"cwd_prefix"}, "allow.1.c.0.value": {"/work/a"}, "allow.1.c.1.kind": {"cwd_hash"}, "allow.1.c.1.value": {"0123456789abcdef"},
		"allow.2.c.0.kind": {"cwd_prefix"}, "allow.2.c.0.value": {"/work/b"},
		"remove": {"allow.2"}}
	w := postForm(h, "/profiles/default/preview", form, cookie)
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body)
	}
	got := hiddenValue(t, w.Body.String(), "document")
	if want := `{"projects":{"allow":[{"git_remote":"git.example/team/app"},{"cwd_prefix":"/work/a","cwd_hash":"0123456789abcdef"}]}}`; got != want {
		t.Errorf("document %s, want %s", got, want)
	}

	form.Del("remove")
	form.Set("allow.2.c.1.kind", "cwd_prefix")
	form.Set("allow.2.c.1.value", "/work/c")
	w = postForm(h, "/profiles/default/preview", form, cookie)
	body := w.Body.String()
	if w.Code != 400 || !strings.Contains(body, "allow rule 3: one rule sets cwd_prefix twice") || !strings.Contains(body, `value="0123456789abcdef"`) ||
		!strings.Contains(body, `name="allow.2.c.1.value" value="/work/c"`) {
		t.Errorf("a rule setting one field twice: %d", w.Code)
	}
}

// TKT-01M6ETZV7C0P: Move swaps a rule with its neighbour, and Sort puts
// folders by path and then remotes by remote, keeping empty rows last.
func TestReorder(t *testing.T) {
	r := func(s string) config.ProjectMatch { return config.ProjectMatch{GitRemote: s} }
	f := func(s string) config.ProjectMatch { return config.ProjectMatch{CWDPrefix: s} }
	rows := func() []config.ProjectMatch {
		return []config.ProjectMatch{r("h/o/b"), f("/wt/b"), r("h/a/x"), f("/wt/a"), {}}
	}
	for _, tc := range []struct {
		name string
		form url.Values
		want []config.ProjectMatch
	}{
		{"up", url.Values{"move": {"allow.2.up"}}, []config.ProjectMatch{r("h/o/b"), r("h/a/x"), f("/wt/b"), f("/wt/a"), {}}},
		{"down", url.Values{"move": {"allow.0.down"}}, []config.ProjectMatch{f("/wt/b"), r("h/o/b"), r("h/a/x"), f("/wt/a"), {}}},
		{"up from the top", url.Values{"move": {"allow.0.up"}}, rows()},
		{"another list", url.Values{"move": {"deny.2.up"}}, rows()},
		{"not a row", url.Values{"move": {"allow.x.up"}}, rows()},
		{"sort", url.Values{"sort": {"allow"}}, []config.ProjectMatch{f("/wt/a"), f("/wt/b"), r("h/a/x"), r("h/o/b"), {}}},
		{"sort another list", url.Values{"sort": {"deny"}}, rows()},
	} {
		if got := reorder(tc.form, "allow", rows()); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TKT-01M6ETZV7C0P: the editor's Move and Sort buttons preview the rules
// in their new order.
func TestEditorMovesAndSorts(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	doc := `{"projects":{"allow":[{"git_remote":"git.example/team/b"},{"cwd_prefix":"/work/a"},{"git_remote":"git.example/team/a"}]}}`
	p, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, []byte(doc), "test", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	page := get(h, "/profiles/default/edit", cookie).Body.String()
	for _, want := range []string{`name="move" value="allow.1.up"`, `name="move" value="allow.1.down"`, `name="sort" value="allow"`, `class="rule-handle"`} {
		if !strings.Contains(page, want) {
			t.Errorf("editor lacks %q", want)
		}
	}
	form := url.Values{"csrf": {csrf}, "base": {strconvI(p.Revision)}, "allow_rows": {"3"}, "deny_rows": {"0"},
		"allow.0.git_remote": {"git.example/team/b"}, "allow.1.cwd_prefix": {"/work/a"}, "allow.2.git_remote": {"git.example/team/a"}}
	preview := func(extra url.Values) string {
		t.Helper()
		v := url.Values{}
		for k, vs := range form {
			v[k] = vs
		}
		for k, vs := range extra {
			v[k] = vs
		}
		w := postForm(h, "/profiles/default/preview", v, cookie)
		if w.Code != 200 {
			t.Fatalf("preview: %d %s", w.Code, w.Body)
		}
		return hiddenValue(t, w.Body.String(), "document")
	}
	if got, want := preview(url.Values{"move": {"allow.2.up"}}), `{"projects":{"allow":[{"git_remote":"git.example/team/b"},{"git_remote":"git.example/team/a"},{"cwd_prefix":"/work/a"}]}}`; got != want {
		t.Errorf("move: %s, want %s", got, want)
	}
	if got, want := preview(url.Values{"sort": {"allow"}}), `{"projects":{"allow":[{"cwd_prefix":"/work/a"},{"git_remote":"git.example/team/a"},{"git_remote":"git.example/team/b"}]}}`; got != want {
		t.Errorf("sort: %s, want %s", got, want)
	}
}

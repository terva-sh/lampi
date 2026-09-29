package web

import (
	"encoding/json"
	"html"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/config"
)

// ruleForm is the editor's form holding rules as its allow list.
func ruleForm(csrf string, base int64, rules []config.ProjectMatch) url.Values {
	f := url.Values{"csrf": {csrf}, "base": {strconv.FormatInt(base, 10)}, "allow_rows": {strconv.Itoa(len(rules))}, "deny_rows": {"0"}}
	for i, r := range rules {
		p := "allow." + strconv.Itoa(i) + "."
		f.Set(p+"git_remote", r.GitRemote)
		f.Set(p+"git_remote_prefix", r.GitRemotePrefix)
		f.Set(p+"cwd_prefix", r.CWDPrefix)
		f.Set(p+"cwd_hash", r.CWDHash)
		f.Set(p+"cwd_glob", r.CWDGlob)
	}
	return f
}

// TKT-01M3NM01N: the editor offers to remove the allow rules another
// rule covers, and to fold an owner's exact remotes into one prefix.
// Each offer changes the form and previews it; neither saves.
func TestEditorOffersFewerRules(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	rules := []config.ProjectMatch{
		{GitRemote: "git.example/team/app"},
		{GitRemote: "git.example/team/lib"},
		{CWDPrefix: "/work"},
		{GitRemote: "git.example/team/cli"},
		{CWDPrefix: "/work/app"},
		{GitRemote: "github.com/solo/one"},
		{GitRemote: "github.com/solo/one"},
	}
	doc, _ := json.Marshal(config.Profile{Projects: config.Projects{Allow: rules}})
	p, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, doc, "test", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)

	page := html.UnescapeString(get(h, "/profiles/default/edit", cookie).Body.String())
	for _, want := range []string{
		"2 allow rules add nothing",
		"cwd_prefix /work/app covered by cwd_prefix /work",
		"git_remote github.com/solo/one covered by git_remote github.com/solo/one",
		"3 git_remote rules name repositories under git.example/team",
	} {
		if !strings.Contains(squash(page), want) {
			t.Errorf("editor lacks %q", want)
		}
	}
	// Enter in a field submits the form's first button, which must be
	// the plain preview, not an offer.
	if i, j := strings.Index(page, "Preview changes"), strings.Index(page, `name="tidy"`); i < 0 || j < i {
		t.Error("an offer comes before Preview changes in the form")
	}

	previewed := func(v url.Values) ([]config.ProjectMatch, string) {
		t.Helper()
		w := postForm(h, "/profiles/default/preview", v, cookie)
		if w.Code != 200 {
			t.Fatalf("preview: %d %s", w.Code, w.Body)
		}
		body := w.Body.String()
		var got config.Profile
		if err := json.Unmarshal([]byte(hiddenValue(t, body, "document")), &got); err != nil {
			t.Fatal(err)
		}
		return got.Projects.Allow, hiddenValue(t, body, "note")
	}

	v := ruleForm(csrf, p.Revision, rules)
	v.Set("tidy", "covered")
	got, note := previewed(v)
	want := []config.ProjectMatch{rules[0], rules[1], rules[2], rules[3], rules[5]}
	if !slices.Equal(got, want) || note != "Remove 2 covered allow rules" {
		t.Errorf("remove covered: %+v, note %q", got, note)
	}

	v = ruleForm(csrf, p.Revision, rules)
	v.Set("fold", "git.example/team")
	v.Set("note", "mine")
	got, note = previewed(v)
	want = []config.ProjectMatch{{GitRemotePrefix: "git.example/team"}, rules[2], rules[4], rules[5], rules[6]}
	if !slices.Equal(got, want) || note != "mine" {
		t.Errorf("fold: %+v, note %q", got, note)
	}

	// An owner the editor does not offer changes nothing: one it holds
	// no remote under, and one with fewer remotes than an offer needs.
	for _, owner := range []string{"git.example", "github.com/solo"} {
		v = ruleForm(csrf, p.Revision, rules)
		v.Set("fold", owner)
		if w := postForm(h, "/profiles/default/preview", v, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "This changes nothing") {
			t.Errorf("fold of %s, which is not offered: %d", owner, w.Code)
		}
	}

	if cur, _ := lake.Catalog.ProfileByName(ctx, config.DefaultProfile); cur.Revision != p.Revision {
		t.Error("an offer saved the profile")
	}
}

func TestOwnerFolds(t *testing.T) {
	remote := func(s string) config.ProjectMatch { return config.ProjectMatch{GitRemote: s} }
	for _, c := range []struct {
		name  string
		rules []config.ProjectMatch
		want  []ownerFold
	}{
		{"three under one owner", []config.ProjectMatch{remote("h/o/a"), remote("git@h:o/b.git"), remote("https://h/o/c")}, []ownerFold{{"h/o", 3}}},
		{"two is not enough", []config.ProjectMatch{remote("h/o/a"), remote("h/o/b")}, nil},
		{"never the bare host", []config.ProjectMatch{remote("h/a"), remote("h/b"), remote("h/c")}, nil},
		{"a rule with another field is not exact", []config.ProjectMatch{remote("h/o/a"), remote("h/o/b"), {GitRemote: "h/o/c", CWDPrefix: "/w"}}, nil},
		{"not when a prefix already covers the owner", []config.ProjectMatch{remote("h/o/a"), remote("h/o/b"), remote("h/o/c"), {GitRemotePrefix: "h"}}, nil},
		{"nested groups fold to the group", []config.ProjectMatch{remote("h/o/g/a"), remote("h/o/g/b"), remote("h/o/g/c"), remote("h/o/d")}, []ownerFold{{"h/o/g", 3}}},
	} {
		if got := ownerFolds(c.rules); !slices.Equal(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestWithRules(t *testing.T) {
	remote := func(s string) config.ProjectMatch { return config.ProjectMatch{GitRemote: s} }
	prefix := func(s string) config.ProjectMatch { return config.ProjectMatch{GitRemotePrefix: s} }
	for _, c := range []struct {
		name              string
		allow, add        []config.ProjectMatch
		wantOut, wantGone []config.ProjectMatch
	}{
		{"a prefix drops what it covers", []config.ProjectMatch{remote("h/o/a"), remote("h/x/b")}, []config.ProjectMatch{prefix("h/o")},
			[]config.ProjectMatch{remote("h/x/b"), prefix("h/o")}, []config.ProjectMatch{remote("h/o/a")}},
		{"a rule already there stays where it is", []config.ProjectMatch{prefix("h/o"), remote("h/x/b")}, []config.ProjectMatch{prefix("h/o")},
			[]config.ProjectMatch{prefix("h/o"), remote("h/x/b")}, nil},
		{"a rule a kept rule covers is not added", []config.ProjectMatch{prefix("h")}, []config.ProjectMatch{remote("h/o/a")},
			[]config.ProjectMatch{prefix("h")}, nil},
		{"nested new prefixes keep the wider", nil, []config.ProjectMatch{prefix("h/o/g"), prefix("h/o")},
			[]config.ProjectMatch{prefix("h/o")}, nil},
	} {
		out, gone := withRules(c.allow, c.add)
		if !slices.Equal(out, c.wantOut) || !slices.Equal(gone, c.wantGone) {
			t.Errorf("%s: %+v less %+v, want %+v less %+v", c.name, out, gone, c.wantOut, c.wantGone)
		}
	}
}

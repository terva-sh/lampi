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
		"/mnt/c/Users":           false,
		"/mnt/c/Users/me":        false,
		"relative/path":          false,
	} {
		if got := wideFolder(dir); got != want {
			t.Errorf("wideFolder(%q) = %v, want %v", dir, got, want)
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
		{"", "/", ""},
		{"", "", ""},
		{"", `C:\work\x`, ""},
	} {
		if got := parentOf(c.remote, c.cwd); got != c.want {
			t.Errorf("parentOf(%q, %q) = %q, want %q", c.remote, c.cwd, got, c.want)
		}
	}
}

func TestFolderFolds(t *testing.T) {
	cwd := func(s string) config.ProjectMatch { return config.ProjectMatch{CWDPrefix: s} }
	wt := "/home/me/.t3/worktrees"
	for _, c := range []struct {
		name  string
		rules []config.ProjectMatch
		want  []folderFold
	}{
		{"three siblings", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2"), cwd(wt + "/a/t3/")}, []folderFold{{wt + "/a", 3}}},
		{"two is not enough", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2")}, nil},
		{"three under one child is one subfolder", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2"), cwd(wt + "/a/t3"), cwd(wt + "/b/t1"), cwd(wt + "/c/t1")},
			[]folderFold{{wt, 5}, {wt + "/a", 3}}},
		{"never a home or a top-level folder", []config.ProjectMatch{cwd("/home/me/a"), cwd("/home/me/b"), cwd("/home/you/c"), cwd("/tmp/x"), cwd("/tmp/y"), cwd("/tmp/z")}, nil},
		{"not when a rule already covers the folder", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2"), cwd(wt + "/a/t3"), cwd(wt)}, nil},
		{"a rule with another field is not a folder rule", []config.ProjectMatch{cwd(wt + "/a/t1"), cwd(wt + "/a/t2"), {CWDPrefix: wt + "/a/t3", GitRemote: "h/o/r"}}, nil},
	} {
		if got := folderFolds(c.rules); !slices.Equal(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	rules := []config.ProjectMatch{{GitRemote: "h/o/r"}, cwd(wt + "/a/t1"), cwd("/other"), cwd(wt + "/b/t1/"), {CWDPrefix: wt + "/c", GitRemote: "h/o/s"}, cwd(wt + "/a/t2")}
	got, n := foldFolder(rules, wt)
	want := []config.ProjectMatch{{GitRemote: "h/o/r"}, cwd(wt), cwd("/other"), {CWDPrefix: wt + "/c", GitRemote: "h/o/s"}}
	if n != 3 || !slices.Equal(got, want) {
		t.Errorf("foldFolder: %d %+v, want 3 %+v", n, got, want)
	}
}

// The editor offers to fold sibling folder rules into their folder, and
// the offer goes through the preview like the owner fold.
func TestEditorFoldsFolders(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	wt := "/home/me/.t3/worktrees/lampi"
	rules := []config.ProjectMatch{
		{CWDPrefix: wt + "/t3-1"},
		{GitRemote: "git.example/team/app"},
		{CWDPrefix: wt + "/t3-2"},
		{CWDPrefix: wt + "/t3-3"},
		{CWDPrefix: "/home/me/a"},
		{CWDPrefix: "/home/me/b"},
		{CWDPrefix: "/home/me/c"},
	}
	doc, _ := json.Marshal(config.Profile{Projects: config.Projects{Allow: rules}})
	p, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, doc, "test", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)

	page := squash(html.UnescapeString(get(h, "/profiles/default/edit", cookie).Body.String()))
	if !strings.Contains(page, "3 cwd_prefix rules name folders under "+wt+" . One cwd_prefix would cover them") {
		t.Errorf("editor offers no fold of %s", wt)
	}
	if strings.Contains(page, `value="/home/me"`) {
		t.Error("the editor offers to fold into a home directory")
	}

	v := ruleForm(csrf, p.Revision, rules)
	v.Set("folder", wt)
	w := postForm(h, "/profiles/default/preview", v, cookie)
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body)
	}
	var got config.Profile
	if err := json.Unmarshal([]byte(hiddenValue(t, w.Body.String(), "document")), &got); err != nil {
		t.Fatal(err)
	}
	want := []config.ProjectMatch{{CWDPrefix: wt}, rules[1], rules[4], rules[5], rules[6]}
	if !slices.Equal(got.Projects.Allow, want) {
		t.Errorf("fold: %+v, want %+v", got.Projects.Allow, want)
	}
	if note := hiddenValue(t, w.Body.String(), "note"); note != "Replace 3 cwd_prefix rules under "+wt+" with one cwd_prefix" {
		t.Errorf("note %q", note)
	}

	v = ruleForm(csrf, p.Revision, rules)
	v.Set("folder", "/home/me")
	if w := postForm(h, "/profiles/default/preview", v, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "This changes nothing") {
		t.Errorf("a fold the editor does not offer: %d", w.Code)
	}
}

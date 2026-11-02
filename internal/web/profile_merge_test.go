package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

func TestCommonPrefix(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{[]string{"/home/me/wt/lampi/a", "/home/me/wt/ledger/b"}, "home/me/wt"},
		{[]string{"/a/b", "/a/b/c"}, "a/b"},
		{[]string{"/a", "/b"}, ""},
		{[]string{"git.example/o/a", "git.example/o/b"}, "git.example/o"},
		{[]string{"git.example/o/a", "git.example/p"}, "git.example"},
	} {
		if got := commonPrefix(tc.in); got != tc.want {
			t.Errorf("commonPrefix(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TKT-01M6ETH6H726: Merge proposes the ticked rules' common folder, shows
// the rules it replaces and the projects it newly admits, hidden ones
// first, takes an edited prefix, refuses a wide one, and applies to the
// form.
func TestEditorMergesTickedRules(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
		{CWD: "/home/me/wt/lampi/a", CWDs: 1, Sessions: 2, Allowed: true},
		{CWD: "/home/me/wt/lampi/new", CWDs: 1, Sessions: 3, Reason: config.RefusedNoMatch},
		{CWD: "/home/me/wt/zot/old", CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch},
		{CWD: "/srv/other", CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch},
	}}
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[0].ID, inv, now); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.HideProjects(ctx, []catalog.ProjectKey{{Kind: catalog.KeyCWD, Key: "/home/me/wt/zot/old"}}, "test", "", now); err != nil {
		t.Fatal(err)
	}
	doc := `{"projects":{"allow":[{"git_remote":"git.example/team/app"},{"cwd_prefix":"/home/me/wt/lampi/a"},{"cwd_prefix":"/home/me/wt/ledger/b"},{"cwd_prefix":"/home/me/wt/lampi/c"}]}}`
	p, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, []byte(doc), "test", "", now)
	if err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	if page := get(h, "/profiles/default/edit", cookie).Body.String(); !strings.Contains(page, `name="pick" value="allow.1"`) || !strings.Contains(page, `name="merge" value="allow"`) {
		t.Fatal("the editor offers no merge")
	}
	form := func(extra url.Values) url.Values {
		v := url.Values{"csrf": {csrf}, "base": {strconvI(p.Revision)}, "allow_rows": {"4"}, "deny_rows": {"0"},
			"allow.0.git_remote": {"git.example/team/app"}, "allow.1.cwd_prefix": {"/home/me/wt/lampi/a"},
			"allow.2.cwd_prefix": {"/home/me/wt/ledger/b"}, "allow.3.cwd_prefix": {"/home/me/wt/lampi/c"}}
		for k, vs := range extra {
			v[k] = vs
		}
		return v
	}

	w := postForm(h, "/profiles/default/preview", form(url.Values{"merge": {"allow"}, "pick": {"allow.1", "allow.2"}}), cookie)
	page := w.Body.String()
	text := squash(page)
	if w.Code != 200 {
		t.Fatalf("plan: %d", w.Code)
	}
	for _, want := range []string{
		`name="merge_prefix" value="/home/me/wt"`, `name="pick" value="allow.1" aria-label="Tick allow rule 1 to merge" checked`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("merge panel lacks %q", want)
		}
	}
	for _, want := range []string{
		"cwd_prefix /home/me/wt takes the profile from 4 to 2 allow rules",
		"It replaces 3 rules, ticked or not",
		"It admits 2 projects that no rule admits now, 1 of them hidden from review",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("merge panel lacks %q", want)
		}
	}
	if i, j := strings.Index(text, "/home/me/wt/zot/old"), strings.Index(text, "/home/me/wt/lampi/new"); i < 0 || j < i {
		t.Error("the hidden project is not listed first")
	}
	if strings.Contains(text, "/srv/other") || strings.Contains(page, `id="preview-heading"`) {
		t.Error("the panel lists a project the merge does not admit, or a preview")
	}

	// An edited prefix is checked, and a wide one refused.
	w = postForm(h, "/profiles/default/preview", form(url.Values{"merge": {"allow"}, "pick": {"allow.1", "allow.3"}, "merge_prefix": {"/home/me/wt/lampi"}}), cookie)
	if text := squash(w.Body.String()); w.Code != 200 || !strings.Contains(text, "It replaces 2 rules") || !strings.Contains(text, "It admits 1 project") {
		t.Errorf("edited prefix: %d", w.Code)
	}
	w = postForm(h, "/profiles/default/preview", form(url.Values{"merge": {"allow"}, "pick": {"allow.1", "allow.2"}, "merge_prefix": {"/home/me"}}), cookie)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Merge does not offer /home/me") {
		t.Errorf("wide prefix: %d", w.Code)
	}
	for name, pick := range map[string][]string{"one rule": {"allow.1"}, "two kinds": {"allow.0", "allow.1"}} {
		if w := postForm(h, "/profiles/default/preview", form(url.Values{"merge": {"allow"}, "pick": pick}), cookie); w.Code != 409 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}

	// Apply puts the rule where the first rule it replaces stood.
	w = postForm(h, "/profiles/default/preview", form(url.Values{"merge_apply": {"allow"}, "pick": {"allow.1", "allow.2"}, "merge_prefix": {"/home/me/wt"}}), cookie)
	if w.Code != 200 {
		t.Fatalf("apply: %d %s", w.Code, w.Body)
	}
	if got, want := hiddenValue(t, w.Body.String(), "document"), `{"projects":{"allow":[{"git_remote":"git.example/team/app"},{"cwd_prefix":"/home/me/wt"}]}}`; got != want {
		t.Errorf("applied %s, want %s", got, want)
	}
	if !strings.Contains(w.Body.String(), "Merge 3 allow rules into cwd_prefix /home/me/wt") {
		t.Error("the note does not say what the merge did")
	}
}

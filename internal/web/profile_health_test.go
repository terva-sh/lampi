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

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// healthLake is a lake whose default profile has a home directory rule,
// a covered rule, a temporary rule the device still holds, and a rule
// nothing matches, with one device, deck, that takes it.
func healthLake(t *testing.T) (*lakeFixture, []config.ProjectMatch) {
	t.Helper()
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "deck"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	rules := []config.ProjectMatch{
		{CWDPrefix: "/home/steamos"},
		{CWDPrefix: "/srv/work"},
		{CWDPrefix: "/srv/work/app"},
		{CWDPrefix: "/tmp/probe-1"},
		{GitRemote: "git.example/team/new"},
	}
	doc, _ := json.Marshal(config.Profile{Projects: config.Projects{Allow: rules}})
	if _, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, doc, "test", "", now); err != nil {
		t.Fatal(err)
	}
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
		{CWD: "/home/steamos/games", CWDs: 1, Sessions: 3, Allowed: true},
		{CWD: "/srv/work/app", CWDs: 1, Sessions: 1, Allowed: true},
		{CWD: "/tmp/probe-1", CWDs: 1, Sessions: 1, Allowed: true},
	}}
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[0].ID, inv, now); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	return &lakeFixture{lake: lake, h: h, cookie: cookie, csrf: csrfOf(t, h, cookie)}, rules
}

func TestProfileHealth(t *testing.T) {
	f, rules := healthLake(t)
	ctx := t.Context()
	stored, _ := f.lake.Catalog.ProfileByName(ctx, config.DefaultProfile)

	page := squash(html.UnescapeString(get(f.h, "/profiles/default", f.cookie).Body.String()))
	for _, want := range []string{
		"Health",
		"5 of 500 allow rules · 0 waiting in review",
		"problem cwd_prefix /home/steamos home_folder",
		"Narrowing keeps cwd_prefix /home/steamos/games",
		"1 safe fix changes no project's verdict",
		"Fix in the editor",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("profile page lacks %q", want)
		}
	}
	if strings.Contains(page, `name="narrow"`) {
		t.Error("the profile page offers a fix button outside the editor")
	}

	edit := squash(html.UnescapeString(get(f.h, "/profiles/default/edit", f.cookie).Body.String()))
	for _, want := range []string{"Apply 1 safe fix and preview", "Narrow to 1 exact rule and preview", "Remove and hide 1 project and preview", "Remove 1 unmatched rule and preview"} {
		if !strings.Contains(edit, want) {
			t.Errorf("editor lacks %q", want)
		}
	}

	preview := func(v url.Values) (string, config.Profile) {
		t.Helper()
		w := postForm(f.h, "/profiles/default/preview", v, f.cookie)
		if w.Code != 200 {
			t.Fatalf("preview: %d %s", w.Code, w.Body)
		}
		var p config.Profile
		if err := json.Unmarshal([]byte(hiddenValue(t, w.Body.String(), "document")), &p); err != nil {
			t.Fatal(err)
		}
		return w.Body.String(), p
	}

	v := ruleForm(f.csrf, stored.Revision, rules)
	v.Set("narrow", "0")
	_, p := preview(v)
	if want := []config.ProjectMatch{{CWDPrefix: "/home/steamos/games"}, rules[1], rules[2], rules[3], rules[4]}; !slices.Equal(p.Projects.Allow, want) {
		t.Errorf("narrowed %+v, want %+v", p.Projects.Allow, want)
	}

	v = ruleForm(f.csrf, stored.Revision, rules)
	v.Set("doctor", "unmatched")
	if _, p = preview(v); !slices.Equal(p.Projects.Allow, []config.ProjectMatch{rules[0], rules[1], rules[3]}) {
		t.Errorf("unmatched removed to %+v", p.Projects.Allow)
	}

	// The temporary rule's fix removes it, and the save hides what it
	// admitted from review.
	v = ruleForm(f.csrf, stored.Revision, rules)
	v.Set("narrow", "3")
	body, p := preview(v)
	if slices.Contains(p.Projects.Allow, rules[3]) {
		t.Error("the temporary rule stayed")
	}
	if !strings.Contains(squash(html.UnescapeString(body)), "Saving also hides 1 project from the review queue, so it does not come back as new work: cwd /tmp/probe-1 .") {
		t.Error("the preview does not name the project it hides")
	}
	save := formFields(t, body, "/profiles/default/save")
	if !slices.Equal(save["hide"], []string{"cwd /tmp/probe-1"}) {
		t.Fatalf("the save form hides %v", save["hide"])
	}
	save.Set("note", "probe cleanup")
	if w := postForm(f.h, "/profiles/default/save", save, f.cookie); w.Code != 303 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	hidden, _ := f.lake.Catalog.HiddenProjects(ctx)
	if len(hidden) != 1 || hidden[0].Key != (catalog.ProjectKey{Kind: catalog.KeyCWD, Key: "/tmp/probe-1"}) || hidden[0].Note != "Removed from the profile: probe cleanup" {
		t.Errorf("hidden %+v", hidden)
	}
	if cur, _ := f.lake.Catalog.ProfileByName(ctx, config.DefaultProfile); slices.Contains(cur.Config.Projects.Allow, rules[3]) {
		t.Error("the save kept the temporary rule")
	}

	// A narrowing for an index the doctor does not flag changes nothing.
	cur, _ := f.lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	v = ruleForm(f.csrf, cur.Revision, cur.Config.Projects.Allow)
	v.Set("narrow", "1")
	if w := postForm(f.h, "/profiles/default/preview", v, f.cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "This changes nothing") {
		t.Errorf("an unflagged narrowing: %d", w.Code)
	}
	v.Set("hide", "not a key")
	if w := postForm(f.h, "/profiles/default/preview", v, f.cookie); w.Code != 400 {
		t.Errorf("a bad hide key: %d", w.Code)
	}
}

// A Remove and hide that would queue more hides than one save takes is
// refused with a reason, and the form stays savable.
func TestRemoveAndHideStaysSavable(t *testing.T) {
	f, rules := healthLake(t)
	stored, _ := f.lake.Catalog.ProfileByName(t.Context(), config.DefaultProfile)
	// An edit of its own, so the preview has a change to save.
	v := ruleForm(f.csrf, stored.Revision, append(slices.Clone(rules), config.ProjectMatch{GitRemote: "git.example/team/extra"}))
	for i := range maxReviewBatch {
		v.Add("hide", "cwd /tmp/queued-"+strconv.Itoa(i))
	}
	v.Set("narrow", "3")
	w := postForm(f.h, "/profiles/default/preview", v, f.cookie)
	if w.Code != 409 || !strings.Contains(html.UnescapeString(w.Body.String()), "would hide 501 projects from review, and one save hides at most 500") {
		t.Fatalf("an oversized hide list: %d", w.Code)
	}
	var p config.Profile
	if err := json.Unmarshal([]byte(hiddenValue(t, w.Body.String(), "document")), &p); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.Projects.Allow, rules[3]) {
		t.Error("the refused fix removed the rule")
	}
	if save := formFields(t, w.Body.String(), "/profiles/default/save"); len(save["hide"]) != maxReviewBatch {
		t.Errorf("the save form hides %d projects, want the %d queued", len(save["hide"]), maxReviewBatch)
	}
}

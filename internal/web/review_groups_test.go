package web

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

func folderRow(cwd string, sessions int) reviewRow {
	return reviewRow{Key: catalog.ProjectKey{Kind: catalog.KeyCWD, Key: cwd}, Sessions: sessions,
		Sightings: []reviewSighting{{ReviewSighting: catalog.ReviewSighting{Project: protocol.InventoryProject{CWD: cwd}}}}}
}

func remoteRow(remote string) reviewRow {
	return reviewRow{Key: catalog.ProjectKey{Kind: catalog.KeyGitRemote, Key: remote}, Sessions: 1,
		Sightings: []reviewSighting{{ReviewSighting: catalog.ReviewSighting{Project: protocol.InventoryProject{GitRemote: remote, CWD: "/src/" + remote}}}}}
}

// TKT-01M6ETFR3B: the queue groups projects by the rule an owner-width
// Allow adds, three or more to a group, and puts projects in temporary
// folders in a group of their own.
func TestGroupNeeds(t *testing.T) {
	rows := []reviewRow{
		folderRow("/home/me/wt/a", 1), folderRow("/home/me/wt/b", 2), folderRow("/home/me/wt/c", 3),
		remoteRow("git.example/team/x"), remoteRow("git.example/team/y"), remoteRow("git.example/team/z"),
		folderRow("/tmp/run/1", 1), folderRow("/tmp/run/2", 1), folderRow("/home/me/.cache/x/3", 1),
		folderRow("/srv/two/a", 1), folderRow("/srv/two/b", 1),
		folderRow("/tmp/lone", 1),
		folderRow("/home/me", 1),
	}
	groups, singles := groupNeeds(rows)
	var ids []string
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	// Most projects first: the temporary group holds four.
	if want := []string{"temporary", "folder /home/me/wt", "owner git.example/team"}; !slices.Equal(ids, want) {
		t.Fatalf("groups %v, want %v", ids, want)
	}
	wt := groups[1]
	if wt.Kind != groupFolder || *wt.Rule != (config.ProjectMatch{CWDPrefix: "/home/me/wt"}) || len(wt.Keys) != 3 || wt.Sessions != 6 {
		t.Errorf("folder group %+v", wt)
	}
	if *groups[2].Rule != (config.ProjectMatch{GitRemotePrefix: "git.example/team"}) {
		t.Errorf("owner group rule %+v", groups[2].Rule)
	}
	if tmp := groups[0]; tmp.Rule != nil || len(tmp.Keys) != 4 || !tmp.Rows[0].Temporary {
		t.Errorf("temporary group %+v", tmp)
	}
	var left []string
	for _, r := range singles {
		left = append(left, r.Key.Key)
	}
	// Two under one folder are no group; a home directory names no
	// folder narrower than the homes.
	if want := []string{"/srv/two/a", "/srv/two/b", "/home/me"}; !slices.Equal(left, want) {
		t.Errorf("singles %v, want %v", left, want)
	}
	if !rows[11].Temporary || rows[0].Temporary {
		t.Error("a lone temporary project is not marked, or a kept one is")
	}
}

// TKT-01M6ETFR3B: a group's Allow plans one rule for the whole group,
// however many projects it holds, and the confirm page names the group
// rather than its keys. Hide all hides a group in one step.
func TestReviewGroupAllowAndHide(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	ctx := t.Context()
	n := maxReviewBatch + 10
	desk := []protocol.InventoryProject{}
	for i := range n {
		desk = append(desk, protocol.InventoryProject{CWD: fmt.Sprintf("/home/me/wt/%04d", i), CWDs: 1, Harnesses: []string{"claude"}, Sessions: 1, Reason: config.RefusedNoMatch})
	}
	for _, d := range []string{"/tmp/t1", "/tmp/t2", "/tmp/t3"} {
		desk = append(desk, protocol.InventoryProject{CWD: d, CWDs: 1, Harnesses: []string{"claude"}, Sessions: 1, Reason: config.RefusedNoMatch})
	}
	now := time.Now()
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[1].ID, protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: desk}, now); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)

	page := get(h, "/review", cookie).Body.String()
	for _, want := range []string{
		`<span class="mono">cwd_prefix /home/me/wt</span>`, fmt.Sprintf("%d projects", n), `value="folder /home/me/wt"`,
		"Allow cwd_prefix /home/me/wt…", fmt.Sprintf("Hide all %d", n),
		"Temporary folders", `value="temporary"`, "Hide all 3", "Other projects",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("review page lacks %q", want)
		}
	}
	if strings.Contains(page, "Allow cwd_prefix /tmp") {
		t.Error("the temporary group offers Allow")
	}

	var api reviewView
	if err := json.Unmarshal(get(h, "/api/web/v1/review", cookie).Body.Bytes(), &api); err != nil || len(api.Groups) != 2 || len(api.Groups[0].Keys) != n || len(api.Groups[0].Version) != 16 {
		t.Fatalf("api groups: %v %+v", err, api.Groups)
	}
	if !strings.Contains(page, `name="group_version" value="`+api.Groups[0].Version+`"`) {
		t.Error("the group's form does not carry its version")
	}

	// Allow the folder group: one rule, though the group is past one batch.
	group := url.Values{"csrf": {csrf}, "return": {"/review"}, "group": {"folder /home/me/wt"}, "group_version": {api.Groups[0].Version}}
	w := postForm(h, "/review/allow", group, cookie)
	confirm := w.Body.String()
	if w.Code != 200 || !strings.Contains(confirm, "gains 1 allow rule</h2>") || !strings.Contains(confirm, "cwd_prefix /home/me/wt") {
		t.Fatalf("group allow: %d", w.Code)
	}
	save := formFields(t, confirm, "/review/allow/save")
	if save.Get("group") != "folder /home/me/wt" || save.Get("group_version") != api.Groups[0].Version || len(save["key"]) != 0 {
		t.Fatalf("the confirm form carries %v keys and group %q", len(save["key"]), save.Get("group"))
	}
	if w := postForm(h, "/review/allow/save", save, cookie); w.Code != 303 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	ci, _ := lake.Catalog.ProfileByName(ctx, "ci")
	if !slices.Equal(ci.Config.Projects.Allow, []config.ProjectMatch{{CWDPrefix: "/home/me/wt"}}) {
		t.Fatalf("ci allows %+v", ci.Config.Projects.Allow)
	}

	// The group is gone from the queue now, and a stale form says so.
	if w := postForm(h, "/review/allow", group, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "That group changed") {
		t.Errorf("stale group: %d", w.Code)
	}
	// A temporary group is hidden, not allowed.
	tmp := url.Values{"csrf": {csrf}, "return": {"/review"}, "group": {"temporary"}, "group_version": {api.Groups[1].Version}}
	if w := postForm(h, "/review/allow", tmp, cookie); w.Code != 400 {
		t.Errorf("allow temporary: %d", w.Code)
	}
	if w := postForm(h, "/review/hide", tmp, cookie); w.Code != 303 {
		t.Fatalf("hide temporary: %d %s", w.Code, w.Body.String())
	}
	hides, _ := lake.Catalog.HiddenProjects(ctx)
	if len(hides) != 3 || hides[0].Note != "Hidden with the temporary folders group" {
		t.Errorf("hidden %+v", hides)
	}
}

// TKT-01M6ETFR3B: Hide all hides a group larger than one batch.
func TestReviewGroupHidePastOneBatch(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	ctx := t.Context()
	n := maxReviewBatch + 1
	var ps []protocol.InventoryProject
	for i := range n {
		ps = append(ps, protocol.InventoryProject{GitRemote: fmt.Sprintf("git.example/many/r%04d", i), CWD: "/src/r", CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch})
	}
	now := time.Now()
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[1].ID, protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: ps}, now); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	var api reviewView
	if err := json.Unmarshal(get(h, "/api/web/v1/review", cookie).Body.Bytes(), &api); err != nil || len(api.Groups) != 1 {
		t.Fatalf("api groups: %v %+v", err, api.Groups)
	}
	form := url.Values{"csrf": {csrfOf(t, h, cookie)}, "return": {"/review"}, "group": {"owner git.example/many"}, "group_version": {api.Groups[0].Version}, "note": {"forks"}}
	if w := postForm(h, "/review/hide", form, cookie); w.Code != 303 {
		t.Fatalf("hide: %d %s", w.Code, w.Body.String())
	}
	if hides, _ := lake.Catalog.HiddenProjects(ctx); len(hides) != n || hides[0].Note != "forks" {
		t.Fatalf("hid %d", len(hides))
	}
}

// TKT-01M6ETFR3B: a group whose projects changed after the page showed
// it is refused, though a group of that name is still on the queue, so
// Hide all never reaches a project the operator did not see.
func TestReviewGroupRefusesChangedMembers(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	ctx := t.Context()
	tmp := func(dirs ...string) {
		var ps []protocol.InventoryProject
		for _, d := range dirs {
			ps = append(ps, protocol.InventoryProject{CWD: d, CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch})
		}
		now := time.Now()
		if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[1].ID, protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: ps}, now); err != nil {
			t.Fatal(err)
		}
	}
	tmp("/tmp/a", "/tmp/b", "/tmp/c")
	cookie, _ := signIn(t, idp, h)
	var api reviewView
	if err := json.Unmarshal(get(h, "/api/web/v1/review", cookie).Body.Bytes(), &api); err != nil || len(api.Groups) != 1 {
		t.Fatalf("api groups: %v %+v", err, api.Groups)
	}
	seen := api.Groups[0].Version
	tmp("/tmp/a", "/tmp/b", "/tmp/c", "/tmp/new")
	form := url.Values{"csrf": {csrfOf(t, h, cookie)}, "return": {"/review"}, "group": {"temporary"}, "group_version": {seen}}
	if w := postForm(h, "/review/hide", form, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "That group changed") {
		t.Fatalf("hide a changed group: %d", w.Code)
	}
	if w := postForm(h, "/review/allow", url.Values{"csrf": form["csrf"], "return": {"/review"}, "group": {"temporary"}}, cookie); w.Code != 409 {
		t.Errorf("a group form with no version: %d", w.Code)
	}
	if hides, _ := lake.Catalog.HiddenProjects(ctx); len(hides) != 0 {
		t.Fatalf("hid %d", len(hides))
	}
}

// TKT-01M6ETFR3B: a group Allow's confirm page saves nothing once the
// group's projects changed after it was planned.
func TestReviewGroupAllowSaveRefusesChangedMembers(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	ctx := t.Context()
	put := func(dirs ...string) {
		var ps []protocol.InventoryProject
		for _, d := range dirs {
			ps = append(ps, protocol.InventoryProject{CWD: d, CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch})
		}
		now := time.Now()
		if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[1].ID, protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: ps}, now); err != nil {
			t.Fatal(err)
		}
	}
	put("/srv/wt/a", "/srv/wt/b", "/srv/wt/c")
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	var api reviewView
	if err := json.Unmarshal(get(h, "/api/web/v1/review", cookie).Body.Bytes(), &api); err != nil || len(api.Groups) != 1 {
		t.Fatalf("api groups: %v %+v", err, api.Groups)
	}
	w := postForm(h, "/review/allow", url.Values{"csrf": {csrf}, "return": {"/review"}, "group": {"folder /srv/wt"}, "group_version": {api.Groups[0].Version}}, cookie)
	if w.Code != 200 {
		t.Fatalf("plan: %d", w.Code)
	}
	save := formFields(t, w.Body.String(), "/review/allow/save")
	put("/srv/wt/a", "/srv/wt/b", "/srv/wt/c", "/srv/wt/new")
	if w := postForm(h, "/review/allow/save", save, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "That group changed") {
		t.Fatalf("save a changed group: %d", w.Code)
	}
	if ci, _ := lake.Catalog.ProfileByName(ctx, "ci"); len(ci.Config.Projects.Allow) != 0 {
		t.Fatalf("saved %+v", ci.Config.Projects.Allow)
	}
}

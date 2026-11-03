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

// Exercise the screenshot's broad /tmp refusal, then the alternative through
// real editor forms, save, conflicting save, and rollback.
func TestConvertTemporaryAllowsToHidePrefix(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx, now := t.Context(), time.Now()
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	var original config.Profile
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now}
	for i := range 21 {
		cwd := fmt.Sprintf("/tmp/probe-%02d", i)
		original.Projects.Allow = append(original.Projects.Allow, config.ProjectMatch{CWDPrefix: cwd})
		inv.Projects = append(inv.Projects, protocol.InventoryProject{CWD: cwd, CWDs: 1, Sessions: 1, Allowed: true})
	}
	original.Projects.Allow = append(original.Projects.Allow, config.ProjectMatch{CWDPrefix: "/tmp/keep"}, config.ProjectMatch{CWDPrefix: "/srv/main"})
	original.Review.Rules = []config.ReviewRule{{CWDPrefix: "/tmp/shown", Action: "show"}}
	inv.Projects = append(inv.Projects, protocol.InventoryProject{CWD: "/tmp/future", Sessions: 3, Reason: config.RefusedNoMatch})
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[0].ID, inv, now); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(original)
	stored, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, raw, "test", "", now)
	if err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	form := organizationForm(t, get(h, "/profiles/default/edit", cookie).Body.String(), "profile-form")
	for i := range 21 {
		form.Add("pick", fmt.Sprintf("allow.%d", i))
	}
	form.Set("merge", "allow")
	w := postForm(h, "/profiles/default/preview", form, cookie)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Hide this prefix instead") {
		t.Fatalf("broad capture merge: %d %s", w.Code, squash(w.Body.String()))
	}
	form = organizationForm(t, w.Body.String(), "profile-form")
	form.Set("merge", "hide")
	w = postForm(h, "/profiles/default/preview", form, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Replace selected rules with hide and preview") || !strings.Contains(w.Body.String(), `value="/tmp"`) {
		t.Fatalf("hide plan: %d %s", w.Code, squash(w.Body.String()))
	}
	form = organizationForm(t, w.Body.String(), "profile-form")
	form.Set("merge_apply", "hide")
	w = postForm(h, "/profiles/default/preview", form, cookie)
	if w.Code != 200 {
		t.Fatalf("apply hide plan: %d %s", w.Code, squash(w.Body.String()))
	}
	page := w.Body.String()
	if !strings.Contains(squash(page), "Hides 22 projects from review") || !strings.Contains(squash(page), "Stops 21 projects") {
		t.Errorf("capture and visibility effects missing: %s", squash(page))
	}
	preview, err := config.ParseProfile([]byte(hiddenValue(t, page, "document")))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(preview.Projects.Allow, original.Projects.Allow[21:]) || len(preview.Review.Rules) != 2 || preview.Review.Rules[0] != original.Review.Rules[0] {
		t.Fatalf("conversion lost exceptions: %+v", preview)
	}
	untouched, _ := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	if untouched.Revision != stored.Revision {
		t.Fatal("preview changed the catalog")
	}
	// A subsequent editor submission (using condition pickers, selects and
	// blank rows) must retain both kinds of visibility rule.
	form = organizationForm(t, page, "profile-form")
	form.Set("sort", "1")
	w = postForm(h, "/profiles/default/preview", form, cookie)
	if w.Code != 200 {
		t.Fatalf("editor round trip: %d %s", w.Code, squash(w.Body.String()))
	}
	save := formFields(t, w.Body.String(), "/profiles/default/save")
	if got := postForm(h, "/profiles/default/save", save, cookie); got.Code != 303 {
		t.Fatalf("save: %d %s", got.Code, got.Body)
	}
	after, _ := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	if !slices.Equal(after.Config.Review.Rules, preview.Review.Rules) {
		t.Fatalf("save lost review rules: %+v", after.Config)
	}
	if got := postForm(h, "/profiles/default/save", save, cookie); got.Code != 409 {
		t.Fatalf("stale save: %d", got.Code)
	}
	// A newly discovered refused temp project is hidden without another edit.
	inv.GeneratedAt = now.Add(time.Minute)
	inv.Projects = append(inv.Projects, protocol.InventoryProject{CWD: "/tmp/new-later", Sessions: 1, Reason: config.RefusedNoMatch})
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[0].ID, inv, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	hidden := get(h, "/review?tab=hidden", cookie).Body.String()
	if !strings.Contains(hidden, "/tmp/new-later") || !strings.Contains(hidden, "Edit hide rules") || strings.Contains(hidden, "/review/unhide") {
		t.Errorf("rule-hidden projects must offer policy editing: %s", squash(hidden))
	}
	device := get(h, "/devices/"+ds[0].ID, cookie).Body.String()
	if !strings.Contains(device, "Edit hide rule") {
		t.Error("device inventory offers no hide rule editor")
	}
	csrf := csrfOf(t, h, cookie)
	rollback := func(rev, base int64) {
		t.Helper()
		got := postForm(h, "/profiles/default/rollback/"+strconvI(rev), url.Values{"csrf": {csrf}, "base": {strconvI(base)}}, cookie)
		if got.Code != 303 {
			t.Fatalf("rollback: %d %s", got.Code, got.Body)
		}
	}
	rollback(stored.Revision, after.Revision)
	restored, _ := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	if restored.Version != stored.Version {
		t.Fatal("rollback did not restore original policy")
	}
	rollback(after.Revision, restored.Revision)
	restored, _ = lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	if restored.Version != after.Version {
		t.Fatal("rollback did not restore hide policy")
	}
}

func TestReviewVisibilityIsPerSightingAndPreservesManualHides(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx, now := t.Context(), time.Now()
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{
		{Hash: strings.Repeat("a", 64), Name: "laptop"}, {Hash: strings.Repeat("b", 64), Name: "desktop"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"default": `{"review":{"rules":[{"cwd_prefix":"/tmp","action":"hide"},{"cwd_prefix":"/tmp/shown","action":"show"},{"cwd_prefix":"/tmp/shown/private","action":"hide"}]},"projects":{"allow":[{"cwd_prefix":"/tmp/keep"}]}}`,
		"work":    `{}`,
	} {
		if _, _, err := lake.Catalog.PutProfile(ctx, name, []byte(doc), "test", "", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := lake.Catalog.SetDeviceProfile(ctx, "desktop", "work", "work", "test", now); err != nil {
		t.Fatal(err)
	}
	var laptop, desktop catalog.Device
	for _, d := range ds {
		if d.Name == "laptop" {
			laptop = d
		} else {
			desktop = d
		}
	}
	var projects []protocol.InventoryProject
	for _, cwd := range []string{"/tmp/hidden", "/tmp/keep/app", "/tmp/shown/app", "/tmp/shown/private/app", "/tmp-other/app", "/tmp/manual"} {
		projects = append(projects, protocol.InventoryProject{CWD: cwd, CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch})
	}
	projects = append(projects,
		protocol.InventoryProject{CWD: "/tmp/mixed", GitRemote: "git.example/team/mixed", CWDs: 2, Sessions: 2, Reason: config.RefusedNoMatch},
		protocol.InventoryProject{CWD: "/tmp/shared", GitRemote: "git.example/team/shared", CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch})
	for id, ps := range map[string][]protocol.InventoryProject{
		laptop.ID:  projects,
		desktop.ID: {{CWD: "/srv/shared", GitRemote: "git.example/team/shared", CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch}},
	} {
		if _, err := lake.Catalog.PutDeviceInventory(ctx, id, protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: ps}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := lake.Catalog.HideProjects(ctx, []catalog.ProjectKey{{Kind: catalog.KeyCWD, Key: "/tmp/manual"}}, "test", "manual", now); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	var queue reviewView
	if err := json.Unmarshal(get(h, "/api/web/v1/review", cookie).Body.Bytes(), &queue); err != nil {
		t.Fatal(err)
	}
	visible := append(slices.Clone(queue.Needs), queue.Pending...)
	for _, key := range []string{"/tmp/keep/app", "/tmp/shown/app", "/tmp-other/app", "git.example/team/mixed", "git.example/team/shared"} {
		if !slices.ContainsFunc(visible, func(row reviewRow) bool { return row.Key.Key == key }) {
			t.Errorf("expected visible project %s", key)
		}
	}
	if len(visible) != 5 || len(queue.Hidden) != 4 {
		t.Fatalf("visible=%+v hidden=%+v", visible, queue.Hidden)
	}
	for _, row := range visible {
		if row.Key.Key == "git.example/team/shared" && (len(row.Sightings) != 1 || row.Sightings[0].DeviceID != desktop.ID) {
			t.Fatal("hiding one copy hid or retained the wrong device's copy")
		}
	}
	hidden := get(h, "/review?tab=hidden", cookie).Body.String()
	if strings.Count(hidden, `form="hidden-unhide-one"`) != 1 {
		t.Error("only the explicit manual hide should offer Unhide")
	}
}

func TestAddingAllowsPreservesVisibilityExceptions(t *testing.T) {
	review := config.ReviewConfig{Rules: []config.ReviewRule{{CWDPrefix: "/tmp", Action: "hide"}}}
	narrow, broad := config.ProjectMatch{CWDPrefix: "/tmp/keep"}, config.ProjectMatch{CWDPrefix: "/tmp"}
	for _, tc := range []struct{ allow, add []config.ProjectMatch }{
		{[]config.ProjectMatch{narrow}, []config.ProjectMatch{broad}},
		{[]config.ProjectMatch{broad}, []config.ProjectMatch{narrow}},
		{nil, []config.ProjectMatch{narrow, broad}},
	} {
		got, removed := withRules(tc.allow, tc.add, review)
		if !slices.Contains(got, narrow) || len(removed) != 0 {
			t.Fatalf("visibility exception lost: got=%+v removed=%+v", got, removed)
		}
	}
}

func TestReviewPreviewNamesDevicesWithoutInventories(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	if _, err := lake.Catalog.SyncTokenFile(t.Context(), []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "unlisted-laptop"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	form := organizationForm(t, get(h, "/profiles/default/edit", cookie).Body.String(), "profile-form")
	form.Set("review.0.cwd_prefix", "/tmp")
	w := postForm(h, "/profiles/default/preview", form, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Review effects cannot be fully enumerated for unlisted-laptop") {
		t.Fatalf("missing unknown visibility effects: %d %s", w.Code, squash(w.Body.String()))
	}
}

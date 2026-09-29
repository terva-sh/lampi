package web

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// lakeFixture is a lake with an operator signed in.
type lakeFixture struct {
	lake   *api.Server
	h      http.Handler
	cookie *http.Cookie
	csrf   string
}

// widthLake is a lake with one device, laptop, on the default profile,
// which allows one old repository under git.example/team. laptop
// refuses three more repositories there, one under a bare host, and a
// folder.
func widthLake(t *testing.T) (*lakeFixture, catalog.Device) {
	t.Helper()
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, []byte(`{"projects":{"allow":[{"git_remote":"git.example/team/old"}]}}`), "test", "", now); err != nil {
		t.Fatal(err)
	}
	refused := func(remote, cwd string, n int) protocol.InventoryProject {
		return protocol.InventoryProject{GitRemote: remote, CWD: cwd, CWDs: 1, Sessions: n, Reason: config.RefusedNoMatch}
	}
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
		refused("git.example/team/app", "/work/app", 3),
		refused("git.example/team/lib", "/work/lib", 2),
		refused("git.example/team/unselected", "/work/unselected", 7),
		refused("git.example/solo", "/work/solo", 1),
		refused("", "/home/me/scratch", 4),
	}}
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[0].ID, inv, now); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	return &lakeFixture{lake: lake, h: h, cookie: cookie, csrf: csrfOf(t, h, cookie)}, ds[0]
}

// TKT-01M3NM01Q: Allow selected at owner width adds one
// git_remote_prefix per owner, drops the rules it covers, and lists
// what the prefix admits beyond the selection.
func TestAllowSelectedAtOwnerWidth(t *testing.T) {
	f, _ := widthLake(t)
	ctx := t.Context()
	review := get(f.h, "/review", f.cookie).Body.String()
	if !strings.Contains(review, `<option value="owner">each repository&#39;s owner</option>`) {
		t.Fatal("the queue offers no owner width")
	}
	sel := url.Values{"csrf": {f.csrf}, "return": {"/review"}, "width": {"owner"},
		"key": {"git_remote git.example/team/app", "git_remote git.example/team/lib", "git_remote git.example/solo", "cwd /home/me/scratch"}}
	w := postForm(f.h, "/review/allow", sel, f.cookie)
	if w.Code != 200 {
		t.Fatalf("allow selected: %d %s", w.Code, w.Body)
	}
	page := html.UnescapeString(w.Body.String())
	for _, want := range []string{
		"gains 3 allow rules",
		"git_remote_prefix git.example/team for laptop; covers every repository under it, selected or not",
		// The owner of git.example/solo is the bare host: it keeps its
		// exact rule.
		"git_remote git.example/solo for laptop",
		"cwd_prefix /home/me/scratch for laptop",
		"It drops 1 rule that the new rules cover: git_remote git.example/team/old",
		"Admits 5 projects",
		"git.example/team/unselected repository on laptop · 7 sessions",
	} {
		if !strings.Contains(squash(page), want) {
			t.Errorf("confirm page lacks %q", want)
		}
	}
	save := formFields(t, w.Body.String(), "/review/allow/save")
	if save.Get("width") != "owner" {
		t.Fatalf("the save form carries width %q", save.Get("width"))
	}
	if w := postForm(f.h, "/review/allow/save", save, f.cookie); w.Code != 303 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	p, _ := f.lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	want := []config.ProjectMatch{{GitRemotePrefix: "git.example/team"}, {GitRemote: "git.example/solo"}, {CWDPrefix: "/home/me/scratch"}}
	if !slices.Equal(p.Config.Projects.Allow, want) {
		t.Fatalf("saved %+v, want %+v", p.Config.Projects.Allow, want)
	}

	if w := postForm(f.h, "/review/allow", url.Values{"csrf": {f.csrf}, "width": {"host"}, "key": {"cwd /home/me/scratch"}}, f.cookie); w.Code != 400 {
		t.Errorf("an unknown width: %d", w.Code)
	}
}

// The API plans at owner width too, and names the rules it drops and
// what the change admits; repository stays the default.
func TestAllowSelectedAPIWidth(t *testing.T) {
	f, _ := widthLake(t)
	hdr := map[string]string{CSRFHeader: f.csrf}
	plan := func(body string) (int, allowView) {
		t.Helper()
		w := post(f.h, "/api/web/v1/review/allow", body, f.cookie, hdr)
		var v allowView
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, v
	}
	code, v := plan(`{"keys":[{"kind":"git_remote","key":"git.example/team/app"}],"width":"owner"}`)
	if code != 200 || len(v.Profiles) != 1 {
		t.Fatalf("owner plan: %d %+v", code, v)
	}
	p := v.Profiles[0]
	if p.Rules[0].Rule != (config.ProjectMatch{GitRemotePrefix: "git.example/team"}) || !slices.Equal(p.Removed, []config.ProjectMatch{{GitRemote: "git.example/team/old"}}) {
		t.Errorf("owner plan rules %+v removed %+v", p.Rules, p.Removed)
	}
	if p.Reach == nil || len(p.Reach.Admits) != 3 {
		t.Errorf("owner plan reach %+v", p.Reach)
	}
	code, v = plan(`{"keys":[{"kind":"git_remote","key":"git.example/team/app"}]}`)
	if code != 200 || v.Profiles[0].Rules[0].Rule != (config.ProjectMatch{GitRemote: "git.example/team/app"}) || len(v.Profiles[0].Removed) != 0 {
		t.Errorf("default plan: %d %+v", code, v)
	}
	if code, _ := plan(`{"keys":[{"kind":"git_remote","key":"git.example/team/app"}],"width":"host"}`); code != 400 {
		t.Errorf("an unknown width: %d", code)
	}
}

// Allow on the device page offers the owner beside the repository, and
// not for a remote whose owner is the bare host or a folder.
func TestAllowOneProjectAtOwnerWidth(t *testing.T) {
	f, laptop := widthLake(t)
	page := html.UnescapeString(get(f.h, "/devices/"+laptop.ID, f.cookie).Body.String())
	if n := strings.Count(page, ">Allow git.example/team/…</button>"); n != 3 {
		t.Errorf("%d owner buttons, want one per repository under git.example/team", n)
	}
	if strings.Contains(page, ">Allow git.example/…</button>") {
		t.Error("the page offers a host-wide rule")
	}
	w := postForm(f.h, "/devices/"+laptop.ID+"/allow", url.Values{"csrf": {f.csrf}, "git_remote": {"git.example/team/app"}, "cwd": {"/work/app"}, "width": {"owner"}}, f.cookie)
	if w.Code != 200 {
		t.Fatalf("allow: %d %s", w.Code, w.Body)
	}
	body := html.UnescapeString(w.Body.String())
	if !strings.Contains(body, "Adds an allow rule for git_remote_prefix git.example/team to profile default, which laptop uses, and drops 1 rule it covers.") {
		t.Error("the notice does not name the prefix and the rule it drops")
	}
	var doc config.Profile
	if err := json.Unmarshal([]byte(hiddenValue(t, w.Body.String(), "document")), &doc); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(doc.Projects.Allow, []config.ProjectMatch{{GitRemotePrefix: "git.example/team"}}) {
		t.Errorf("document allow %+v", doc.Projects.Allow)
	}
	if !strings.Contains(squash(body), "git.example/team/unselected repository on laptop · 7 sessions") {
		t.Error("the editor does not list what the prefix admits")
	}
}

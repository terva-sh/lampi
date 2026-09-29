package web

import (
	"html"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// hiddenValue is the value of the hidden input called name in page.
func hiddenValue(t *testing.T, page, name string) string {
	t.Helper()
	m := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no %s field", name)
	}
	return html.UnescapeString(m[1])
}

// TKT-01M3M7M11S: Allow on a refused project opens the device's profile
// in the editor with the rule added and previewed; the operator saves it
// from there, and the lake's profile then allows the project.
func TestAllowARefusedProjectFromTheDevicePage(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	created, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{
		{Hash: strings.Repeat("a", 64), Name: "laptop"},
		{Hash: strings.Repeat("b", 64), Name: "gone"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	laptop, gone := created[0], created[1]
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
		{GitRemote: "git.example/team/app", CWD: "/work/app", CWDs: 1, Sessions: 3, Reason: config.RefusedNoMatch},
		{CWD: "/home/me/scratch", CWDs: 1, Sessions: 9, Reason: config.RefusedNoMatch},
		{CWD: "/home/me/secret", CWDs: 1, Sessions: 1, Reason: config.RefusedByDeny},
		{CWD: "/work/ok", CWDs: 1, Sessions: 1, Allowed: true},
	}}
	if _, err := lake.Catalog.PutDeviceInventory(ctx, laptop.ID, inv, now); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)

	page := get(h, "/devices/"+laptop.ID, cookie).Body.String()
	if n := strings.Count(page, `action="/devices/`+laptop.ID+`/allow"`); n != 2 {
		t.Fatalf("%d Allow forms, want one for each project an allow rule can let through", n)
	}
	if !strings.Contains(page, "A deny rule wins") {
		t.Fatal("the denied project does not say why it has no Allow")
	}

	allow := func(id string, v url.Values) (int, string) {
		v.Set("csrf", csrf)
		w := postForm(h, "/devices/"+id+"/allow", v, cookie)
		return w.Code, w.Body.String()
	}
	code, editor := allow(laptop.ID, url.Values{"git_remote": {"git.example/team/app"}, "cwd": {"/work/app"}})
	if code != 200 || !strings.Contains(editor, "Adds an allow rule for git_remote git.example/team/app to profile default") || !strings.Contains(editor, "laptop") {
		t.Fatalf("allow: %d", code)
	}
	doc := hiddenValue(t, editor, "document")
	if !strings.Contains(doc, `"git_remote":"git.example/team/app"`) {
		t.Fatalf("document %s", doc)
	}
	w := postForm(h, "/profiles/default/save", url.Values{"csrf": {csrf}, "base": {hiddenValue(t, editor, "base")}, "document": {doc}, "note": {"Allow app, refused on laptop"}}, cookie)
	if w.Code != 303 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	p, err := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	if err != nil || !slices.Contains(p.Config.Projects.Allow, config.ProjectMatch{GitRemote: "git.example/team/app"}) {
		t.Fatalf("profile %+v %v", p.Config.Projects, err)
	}

	// A project with no remote gets a cwd rule.
	if code, editor := allow(laptop.ID, url.Values{"cwd": {"/home/me/scratch"}}); code != 200 || !strings.Contains(hiddenValue(t, editor, "document"), `"cwd_prefix":"/home/me/scratch"`) {
		t.Fatalf("cwd rule: %d", code)
	}
	// The app is allowed now, though the inventory still says refused
	// until the agent syncs again.
	if code, body := allow(laptop.ID, url.Values{"git_remote": {"git.example/team/app"}, "cwd": {"/work/app"}}); code != 409 || !strings.Contains(body, "already allows") {
		t.Fatalf("already allowed: %d", code)
	}
	// A rule that covers the project without being the one Allow would
	// add counts as allowing it.
	doc = `{"projects":{"allow":[{"git_remote":"git.example/team/app"},{"cwd_prefix":"/home/me"}]}}`
	cur, _ := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	if w := postForm(h, "/profiles/default/save", url.Values{"csrf": {csrf}, "base": {strconv.FormatInt(cur.Revision, 10)}, "document": {doc}}, cookie); w.Code != 303 {
		t.Fatalf("save prefix: %d %s", w.Code, w.Body)
	}
	if code, body := allow(laptop.ID, url.Values{"cwd": {"/home/me/scratch"}}); code != 409 || !strings.Contains(body, "already allows") {
		t.Fatalf("covered by a prefix: %d", code)
	}
	// What the form names must be a refused project the device sent: not
	// an allowed one, not a denied one, not one it never sent.
	for _, v := range []url.Values{
		{"cwd": {"/work/ok"}},
		{"cwd": {"/home/me/secret"}},
		{"cwd": {"/somewhere/else"}},
		{"git_remote": {"git.example/team/app"}},
		{},
	} {
		if code, _ := allow(laptop.ID, v); code != 409 {
			t.Errorf("%v: %d", v, code)
		}
	}
	if _, err := lake.Catalog.RevokeDevice(ctx, gone.Name, "test", now); err != nil {
		t.Fatal(err)
	}
	if code, _ := allow(gone.ID, url.Values{"cwd": {"/x"}}); code != 409 {
		t.Fatalf("revoked: %d", code)
	}
	if code, _ := allow("dev_nope", url.Values{"cwd": {"/x"}}); code != 404 {
		t.Fatalf("no device: %d", code)
	}
	if w := postForm(h, "/devices/"+laptop.ID+"/allow", url.Values{"csrf": {"wrong"}, "cwd": {"/x"}}, cookie); w.Code != 403 {
		t.Fatalf("wrong csrf: %d", w.Code)
	}

	// A viewer gets neither the form nor the action.
	vlake, vidp, vh, _ := operatorLake(t, "", "readers")
	vcreated, err := vlake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vlake.Catalog.PutDeviceInventory(ctx, vcreated[0].ID, inv, now); err != nil {
		t.Fatal(err)
	}
	viewer, _ := signIn(t, vidp, vh)
	if body := get(vh, "/devices/"+vcreated[0].ID, viewer).Body.String(); strings.Contains(body, "/allow\"") {
		t.Fatal("viewer sees Allow")
	}
	if w := postForm(vh, "/devices/"+vcreated[0].ID+"/allow", url.Values{"csrf": {csrfOf(t, vh, viewer)}, "cwd": {"/x"}}, viewer); w.Code != 404 {
		t.Fatalf("viewer allow: %d", w.Code)
	}
}

// TKT-01M3N8FHQD: Allow comes back to the device's page. The editor it
// opens offers Back to the device, carries the page through preview and
// a refused save, and a save redirects to the page, which says what the
// revision saved.
func TestAllowReturnsToTheDevicePage(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	created, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	laptop := created[0]
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
		{GitRemote: "git.example/team/app", CWD: "/work/app", CWDs: 1, Sessions: 3, Reason: config.RefusedNoMatch},
	}}
	if _, err := lake.Catalog.PutDeviceInventory(ctx, laptop.ID, inv, now); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	page := deviceURL(laptop.ID) + "?show=refused"

	if body := get(h, page, cookie).Body.String(); hiddenValue(t, body, "return") != page {
		t.Fatal("the Allow form does not name the page it is on")
	}
	w := postForm(h, "/devices/"+laptop.ID+"/allow", url.Values{"csrf": {csrf}, "git_remote": {"git.example/team/app"}, "cwd": {"/work/app"}, "return": {page}}, cookie)
	editor := w.Body.String()
	if w.Code != 200 || !strings.Contains(editor, "Back to laptop") || strings.Contains(editor, ">Cancel<") {
		t.Fatalf("allow: %d, no Back to laptop", w.Code)
	}
	if hiddenValue(t, editor, "return") != page {
		t.Fatal("the editor does not carry the page")
	}

	// Previewing again keeps the page.
	v := url.Values{"csrf": {csrf}, "base": {hiddenValue(t, editor, "base")}, "return": {page}, "allow_rows": {"1"}, "deny_rows": {"0"}, "allow.0.git_remote": {"git.example/team/app"}}
	if w := postForm(h, "/profiles/default/preview", v, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "Back to laptop") {
		t.Fatalf("preview: %d, no Back to laptop", w.Code)
	}

	// A save refused as stale shows the editor again, still going back.
	save := url.Values{"csrf": {csrf}, "base": {"99"}, "document": {hiddenValue(t, editor, "document")}, "note": {"Allow app"}, "return": {page}}
	if w := postForm(h, "/profiles/default/save", save, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "Back to laptop") {
		t.Fatalf("stale save: %d, no Back to laptop", w.Code)
	}

	save.Set("base", hiddenValue(t, editor, "base"))
	w = postForm(h, "/profiles/default/save", save, cookie)
	p, err := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	want := page + "&saved=default&revision=" + strconv.FormatInt(p.Revision, 10)
	if w.Code != 303 || w.Header().Get("Location") != want {
		t.Fatalf("save: %d to %q, want %q", w.Code, w.Header().Get("Location"), want)
	}
	back := get(h, want, cookie)
	notice := "Profile default revision " + strconv.FormatInt(p.Revision, 10) + " was saved by "
	if body := back.Body.String(); back.Code != 200 || !strings.Contains(body, notice) || !strings.Contains(body, ": Allow app. Devices on it fetch it") {
		t.Fatalf("device page after save: %d, no notice", back.Code)
	}

	// The notice states what the catalog records and nothing else: a
	// link naming a revision never saved, or another profile's, shows
	// the page without one.
	if _, _, err := lake.Catalog.PutProfile(ctx, "ci", []byte(`{}`), "test", "", now); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"?saved=default&revision=999", "?saved=ci&revision=1", "?saved=default", "?saved=../x&revision=1"} {
		if w := get(h, deviceURL(laptop.ID)+q, cookie); w.Code != 200 || strings.Contains(w.Body.String(), "was saved by") {
			t.Fatalf("%s: %d", q, w.Code)
		}
	}

	// A return that is not a dashboard page is dropped: Allow falls back
	// to the device's page, and a save to the profile's.
	w = postForm(h, "/devices/"+laptop.ID+"/allow", url.Values{"csrf": {csrf}, "cwd": {"/work/app"}, "git_remote": {"git.example/team/app"}, "return": {"https://evil.example/"}}, cookie)
	if w.Code != 409 {
		// Already allowed now; the check below uses the save.
		t.Fatalf("allow again: %d", w.Code)
	}
	cur, _ := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	save = url.Values{"csrf": {csrf}, "base": {strconv.FormatInt(cur.Revision, 10)}, "document": {`{"projects":{"allow":[{"cwd_prefix":"/work"}]}}`}, "return": {"//evil.example/devices/" + laptop.ID}}
	if w := postForm(h, "/profiles/default/save", save, cookie); w.Code != 303 || w.Header().Get("Location") != "/profiles/default" {
		t.Fatalf("save with a foreign return: %d to %q", w.Code, w.Header().Get("Location"))
	}
}

func TestReturnPathAcceptsDashboardPagesOnly(t *testing.T) {
	for raw, want := range map[string]string{
		"/devices/dev_abc234":              "/devices/dev_abc234",
		"/devices/dev_abc234?show=refused": "/devices/dev_abc234?show=refused",
		"":                                 "",
		"https://evil.example/devices/dev_abc234": "",
		"//evil.example/devices/dev_abc234":       "",
		"/\\evil.example":                         "",
		"javascript:alert(1)":                     "",
		"/devices/dev_abc234?next=//evil.example": "",
		"/devices/dev_abc234?show=all":            "",
		"/devices/dev_abc234#top":                 "",
		"/devices/dev_abc234/../../profiles":      "",
		"/devices/dev_ab%2Fc":                     "",
		"/devices/dev_":                           "",
		"/devices/laptop":                         "",
		"/profiles/default":                       "",
		"/devices/dev_abc234\r\nSet-Cookie: x=y":  "",
	} {
		if got := returnPath(raw); got != want {
			t.Errorf("returnPath(%q) = %q, want %q", raw, got, want)
		}
	}
}

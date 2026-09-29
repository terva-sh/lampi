package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/testidp"
)

// reviewLake is a lake with a queue to review: laptop (default profile)
// and desk (profile ci) both refuse the app repository; laptop also
// holds scratch, which needs review, pending, which the default profile
// now allows, and secret, which a deny rule refuses. locked is strict
// and old is revoked.
func reviewLake(t *testing.T, groups ...string) (*api.Server, *testidp.Server, http.Handler, []catalog.Device) {
	t.Helper()
	lake, idp, h, _ := operatorLake(t, "", groups...)
	ctx := t.Context()
	now := time.Now()
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{
		{Hash: strings.Repeat("a", 64), Name: "laptop"},
		{Hash: strings.Repeat("b", 64), Name: "desk"},
		{Hash: strings.Repeat("c", 64), Name: "locked"},
		{Hash: strings.Repeat("d", 64), Name: "old"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := lake.Catalog.PutProfile(ctx, "ci", []byte(`{}`), "test", "", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, []byte(`{"projects":{"allow":[{"cwd_prefix":"/work/pending"}]}}`), "test", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.SetDeviceProfileByID(ctx, ds[1].ID, "ci", "ci", "test", now); err != nil {
		t.Fatal(err)
	}
	put := func(id, mode string, at time.Time, ps ...protocol.InventoryProject) {
		inv := protocol.AgentInventory{Mode: mode, GeneratedAt: at, Projects: ps, RefusedSessions: 5, RefusedBytes: 500}
		if _, err := lake.Catalog.PutDeviceInventory(ctx, id, inv, at); err != nil {
			t.Fatal(err)
		}
	}
	app := protocol.InventoryProject{GitRemote: "git.example/team/app", CWD: "/work/app", CWDs: 1, Harnesses: []string{"claude"}, Sessions: 3, Reason: config.RefusedNoMatch}
	put(ds[0].ID, protocol.InventorySociable, now.Add(-time.Hour), app,
		protocol.InventoryProject{CWD: "/home/me/scratch", CWDs: 1, Harnesses: []string{"codex"}, Sessions: 2, Reason: config.RefusedNoMatch},
		protocol.InventoryProject{CWD: "/work/pending", CWDs: 1, Harnesses: []string{"claude"}, Sessions: 1, Reason: config.RefusedNoMatch},
		protocol.InventoryProject{CWD: "/home/me/secret", CWDs: 1, Harnesses: []string{"claude"}, Sessions: 1, Reason: config.RefusedByDeny},
	)
	deskApp := app
	deskApp.GitRemote, deskApp.CWD = "git@git.example:team/app.git", "/src/app"
	put(ds[1].ID, protocol.InventorySociable, now, deskApp)
	put(ds[2].ID, protocol.InventoryStrict, now)
	put(ds[3].ID, protocol.InventorySociable, now, protocol.InventoryProject{CWD: "/gone", Sessions: 1, Reason: config.RefusedNoMatch})
	if _, err := lake.Catalog.RevokeDeviceByID(ctx, ds[3].ID, "test", now); err != nil {
		t.Fatal(err)
	}
	return lake, idp, h, ds
}

// section is the part of page from the heading h2 to the end of its
// panel.
func section(t *testing.T, page, h2 string) string {
	t.Helper()
	i := strings.Index(page, h2)
	if i < 0 {
		t.Fatalf("no %s section", h2)
	}
	end := strings.Index(page[i:], "</section>")
	if d := strings.Index(page[i:], "</details>"); d >= 0 && (end < 0 || d < end) {
		end = d
	}
	return page[i : i+end]
}

// TKT-01M3N8FHVN: /review lists what needs a decision across devices,
// grouped by project, apart from what is allow pending, denied or
// hidden, and the header counts it.
func TestReviewListsProjectsNeedingADecision(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	laptop, desk := ds[0], ds[1]
	cookie, _ := signIn(t, idp, h)

	w := get(h, "/review", cookie)
	page := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("review: %d", w.Code)
	}
	needs := section(t, page, "<h2>Needs review</h2>")
	// The app is one row though its devices name it by different
	// remotes, and it lists both devices.
	if n := strings.Count(needs, ">git.example/team/app<"); n != 1 {
		t.Fatalf("app rows: %d", n)
	}
	for _, want := range []string{">laptop<", ">desk<", "/home/me/scratch", `name="return" value="/review"`, "Allow in ci…", "Allow in default…"} {
		if !strings.Contains(needs, want) {
			t.Errorf("needs review lacks %q", want)
		}
	}
	for _, not := range []string{"/work/pending", "/home/me/secret", "/gone"} {
		if strings.Contains(needs, not) {
			t.Errorf("needs review lists %s", not)
		}
	}
	if !strings.Contains(section(t, page, "Allow pending (1)"), "/work/pending") {
		t.Error("pending project not under Allow pending")
	}
	if !strings.Contains(section(t, page, "Denied (1)"), "/home/me/secret") {
		t.Error("denied project not under Denied")
	}
	if !strings.Contains(page, "locked</a> refuses 5 sessions") {
		t.Error("strict device's totals missing")
	}
	if !strings.Contains(page, `<span class="sr-only">, </span>2<span class="sr-only"> to review</span>`) {
		t.Error("header does not count 2 projects to review")
	}
	// The header counts on every page, not only this one.
	if !strings.Contains(get(h, "/devices", cookie).Body.String(), "</span>2<span class=\"sr-only\"> to review") {
		t.Error("devices page header has no count")
	}

	// Hiding scratch takes it off the queue and puts it on the Hidden
	// tab.
	if _, err := lake.Catalog.HideProjects(t.Context(), []catalog.ProjectKey{{Kind: catalog.KeyCWD, Key: "/home/me/scratch"}}, "oidc:ops", "throwaway", time.Now()); err != nil {
		t.Fatal(err)
	}
	page = get(h, "/review", cookie).Body.String()
	if strings.Contains(section(t, page, "<h2>Needs review</h2>"), "scratch") || !strings.Contains(page, "</span>1<span class=\"sr-only\"> to review") {
		t.Error("hidden project still counted for review")
	}
	hidden := get(h, "/review?tab=hidden", cookie).Body.String()
	if hs := section(t, hidden, "<h2>Hidden</h2>"); !strings.Contains(hs, "/home/me/scratch") || !strings.Contains(hs, "throwaway") || !strings.Contains(hs, "oidc:ops") {
		t.Error("Hidden tab lacks the hide")
	}

	// Filters narrow by device, harness and profile.
	for q, want := range map[string]bool{"?device=" + desk.ID: true, "?profile=default": true, "?harness=codex": false, "?device=" + laptop.ID + "&harness=claude": true} {
		got := strings.Contains(section(t, get(h, "/review"+q, cookie).Body.String(), "<h2>Needs review</h2>"), "git.example/team/app")
		if got != want {
			t.Errorf("%s lists app: %v, want %v", q, got, want)
		}
	}
	if s := section(t, get(h, "/review?device="+desk.ID, cookie).Body.String(), "<h2>Needs review</h2>"); strings.Contains(s, ">laptop<") {
		t.Error("device filter keeps another device's copy")
	}
	for _, q := range []string{"?device=laptop", "?harness=vim", "?tab=all", "?profile=Bad", "?x=1", "?device=a&device=b"} {
		if w := get(h, "/review"+q, cookie); w.Code != 400 {
			t.Errorf("%s: %d", q, w.Code)
		}
	}
}

// TKT-01M3N8FHVN: the browser API returns the same queue.
func TestReviewAPI(t *testing.T) {
	_, idp, h, ds := reviewLake(t, "readers")
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/api/web/v1/review", cookie)
	if w.Code != 200 {
		t.Fatalf("api: %d", w.Code)
	}
	var v struct {
		Needs []struct {
			Key     catalog.ProjectKey `json:"key"`
			Devices []struct {
				DeviceID string `json:"device_id"`
				Profile  string `json:"profile"`
				State    string `json:"state"`
			} `json:"devices"`
			Sessions int `json:"sessions"`
		} `json:"needs_review"`
		Pending []json.RawMessage        `json:"allow_pending"`
		Denied  []json.RawMessage        `json:"denied"`
		Hidden  []json.RawMessage        `json:"hidden"`
		Strict  []catalog.StrictRefusals `json:"strict"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Needs) != 2 || len(v.Pending) != 1 || len(v.Denied) != 1 || len(v.Hidden) != 0 || len(v.Strict) != 1 {
		t.Fatalf("sections %d/%d/%d/%d/%d", len(v.Needs), len(v.Pending), len(v.Denied), len(v.Hidden), len(v.Strict))
	}
	var app = v.Needs[0]
	if app.Key.Key != "git.example/team/app" {
		app = v.Needs[1]
	}
	if app.Key.Kind != catalog.KeyGitRemote || app.Sessions != 6 || len(app.Devices) != 2 || app.Devices[0].DeviceID != ds[1].ID || app.Devices[0].Profile != "ci" || app.Devices[0].State != stateNeeds {
		t.Fatalf("app %+v", app)
	}
	for _, q := range []string{"?tab=hidden", "?nope=1"} {
		if w := get(h, "/api/web/v1/review"+q, cookie); w.Code != 400 {
			t.Errorf("%s: %d", q, w.Code)
		}
	}
}

// TKT-01M3N8FHVN: a viewer sees the queue without the operator's forms;
// an operator's Allow from the queue comes back to it.
func TestReviewAllowComesBackToTheQueue(t *testing.T) {
	_, vidp, vh, _ := reviewLake(t, "readers")
	viewer, _ := signIn(t, vidp, vh)
	if page := get(vh, "/review", viewer).Body.String(); strings.Contains(page, "/allow\"") || !strings.Contains(page, "git.example/team/app") {
		t.Fatal("viewer: wrong forms or no queue")
	}

	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	here := "/review?device=" + ds[0].ID
	w := postForm(h, "/devices/"+ds[0].ID+"/allow", url.Values{"csrf": {csrf}, "cwd": {"/home/me/scratch"}, "return": {here}}, cookie)
	editor := w.Body.String()
	if w.Code != 200 || !strings.Contains(editor, "Back to review") || hiddenValue(t, editor, "return") != here {
		t.Fatalf("allow from review: %d", w.Code)
	}
	w = postForm(h, "/profiles/default/save", url.Values{"csrf": {csrf}, "base": {hiddenValue(t, editor, "base")}, "document": {hiddenValue(t, editor, "document")}, "note": {"scratch is fine"}, "return": {here}}, cookie)
	p, _ := lake.Catalog.ProfileByName(t.Context(), config.DefaultProfile)
	want := here + "&saved=default&revision=" + strconv.FormatInt(p.Revision, 10)
	if w.Code != 303 || w.Header().Get("Location") != want {
		t.Fatalf("save: %d to %q, want %q", w.Code, w.Header().Get("Location"), want)
	}
	page := get(h, want, cookie).Body.String()
	if !strings.Contains(page, ": scratch is fine.") || !strings.Contains(section(t, page, "Allow pending (2)"), "/home/me/scratch") {
		t.Fatal("after saving, the queue lacks the notice or scratch is not pending")
	}
}

func TestReturnPathAcceptsTheReviewQueue(t *testing.T) {
	for raw, want := range map[string]string{
		"/review":                              "/review",
		"/review?tab=hidden&device=dev_abc234": "/review?device=dev_abc234&tab=hidden",
		"/review?harness=":                     "/review",
		"/review?x=1":                          "",
		"/review?device=laptop":                "",
		"/review/../devices":                   "",
	} {
		if got := returnPath(raw); got != want {
			t.Errorf("returnPath(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TKT-01M3N8FHVN: a device whose config.json sets its allow rules takes
// none from its profile, so a profile rule that matches its copy leaves
// the copy needing review, with no Allow to offer (review 1300).
func TestReviewLocalAllowRulesAreNotPending(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	if err := lake.Catalog.PutDeviceReport(t.Context(), ds[0].ID, protocol.AgentReport{AgentVersion: "v0.2.0", AllowSource: config.OriginLocal, DenySource: "none"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	page := get(h, "/review", cookie).Body.String()
	needs := section(t, page, "<h2>Needs review</h2>")
	if !strings.Contains(needs, "/work/pending") || strings.Contains(page, "Allow pending (") {
		t.Fatal("laptop's copy of /work/pending reads allow pending though its profile's allow rules do not reach it")
	}
	if strings.Contains(needs, `action="/devices/`+ds[0].ID+`/allow"`) {
		t.Fatal("Allow offered for a device whose profile rules do not reach it")
	}
	if !strings.Contains(needs, "Sets its own allow rules in config.json") {
		t.Fatal("the copy does not say why")
	}
}

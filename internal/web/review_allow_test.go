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

var hiddenInput = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)

// formFields is the hidden fields of the form in page whose action is
// action.
func formFields(t *testing.T, page, action string) url.Values {
	t.Helper()
	i := strings.Index(page, `action="`+action+`"`)
	if i < 0 {
		t.Fatalf("no form to %s", action)
	}
	form := page[i:]
	form = form[:strings.Index(form, "</form>")]
	v := url.Values{}
	for _, m := range hiddenInput.FindAllStringSubmatch(form, -1) {
		v.Add(m[1], html.UnescapeString(m[2]))
	}
	return v
}

// TKT-01M3N8FHZR: Allow selected confirms, per profile, the rules it
// adds and the devices each profile reaches, then saves every profile
// against the revision it read and returns to the queue.
func TestAllowSelectedFromTheQueue(t *testing.T) {
	lake, idp, h, _ := reviewLake(t, "readers", "admins")
	ctx := t.Context()
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	if page := get(h, "/review", cookie).Body.String(); !strings.Contains(page, `formaction="/review/allow">Allow selected…`) {
		t.Fatal("the queue offers no Allow selected")
	}

	sel := url.Values{"csrf": {csrf}, "return": {"/review"}, "key": {"git_remote git.example/team/app", "cwd /home/me/scratch", "cwd /work/pending"}}
	w := postForm(h, "/review/allow", sel, cookie)
	page := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("allow selected: %d", w.Code)
	}
	for _, want := range []string{
		`gains 1 allow rule</h2>`, `gains 2 allow rules</h2>`,
		"git_remote git.example/team/app", "cwd_prefix /home/me/scratch",
		"1 selected project is left out", "/work/pending",
		"← Back to review", "Save 2 profiles", "Allow 2 projects from review",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("confirm page lacks %q", want)
		}
	}
	// It saves nothing by itself.
	if p, _ := lake.Catalog.ProfileByName(ctx, "ci"); len(p.Config.Projects.Allow) != 0 {
		t.Fatal("the confirm page saved ci")
	}

	save := formFields(t, page, "/review/allow/save")
	if !slices.Equal(save["profile"], []string{"ci", "default"}) {
		t.Fatalf("profiles %v", save["profile"])
	}
	save.Set("note", "onboarding laptop and desk")
	w = postForm(h, "/review/allow/save", save, cookie)
	ci, _ := lake.Catalog.ProfileByName(ctx, "ci")
	def, _ := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	want := "/review?saved=ci&revision=" + strconv.FormatInt(ci.Revision, 10) + "&saved=default&revision=" + strconv.FormatInt(def.Revision, 10)
	if w.Code != 303 || w.Header().Get("Location") != want {
		t.Fatalf("save: %d to %q, want %q", w.Code, w.Header().Get("Location"), want)
	}
	if !slices.Contains(ci.Config.Projects.Allow, config.ProjectMatch{GitRemote: "git.example/team/app"}) ||
		!slices.Contains(def.Config.Projects.Allow, config.ProjectMatch{CWDPrefix: "/home/me/scratch"}) ||
		!slices.Contains(def.Config.Projects.Allow, config.ProjectMatch{GitRemote: "git.example/team/app"}) {
		t.Fatalf("saved ci %+v default %+v", ci.Config.Projects.Allow, def.Config.Projects.Allow)
	}
	back := get(h, want, cookie).Body.String()
	if strings.Count(back, ": onboarding laptop and desk.") != 2 {
		t.Fatal("the queue does not name both saved revisions")
	}
	if !strings.Contains(back, "Nothing to review") || !strings.Contains(back, "Allow pending (3)") {
		t.Fatal("the allowed projects are not allow pending")
	}
}

// TKT-01M3N8FHZR: a profile that moved after the confirm page read it
// refuses the whole save, and the page is planned again.
func TestAllowSelectedIsAllOrNothing(t *testing.T) {
	lake, idp, h, _ := reviewLake(t, "readers", "admins")
	ctx := t.Context()
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	page := postForm(h, "/review/allow", url.Values{"csrf": {csrf}, "return": {"/review"}, "key": {"git_remote git.example/team/app", "cwd /home/me/scratch"}}, cookie).Body.String()
	save := formFields(t, page, "/review/allow/save")

	// Someone saves default meanwhile.
	if _, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, []byte(`{"projects":{"allow":[{"cwd_prefix":"/work/pending"},{"cwd_prefix":"/elsewhere"}]}}`), "other", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	ciBefore, _ := lake.Catalog.ProfileByName(ctx, "ci")
	w := postForm(h, "/review/allow/save", save, cookie)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "nothing was saved") || !strings.Contains(w.Body.String(), "/elsewhere") {
		t.Fatalf("stale save: %d", w.Code)
	}
	if ci, _ := lake.Catalog.ProfileByName(ctx, "ci"); ci.Revision != ciBefore.Revision {
		t.Fatal("a refused batch saved ci")
	}
	// The page shown again saves.
	if w := postForm(h, "/review/allow/save", formFields(t, w.Body.String(), "/review/allow/save"), cookie); w.Code != 303 {
		t.Fatalf("second save: %d", w.Code)
	}

	// Nothing left to allow, a bad key, or a viewer: refused.
	if w := postForm(h, "/review/allow", url.Values{"csrf": {csrf}, "key": {"cwd /home/me/scratch"}}, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "No rule can be added") {
		t.Fatalf("nothing to allow: %d", w.Code)
	}
	if w := postForm(h, "/review/allow", url.Values{"csrf": {csrf}, "key": {"bogus"}}, cookie); w.Code != 400 {
		t.Fatalf("bad key: %d", w.Code)
	}
	if w := postForm(h, "/review/allow/save", url.Values{"csrf": {csrf}, "profile": {"default"}, "base.0": {"x"}}, cookie); w.Code != 400 {
		t.Fatalf("bad save form: %d", w.Code)
	}
	_, vidp, vh, _ := reviewLake(t, "readers")
	viewer, _ := signIn(t, vidp, vh)
	if w := postForm(vh, "/review/allow", url.Values{"csrf": {csrfOf(t, vh, viewer)}, "key": {"cwd /home/me/scratch"}}, viewer); w.Code != 404 {
		t.Fatalf("viewer: %d", w.Code)
	}
}

// TKT-01M3N8FHZR: from a device's page, Allow selected covers that
// device's copies alone and comes back to the page; each profile can be
// opened in the full editor instead.
func TestAllowSelectedFromTheDevicePage(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	dev := deviceURL(ds[0].ID) + "?show=refused"
	body := get(h, dev, cookie).Body.String()
	if !strings.Contains(body, `form="device-select" name="key" value="git_remote git.example/team/app"`) || !strings.Contains(body, `value="cwd /home/me/secret" aria-label="Select`) || !strings.Contains(body, `data-select-all="device-select"`) {
		t.Fatal("device page selection is wrong")
	}
	page := postForm(h, "/review/allow", url.Values{"csrf": {csrf}, "return": {dev}, "key": {"git_remote git.example/team/app", "cwd /home/me/secret"}}, cookie).Body.String()
	// A denied project can be selected, to hide it, and Allow leaves it out.
	if !strings.Contains(page, "1 selected project is left out") || !strings.Contains(page, "/home/me/secret") {
		t.Fatal("the denied project is not named as left out")
	}
	if strings.Contains(page, "Profile <a href=\"/profiles/ci\">ci</a>") || !strings.Contains(page, "← Back to laptop") {
		t.Fatal("allowing from laptop's page reached desk's profile, or has no Back to laptop")
	}

	// The full editor opens with the rule added and goes back to the
	// device page.
	ed := formFields(t, page, "/profiles/default/preview")
	w := postForm(h, "/profiles/default/preview", ed, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Back to laptop") || !strings.Contains(hiddenValue(t, w.Body.String(), "document"), "git.example/team/app") {
		t.Fatalf("full editor: %d", w.Code)
	}

	w = postForm(h, "/review/allow/save", formFields(t, page, "/review/allow/save"), cookie)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), dev+"&saved=default&revision=") {
		t.Fatalf("save: %d to %q", w.Code, w.Header().Get("Location"))
	}
	if ci, _ := lake.Catalog.ProfileByName(t.Context(), "ci"); len(ci.Config.Projects.Allow) != 0 {
		t.Fatal("saving from laptop's page changed ci")
	}
}

// TKT-01M3N8FHZR: the API plans and saves the same way.
func TestAllowSelectedAPI(t *testing.T) {
	lake, idp, h, _ := reviewLake(t, "readers", "admins")
	cookie, _ := signIn(t, idp, h)
	hdr := map[string]string{CSRFHeader: csrfOf(t, h, cookie)}
	w := post(h, "/api/web/v1/review/allow", `{"keys":[{"kind":"cwd","key":"/home/me/scratch"},{"kind":"cwd","key":"/nowhere"}]}`, cookie, hdr)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"default"`) || !strings.Contains(w.Body.String(), `"skipped":[{"kind":"cwd","key":"/nowhere"}]`) {
		t.Fatalf("plan: %d %s", w.Code, w.Body)
	}
	def, _ := lake.Catalog.ProfileByName(t.Context(), config.DefaultProfile)
	doc := `{"projects":{"allow":[{"cwd_prefix":"/work/pending"},{"cwd_prefix":"/home/me/scratch"}]}}`
	saveBody := `{"profiles":[{"name":"default","base_revision":` + strconv.FormatInt(def.Revision, 10) + `,"document":` + doc + `}],"note":"api"}`
	if w := post(h, "/api/web/v1/review/allow/save", saveBody, cookie, hdr); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"default"`) {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/review/allow/save", saveBody, cookie, hdr); w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"changed"`) {
		t.Fatalf("stale save: %d %s", w.Code, w.Body)
	}
	for name, c := range map[string]struct{ path, body string }{
		"no keys":     {"/api/web/v1/review/allow", `{"keys":[]}`},
		"bad device":  {"/api/web/v1/review/allow", `{"keys":[{"kind":"cwd","key":"/a"}],"device":"laptop"}`},
		"no base":     {"/api/web/v1/review/allow/save", `{"profiles":[{"name":"default","document":{}}]}`},
		"bad profile": {"/api/web/v1/review/allow/save", `{"profiles":[{"name":"default","base_revision":1,"document":{"nope":1}}]}`},
	} {
		if w := post(h, c.path, c.body, cookie, hdr); w.Code != 400 {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if w := post(h, "/api/web/v1/review/allow", `{"keys":[{"kind":"cwd","key":"/a"}]}`, cookie, nil); w.Code != 403 {
		t.Fatalf("no csrf: %d", w.Code)
	}
}

// TKT-01M3N8FHZR: a copy on a device whose config.json sets its allow
// rules gets no profile rule, since none would reach it.
func TestAllowSelectedSkipsLocalAllowDevices(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	if err := lake.Catalog.PutDeviceReport(t.Context(), ds[1].ID, protocol.AgentReport{AgentVersion: "v0.2.0", AllowSource: config.OriginLocal, DenySource: "none"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	page := postForm(h, "/review/allow", url.Values{"csrf": {csrf}, "return": {"/review"}, "key": {"git_remote git.example/team/app"}}, cookie).Body.String()
	if strings.Contains(page, `Profile <a href="/profiles/ci">ci</a>`) || !strings.Contains(page, `Profile <a href="/profiles/default">default</a>`) {
		t.Fatal("desk's local allow rules still got a rule in ci, or laptop's copy got none")
	}
	// Selecting only desk's copy leaves nothing to add.
	if w := postForm(h, "/review/allow", url.Values{"csrf": {csrf}, "return": {"/review?device=" + ds[1].ID}, "key": {"git_remote git.example/team/app"}}, cookie); w.Code != 409 {
		t.Fatalf("desk alone: %d", w.Code)
	}
}

// Review 1311: a plan one profile of which would be invalid is no plan:
// neither the confirm page nor the API offers the other profiles alone.
func TestAllowSelectedRefusesAPartialPlan(t *testing.T) {
	lake, idp, h, _ := reviewLake(t, "readers", "admins")
	var rules []string
	for i := range maxRuleRows {
		rules = append(rules, `{"cwd_prefix":"/r/`+strconv.Itoa(i)+`"}`)
	}
	// ci sorts before default and is full: adding the app's rule to it
	// is invalid.
	if _, _, err := lake.Catalog.PutProfile(t.Context(), "ci", []byte(`{"projects":{"allow":[`+strings.Join(rules, ",")+`]}}`), "test", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	w := postForm(h, "/review/allow", url.Values{"csrf": {csrf}, "return": {"/review"}, "key": {"git_remote git.example/team/app"}}, cookie)
	if w.Code != 400 || strings.Contains(w.Body.String(), "/review/allow/save") || !strings.Contains(w.Body.String(), "invalid profile") {
		t.Fatalf("partial plan: %d", w.Code)
	}
	if w := post(h, "/api/web/v1/review/allow", `{"keys":[{"kind":"git_remote","key":"git.example/team/app"}]}`, cookie, map[string]string{CSRFHeader: csrf}); w.Code != 400 || strings.Contains(w.Body.String(), `"profiles"`) {
		t.Fatalf("api partial plan: %d %s", w.Code, w.Body)
	}
}

// Review 1313: a project that needs review on one device and is denied on
// another gets a rule only in the profile of the device that needs it.
func TestAllowSelectedSkipsDeniedCopies(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	desk := protocol.InventoryProject{GitRemote: "git@git.example:team/app.git", CWD: "/src/app", CWDs: 1, Harnesses: []string{"claude"}, Sessions: 3, Reason: config.RefusedByDeny}
	if _, err := lake.Catalog.PutDeviceInventory(t.Context(), ds[1].ID, protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: time.Now().Add(time.Hour), Projects: []protocol.InventoryProject{desk}}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	page := get(h, "/review", cookie).Body.String()
	if !strings.Contains(section(t, page, "Denied ("), ">desk<") || strings.Contains(section(t, page, "<h2>Needs review</h2>"), ">desk<") {
		t.Fatal("desk's denied copy is not filed under Denied alone")
	}
	plan := postForm(h, "/review/allow", url.Values{"csrf": {csrfOf(t, h, cookie)}, "return": {"/review"}, "key": {"git_remote git.example/team/app"}}, cookie).Body.String()
	if strings.Contains(plan, `Profile <a href="/profiles/ci">ci</a>`) || !strings.Contains(plan, `Profile <a href="/profiles/default">default</a>`) {
		t.Fatal("desk's denied copy got a rule in ci, or laptop's none in default")
	}
}

// Review 1315: a save of more profiles than a notice names still gets a
// notice, for the first maxSavedRefs of them.
func TestSavedURLKeepsANoticeForLargeBatches(t *testing.T) {
	var ps []catalog.Profile
	for i := range maxSavedRefs + 8 {
		ps = append(ps, catalog.Profile{Name: "p" + strconv.Itoa(i), Revision: int64(i + 1)})
	}
	u, _ := url.Parse(savedURL("/review", ps...))
	refs, present := savedQuery(u.Query())
	if !present || len(refs) != maxSavedRefs || refs[0] != (savedRef{"p0", 1}) {
		t.Fatalf("refs %d present %v", len(refs), present)
	}
}

package web

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
)

// TKT-01M3N8FHXP: operators hide and unhide projects from the queue,
// singly or in bulk, with a note; a hide is lake-wide and audited, and
// the page says it does not stop the agent.
func TestHideAndUnhideFromTheReviewQueue(t *testing.T) {
	lake, idp, h, ds, dir := reviewLakeDir(t, "readers", "admins")
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)

	page := get(h, "/review", cookie).Body.String()
	for _, want := range []string{`form="review-select" name="key" value="git_remote git.example/team/app"`, `form="review-hide-one" name="key" value="cwd /home/me/scratch"`, "Hide selected", "the agent keeps refusing the project"} {
		if !strings.Contains(page, want) {
			t.Errorf("review page lacks %q", want)
		}
	}

	here := "/review?device=" + ds[0].ID
	w := postForm(h, "/review/hide", url.Values{"csrf": {csrf}, "key": {"git_remote git.example/team/app", "cwd /home/me/scratch"}, "note": {"not ours"}, "return": {here}}, cookie)
	if w.Code != 303 || w.Header().Get("Location") != here {
		t.Fatalf("hide: %d to %q", w.Code, w.Header().Get("Location"))
	}
	hides, err := lake.Catalog.HiddenProjects(t.Context())
	if err != nil || len(hides) != 2 || hides[0].Note != "not ours" || !strings.HasPrefix(hides[0].By, "web:") {
		t.Fatalf("hides %+v %v", hides, err)
	}
	// Lake-wide: the app is gone from desk's slice of the queue too.
	if s := section(t, get(h, "/review?device="+ds[1].ID, cookie).Body.String(), "<h2>Needs review</h2>"); strings.Contains(s, "git.example/team/app") {
		t.Fatal("a hide on laptop's slice left the app on desk's")
	}
	raw, err := os.ReadFile(audit.Path(dir))
	if err != nil || strings.Count(string(raw), `"kind":"project.hidden"`) != 2 || !strings.Contains(string(raw), "note=not ours") {
		t.Fatalf("audit: %v\n%s", err, raw)
	}

	hidden := get(h, "/review?tab=hidden", cookie).Body.String()
	if !strings.Contains(hidden, `form="hidden-unhide-one" name="key" value="cwd /home/me/scratch"`) || !strings.Contains(hidden, "Unhide selected") {
		t.Fatal("Hidden tab offers no Unhide")
	}
	w = postForm(h, "/review/unhide", url.Values{"csrf": {csrf}, "key": {"cwd /home/me/scratch"}, "return": {"/review?tab=hidden"}}, cookie)
	if w.Code != 303 || w.Header().Get("Location") != "/review?tab=hidden" {
		t.Fatalf("unhide: %d to %q", w.Code, w.Header().Get("Location"))
	}
	if hides, _ := lake.Catalog.HiddenProjects(t.Context()); len(hides) != 1 || hides[0].Key.Key != "git.example/team/app" {
		t.Fatalf("after unhide %+v", hides)
	}

	// Refusals show the queue again with the problem, and change nothing.
	for name, v := range map[string]url.Values{
		"no key":    {},
		"bad key":   {"key": {"cwd_prefix /x"}},
		"long note": {"key": {"cwd /x"}, "note": {strings.Repeat("n", maxProfileNote+1)}},
	} {
		v.Set("csrf", csrf)
		if w := postForm(h, "/review/hide", v, cookie); w.Code != 400 || !strings.Contains(w.Body.String(), `role="alert"`) {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := postForm(h, "/review/unhide", url.Values{"csrf": {csrf}, "key": {"cwd /x"}, "note": {"why"}}, cookie); w.Code != 400 {
		t.Errorf("unhide with a note: %d", w.Code)
	}
	if w := postForm(h, "/review/hide", url.Values{"csrf": {"wrong"}, "key": {"cwd /x"}}, cookie); w.Code != 403 {
		t.Errorf("wrong csrf: %d", w.Code)
	}
	if w := postForm(h, "/review/purge", url.Values{"csrf": {csrf}, "key": {"cwd /x"}}, cookie); w.Code != 404 {
		t.Errorf("unknown action: %d", w.Code)
	}
	if hides, _ := lake.Catalog.HiddenProjects(t.Context()); len(hides) != 1 {
		t.Fatalf("a refused request changed hides: %+v", hides)
	}

	// A viewer sees no forms and cannot post.
	_, vidp, vh, _ := reviewLake(t, "readers")
	viewer, _ := signIn(t, vidp, vh)
	if page := get(vh, "/review", viewer).Body.String(); strings.Contains(page, "review-select") || strings.Contains(page, "/review/hide") {
		t.Fatal("viewer sees hide forms")
	}
	if w := postForm(vh, "/review/hide", url.Values{"csrf": {csrfOf(t, vh, viewer)}, "key": {"cwd /x"}}, viewer); w.Code != 404 {
		t.Fatalf("viewer hide: %d", w.Code)
	}
}

// TKT-01M3N8FHXP: a device's refused projects offer Hide from review,
// which comes back to the device page, and a hidden one says so and
// offers Unhide.
func TestHideFromTheDevicePage(t *testing.T) {
	lake, idp, h, ds := reviewLake(t, "readers", "admins")
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	page := deviceURL(ds[0].ID) + "?show=refused"
	body := get(h, page, cookie).Body.String()
	if !strings.Contains(body, `name="key" value="cwd /home/me/scratch"`) || !strings.Contains(body, "Hide from review") {
		t.Fatal("device page offers no Hide")
	}
	w := postForm(h, "/review/hide", url.Values{"csrf": {csrf}, "key": {"cwd /home/me/scratch"}, "return": {page}}, cookie)
	if w.Code != 303 || w.Header().Get("Location") != page {
		t.Fatalf("hide: %d to %q", w.Code, w.Header().Get("Location"))
	}
	body = get(h, page, cookie).Body.String()
	if !strings.Contains(body, `<span class="badge">hidden</span>`) || !strings.Contains(body, `action="/review/unhide"`) {
		t.Fatal("device page does not show the hide")
	}
	if _, err := lake.Catalog.UnhideProjects(t.Context(), []catalog.ProjectKey{{Kind: catalog.KeyCWD, Key: "/home/me/scratch"}}, "t", time.Now()); err != nil {
		t.Fatal(err)
	}
	// A refused hide from the device page shows the device page.
	if w := postForm(h, "/review/hide", url.Values{"csrf": {csrf}, "key": {"nope"}, "return": {page}}, cookie); w.Code != 400 || !strings.Contains(w.Body.String(), "Projects on this machine") {
		t.Fatalf("refused hide from device page: %d", w.Code)
	}
}

// TKT-01M3N8FHXP: the browser API hides and unhides a list of keys.
func TestHideAPI(t *testing.T) {
	_, idp, h, _ := reviewLake(t, "readers", "admins")
	cookie, _ := signIn(t, idp, h)
	hdr := map[string]string{CSRFHeader: csrfOf(t, h, cookie)}
	body := `{"keys":[{"kind":"cwd","key":"/home/me/scratch"}],"note":"meh"}`
	if w := post(h, "/api/web/v1/review/hide", body, cookie, hdr); w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":[{"kind":"cwd","key":"/home/me/scratch"}]`) {
		t.Fatalf("hide: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/review/hide", body, cookie, hdr); w.Code != 200 || !strings.Contains(w.Body.String(), `"changed":[]`) {
		t.Fatalf("hide again: %d %s", w.Code, w.Body)
	}
	for name, c := range map[string]struct {
		path, body string
		code       int
	}{
		"bad kind":        {"/api/web/v1/review/hide", `{"keys":[{"kind":"x","key":"/a"}]}`, 400},
		"no keys":         {"/api/web/v1/review/hide", `{"keys":[]}`, 400},
		"unknown field":   {"/api/web/v1/review/hide", `{"keys":[{"kind":"cwd","key":"/a"}],"why":1}`, 400},
		"unhide and note": {"/api/web/v1/review/unhide", `{"keys":[{"kind":"cwd","key":"/a"}],"note":"x"}`, 400},
		"unknown action":  {"/api/web/v1/review/drop", `{"keys":[{"kind":"cwd","key":"/a"}]}`, 404},
	} {
		if w := post(h, c.path, c.body, cookie, hdr); w.Code != c.code {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if w := post(h, "/api/web/v1/review/unhide", `{"keys":[{"kind":"cwd","key":"/home/me/scratch"}]}`, cookie, hdr); w.Code != 200 || !strings.Contains(w.Body.String(), "/home/me/scratch") {
		t.Fatalf("unhide: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/review/hide", body, cookie, nil); w.Code != 403 {
		t.Fatalf("no csrf: %d", w.Code)
	}
}

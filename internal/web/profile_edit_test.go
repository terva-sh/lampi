package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

var documentField = regexp.MustCompile(`name="document" value="([^"]*)"`)

// editForm is the editor's form for base with the given fields set.
func editForm(csrf string, base string, fields map[string]string) url.Values {
	f := url.Values{"csrf": {csrf}, "base": {base}, "allow_rows": {"3"}, "deny_rows": {"3"}}
	for k, v := range fields {
		f.Set(k, v)
	}
	return f
}

// TKT-01M3M7M0ZY: an operator creates a profile, previews a change with
// its diff and the devices it reaches, and saves it with the revision
// they read; a stale save and a bad field are refused.
func TestOperatorEditsAProfile(t *testing.T) {
	lake, idp, h, dir := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)

	w := postForm(h, "/profiles", url.Values{"csrf": {csrf}, "name": {"team"}}, cookie)
	if w.Code != 303 || w.Header().Get("Location") != "/profiles/team/edit" {
		t.Fatalf("create: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := postForm(h, "/profiles", url.Values{"csrf": {csrf}, "name": {"Bad Name"}}, cookie); w.Code != 400 || !strings.Contains(w.Body.String(), "lowercase letters") {
		t.Fatalf("bad name: %d", w.Code)
	}
	if page := get(h, "/profiles/team/edit", cookie); page.Code != 200 || !strings.Contains(page.Body.String(), `name="allow_rows" value="3"`) {
		t.Fatalf("edit page: %d", page.Code)
	}

	fields := map[string]string{
		"allow.0.git_remote": "git@GitHub.com:acme/app.git", "allow.1.cwd_prefix": " /work ",
		"deny.2.git_remote_prefix": "https://github.com/acme/secret/", "harness.codex": "off", "debounce": "5s", "note": "start",
	}
	w = postForm(h, "/profiles/team/preview", editForm(csrf, "0", fields), cookie)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, body)
	}
	for _, want := range []string{`value="github.com/acme/app"`, `value="/work"`, `value="github.com/acme/secret"`, "diff-add", "Save team", "projects.allow, projects.deny, harnesses, agent"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview missing %q", want)
		}
	}
	m := documentField.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no document to save")
	}
	doc := html.UnescapeString(m[1])

	if w := postForm(h, "/profiles/team/preview", editForm(csrf, "0", map[string]string{"debounce": "soon"}), cookie); w.Code != 400 || !strings.Contains(w.Body.String(), "debounce") {
		t.Errorf("bad debounce: %d", w.Code)
	}
	if w := postForm(h, "/profiles/team/preview", editForm("wrong", "0", nil), cookie); w.Code != 403 {
		t.Errorf("wrong csrf: %d", w.Code)
	}

	w = postForm(h, "/profiles/team/save", url.Values{"csrf": {csrf}, "base": {"0"}, "document": {doc}, "note": {"start"}}, cookie)
	if w.Code != 303 || w.Header().Get("Location") != "/profiles/team" {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	saved, err := lake.Catalog.ProfileByName(ctx, "team")
	if err != nil || len(saved.Config.Projects.Allow) != 2 || saved.Config.Projects.Allow[0].GitRemote != "github.com/acme/app" || !strings.HasPrefix(saved.UpdatedBy, "web:") {
		t.Fatalf("saved %+v %v", saved, err)
	}
	// A second editor who opened the new profile before the save loses.
	w = postForm(h, "/profiles/team/save", url.Values{"csrf": {csrf}, "base": {"0"}, "document": {`{}`}}, cookie)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Someone saved or deleted this profile") {
		t.Fatalf("stale save: %d", w.Code)
	}
	// Whatever the form sends is checked again on save.
	if w := postForm(h, "/profiles/team/save", url.Values{"csrf": {csrf}, "base": {"1"}, "document": {`{"harnesses":{"claude":{"enabled":true,"root":"/x"}}}`}}, cookie); w.Code != 400 {
		t.Fatalf("root on save: %d", w.Code)
	}

	// A device on the profile whose config.json sets its allow rules is
	// warned about when the allow rules change.
	created, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "runner"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.SetDeviceProfile(ctx, "runner", "team", "team", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.PutDeviceReport(ctx, created[0].ID, protocol.AgentReport{AllowSource: "local"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	rev := strconvI(saved.Revision)
	body = postForm(h, "/profiles/team/preview", editForm(csrf, rev, map[string]string{"allow.0.cwd_prefix": "/elsewhere"}), cookie).Body.String()
	if !strings.Contains(body, "reaches 1 active device: runner") || !strings.Contains(body, "own allow rules in config.json") {
		t.Errorf("preview does not name the device or the local allow rules")
	}

	// The API saves a whole document against a revision.
	hdr := map[string]string{CSRFHeader: csrf}
	api := "/api/web/v1/profiles/team"
	if w := put(h, api, `{"document":{"agent":{"debounce":"9s"}},"base_revision":`+rev+`,"note":"api"}`, cookie, hdr); w.Code != 200 || !strings.Contains(w.Body.String(), `"revision"`) {
		t.Fatalf("api put: %d %s", w.Code, w.Body)
	}
	for body, want := range map[string]string{
		`{"document":{},"base_revision":` + rev + `}`:                        "changed",
		`{"document":{"redaction":{"upload_hits":true}},"base_revision":99}`: "invalid_profile",
		`{"document":{}}`:                             "invalid_request",
		`{"document":[],"base_revision":0}`:           "invalid_request",
		`{"document":{},"base_revision":0,"extra":1}`: "invalid_request",
	} {
		if w := put(h, api, body, cookie, hdr); w.Code < 400 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	cur, _ := lake.Catalog.ProfileByName(ctx, "team")
	curRev := strconvI(cur.Revision)
	if w := del(h, api, `{"base_revision":`+curRev+`}`, cookie, hdr); w.Code != 409 || !strings.Contains(w.Body.String(), "in_use") {
		t.Fatalf("delete in use: %d %s", w.Code, w.Body)
	}
	if _, err := lake.Catalog.RevokeDevice(ctx, "runner", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	w = postForm(h, "/profiles/team/delete", url.Values{"csrf": {csrf}, "base": {curRev}, "note": {"retired"}}, cookie)
	if w.Code != 303 || w.Header().Get("Location") != "/profiles" {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if w := get(h, "/profiles/team", cookie); w.Code != 404 {
		t.Fatalf("deleted profile: %d", w.Code)
	}

	log, err := os.ReadFile(filepath.Join(dir, audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kind":"profile.put","actor":"web:`, "changed=new", "changed=projects.allow,projects.deny,harnesses,agent", `"kind":"profile.delete","actor":"web:`} {
		if !strings.Contains(string(log), want) {
			t.Errorf("audit log missing %q:\n%s", want, log)
		}
	}
}

func TestViewersCannotEditProfiles(t *testing.T) {
	_, idp, h, _ := operatorLake(t, "", "readers")
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	if body := get(h, "/profiles/ci", cookie).Body.String(); strings.Contains(body, "Edit profile") || strings.Contains(body, "/delete") {
		t.Fatal("viewer sees the editor's controls")
	}
	if body := get(h, "/profiles", cookie).Body.String(); strings.Contains(body, "New profile") {
		t.Fatal("viewer sees the new profile form")
	}
	for _, code := range []int{
		get(h, "/profiles/ci/edit", cookie).Code,
		postForm(h, "/profiles", url.Values{"csrf": {csrf}, "name": {"x"}}, cookie).Code,
		postForm(h, "/profiles/ci/preview", editForm(csrf, "1", nil), cookie).Code,
		postForm(h, "/profiles/ci/save", url.Values{"csrf": {csrf}, "base": {"1"}, "document": {"{}"}}, cookie).Code,
		postForm(h, "/profiles/ci/delete", url.Values{"csrf": {csrf}, "base": {"1"}}, cookie).Code,
		put(h, "/api/web/v1/profiles/ci", `{"document":{},"base_revision":1}`, cookie, map[string]string{CSRFHeader: csrf}).Code,
		del(h, "/api/web/v1/profiles/ci", `{"base_revision":1}`, cookie, map[string]string{CSRFHeader: csrf}).Code,
	} {
		if code != 404 {
			t.Errorf("viewer write: %d", code)
		}
	}
}

func TestLineDiff(t *testing.T) {
	got := lineDiff([]string{"a", "b", "c"}, []string{"a", "x", "c", "d"})
	var s []string
	for _, l := range got {
		s = append(s, l.Op+l.Text)
	}
	if strings.Join(s, ",") != " a,-b,+x, c,+d" {
		t.Fatalf("diff %v", s)
	}
}

func strconvI(n int64) string { return strconv.FormatInt(n, 10) }

func put(h http.Handler, path, body string, c *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	return send(h, "PUT", path, body, c, headers)
}

func del(h http.Handler, path, body string, c *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	return send(h, "DELETE", path, body, c, headers)
}

func send(h http.Handler, method, path, body string, c *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://lake.example"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

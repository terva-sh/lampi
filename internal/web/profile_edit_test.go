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
	for _, want := range []string{`value="github.com/acme/app"`, `value="/work"`, `value="github.com/acme/secret"`, "diff-add", "Save team", "Creates <strong>team</strong>"} {
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
	// Recreating it names the deletion as the revision read.
	w = get(h, api, cookie)
	m = regexp.MustCompile(`"latest_revision":(\d+)`).FindStringSubmatch(w.Body.String())
	if w.Code != 404 || m == nil || m[1] == "0" {
		t.Fatalf("deleted profile api: %d %s", w.Code, w.Body)
	}
	if w := put(h, api, `{"document":{},"base_revision":0}`, cookie, hdr); w.Code != 409 {
		t.Fatalf("recreate from a stale read: %d", w.Code)
	}
	if w := put(h, api, `{"document":{},"base_revision":`+m[1]+`}`, cookie, hdr); w.Code != 200 {
		t.Fatalf("recreate: %d %s", w.Code, w.Body)
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

// Review of #101: an empty new profile can be saved, a full rule list
// still posts, and a delete that lost to another delete is a conflict.
func TestProfileEditorEdges(t *testing.T) {
	lake, idp, h, dir := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	body := postForm(h, "/profiles/empty/preview", editForm(csrf, "0", nil), cookie).Body.String()
	if !strings.Contains(body, "Creates <strong>empty</strong>") || !strings.Contains(body, "Save empty") {
		t.Fatal("an empty new profile cannot be saved")
	}

	rules := make([]string, maxRuleRows)
	for i := range rules {
		rules[i] = `{"cwd_prefix":"/w/` + strconv.Itoa(i) + `"}`
	}
	if _, _, err := lake.Catalog.PutProfile(ctx, "big", []byte(`{"projects":{"allow":[`+strings.Join(rules, ",")+`]}}`), "op", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	page := get(h, "/profiles/big/edit", cookie).Body.String()
	if !strings.Contains(page, `name="allow_rows" value="500"`) {
		t.Fatal("editor offers blank rows past the rule limit")
	}
	// A save past the limit is refused with the limit named, from the API
	// and from a form posting a profile stored some other way.
	over := `{"document":{"projects":{"allow":[` + strings.Join(rules, ",") + `,{"cwd_prefix":"/x"}]}},"base_revision":0}`
	if w := put(h, "/api/web/v1/profiles/huge", over, cookie, map[string]string{CSRFHeader: csrf}); w.Code != 400 || !strings.Contains(w.Body.String(), "at most 500") {
		t.Fatalf("api over the limit: %d %s", w.Code, w.Body)
	}
	f := editForm(csrf, "0", nil)
	f.Set("allow_rows", "501")
	for i := 0; i < 501; i++ {
		f.Set("allow."+strconv.Itoa(i)+".cwd_prefix", "/w/"+strconv.Itoa(i))
	}
	if w := postForm(h, "/profiles/huge/preview", f, cookie); w.Code != 400 || !strings.Contains(w.Body.String(), "at most 500") {
		t.Fatalf("form over the limit: %d", w.Code)
	}

	p, _, err := lake.Catalog.PutProfile(ctx, "gone", []byte(`{}`), "op", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.DeleteProfile(ctx, "gone", "op", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := del(h, "/api/web/v1/profiles/Bad!", `{"base_revision":3}`, cookie, map[string]string{CSRFHeader: csrf}); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_name") {
		t.Fatalf("delete a bad name: %d %s", w.Code, w.Body)
	}
	w := del(h, "/api/web/v1/profiles/gone", `{"base_revision":`+strconvI(p.Revision)+`}`, cookie, map[string]string{CSRFHeader: csrf})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "changed") {
		t.Fatalf("delete after a delete: %d %s", w.Code, w.Body)
	}

	// A delete whose audit line cannot be written stands, and says so.
	q, _, err := lake.Catalog.PutProfile(ctx, "quiet", []byte(`{}`), "op", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, audit.FileName)
	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o700); err != nil {
		t.Fatal(err)
	}
	w = postForm(h, "/profiles/quiet/delete", url.Values{"csrf": {csrf}, "base": {strconvI(q.Revision)}}, cookie)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "The profile is deleted, but writing it to the audit log failed") {
		t.Fatalf("audit failure: %d", w.Code)
	}
	if _, err := lake.Catalog.ProfileByName(ctx, "quiet"); err == nil {
		t.Fatal("the delete did not stand")
	}
}

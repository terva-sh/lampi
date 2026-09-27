package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func postForm(h http.Handler, path string, form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://lake.example"+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://lake.example")
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

var attemptField = regexp.MustCompile(`name="attempt" value="([0-9a-f]{32})"`)

// attemptOf reads a fresh attempt token from the mint form. A stale
// sign-in has no form, so it makes one up.
func attemptOf(t *testing.T, h http.Handler, c *http.Cookie) string {
	t.Helper()
	if m := attemptField.FindStringSubmatch(get(h, "/admin/registrations", c).Body.String()); m != nil {
		return m[1]
	}
	return newAttempt()
}

func TestRegistrationPagesAreOperatorOnly(t *testing.T) {
	_, idp, h, _ := operatorLake(t, "", "readers")
	cookie, _ := signIn(t, idp, h)
	if w := get(h, "/admin/registrations", cookie); w.Code != 404 {
		t.Fatalf("viewer page: %d", w.Code)
	}
	if strings.Contains(get(h, "/", cookie).Body.String(), "/admin/registrations") {
		t.Fatal("viewer sees the registrations link")
	}
	csrf := csrfOf(t, h, cookie)
	for _, path := range []string{"/admin/registrations", "/admin/registrations/reg_x/revoke"} {
		if w := postForm(h, path, url.Values{"csrf": {csrf}, "name": {"box"}}, cookie); w.Code != 404 {
			t.Fatalf("viewer POST %s: %d", path, w.Code)
		}
	}
	if w := get(h, "/admin/registrations", nil); w.Code != 303 {
		t.Fatalf("signed out: %d", w.Code)
	}
}

func TestOperatorMintsOnThePage(t *testing.T) {
	_, idp, h, _ := operatorLake(t, "v0.1.1", "admins")
	idp.AuthTime = time.Now().Add(-time.Hour)
	stale, _ := signIn(t, idp, h)
	page := get(h, "/admin/registrations", stale)
	body := page.Body.String()
	if page.Code != 200 || !strings.Contains(body, "Sign in again to mint") || !strings.Contains(body, "fresh=1") || strings.Contains(body, `name="expires"`) {
		t.Fatalf("stale page: %d\n%s", page.Code, body)
	}
	if !strings.Contains(get(h, "/", stale).Body.String(), `href="/admin/registrations"`) {
		t.Fatal("operator has no registrations link")
	}
	w := postForm(h, "/admin/registrations", url.Values{"attempt": {attemptOf(t, h, stale)}, "csrf": {csrfOf(t, h, stale)}, "name": {"box"}, "expires": {"1h"}}, stale)
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "fresh=1") {
		t.Fatalf("stale mint: %d %s", w.Code, w.Header().Get("Location"))
	}

	idp.AuthTime = time.Now()
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	if body := get(h, "/admin/registrations", cookie).Body.String(); !strings.Contains(body, `name="expires"`) || !strings.Contains(body, `<option value="ci"`) {
		t.Fatalf("fresh page has no mint form:\n%s", body)
	}
	if w := postForm(h, "/admin/registrations", url.Values{"csrf": {"wrong"}, "name": {"box"}, "expires": {"1h"}}, cookie); w.Code != 403 {
		t.Fatalf("bad csrf: %d", w.Code)
	}
	w = postForm(h, "/admin/registrations", url.Values{"attempt": {attemptOf(t, h, cookie)}, "csrf": {csrf}, "name": {"Bad Name"}, "expires": {"1h"}}, cookie)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "A device name is lowercase") || !strings.Contains(w.Body.String(), `value="Bad Name"`) {
		t.Fatalf("bad name: %d\n%s", w.Code, w.Body)
	}
	if w := postForm(h, "/admin/registrations", url.Values{"attempt": {attemptOf(t, h, cookie)}, "csrf": {csrf}, "name": {"box"}, "expires": {"90m"}}, cookie); w.Code != 400 {
		t.Fatalf("an expiry the form does not offer: %d", w.Code)
	}

	mintAttempt := attemptOf(t, h, cookie)
	w = postForm(h, "/admin/registrations", url.Values{"attempt": {mintAttempt}, "csrf": {csrf}, "name": {"box"}, "profile": {"ci"}, "expires": {"24h"}}, cookie)
	body = html.UnescapeString(w.Body.String())
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("mint: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	i := strings.Index(body, `id="code-only">`)
	if i < 0 {
		t.Fatalf("no code on the page:\n%s", body)
	}
	code := body[i+len(`id="code-only">`):]
	code = code[:strings.Index(code, "<")]
	if !strings.Contains(body, "TERVA_LAMPI_CODE='"+code+"' sh -s -- --version v0.1.1 --register --fingerprint SHA256:") || !strings.Contains(body, `id="install-line"> curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/v0.1.1/install.sh`) {
		t.Fatalf("install line:\n%s", body)
	}
	if strings.Contains(body, "installs the latest release") {
		t.Fatal("pinned lake says it installs the latest release")
	}

	// Reloading the minted page sends the same form again. It mints
	// nothing, does not show the code, and names the code it made.
	again := postForm(h, "/admin/registrations", url.Values{"attempt": {mintAttempt}, "csrf": {csrf}, "name": {"box"}, "profile": {"ci"}, "expires": {"24h"}}, cookie)
	if again.Code != 409 || strings.Contains(again.Body.String(), code) || !strings.Contains(again.Body.String(), "already minted reg_") {
		t.Fatalf("resubmitted mint: %d\n%s", again.Code, again.Body)
	}
	if w := postForm(h, "/admin/registrations", url.Values{"csrf": {csrf}, "name": {"other"}, "expires": {"1h"}}, cookie); w.Code != 409 {
		t.Fatalf("mint without an attempt: %d", w.Code)
	}

	list := get(h, "/admin/registrations", cookie).Body.String()
	if strings.Contains(list, code) || !strings.Contains(list, "web:synthetic-viewer (Lake viewer)") {
		t.Fatalf("list after mint:\n%s", list)
	}
	id := list[strings.Index(list, "/admin/registrations/reg_")+len("/admin/registrations/"):]
	id = id[:strings.Index(id, "/")]
	if w := postForm(h, "/admin/registrations/"+id+"/revoke", url.Values{"csrf": {"wrong"}}, cookie); w.Code != 403 {
		t.Fatalf("cancel with bad csrf: %d", w.Code)
	}
	if w := postForm(h, "/admin/registrations/"+id+"/revoke", url.Values{"csrf": {csrf}}, cookie); w.Code != 303 {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	list = get(h, "/admin/registrations", cookie).Body.String()
	if strings.Contains(list, "/revoke") || !strings.Contains(list, `<span class="badge revoked">revoked</span>`) {
		t.Fatalf("list after cancel:\n%s", list)
	}
	// A second cancel of the same code lands on the list.
	if w := postForm(h, "/admin/registrations/"+id+"/revoke", url.Values{"csrf": {csrf}}, cookie); w.Code != 303 {
		t.Fatalf("second cancel: %d", w.Code)
	}
}

func TestUnpinnedLakeSaysItInstallsTheLatest(t *testing.T) {
	_, idp, h, _ := operatorLake(t, "", "admins")
	idp.AuthTime = time.Now()
	cookie, _ := signIn(t, idp, h)
	w := postForm(h, "/admin/registrations", url.Values{"attempt": {attemptOf(t, h, cookie)}, "csrf": {csrfOf(t, h, cookie)}, "name": {"box"}, "expires": {"1h"}}, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "installs the latest release") || !strings.Contains(w.Body.String(), "lampi/main/install.sh") {
		t.Fatalf("unpinned mint: %d\n%s", w.Code, w.Body)
	}
}

package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
)

var mintedToken = regexp.MustCompile(`id="token-only">(lrt_[A-Za-z0-9_-]+)<`)

// mintReadToken mints a token through the admin page and returns it.
func mintReadToken(t *testing.T, h http.Handler, admin *http.Cookie, label, sessions string) string {
	t.Helper()
	page := get(h, adminReadTokensPath, admin).Body.String()
	a := attemptField.FindStringSubmatch(page)
	if a == nil {
		t.Fatal("no mint form")
	}
	w := postForm(h, adminReadTokensPath, url.Values{"csrf": {csrfOf(t, h, admin)}, "attempt": {a[1]}, "label": {label}, "sessions": {sessions}, "expires": {"24h"}}, admin)
	m := mintedToken.FindStringSubmatch(w.Body.String())
	if w.Code != 200 || m == nil {
		t.Fatalf("mint %d: %s", w.Code, w.Body.String())
	}
	return m[1]
}

func bearer(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://lake.example"+path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TKT-01M3NM6FW7: the token pages are the admin's. Operators and
// viewers get 404 and no link.
func TestReadTokenPagesAreAdminOnly(t *testing.T) {
	_, idp, h, _ := rawLake(t, nil)
	for _, group := range []string{"readers", "ops"} {
		c := signInAs(t, idp, h, group)
		if w := get(h, adminReadTokensPath, c); w.Code != 404 {
			t.Errorf("%s page: %d", group, w.Code)
		}
		if w := postForm(h, adminReadTokensPath, url.Values{"csrf": {csrfOf(t, h, c)}, "label": {"x"}, "expires": {"24h"}}, c); w.Code != 404 {
			t.Errorf("%s mint: %d", group, w.Code)
		}
		if strings.Contains(get(h, "/", c).Body.String(), adminReadTokensPath) {
			t.Errorf("%s sees the link", group)
		}
	}
	admin := signInAs(t, idp, h, "owners")
	if !strings.Contains(get(h, "/", admin).Body.String(), `href="`+adminReadTokensPath+`"`) {
		t.Fatal("admin has no link")
	}
}

// Minting adds access, so it needs a recent sign-in.
func TestMintReadTokenNeedsFreshSignIn(t *testing.T) {
	_, idp, h, _ := rawLake(t, nil)
	idp.AuthTime = time.Now().Add(-time.Hour)
	admin := signInAs(t, idp, h, "owners")
	if strings.Contains(get(h, adminReadTokensPath, admin).Body.String(), `name="label"`) {
		t.Fatal("stale sign-in offered the form")
	}
	w := postForm(h, adminReadTokensPath, url.Values{"csrf": {csrfOf(t, h, admin)}, "attempt": {"none"}, "label": {"x"}, "expires": {"24h"}}, admin)
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "fresh=1") {
		t.Fatalf("stale mint: %d %v", w.Code, w.Header())
	}
}

// A token is shown once, reads raw bytes, is recorded in the audit log
// at every step without its secret, and stops working when revoked.
func TestReadTokenLifecycle(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	body := []byte(`{"synthetic":"raw by token"}` + "\n")
	uid, digest := storeSession(t, lake, "tok-a", body)
	admin := signInAs(t, idp, h, "owners")
	tok := mintReadToken(t, h, admin, "nightly import", "")
	if strings.Contains(get(h, adminReadTokensPath, admin).Body.String(), tok) {
		t.Fatal("token shown again")
	}

	w := bearer(h, "GET", rawTokenPath(uid, digest), tok)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), body) || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("token read %d %q", w.Code, w.Body.String())
	}
	tokens, err := lake.Catalog.ReadTokens(t.Context())
	if err != nil || len(tokens) != 1 || tokens[0].LastUsed.IsZero() {
		t.Fatalf("tokens %+v %v", tokens, err)
	}
	id := tokens[0].ID

	w = postForm(h, adminReadTokensPath+"/"+id+"/revoke", url.Values{"csrf": {csrfOf(t, h, admin)}}, admin)
	if w.Code != 303 {
		t.Fatalf("revoke %d", w.Code)
	}
	if w := bearer(h, "GET", rawTokenPath(uid, digest), tok); w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("revoked token: %d", w.Code)
	}

	log, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	s := string(log)
	for _, want := range []string{`"kind":"read_token.created"`, `"kind":"read_token.revoked"`, `"actor":"token:` + id + ` (nightly import)"`, "read_token=" + id} {
		if !strings.Contains(s, want) {
			t.Errorf("audit lacks %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, tok) || strings.Contains(s, strings.TrimPrefix(tok, ReadTokenPrefix)) || strings.Contains(s, "raw by token") {
		t.Fatal("audit holds the secret or the content")
	}
}

// A token scoped to sessions reads those and gets 404 for the rest; an
// unknown session refuses the mint.
func TestReadTokenScope(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	mine, mineDigest := storeSession(t, lake, "tok-mine", []byte("mine\n"))
	other, otherDigest := storeSession(t, lake, "tok-other", []byte("other\n"))
	admin := signInAs(t, idp, h, "owners")
	tok := mintReadToken(t, h, admin, "one session", mine)
	if w := bearer(h, "GET", rawTokenPath(mine, mineDigest), tok); w.Code != 200 {
		t.Fatalf("in scope: %d", w.Code)
	}
	if w := bearer(h, "GET", rawTokenPath(other, otherDigest), tok); w.Code != 404 {
		t.Fatalf("out of scope: %d", w.Code)
	}
	if w := bearer(h, "GET", rawTokenPath(mine, otherDigest), tok); w.Code != 404 {
		t.Fatalf("foreign digest: %d", w.Code)
	}

	a := attemptField.FindStringSubmatch(get(h, adminReadTokensPath, admin).Body.String())
	w := postForm(h, adminReadTokensPath, url.Values{"csrf": {csrfOf(t, h, admin)}, "attempt": {a[1]}, "label": {"typo"}, "sessions": {mine + " 01NOSUCHSESSION"}, "expires": {"24h"}}, admin)
	if w.Code != 400 || mintedToken.MatchString(w.Body.String()) || !strings.Contains(w.Body.String(), "not in the lake") {
		t.Fatalf("unknown session mint: %d", w.Code)
	}
	for _, bad := range []url.Values{{"label": {""}, "expires": {"24h"}}, {"label": {"x"}, "expires": {"8760h"}}, {"label": {"separators"}, "sessions": {" , ,\n"}, "expires": {"24h"}}} {
		a := attemptField.FindStringSubmatch(get(h, adminReadTokensPath, admin).Body.String())
		bad.Set("csrf", csrfOf(t, h, admin))
		bad.Set("attempt", a[1])
		if w := postForm(h, adminReadTokensPath, bad, admin); w.Code != 400 || mintedToken.MatchString(w.Body.String()) {
			t.Errorf("mint %v: %d", bad, w.Code)
		}
	}
	// review 1331 finding-1: none of those minted, so one token exists.
	if tokens, err := lake.Catalog.ReadTokens(t.Context()); err != nil || len(tokens) != 1 {
		t.Fatalf("tokens after refused mints: %d %v", len(tokens), err)
	}
}

// An expired token is refused like an unknown one.
func TestExpiredReadTokenIsRefused(t *testing.T) {
	lake, _, h, _ := rawLake(t, nil)
	uid, digest := storeSession(t, lake, "tok-old", []byte("old\n"))
	secret := ReadTokenPrefix + "synthetic-expired-token"
	past := time.Now().Add(-48 * time.Hour)
	if _, err := lake.Catalog.CreateReadToken(t.Context(), catalog.ReadToken{Label: "old", Permissions: []string{catalog.PermRawRead}, CreatedBy: "test", Expires: past.Add(time.Hour)}, hashReadToken(secret), past); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{secret, ReadTokenPrefix + "never-minted", "", "not-a-read-token"} {
		if w := bearer(h, "GET", rawTokenPath(uid, digest), tok); w.Code != 401 {
			t.Errorf("token %q: %d", tok, w.Code)
		}
	}
}

// A read token reads the raw route and nothing else, and a device token
// or a browser session does not read the token route.
func TestReadTokenIsNotAnyOtherCredential(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	device := strings.Repeat("d", 64)
	lake.Allow(device)
	uid, digest := storeSession(t, lake, "tok-sep", []byte("separate\n"))
	admin := signInAs(t, idp, h, "owners")
	tok := mintReadToken(t, h, admin, "separation", "")

	if w := bearer(h, "GET", rawTokenPath(uid, digest), device); w.Code != 401 {
		t.Errorf("device token on the raw route: %d", w.Code)
	}
	if w := get(h, rawTokenPath(uid, digest), admin); w.Code != 401 {
		t.Errorf("browser session on the raw route: %d", w.Code)
	}
	for _, path := range []string{"/api/web/v1/sessions", "/api/web/v1/sessions/" + uid} {
		if w := bearer(h, "GET", path, tok); w.Code != 401 {
			t.Errorf("read token on %s: %d", path, w.Code)
		}
	}
	if w := bearer(h, "GET", rawPath(uid)+"/"+digest, tok); w.Code == 200 {
		t.Error("read token on the browser raw route")
	}
	r := httptest.NewRequest("POST", "https://lake.example/v1/hello", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Errorf("read token on /v1: %d", w.Code)
	}
}

// breakAuditLog puts a directory where audit.jsonl goes, so a flush
// fails while the outbox still queues.
func breakAuditLog(t *testing.T, dir string) {
	t.Helper()
	if err := os.RemoveAll(audit.Path(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(audit.Path(dir), 0o700); err != nil {
		t.Fatal(err)
	}
}

// review 1330 finding-1: a mint whose audit line cannot be written is
// revoked and its secret never shown.
func TestUnauditedMintIsRevokedAndNotShown(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	admin := signInAs(t, idp, h, "owners")
	a := attemptField.FindStringSubmatch(get(h, adminReadTokensPath, admin).Body.String())
	breakAuditLog(t, dir)
	w := postForm(h, adminReadTokensPath, url.Values{"csrf": {csrfOf(t, h, admin)}, "attempt": {a[1]}, "label": {"unaudited"}, "expires": {"24h"}}, admin)
	if w.Code != 500 || mintedToken.MatchString(w.Body.String()) || strings.Contains(w.Body.String(), ReadTokenPrefix) {
		t.Fatalf("unaudited mint %d showed a token", w.Code)
	}
	tokens, err := lake.Catalog.ReadTokens(t.Context())
	if err != nil || len(tokens) != 1 || tokens[0].State(time.Now()) != "revoked" {
		t.Fatalf("token after an unaudited mint: %+v %v", tokens, err)
	}
}

// review 1330 finding-2: a revoke that stands answers as one, even when
// its audit line stays queued.
func TestRevokeWithQueuedAuditSaysItIsRevoked(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	uid, digest := storeSession(t, lake, "tok-q", []byte("q\n"))
	admin := signInAs(t, idp, h, "owners")
	tok := mintReadToken(t, h, admin, "queued revoke", "")
	tokens, _ := lake.Catalog.ReadTokens(t.Context())
	breakAuditLog(t, dir)
	w := postForm(h, adminReadTokensPath+"/"+tokens[0].ID+"/revoke", url.Values{"csrf": {csrfOf(t, h, admin)}}, admin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "is revoked and no longer works") {
		t.Fatalf("revoke with a failing flush: %d", w.Code)
	}
	if w := bearer(h, "GET", rawTokenPath(uid, digest), tok); w.Code != 401 {
		t.Fatalf("revoked token read: %d", w.Code)
	}
	if n, err := lake.Catalog.PendingAudit(t.Context()); err != nil || n < 1 {
		t.Fatalf("revocation not queued: %d %v", n, err)
	}
}

// review 1331 finding-2: a catalog failure while checking the sessions is
// a server error, not a bad form.
func TestMintSessionCheckFailureIsAServerError(t *testing.T) {
	// The page lists tokens through its own handle on the catalog, so
	// closing the dashboard's handle fails only the session check.
	lake, idp, h, _ := rawLake(t, func(dir string) *catalog.Catalog {
		c, err := catalog.Open(filepath.Join(dir, "catalog.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	})
	uid, _ := storeSession(t, lake, "tok-db", []byte("db\n"))
	admin := signInAs(t, idp, h, "owners")
	a := attemptField.FindStringSubmatch(get(h, adminReadTokensPath, admin).Body.String())
	csrf := csrfOf(t, h, admin)
	if err := lake.Catalog.Close(); err != nil {
		t.Fatal(err)
	}
	w := postForm(h, adminReadTokensPath, url.Values{"csrf": {csrf}, "attempt": {a[1]}, "label": {"x"}, "sessions": {uid}, "expires": {"24h"}}, admin)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "Minting failed") {
		t.Fatalf("session check on a closed catalog: %d", w.Code)
	}
}

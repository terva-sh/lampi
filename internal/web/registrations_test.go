package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/testidp"
	"terva.sh/lampi/internal/webconfig"
)

// operatorLake is a lake with an identity, a public URL that reaches it,
// and the dashboard with registrations on. The IdP puts its user in
// groups.
func operatorLake(t *testing.T, release string, groups ...string) (*api.Server, *testidp.Server, http.Handler, string) {
	t.Helper()
	idp := testidp.New()
	t.Cleanup(idp.Close)
	idp.Groups = groups
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	if _, err := lake.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	lake.SetProfiles(config.Profiles{config.DefaultProfile: {}, "ci": {}})
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer", "admins": "operator"}}}
	reg := &Registrations{
		Lake: func() registrar.Lake {
			return registrar.Lake{Catalog: lake.Catalog, Identity: lake.Identity(), Dir: dir, Profiles: lake.Profiles()}
		},
		Release: release,
	}
	lake.Web, err = New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), nil, reg, nil, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	if err := lake.Catalog.SetPublicURL(t.Context(), srv.URL); err != nil {
		t.Fatal(err)
	}
	return lake, idp, lake.Handler(), dir
}

var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// csrfOf reads the session's CSRF token from the sign-out form.
func csrfOf(t *testing.T, h http.Handler, c *http.Cookie) string {
	t.Helper()
	m := csrfField.FindStringSubmatch(get(h, "/", c).Body.String())
	if m == nil {
		t.Fatal("no csrf token on the page")
	}
	return m[1]
}

func post(h http.Handler, path, body string, c *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://lake.example"+path, strings.NewReader(body))
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

func TestViewersGet404OnRegistrationRoutes(t *testing.T) {
	_, idp, h, _ := operatorLake(t, "", "readers")
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	for _, w := range []*httptest.ResponseRecorder{
		get(h, "/api/web/v1/registrations", cookie),
		post(h, "/api/web/v1/registrations", `{"name":"box"}`, cookie, map[string]string{CSRFHeader: csrf}),
		post(h, "/api/web/v1/registrations/reg_x/revoke", "", cookie, map[string]string{CSRFHeader: csrf}),
	} {
		if w.Code != 404 {
			t.Fatalf("viewer: %d %s", w.Code, w.Body)
		}
	}
	if w := get(h, "/api/web/v1/registrations", nil); w.Code != 401 {
		t.Fatalf("signed out: %d", w.Code)
	}
}

func TestOperatorMintsListsAndRevokes(t *testing.T) {
	_, idp, h, dir := operatorLake(t, "v0.1.1", "readers", "admins")
	// A sign-in the IdP dates an hour ago is not fresh enough to mint.
	idp.AuthTime = time.Now().Add(-time.Hour)
	stale, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, stale)
	w := post(h, "/api/web/v1/registrations", `{"name":"box"}`, stale, map[string]string{CSRFHeader: csrf})
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"fresh_login_required"`) || !strings.Contains(w.Body.String(), "fresh=1") {
		t.Fatalf("stale mint: %d %s", w.Code, w.Body)
	}
	// Listing needs only the operator session.
	if w := get(h, "/api/web/v1/registrations", stale); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}

	idp.AuthTime = time.Now()
	cookie, _ := signIn(t, idp, h)
	csrf = csrfOf(t, h, cookie)
	if w := post(h, "/api/web/v1/registrations", `{"name":"box"}`, cookie, nil); w.Code != 403 || !strings.Contains(w.Body.String(), "csrf_failed") {
		t.Fatalf("no csrf: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/registrations", `{"name":"box"}`, cookie, map[string]string{CSRFHeader: csrf, "Origin": "https://evil.example"}); w.Code != 403 {
		t.Fatalf("cross origin: %d %s", w.Code, w.Body)
	}
	for body, want := range map[string]string{
		`{"name":"Bad Name"}`:             "invalid_name",
		`{"name":"box","expires":"800h"}`: "invalid_expiry",
		`{"name":"box","expires":"soon"}`: "invalid_expiry",
		`{"name":"box","profile":"nope"}`: "unknown_profile",
		`{"name":"box","extra":1}`:        "invalid_request",
		`{"name":"box"}{"name":"other"}`:  "invalid_request",
		`not json`:                        "invalid_request",
		`null`:                            "invalid_request",
		`{"name":"box"}]`:                 "invalid_request",
		`{"name":"box"}}`:                 "invalid_request",
	} {
		if w := post(h, "/api/web/v1/registrations", body, cookie, map[string]string{CSRFHeader: csrf}); w.Code != 400 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
	}

	w = post(h, "/api/web/v1/registrations", `{"name":"box","profile":"ci"}`, cookie, map[string]string{CSRFHeader: csrf})
	if w.Code != 201 {
		t.Fatalf("mint: %d %s", w.Code, w.Body)
	}
	var m mintedView
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Code == "" || m.Registration.State != "pending" || m.Registration.Profile != "ci" || m.Registration.CreatedBy != "web:synthetic-viewer (Lake viewer)" || !m.Pinned {
		t.Fatalf("minted %+v", m)
	}
	if got, _ := time.Parse(time.RFC3339, m.Registration.Expires); got.Sub(time.Now()) > DefaultCodeLifetime || got.Sub(time.Now()) < DefaultCodeLifetime-time.Minute {
		t.Fatalf("default expiry %s", m.Registration.Expires)
	}
	want := " curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/v0.1.1/install.sh | TERVA_LAMPI_CODE='" + m.Code + "' sh -s -- --version v0.1.1 --register --fingerprint " + m.Fingerprint
	if m.Install != want {
		t.Fatalf("install line\n got %q\nwant %q", m.Install, want)
	}
	if w := post(h, "/api/web/v1/registrations", `{"name":"box"}`, cookie, map[string]string{CSRFHeader: csrf}); w.Code != 409 || !strings.Contains(w.Body.String(), "name_taken") {
		t.Fatalf("second code for box: %d %s", w.Code, w.Body)
	}

	// The list never carries the code.
	w = get(h, "/api/web/v1/registrations", cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), m.Code) || !strings.Contains(w.Body.String(), `"created_by":"web:synthetic-viewer (Lake viewer)"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}

	if w := post(h, "/api/web/v1/registrations/reg_nope/revoke", "", cookie, map[string]string{CSRFHeader: csrf}); w.Code != 404 {
		t.Fatalf("unknown code: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/registrations/box/revoke", "", cookie, map[string]string{CSRFHeader: csrf}); w.Code != 404 {
		t.Fatalf("revoke by name: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/registrations/"+m.Registration.ID+"/revoke", "", cookie, nil); w.Code != 403 {
		t.Fatalf("revoke without csrf: %d %s", w.Code, w.Body)
	}
	w = post(h, "/api/web/v1/registrations/"+m.Registration.ID+"/revoke", "", stale, map[string]string{CSRFHeader: csrfOf(t, h, stale)})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"revoked"`) || !strings.Contains(w.Body.String(), `"revoked_by":"web:synthetic-viewer (Lake viewer)"`) {
		t.Fatalf("revoke from a stale session: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/registrations/"+m.Registration.ID+"/revoke", "", cookie, map[string]string{CSRFHeader: csrf}); w.Code != 409 || !strings.Contains(w.Body.String(), "already_revoked") {
		t.Fatalf("second revoke: %d %s", w.Code, w.Body)
	}

	raw, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(m.Code)) {
		t.Fatal("audit holds the code")
	}
	for _, kind := range []string{audit.RegistrationCreated, audit.RegistrationRevoked} {
		if !bytes.Contains(raw, []byte(`"kind":"`+kind+`","device":"box","actor":"web:synthetic-viewer (Lake viewer)"`)) {
			t.Fatalf("audit lacks %s by the operator:\n%s", kind, raw)
		}
	}
}

func TestMintIsRateLimited(t *testing.T) {
	_, idp, h, _ := operatorLake(t, "", "admins")
	idp.AuthTime = time.Now()
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	// Refused input does not use up the allowance.
	for range mintBurst + 1 {
		if w := post(h, "/api/web/v1/registrations", `{"name":"box","profile":"nope"}`, cookie, map[string]string{CSRFHeader: csrf}); w.Code != 400 {
			t.Fatalf("unknown profile: %d %s", w.Code, w.Body)
		}
	}
	var last *httptest.ResponseRecorder
	for i := range mintBurst + 1 {
		last = post(h, "/api/web/v1/registrations", `{"name":"box-`+string(rune('a'+i))+`"}`, cookie, map[string]string{CSRFHeader: csrf})
		if i < mintBurst && last.Code != 201 {
			t.Fatalf("mint %d: %d %s", i, last.Code, last.Body)
		}
	}
	if last.Code != 429 {
		t.Fatalf("mint past the burst: %d %s", last.Code, last.Body)
	}
	// An unpinned lake installs the latest release.
	w := get(h, "/api/web/v1/registrations", cookie)
	body, _ := io.ReadAll(w.Body)
	if strings.Count(string(body), `"state":"pending"`) != mintBurst {
		t.Fatalf("list: %s", body)
	}
}

func TestInstallCommandFallsBackToLatest(t *testing.T) {
	for _, release := range []string{"", "0.0.0", "v0.1.2-0.20260927120000-abcdef123456", "v0.1.1+dirty", "v0.2.0-rc.1"} {
		line, pinned := installCommand(release, "lampi1.x.y", "SHA256:abc")
		if pinned || line != " curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/main/install.sh | TERVA_LAMPI_CODE='lampi1.x.y' sh -s -- --register --fingerprint SHA256:abc" {
			t.Fatalf("%q: %v %q", release, pinned, line)
		}
	}
}

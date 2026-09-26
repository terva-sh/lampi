package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/testidp"
	"terva.sh/lampi/internal/webconfig"
)

func fixture(t *testing.T) (*api.Server, *testidp.Server, http.Handler, *bytes.Buffer) {
	t.Helper()
	idp := testidp.New()
	t.Cleanup(idp.Close)
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Allow(strings.Repeat("a", 64))
	logs := &bytes.Buffer{}
	lake.Log = slog.New(slog.NewTextHandler(logs, nil))
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer"}}}
	lake.Web, err = New(cfg, lake.Catalog, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	return lake, idp, lake.Handler(), logs
}
func get(h http.Handler, path string, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "https://lake.example"+path, nil)
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func signIn(t *testing.T, idp *testidp.Server, h http.Handler) (*http.Cookie, string) {
	t.Helper()
	w := get(h, "/auth/oidc/start", nil)
	if w.Code != 303 {
		t.Fatal("start", w.Code)
	}
	u, _ := url.Parse(w.Header().Get("Location"))
	if u.Query().Get("redirect_uri") != "https://lake.example/auth/oidc/callback" {
		t.Fatal("callback origin")
	}
	attempt := w.Result().Cookies()[0]
	hc := idp.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := hc.Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	to, _ := url.Parse(res.Header.Get("Location"))
	w = get(h, to.RequestURI(), attempt)
	if w.Code != 303 {
		t.Fatal("callback", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if strings.HasSuffix(c.Name, "session") {
			return c, to.RawQuery
		}
	}
	t.Fatal("session missing")
	return nil, ""
}
func seedSession(t *testing.T, c *catalog.Catalog, native string) string {
	t.Helper()
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: "codex", NativeSessionID: native, Project: protocol.Project{CWD: "<script>alert('synthetic')</script>"}, Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "session.jsonl", SHA256: strings.Repeat("b", 64), Size: 12}}}
	ack, err := c.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}
func TestReadAPIThroughLakeMux(t *testing.T) {
	lake, idp, h, logs := fixture(t)
	uid := seedSession(t, lake.Catalog, "synthetic")
	cookie, query := signIn(t, idp, h)
	for _, path := range []string{"/api/web/v1/overview", "/api/web/v1/sessions", "/api/web/v1/sessions/" + uid, "/api/web/v1/sessions/" + uid + "/artifacts", "/api/web/v1/sessions/" + uid + "/provenance", "/api/web/v1/sessions/" + uid + "/conflicts", "/api/web/v1/conflicts"} {
		if get(h, path, nil).Code != 401 {
			t.Fatal("unguarded", path)
		}
		w := get(h, path, cookie)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("headers")
		}
		var v any
		if json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("not JSON")
		}
		if strings.Contains(w.Body.String(), "manifest_json") || strings.Contains(w.Body.String(), "content_text") {
			t.Fatal("content exposed")
		}
	}
	for _, path := range []string{"/api/web/v1/sessions?limit=201", "/api/web/v1/sessions?limit=0", "/api/web/v1/sessions?limit=2&limit=3", "/api/web/v1/sessions?unknown=1", "/api/web/v1/sessions?cursor=bad", "/api/web/v1/overview?x=y"} {
		if get(h, path, cookie).Code != 400 {
			t.Fatal("bad input accepted", path)
		}
	}
	if get(h, "/api/web/v1/sessions/missing", cookie).Code != 404 {
		t.Fatal("missing")
	}
	if get(h, "/api/web/v1/sessions/missing", nil).Code != 401 {
		t.Fatal("lookup before guard")
	}
	if get(h, "/v1/stats", cookie).Code != 401 {
		t.Fatal("browser authorizes device API")
	}
	r := httptest.NewRequest("GET", "https://lake.example/api/web/v1/overview", nil)
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("device authorizes web")
	}
	if strings.Contains(logs.String(), cookie.Value) || strings.Contains(logs.String(), query) || strings.Contains(logs.String(), "code=") {
		t.Fatal("credential in access log")
	}
}
func TestWebOffAndCanceledRead(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	cookie, _ := signIn(t, idp, h)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequest("GET", "https://lake.example/api/web/v1/overview", nil).WithContext(ctx)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("canceled", w.Code)
	}
	lake.Web = nil
	if get(lake.Handler(), "/api/web/v1/overview", cookie).Code != 404 {
		t.Fatal("web off")
	}
	if get(lake.Handler(), "/healthz", nil).Code != 200 {
		t.Fatal("healthz")
	}
}

func TestPagesEscapeMetadataAndWorkWithoutScripts(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid := seedSession(t, lake.Catalog, `<script>window.owned=true</script>`)
	cookie, _ := signIn(t, idp, h)
	for _, path := range []string{"/", "/sessions", "/sessions?harness=codex&state=unknown", "/sessions/" + uid, "/sessions/" + uid + "?collection=provenance", "/conflicts"} {
		w := get(h, path, cookie)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "</html>") || !strings.Contains(body, `action="/auth/oidc/logout"`) {
			t.Fatal("incomplete template", path)
		}
		if strings.Contains(body, "<script>window.owned") || strings.Contains(body, "#ZgotmplZ") {
			t.Fatal("unsafe template", path)
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
	w := get(h, "/sessions", cookie)
	if !strings.Contains(w.Body.String(), "&lt;script&gt;") || !strings.Contains(w.Body.String(), `method="get"`) {
		t.Fatal("escaped no-JS form")
	}
	r := httptest.NewRequest("GET", "https://lake.example/", nil)
	r.Header.Set("X-Lampi-Refresh", "1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("expired refresh redirected to IdP")
	}
	for _, path := range []string{"/assets/lake.css", "/assets/lake.js"} {
		if get(h, path, nil).Code != 200 {
			t.Fatal("asset", path)
		}
	}
}

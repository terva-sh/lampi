package webauth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/testidp"
	"testing"
	"time"
)

func browserFixture(t *testing.T) (*Browser, *testidp.Server, http.Handler) {
	t.Helper()
	s := testidp.New()
	t.Cleanup(s.Close)
	b, err := New(providerConfig(s), s.Client())
	if err != nil {
		t.Fatal(err)
	}
	m := http.NewServeMux()
	b.Routes(m)
	m.Handle("GET /{$}", b.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, csrf := Current(r); _, _ = io.WriteString(w, csrf) })))
	m.Handle("GET /api/web/v1/test", b.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })))
	return b, s, Headers(m)
}
func request(h http.Handler, method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func startLogin(t *testing.T, s *testidp.Server, h http.Handler) (string, *http.Cookie) {
	t.Helper()
	w := request(h, "GET", "https://lake.example"+LoginPath+"?next=/sessions")
	if w.Code != 303 {
		t.Fatalf("start %d", w.Code)
	}
	client := s.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Get(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.Header.Get("Location"), w.Result().Cookies()[0]
}
func login(t *testing.T, s *testidp.Server, h http.Handler) *http.Cookie {
	t.Helper()
	to, a := startLogin(t, s, h)
	w := request(h, "GET", to, a)
	if w.Code != 303 {
		t.Fatalf("callback %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if strings.HasSuffix(c.Name, "session") {
			return c
		}
	}
	t.Fatal("no session")
	return nil
}
func TestBrowserFlowGuardsAndLogout(t *testing.T) {
	_, s, h := browserFixture(t)
	cookie := login(t, s, h)
	if !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || !strings.HasPrefix(cookie.Name, "__Host-") {
		t.Fatal("cookie flags")
	}
	if w := request(h, "GET", "https://lake.example/api/web/v1/test"); w.Code != 401 || !strings.Contains(w.Body.String(), "not_authenticated") {
		t.Fatal("API guard")
	}
	if w := request(h, "GET", "https://lake.example/api/web/v1/test", cookie); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "https://lake.example/"); w.Code != 303 {
		t.Fatal("page guard")
	}
	csrf := request(h, "GET", "https://lake.example/", cookie).Body.String()
	for _, origin := range []string{"https://evil.example", "https://lake.example"} {
		r := httptest.NewRequest("POST", "https://lake.example"+LogoutPath, strings.NewReader(url.Values{"csrf": {csrf}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if origin == "https://evil.example" && w.Code != 403 {
			t.Fatal("cross-origin logout")
		}
		if origin == "https://lake.example" && w.Code != 303 {
			t.Fatal("logout")
		}
	}
	if request(h, "GET", "https://lake.example/api/web/v1/test", cookie).Code != 401 {
		t.Fatal("logout retained session")
	}
	if request(h, "GET", "https://lake.example"+LogoutPath, cookie).Code != 405 {
		t.Fatal("GET logout")
	}
}
func TestAttemptsAreBoundSingleUseAndExpiring(t *testing.T) {
	for _, mode := range []string{"missing", "state", "expired", "valid"} {
		t.Run(mode, func(t *testing.T) {
			b, s, h := browserFixture(t)
			to, c := startLogin(t, s, h)
			switch mode {
			case "missing":
				c = &http.Cookie{Name: c.Name, Value: randomID()}
			case "state":
				u, _ := url.Parse(to)
				q := u.Query()
				q.Set("state", "wrong")
				u.RawQuery = q.Encode()
				to = u.String()
			case "expired":
				b.now = func() time.Time { return time.Now().Add(attemptTTL + time.Second) }
			}
			w := request(h, "GET", to, c)
			want := 403
			if mode == "valid" {
				want = 303
			}
			if w.Code != want {
				t.Fatalf("%s %d", mode, w.Code)
			}
			if request(h, "GET", to, c).Code != 403 {
				t.Fatal("replayed callback")
			}
		})
	}
}
func TestHardExpiryDespiteActivityAndRestart(t *testing.T) {
	b, s, h := browserFixture(t)
	now := time.Now()
	b.now = func() time.Time { return now }
	c := login(t, s, h)
	for n := 0; n < 24; n++ {
		now = now.Add(29 * time.Minute)
		if request(h, "GET", "https://lake.example/api/web/v1/test", c).Code != 204 {
			t.Fatal("premature expiry")
		}
	}
	now = now.Add(25 * time.Minute)
	if request(h, "GET", "https://lake.example/api/web/v1/test", c).Code != 401 {
		t.Fatal("hard expiry extended")
	}
	c = login(t, s, h)
	now = now.Add(idleTTL + time.Second)
	if request(h, "GET", "https://lake.example/api/web/v1/test", c).Code != 401 {
		t.Fatal("idle expiry")
	}
	restarted, _ := New(providerConfig(s), s.Client())
	r := httptest.NewRequest("GET", "https://lake.example/", nil)
	r.AddCookie(c)
	if _, ok := restarted.lookup(r); ok {
		t.Fatal("restart retained session")
	}
}
func TestUnmappedGroupsAndLimits(t *testing.T) {
	b, s, h := browserFixture(t)
	s.Groups = []string{"outsiders"}
	to, c := startLogin(t, s, h)
	if request(h, "GET", to, c).Code != 403 {
		t.Fatal("unmapped login")
	}
	for n := 0; n < maxEntries; n++ {
		b.attempts[randomID()] = attempt{expires: time.Now().Add(time.Hour)}
	}
	if request(h, "GET", "https://lake.example"+LoginPath).Code != 503 {
		t.Fatal("attempt cap")
	}
	b.attempts = map[string]attempt{}
	s.Groups = []string{"readers"}
	for n := 0; n < maxEntries; n++ {
		b.sessions[randomID()] = session{Idle: time.Now().Add(time.Hour), Hard: time.Now().Add(time.Hour)}
	}
	to, c = startLogin(t, s, h)
	if request(h, "GET", to, c).Code != 503 {
		t.Fatal("session cap")
	}
}
func TestSafeReturns(t *testing.T) {
	for _, raw := range []string{"https://evil.example", "//evil.example", "/%2fexample", "/%252fexample", "/\\evil", "/%5cevil", "/\r\nLocation:x", ""} {
		if safeReturn(raw) != "/" {
			t.Fatalf("accepted %q", raw)
		}
	}
	if safeReturn("/sessions?harness=codex") != "/sessions?harness=codex" {
		t.Fatal("local path")
	}
}
func TestFullMuxSeparatesDeviceAndBrowser(t *testing.T) {
	_, s, h := browserFixture(t)
	cookie := login(t, s, h)
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer lake.Close()
	lake.Allow(strings.Repeat("a", 64))
	lake.Web = h
	m := lake.Handler()
	if request(m, "GET", "https://lake.example/v1/stats", cookie).Code != 401 {
		t.Fatal("cookie grants device access")
	}
	r := httptest.NewRequest("GET", "https://lake.example/api/web/v1/test", nil)
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("device grants browser access")
	}
}

package webauth

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/testidp"
)

// operatorFixture is browserFixture with an operator group mapped and
// an operator-only page and API route behind Guard.
func operatorFixture(t *testing.T) (*testidp.Server, http.Handler) {
	t.Helper()
	s := testidp.New()
	t.Cleanup(s.Close)
	cfg := providerConfig(s)
	cfg.OIDC.RoleMap = map[string]string{"readers": "viewer", "admins": "operator"}
	b, err := New(cfg, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	m := http.NewServeMux()
	b.Routes(m)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := Current(r)
		_, _ = io.WriteString(w, map[bool]string{true: "fresh", false: "stale"}[Fresh(r, time.Now())]+" "+id.Display)
	})
	m.Handle("GET /admin/x", b.Guard(OperatorOnly(ok)))
	m.Handle("GET /api/web/v1/admin/x", b.Guard(OperatorOnly(ok)))
	return s, Headers(m)
}

// freshLogin is login through /auth/oidc/start?fresh=1.
func freshLogin(t *testing.T, s *testidp.Server, h http.Handler) (*http.Cookie, int) {
	t.Helper()
	w := request(h, "GET", "https://lake.example"+FreshLoginURL("/admin/x"))
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
	cb := request(h, "GET", res.Header.Get("Location"), w.Result().Cookies()[0])
	for _, c := range cb.Result().Cookies() {
		if strings.HasSuffix(c.Name, "session") && c.Value != "" {
			return c, cb.Code
		}
	}
	return nil, cb.Code
}

// TKT-01M3J5HX9: only operators reach operator routes, and a viewer
// gets 404, not 403.
func TestOperatorOnlyHidesRoutesFromViewers(t *testing.T) {
	s, h := operatorFixture(t)
	viewer := login(t, s, h)
	for _, target := range []string{"/admin/x", "/api/web/v1/admin/x"} {
		if w := request(h, "GET", "https://lake.example"+target, viewer); w.Code != 404 {
			t.Errorf("viewer %s: %d, want 404", target, w.Code)
		}
	}
	s.Groups = []string{"admins"}
	op := login(t, s, h)
	if w := request(h, "GET", "https://lake.example/admin/x", op); w.Code != 200 {
		t.Fatalf("operator: %d", w.Code)
	}
}

// A fresh sign-in sends max_age and needs a recent auth_time; a plain
// one sends neither and is stale for Fresh.
func TestFreshSignIn(t *testing.T) {
	s, h := operatorFixture(t)
	s.Groups = []string{"admins"}

	// Single sign-on from an hour ago: a plain login works but is stale.
	s.AuthTime = time.Now().Add(-time.Hour)
	c := login(t, s, h)
	if got := request(h, "GET", "https://lake.example/admin/x", c).Body.String(); !strings.HasPrefix(got, "stale") {
		t.Fatalf("plain login read as %q", got)
	}
	if s.MaxAges[len(s.MaxAges)-1] != "" {
		t.Fatalf("plain login sent max_age %q", s.MaxAges[len(s.MaxAges)-1])
	}

	// With max_age the provider reauthenticates, so auth_time is now.
	c, code := freshLogin(t, s, h)
	if c == nil {
		t.Fatalf("fresh login failed: %d", code)
	}
	if got := s.MaxAges[len(s.MaxAges)-1]; got != "600" {
		t.Fatalf("max_age %q, want 600", got)
	}
	if got := request(h, "GET", "https://lake.example/admin/x", c).Body.String(); !strings.HasPrefix(got, "fresh") {
		t.Fatalf("fresh login read as %q", got)
	}

	// A provider that ignores max_age and reports an old auth_time is
	// refused rather than trusted.
	s.Claims = map[string]any{"auth_time": time.Now().Add(-time.Hour).Unix()}
	if c, code := freshLogin(t, s, h); c != nil || code != 403 {
		t.Fatalf("stale auth_time on a fresh login: session %v code %d", c != nil, code)
	}
	s.Claims = map[string]any{"auth_time": nil}
	if c, code := freshLogin(t, s, h); c != nil || code != 403 {
		t.Fatalf("missing auth_time on a fresh login: session %v code %d", c != nil, code)
	}
}

func TestFreshAt(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, c := range []struct {
		auth time.Time
		want bool
	}{
		{time.Time{}, false},
		{now, true},
		{now.Add(-FreshWindow), true},
		{now.Add(-FreshWindow - time.Second), false},
		{now.Add(30 * time.Second), true},
		{now.Add(2 * time.Minute), false},
	} {
		if got := freshAt(c.auth, now); got != c.want {
			t.Errorf("auth %v: %v, want %v", now.Sub(c.auth), got, c.want)
		}
	}
}

// TKT-01M3NKZT6N: admin implies operator and viewer, and AdminOnly
// answers 404 to operators and viewers alike.
func TestAdminOnlyHidesRoutesFromOperators(t *testing.T) {
	s := testidp.New()
	t.Cleanup(s.Close)
	cfg := providerConfig(s)
	cfg.OIDC.RoleMap = map[string]string{"readers": "viewer", "ops": "operator", "owners": "admin"}
	b, err := New(cfg, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	m := http.NewServeMux()
	b.Routes(m)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := Current(r)
		_, _ = io.WriteString(w, map[bool]string{true: "viewer", false: "-"}[id.Viewer]+" "+map[bool]string{true: "operator", false: "-"}[id.Operator])
	})
	m.Handle("GET /admin/raw", b.Guard(AdminOnly(ok)))
	m.Handle("GET /api/web/v1/raw", b.Guard(AdminOnly(ok)))
	m.Handle("GET /admin/codes", b.Guard(OperatorOnly(ok)))
	h := Headers(m)

	for _, groups := range [][]string{{"readers"}, {"ops"}} {
		s.Groups = groups
		c := login(t, s, h)
		for _, target := range []string{"/admin/raw", "/api/web/v1/raw"} {
			if w := request(h, "GET", "https://lake.example"+target, c); w.Code != 404 {
				t.Errorf("%v %s: %d, want 404", groups, target, w.Code)
			}
		}
	}
	s.Groups = []string{"owners"}
	c := login(t, s, h)
	for _, target := range []string{"/admin/raw", "/admin/codes"} {
		w := request(h, "GET", "https://lake.example"+target, c)
		if w.Code != 200 || w.Body.String() != "viewer operator" {
			t.Errorf("admin %s: %d %q", target, w.Code, w.Body.String())
		}
	}
}

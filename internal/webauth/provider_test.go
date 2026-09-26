package webauth

import (
	"net/url"
	"terva.sh/lampi/internal/testidp"
	"terva.sh/lampi/internal/webconfig"
	"testing"
	"time"
)

func providerConfig(s *testidp.Server) webconfig.Config {
	return webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: s.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer"}}}
}
func TestProviderFlowAndRotation(t *testing.T) {
	s := testidp.New()
	defer s.Close()
	p, err := NewProvider(providerConfig(s), s.Client())
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.AuthURL(t.Context(), "state", "nonce", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(u)
	q := parsed.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("nonce") != "nonce" || q.Get("state") != "state" {
		t.Fatal("flow parameters")
	}
	for n := 0; n < 2; n++ {
		id, err := p.Exchange(t.Context(), s.Issue("nonce", "verifier", "lake"), "nonce", "verifier")
		if err != nil || !id.Viewer || id.Issuer != s.URL() {
			t.Fatalf("flow %d: %v", n, err)
		}
		s.Rotate()
	}
}
func TestProviderRefusesInvalidIdentity(t *testing.T) {
	cases := []struct {
		name   string
		claims map[string]any
		alg    string
		bad    bool
	}{{"issuer", map[string]any{"iss": "https://wrong.example"}, "", false}, {"audience", map[string]any{"aud": "other"}, "", false}, {"expiry", map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}, "", false}, {"nonce", map[string]any{"nonce": "other"}, "", false}, {"subject", map[string]any{"sub": ""}, "", false}, {"none", nil, "none", false}, {"hmac", nil, "HS256", false}, {"signature", nil, "", true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testidp.New()
			defer s.Close()
			s.Claims = tc.claims
			s.Algorithm = tc.alg
			s.BadSignature = tc.bad
			p, _ := NewProvider(providerConfig(s), s.Client())
			if _, err := p.Exchange(t.Context(), s.Issue("nonce", "verifier", "lake"), "nonce", "verifier"); err != ErrIdentity {
				t.Fatalf("got %v", err)
			}
		})
	}
}
func TestProviderGroupsAndAvailability(t *testing.T) {
	for _, g := range []any{[]string{"other"}, []any{"readers", 12}, []any{"readers", nil}, nil} {
		s := testidp.New()
		s.Groups = g
		p, _ := NewProvider(providerConfig(s), s.Client())
		id, err := p.Exchange(t.Context(), s.Issue("n", "v", "lake"), "n", "v")
		s.Close()
		if err != nil || id.Viewer {
			t.Fatalf("groups granted: %v", err)
		}
	}
	s := testidp.New()
	defer s.Close()
	s.Outage = true
	p, _ := NewProvider(providerConfig(s), s.Client())
	if _, err := p.AuthURL(t.Context(), "s", "n", "v"); err != ErrProvider {
		t.Fatal(err)
	}
	s.Outage = false
	s.PlainEndpoint = true
	if _, err := p.AuthURL(t.Context(), "s", "n", "v"); err != ErrProvider {
		t.Fatal("plaintext endpoint accepted")
	}
	s.PlainEndpoint = false
	if _, err := p.AuthURL(t.Context(), "s", "n", "v"); err != nil {
		t.Fatal(err)
	}
}

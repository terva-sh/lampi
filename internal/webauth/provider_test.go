package webauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
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

// staleKeys answers as go-oidc's key set does when a lookup for a new kid
// joins a fetch that finished before the rotation: with the old keys only.
type staleKeys struct{}

func (staleKeys) VerifySignature(context.Context, string) ([]byte, error) {
	return nil, errors.New("failed to verify id token signature")
}

// TestProviderRotationAfterStaleFetch forces the interleaving that made
// TestProviderFlowAndRotation flake: the key set answers a rotated kid
// from a fetch that predates the rotation.
func TestProviderRotationAfterStaleFetch(t *testing.T) {
	s := testidp.New()
	defer s.Close()
	p, err := NewProvider(providerConfig(s), s.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exchange(t.Context(), s.Issue("nonce", "verifier", "lake"), "nonce", "verifier"); err != nil {
		t.Fatal(err)
	}
	s.Rotate()
	keys := p.discovered.keys
	keys.cur = staleKeys{}
	if _, err := p.Exchange(t.Context(), s.Issue("nonce", "verifier", "lake"), "nonce", "verifier"); err != nil {
		t.Fatalf("rotated key refused after a stale fetch: %v", err)
	}
	if _, stale := keys.cur.(staleKeys); stale {
		t.Fatal("stale key set kept after a fresh one verified")
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

func TestIdentityKeepsItsGroups(t *testing.T) {
	s := testidp.New()
	defer s.Close()
	// readers maps to viewer; client-x maps to no role but can hold bays.
	s.Groups = []string{"readers", "client-x", "readers"}
	p, _ := NewProvider(providerConfig(s), s.Client())
	id, err := p.Exchange(t.Context(), s.Issue("n", "v", "lake"), "n", "v")
	if err != nil || !id.Viewer {
		t.Fatalf("id %+v err=%v", id, err)
	}
	if want := []string{"client-x", "readers"}; !slices.Equal(id.Groups, want) {
		t.Fatalf("groups %v want %v", id.Groups, want)
	}
}

// TestTheGroupCapKeepsRoleGroups is review 1401: a role group past the
// cap in claim order is still kept, so its bay grants apply.
func TestTheGroupCapKeepsRoleGroups(t *testing.T) {
	s := testidp.New()
	defer s.Close()
	var claimed []string
	for i := range maxGroups + 10 {
		claimed = append(claimed, fmt.Sprintf("g%03d", i))
	}
	s.Groups = append(claimed, "readers")
	p, _ := NewProvider(providerConfig(s), s.Client())
	id, err := p.Exchange(t.Context(), s.Issue("n", "v", "lake"), "n", "v")
	if err != nil || !id.Viewer {
		t.Fatalf("id %+v err=%v", id, err)
	}
	if len(id.Groups) != maxGroups || !slices.Contains(id.Groups, "readers") {
		t.Fatalf("%d groups, readers kept=%v", len(id.Groups), slices.Contains(id.Groups, "readers"))
	}
}

// TestARoleComesOnlyFromAKeptGroup is review 1402: past the cap, a role
// group that is not kept gives no role, so no role outlives its group.
func TestARoleComesOnlyFromAKeptGroup(t *testing.T) {
	s := testidp.New()
	defer s.Close()
	cfg := providerConfig(s)
	cfg.OIDC.RoleMap = map[string]string{"boss": "admin"}
	var claimed []string
	for i := range maxGroups {
		g := fmt.Sprintf("v%03d", i)
		cfg.OIDC.RoleMap[g] = "viewer"
		claimed = append(claimed, g)
	}
	s.Groups = append(claimed, "boss")
	p, _ := NewProvider(cfg, s.Client())
	id, err := p.Exchange(t.Context(), s.Issue("n", "v", "lake"), "n", "v")
	if err != nil {
		t.Fatal(err)
	}
	if !id.Viewer || id.Admin || slices.Contains(id.Groups, "boss") || len(id.Groups) != maxGroups {
		t.Fatalf("viewer=%v admin=%v groups=%d", id.Viewer, id.Admin, len(id.Groups))
	}
}

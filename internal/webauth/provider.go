// Package webauth implements browser identity separately from device ingestion.
// The design follows the sibling Terva/Canvas flow; this implementation is local.
package webauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"terva.sh/lampi/internal/webconfig"
)

// maxGroups bounds the groups one identity keeps, so a token naming
// thousands cannot grow a session without limit.
const maxGroups = 256

var ErrProvider = errors.New("identity provider unavailable")
var ErrIdentity = errors.New("identity response did not verify")

// Identity contains only verified display/authorization data, never tokens.
// Admin implies Operator, which implies Viewer. AuthTime is the ID token's auth_time: when the
// user last authenticated at the identity provider, which a single
// sign-on can make much earlier than this login. It is zero when the
// token has none.
type Identity struct {
	Issuer   string
	Subject  string
	Display  string
	Viewer   bool
	Operator bool
	Admin    bool
	AuthTime time.Time
	// Groups are the IdP groups the ID token named, sorted, at most
	// maxGroups: those role_map names first, then the rest in claim
	// order. role_map decides the role, from these groups only; bay grants are held by
	// group, so these decide which bays a viewer or operator reads
	// (TKT-01M3N8KHW5), including groups that map to no role. They are
	// a snapshot taken at sign-in.
	Groups []string
}
type discovered struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	keys     *rotatingKeys
}

// rotatingKeys is the provider's JWKS cache. go-oidc's RemoteKeySet
// refetches on an unknown kid, but it signals the end of a fetch before
// it clears the fetch in flight, so a verification that misses the
// cache in that window joins the finished fetch and gets the key set
// from before a rotation; one joining a fetch that began before the
// token was signed gets the same. Either rejects a valid login. When the
// cached set fails, a new set, whose first fetch starts after this
// token arrived, gets one more try and replaces the cache if it verifies.
type rotatingKeys struct {
	fresh func() oidc.KeySet
	mu    sync.Mutex
	cur   oidc.KeySet
}

func (k *rotatingKeys) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	k.mu.Lock()
	cur := k.cur
	k.mu.Unlock()
	payload, err := cur.VerifySignature(ctx, jwt)
	if err == nil {
		return payload, nil
	}
	next := k.fresh()
	if payload, err = next.VerifySignature(ctx, jwt); err != nil {
		return nil, err
	}
	k.mu.Lock()
	if k.cur == cur {
		k.cur = next
	}
	k.mu.Unlock()
	return payload, nil
}

type Provider struct {
	cfg        webconfig.Config
	client     *http.Client
	mu         sync.Mutex
	discovered *discovered
	slots      chan struct{}
}

func NewProvider(cfg webconfig.Config, client *http.Client) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// Copy operator-owned collections: callers cannot change role policy in flight.
	roles := make(map[string]string, len(cfg.OIDC.RoleMap))
	for k, v := range cfg.OIDC.RoleMap {
		roles[k] = v
	}
	cfg.OIDC.RoleMap = roles
	cfg.OIDC.Scopes = append([]string(nil), cfg.OIDC.Scopes...)
	hc := http.Client{Timeout: 10 * time.Second}
	if client != nil {
		hc = *client
		hc.Timeout = 10 * time.Second
	}
	transport := hc.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	hc.Transport = secureTransport{transport}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("OIDC redirects refused") }
	return &Provider{cfg: cfg, client: &hc, slots: make(chan struct{}, 8)}, nil
}

type secureTransport struct{ base http.RoundTripper }

func (s secureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if webconfig.HTTPSURL(r.URL.String()) != nil {
		return nil, ErrProvider
	}
	res, err := s.base.RoundTrip(r)
	if err != nil {
		return nil, ErrProvider
	}
	res.Body = &limitedBody{Reader: io.LimitReader(res.Body, 1<<20), Closer: res.Body}
	return res, nil
}

type limitedBody struct {
	io.Reader
	io.Closer
}

func (p *Provider) enter(ctx context.Context) (func(), error) {
	select {
	case p.slots <- struct{}{}:
		return func() { <-p.slots }, nil
	case <-ctx.Done():
		return nil, ErrProvider
	default:
		return nil, ErrProvider
	}
}
func (p *Provider) discover(ctx context.Context) (*discovered, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discovered != nil {
		return p.discovered, nil
	}
	gp, err := oidc.NewProvider(oidc.ClientContext(ctx, p.client), p.cfg.OIDC.Issuer)
	if err != nil {
		return nil, ErrProvider
	}
	var metadata struct {
		Issuer string `json:"issuer"`
		JWKS   string `json:"jwks_uri"`
	}
	if gp.Claims(&metadata) != nil {
		return nil, ErrProvider
	}
	ep := gp.Endpoint()
	for _, u := range []string{ep.AuthURL, ep.TokenURL, metadata.JWKS} {
		if webconfig.HTTPSURL(u) != nil {
			return nil, ErrProvider
		}
	}
	keyCtx := oidc.ClientContext(context.Background(), p.client)
	fresh := func() oidc.KeySet { return oidc.NewRemoteKeySet(keyCtx, metadata.JWKS) }
	keys := &rotatingKeys{fresh: fresh, cur: fresh()}
	d := &discovered{oauth: oauth2.Config{ClientID: p.cfg.OIDC.ClientID, ClientSecret: p.cfg.Secret(), RedirectURL: p.cfg.CallbackURL(), Endpoint: ep, Scopes: p.cfg.OIDC.Scopes}, verifier: oidc.NewVerifier(metadata.Issuer, keys, &oidc.Config{ClientID: p.cfg.OIDC.ClientID, SupportedSigningAlgs: []string{oidc.RS256, oidc.RS384, oidc.RS512, oidc.ES256, oidc.ES384, oidc.ES512, oidc.PS256, oidc.PS384, oidc.PS512}}), keys: keys}
	p.discovered = d
	return d, nil
}
func (p *Provider) AuthURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	return p.AuthURLMaxAge(ctx, state, nonce, verifier, 0)
}

// AuthURLMaxAge is AuthURL with the OIDC max_age parameter when maxAge
// is positive: the provider must authenticate the user again unless it
// did so within maxAge, and must then return auth_time.
func (p *Provider) AuthURLMaxAge(ctx context.Context, state, nonce, verifier string, maxAge time.Duration) (string, error) {
	release, err := p.enter(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if maxAge > 0 {
		opts = append(opts, oauth2.SetAuthURLParam("max_age", strconv.FormatInt(int64(maxAge/time.Second), 10)))
	}
	return d.oauth.AuthCodeURL(state, opts...), nil
}
func (p *Provider) Exchange(ctx context.Context, code, nonce, verifier string) (Identity, error) {
	if code == "" || nonce == "" || verifier == "" {
		return Identity{}, ErrIdentity
	}
	release, err := p.enter(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, p.client), 10*time.Second)
	defer cancel()
	d, err := p.discover(ctx)
	if err != nil {
		return Identity{}, err
	}
	token, err := d.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, ErrIdentity
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return Identity{}, ErrIdentity
	}
	id, err := d.verifier.Verify(ctx, raw)
	if err != nil || strings.TrimSpace(id.Subject) == "" || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 {
		return Identity{}, ErrIdentity
	}
	var claims map[string]json.RawMessage
	if id.Claims(&claims) != nil {
		return Identity{}, ErrIdentity
	}
	out := Identity{Issuer: id.Issuer, Subject: id.Subject, Display: id.Subject}
	for _, key := range []string{"name", "preferred_username", "email"} {
		var s string
		if json.Unmarshal(claims[key], &s) == nil && s != "" {
			out.Display = s
			break
		}
	}
	claimed := groups(claims[p.cfg.OIDC.GroupsClaim])
	// A group that gives a role is kept before the cap applies, and the
	// role comes only from a group that was kept, so a role and the bay
	// grants of the group that gave it never part (reviews 1401, 1402).
	// Other groups fill the rest in claim order.
	for _, roles := range []bool{true, false} {
		for _, g := range claimed {
			_, role := p.cfg.OIDC.RoleMap[g]
			if role == roles && len(out.Groups) < maxGroups && !slices.Contains(out.Groups, g) {
				out.Groups = append(out.Groups, g)
			}
		}
	}
	for _, g := range out.Groups {
		switch p.cfg.OIDC.RoleMap[g] {
		case webconfig.RoleViewer:
			out.Viewer = true
		case webconfig.RoleOperator:
			out.Viewer, out.Operator = true, true
		case webconfig.RoleAdmin:
			out.Viewer, out.Operator, out.Admin = true, true, true
		}
	}
	slices.Sort(out.Groups)
	var authTime json.Number
	if raw, ok := claims["auth_time"]; ok && json.Unmarshal(raw, &authTime) == nil {
		if sec, err := authTime.Int64(); err == nil && sec > 0 {
			out.AuthTime = time.Unix(sec, 0)
		}
	}
	return out, nil
}
func groups(raw json.RawMessage) []string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []any:
		out := make([]string, 0, len(v))
		for _, entry := range v {
			group, ok := entry.(string)
			if !ok || group == "" {
				return nil
			}
			out = append(out, group)
		}
		return out
	}
	return nil
}

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
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"terva.sh/lampi/internal/webconfig"
)

var ErrProvider = errors.New("identity provider unavailable")
var ErrIdentity = errors.New("identity response did not verify")

// Identity contains only verified display/authorization data, never tokens.
type Identity struct {
	Issuer  string
	Subject string
	Display string
	Viewer  bool
}
type discovered struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
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
		JWKS string `json:"jwks_uri"`
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
	d := &discovered{oauth: oauth2.Config{ClientID: p.cfg.OIDC.ClientID, ClientSecret: p.cfg.Secret(), RedirectURL: p.cfg.CallbackURL(), Endpoint: ep, Scopes: p.cfg.OIDC.Scopes}, verifier: gp.Verifier(&oidc.Config{ClientID: p.cfg.OIDC.ClientID, SupportedSigningAlgs: []string{oidc.RS256, oidc.RS384, oidc.RS512, oidc.ES256, oidc.ES384, oidc.ES512, oidc.PS256, oidc.PS384, oidc.PS512}})}
	p.discovered = d
	return d, nil
}
func (p *Provider) AuthURL(ctx context.Context, state, nonce, verifier string) (string, error) {
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
	return d.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
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
	if err != nil || id.Subject == "" || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 {
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
	for _, g := range groups(claims[p.cfg.OIDC.GroupsClaim]) {
		if p.cfg.OIDC.RoleMap[g] == "viewer" {
			out.Viewer = true
		}
	}
	return out, nil
}
func groups(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil && one != "" {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

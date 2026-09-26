// Package testidp is a synthetic HTTPS OIDC issuer used by tests and the local
// browser smoke fixture. It never reads credentials or reaches a real provider.
package testidp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"
)

type flow struct{ nonce, challenge, client string }
type Server struct {
	Server *httptest.Server
	mu     sync.Mutex
	key    *ecdsa.PrivateKey
	kid    int
	flows  map[string]flow
	// Configure before making requests. Mutations during requests require external synchronization.
	Claims        map[string]any
	Algorithm     string
	BadSignature  bool
	PlainEndpoint bool
	Outage        bool
	Groups        any
}

func New() *Server {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	s := &Server{key: key, flows: map[string]flow{}, Groups: []string{"readers"}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET /authorize", s.authorize)
	mux.HandleFunc("POST /token", s.token)
	mux.HandleFunc("GET /keys", s.keys)
	s.Server = httptest.NewTLSServer(mux)
	return s
}
func (s *Server) Close()               { s.Server.Close() }
func (s *Server) URL() string          { return s.Server.URL }
func (s *Server) Client() *http.Client { return s.Server.Client() }
func (s *Server) Rotate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	s.kid++
}
func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {
	if s.Outage {
		http.Error(w, "synthetic outage", 503)
		return
	}
	endpoint := s.URL() + "/token"
	if s.PlainEndpoint {
		endpoint = "http://invalid.example/token"
	}
	jsonResponse(w, map[string]any{"issuer": s.URL(), "authorization_endpoint": s.URL() + "/authorize", "token_endpoint": endpoint, "jwks_uri": s.URL() + "/keys", "id_token_signing_alg_values_supported": []string{"ES256"}})
}

// Issue records a synthetic authorization code. No login bypass exists in the lake.
func (s *Server) Issue(nonce, verifier, client string) string {
	sum := sha256.Sum256([]byte(verifier))
	return s.issue(flow{nonce, base64.RawURLEncoding.EncodeToString(sum[:]), client})
}
func (s *Server) issue(f flow) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b [32]byte
	_, _ = rand.Read(b[:])
	code := base64.RawURLEncoding.EncodeToString(b[:])
	s.flows[code] = f
	return code
}
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("code_challenge_method") != "S256" {
		http.Error(w, "PKCE required", 400)
		return
	}
	code := s.issue(flow{q.Get("nonce"), q.Get("code_challenge"), q.Get("client_id")})
	u, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "redirect", 400)
		return
	}
	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), 302)
}
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flows[r.Form.Get("code")]
	delete(s.flows, r.Form.Get("code"))
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
		http.Error(w, "invalid grant", 400)
		return
	}
	claims := map[string]any{"iss": s.URL(), "sub": "synthetic-viewer", "aud": f.client, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": f.nonce, "name": "Lake viewer", "groups": s.Groups}
	for k, v := range s.Claims {
		claims[k] = v
	}
	alg := s.Algorithm
	if alg == "" {
		alg = "ES256"
	}
	encode := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	unsigned := encode(map[string]any{"alg": alg, "kid": fmt.Sprint(s.kid)}) + "." + encode(claims)
	hash := sha256.Sum256([]byte(unsigned))
	a, b, _ := ecdsa.Sign(rand.Reader, s.key, hash[:])
	sig := append(a.FillBytes(make([]byte, 32)), b.FillBytes(make([]byte, 32))...)
	if s.BadSignature {
		sig[0] ^= 1
	}
	jsonResponse(w, map[string]any{"access_token": "synthetic-only", "token_type": "Bearer", "id_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)})
}
func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	enc := func(n *big.Int) string { return base64.RawURLEncoding.EncodeToString(n.FillBytes(make([]byte, 32))) }
	jsonResponse(w, map[string]any{"keys": []any{map[string]any{"kty": "EC", "crv": "P-256", "kid": fmt.Sprint(s.kid), "alg": "ES256", "use": "sig", "x": enc(s.key.X), "y": enc(s.key.Y)}}})
}

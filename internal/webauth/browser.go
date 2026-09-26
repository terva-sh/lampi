package webauth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"terva.sh/lampi/internal/webconfig"
)

const (
	LoginPath  = "/auth/oidc/start"
	LogoutPath = "/auth/oidc/logout"
	attemptTTL = 10 * time.Minute
	idleTTL    = time.Hour
	hardTTL    = 12 * time.Hour
	maxEntries = 1024
)

type attempt struct {
	state, nonce, verifier, next string
	expires                      time.Time
}
type session struct {
	Identity   Identity
	CSRF       string
	Idle, Hard time.Time
}
type viewerKey struct{}

// Browser owns bounded, process-local sessions. A restart revokes all of them.
type Browser struct {
	// Logger is optional and must be set before serving requests.
	Logger   *slog.Logger
	cfg      webconfig.Config
	provider *Provider
	mu       sync.Mutex
	attempts map[string]attempt
	sessions map[string]session
	now      func() time.Time
}

func New(cfg webconfig.Config, client *http.Client) (*Browser, error) {
	p, err := NewProvider(cfg, client)
	if err != nil {
		return nil, err
	}
	return &Browser{cfg: p.cfg, provider: p, attempts: map[string]attempt{}, sessions: map[string]session{}, now: time.Now}, nil
}
func randomID() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func (b *Browser) cookieName(kind string) string {
	if b.cfg.Secure() {
		return "__Host-lampi_" + kind
	}
	return "lampi_dev_" + kind
}
func (b *Browser) cookie(w http.ResponseWriter, kind, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: b.cookieName(kind), Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: b.cfg.Secure(), SameSite: http.SameSiteLaxMode})
}
func (b *Browser) sweep() {
	now := b.now()
	for k, a := range b.attempts {
		if !a.expires.After(now) {
			delete(b.attempts, k)
		}
	}
	for k, s := range b.sessions {
		if !s.Idle.After(now) || !s.Hard.After(now) {
			delete(b.sessions, k)
		}
	}
}
func (b *Browser) Routes(m *http.ServeMux) {
	m.HandleFunc("GET "+LoginPath, b.start)
	m.HandleFunc("GET "+webconfig.CallbackPath, b.callback)
	m.HandleFunc("POST "+LogoutPath, b.logout)
}

// Headers applies only to the browser surface, not the ingestion protocol.
func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

var errorPage = template.Must(template.New("auth-error").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Lake sign-in</title><main><h1>{{.}}</h1><p><a href="/auth/oidc/start">Try signing in again</a></p></main></html>`))

func authError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = errorPage.Execute(w, message)
}
func (b *Browser) start(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.sweep()
	full := len(b.attempts) >= maxEntries
	b.mu.Unlock()
	if full {
		b.refuse(w, r, 503, "Too many sign-in attempts. Try again later.")
		return
	}
	a := attempt{state: randomID(), nonce: randomID(), verifier: randomID(), next: safeReturn(r.URL.Query().Get("next")), expires: b.now().Add(attemptTTL)}
	to, err := b.provider.AuthURL(r.Context(), a.state, a.nonce, a.verifier)
	if err != nil {
		b.refuse(w, r, 503, "The identity provider is unavailable. Try again later.")
		return
	}
	id := randomID()
	b.mu.Lock()
	b.sweep()
	if len(b.attempts) >= maxEntries {
		b.mu.Unlock()
		b.refuse(w, r, 503, "Too many sign-in attempts. Try again later.")
		return
	}
	// Starting again from one browser replaces its abandoned attempt.
	if old, err := r.Cookie(b.cookieName("attempt")); err == nil {
		delete(b.attempts, old.Value)
	}
	b.attempts[id] = a
	b.mu.Unlock()
	b.cookie(w, "attempt", id, int(attemptTTL.Seconds()))
	http.Redirect(w, r, to, 303)
}
func (b *Browser) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(b.cookieName("attempt"))
	b.cookie(w, "attempt", "", -1)
	if err != nil {
		b.refuse(w, r, 403, "Sign-in could not be completed.")
		return
	}
	b.mu.Lock()
	a, ok := b.attempts[c.Value]
	delete(b.attempts, c.Value)
	b.mu.Unlock()
	q := r.URL.Query()
	if !ok || !a.expires.After(b.now()) || subtle.ConstantTimeCompare([]byte(a.state), []byte(q.Get("state"))) != 1 || q.Get("error") != "" {
		b.refuse(w, r, 403, "Sign-in could not be completed.")
		return
	}
	id, err := b.provider.Exchange(r.Context(), q.Get("code"), a.nonce, a.verifier)
	if err != nil {
		b.refuse(w, r, 403, "Sign-in could not be completed.")
		return
	}
	if !id.Viewer {
		b.refuse(w, r, 403, "Your account has no lake viewer access. Ask the operator to map your group.")
		return
	}
	now := b.now()
	key := randomID()
	s := session{Identity: id, CSRF: randomID(), Idle: now.Add(idleTTL), Hard: now.Add(hardTTL)}
	b.mu.Lock()
	b.sweep()
	if old, err := r.Cookie(b.cookieName("session")); err == nil {
		delete(b.sessions, old.Value)
	}
	if len(b.sessions) >= maxEntries {
		b.mu.Unlock()
		b.refuse(w, r, 503, "Too many active sessions. Try again later.")
		return
	}
	b.sessions[key] = s
	b.mu.Unlock()
	b.cookie(w, "session", key, int(hardTTL.Seconds()))
	http.Redirect(w, r, a.next, 303)
}
func (b *Browser) lookup(r *http.Request) (session, bool) {
	c, err := r.Cookie(b.cookieName("session"))
	if err != nil || len(c.Value) != 43 {
		return session{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.sessions[c.Value]
	now := b.now()
	if !ok || !s.Idle.After(now) || !s.Hard.After(now) {
		delete(b.sessions, c.Value)
		return session{}, false
	}
	s.Idle = now.Add(idleTTL)
	b.sessions[c.Value] = s
	return s, true
}
func Current(r *http.Request) (Identity, string) {
	s, _ := r.Context().Value(viewerKey{}).(session)
	return s.Identity, s.CSRF
}
func (b *Browser) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := b.lookup(r)
		if !ok {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.Header.Get("X-Lampi-Refresh") == "1" {
				jsonError(w, 401, "not_authenticated")
				return
			}
			http.Redirect(w, r, LoginPath+"?next="+url.QueryEscape(safeReturn(r.URL.RequestURI())), 303)
			return
		}
		if !s.Identity.Viewer {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.Header.Get("X-Lampi-Refresh") == "1" {
				jsonError(w, 403, "not_authorized")
			} else {
				b.refuse(w, r, 403, "Your account has no lake viewer access.")
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), viewerKey{}, s)))
	})
}
func jsonError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
func (b *Browser) logout(w http.ResponseWriter, r *http.Request) {
	s, ok := b.lookup(r)
	if !ok {
		b.refuse(w, r, 401, "Your session has ended.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != b.cfg.BaseURL) || r.Header.Get("Sec-Fetch-Site") == "cross-site" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(s.CSRF)) != 1 {
		b.refuse(w, r, 403, "The sign-out request could not be verified.")
		return
	}
	c, _ := r.Cookie(b.cookieName("session"))
	b.mu.Lock()
	delete(b.sessions, c.Value)
	b.mu.Unlock()
	b.cookie(w, "session", "", -1)
	http.Redirect(w, r, "/", 303)
}

// Decode repeatedly to reject double-encoded network paths and backslashes.
// Ordinary query strings survive; the returned value is the original local URL.
func safeReturn(raw string) string {
	if len(raw) > 2048 {
		return "/"
	}
	decoded := raw
	for n := 0; n < 8; n++ {
		if !strings.HasPrefix(decoded, "/") || strings.HasPrefix(decoded, "//") || strings.ContainsAny(decoded, "\\\r\n\x00") {
			return "/"
		}
		u, err := url.Parse(decoded)
		if err != nil || u.IsAbs() || u.Host != "" {
			return "/"
		}
		next, err := url.PathUnescape(decoded)
		if err != nil {
			return "/"
		}
		if next == decoded {
			return raw
		}
		decoded = next
	}
	return "/"
}

// All reasons passed here are fixed categories, never provider errors or input.
func (b *Browser) refuse(w http.ResponseWriter, r *http.Request, status int, reason string) {
	if b.Logger != nil {
		b.Logger.WarnContext(r.Context(), "browser auth refused", "status", status, "reason", reason)
	}
	authError(w, status, reason)
}

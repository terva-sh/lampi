package web

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/webauth"
)

// Raw-read tokens (TKT-01M3NM6FW7). An admin mints a bearer token that
// reads raw artifacts, for a tool with no browser session. It reads the
// raw artifact route and nothing else: no page, no /api/web/v1 route,
// no /v1 ingestion. Only its SHA-256 is stored, and mint, revoke and
// every read are audited.

const (
	adminReadTokensPath = "/admin/read-tokens"
	// ReadTokenPrefix starts every read token, so that a leaked one is
	// easy to recognise in a log or a secret scanner.
	ReadTokenPrefix = "lrt_"
	// maxReadTokenLabel bounds a token's label, in characters.
	maxReadTokenLabel = 80
)

// rawTokenPath is the raw artifact route a read token authenticates.
func rawTokenPath(uid, digest string) string {
	return "/api/raw/v1/sessions/" + uid + "/artifacts/" + digest
}

// readTokenLifetimes are the expiries the mint form offers. The first
// is the default and the last the maximum: every token expires.
var readTokenLifetimes = []struct{ Value, Label string }{
	{"24h", "1 day"}, {"1h", "1 hour"}, {"168h", "7 days"}, {"720h", "30 days"}, {"2160h", "90 days"},
}

func (s *Server) readTokenRoutes(m *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.AdminOnly(h)) }
	m.Handle("GET "+adminReadTokensPath, admin(s.readTokensPage))
	m.Handle("POST "+adminReadTokensPath, admin(s.mintReadTokenPage))
	m.Handle("POST "+adminReadTokensPath+"/{id}/revoke", admin(s.revokeReadTokenPage))
	// Not behind Guard: the bearer token is the only credential it takes.
	m.HandleFunc("GET "+rawTokenPath("{uid}", "{digest}"), s.rawByToken)
}

// readTokenView is one token as the page shows it. It never holds the
// secret.
type readTokenView struct {
	// Scope is the table's column; Reach finishes "It reads raw
	// artifacts of" on the shown-once panel. Both name a bay limit.
	ID, Label, State, Scope      string
	Reach                        string
	Created, CreatedBy, Expires  string
	Revoked, RevokedBy, LastUsed string
	Permissions                  string
}

// viewReadToken shows t. names maps a bay id to its name; a bay not in
// it is shown by id.
func viewReadToken(t catalog.ReadToken, names map[string]string, now time.Time) readTokenView {
	scope, reach := "Every session", "every session"
	if len(t.Sessions) > 0 {
		scope, reach = strings.Join(t.Sessions, " "), "the listed sessions"
	}
	if t.BayScoped {
		// A bay limit is shown, or the token reads as wider than it is
		// (review 1415).
		var bays []string
		for _, id := range t.Bays {
			bays = append(bays, nameOr(names, id))
		}
		in := "no bay, so nothing"
		if len(bays) > 0 {
			in = "bays " + strings.Join(bays, ", ")
		}
		if len(t.Sessions) > 0 {
			scope, reach = scope+" · in "+in, "the listed sessions that are in "+in
		} else {
			scope, reach = "Sessions in "+in, "the sessions in "+in
		}
	}
	return readTokenView{ID: t.ID, Label: t.Label, State: t.State(now), Scope: scope, Reach: reach, Permissions: strings.Join(t.Permissions, " "),
		Created: stampOf(t.Created), CreatedBy: t.CreatedBy, Expires: stampOf(t.Expires),
		Revoked: stampOf(t.Revoked), RevokedBy: t.RevokedBy, LastUsed: stampOf(t.LastUsed)}
}

// bayNames maps each bay id to its name, or is empty when the bays
// cannot be read; a token then shows its bays by id.
func (s *Server) bayNames(r *http.Request) map[string]string {
	names := map[string]string{}
	bays, err := s.catalog.Bays(r.Context())
	if err != nil {
		s.logError(r, "reading bays for the read token page failed", err)
		return names
	}
	for _, b := range bays {
		names[b.ID] = b.Name
	}
	return names
}

func nameOr(names map[string]string, id string) string {
	if n := names[id]; n != "" {
		return n
	}
	return id
}

// readTokensView is the admin page: tokens, the mint form, and right
// after a mint the token, shown once.
type readTokensView struct {
	Tokens    []readTokenView
	Fresh     bool
	FreshURL  string
	Lifetimes []struct{ Value, Label string }
	Form      readTokenForm
	Attempt   string
	Problem   string
	// Minted is the token just minted, and Secret its value. Secret is
	// set on this one response and nowhere else.
	Minted *readTokenView
	Secret string
	// Example is a fetch with the token, for the minted panel.
	Example string
}

type readTokenForm struct{ Label, Sessions, Bays, Expires string }

func (s *Server) renderReadTokens(w http.ResponseWriter, r *http.Request, v readTokensView, status int) {
	now := s.now()
	tokens, err := s.reg.Lake().Catalog.ReadTokens(r.Context())
	if err != nil {
		s.logError(r, "listing read tokens failed", err)
		// A minted token is shown however the list fares: it cannot be
		// shown again.
		if v.Minted == nil {
			fail(w, r, err)
			return
		}
		v.Problem = "The token was minted, but the list of tokens could not be read. Operator logs hold the details."
	}
	names := s.bayNames(r)
	for _, t := range tokens {
		v.Tokens = append(v.Tokens, viewReadToken(t, names, now))
	}
	v.Fresh = webauth.Fresh(r, now)
	v.FreshURL = webauth.FreshLoginURL(adminReadTokensPath)
	v.Attempt = s.attempts.issue(now)
	v.Lifetimes = readTokenLifetimes
	if v.Form.Expires == "" {
		v.Form.Expires = readTokenLifetimes[0].Value
	}
	renderStatus(w, r, pageData{Title: "Read tokens", View: "read-tokens", ReadTokens: v, AsOf: stampOf(now)}, status)
}

func (s *Server) readTokensPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, r, catalog.ErrPage)
		return
	}
	s.renderReadTokens(w, r, readTokensView{}, http.StatusOK)
}

// readTokenProblems says what to do about each refusal the form meets.
var readTokenProblems = map[string]string{
	"invalid_label":    "A label is 1 to 80 printable characters. Say what the token is for.",
	"invalid_expiry":   "Choose one of the listed expiries.",
	"invalid_sessions": "List session UIDs separated by spaces or new lines, at most 100, or leave the list empty for every session.",
	"unknown_session":  "A listed session is not in the lake. Check the UIDs.",
	"unknown_bay":      "A listed bay is not in the lake. serve bays list names them.",
	"invalid_bays":     "The bays field holds separators and no bay. Leave it empty for no bay limit.",
	"mint_failed":      "Minting failed. Operator logs hold the details.",
	"audit_failed":     "Writing the mint to the audit log failed, so the token was revoked and is not shown. Operator logs hold the details. Fix the audit log and mint again.",
}

func (s *Server) mintReadTokenPage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	now := s.now()
	// Minting adds access to the lake, so it needs a recent sign-in, as
	// minting a registration code does.
	if !webauth.Fresh(r, now) {
		http.Redirect(w, r, webauth.FreshLoginURL(adminReadTokensPath), http.StatusSeeOther)
		return
	}
	form := readTokenForm{Label: strings.TrimSpace(r.PostForm.Get("label")), Sessions: r.PostForm.Get("sessions"), Bays: r.PostForm.Get("bays"), Expires: r.PostForm.Get("expires")}
	attempt := r.PostForm.Get("attempt")
	if prior, ok := s.attempts.claim(attempt, now); !ok {
		problem := "This form was already sent, or is out of date. Mint again from the form below."
		if prior != "" {
			problem = "This form already minted " + prior + ", and its token was shown once. It is not shown again. If it was not copied, revoke " + prior + " below and mint another."
		}
		s.renderReadTokens(w, r, readTokensView{Problem: problem}, http.StatusConflict)
		return
	}
	t, code := s.readTokenRequest(r, form, now)
	if code != "" {
		s.attempts.forget(attempt)
		if !slices.ContainsFunc(readTokenLifetimes, func(l struct{ Value, Label string }) bool { return l.Value == form.Expires }) {
			form.Expires = ""
		}
		status := http.StatusBadRequest
		if code == "mint_failed" {
			status = http.StatusInternalServerError
		}
		s.renderReadTokens(w, r, readTokensView{Form: form, Problem: readTokenProblems[code]}, status)
		return
	}
	secret, err := newReadTokenSecret()
	if err != nil {
		s.attempts.forget(attempt)
		s.logError(r, "making a read token failed", err)
		s.renderReadTokens(w, r, readTokensView{Form: form, Problem: readTokenProblems["mint_failed"]}, http.StatusInternalServerError)
		return
	}
	lake := s.reg.Lake()
	t, err = lake.Catalog.CreateReadToken(r.Context(), t, hashReadToken(secret), now)
	if err != nil {
		s.attempts.forget(attempt)
		s.logError(r, "minting a read token failed", err)
		s.renderReadTokens(w, r, readTokensView{Form: form, Problem: readTokenProblems["mint_failed"]}, http.StatusInternalServerError)
		return
	}
	// The mint is in audit.jsonl before the secret is shown. A token
	// whose line cannot be written is revoked and never shown, as
	// registrar.Mint does with a code.
	if err := lake.Catalog.FlushAudit(r.Context(), lake.Dir); err != nil {
		s.attempts.forget(attempt)
		s.logError(r, "writing a read token's mint to the audit log failed", err)
		problem := readTokenProblems["audit_failed"]
		if _, rerr := lake.Catalog.RevokeReadToken(r.Context(), t.ID, "system: audit write failed", now); rerr != nil {
			s.logError(r, "revoking a read token whose mint was not audited failed", rerr)
			problem = "Writing the mint to the audit log failed, and revoking the token failed too. Revoke " + t.ID + " below. Operator logs hold the details."
		}
		s.renderReadTokens(w, r, readTokensView{Form: form, Problem: problem}, http.StatusInternalServerError)
		return
	}
	s.attempts.done(attempt, t.ID)
	v := viewReadToken(t, s.bayNames(r), now)
	out := readTokensView{Minted: &v, Secret: secret, Example: "curl -fsS -H 'Authorization: Bearer " + secret + "' -o artifact " + s.origin + rawTokenPath("SESSION_UID", "SHA256")}
	s.renderReadTokens(w, r, out, http.StatusOK)
}

// readTokenRequest checks the form and returns the token it asks for,
// or the problem code that refuses it.
func (s *Server) readTokenRequest(r *http.Request, f readTokenForm, now time.Time) (catalog.ReadToken, string) {
	if n := len([]rune(f.Label)); n == 0 || n > maxReadTokenLabel || strings.ContainsFunc(f.Label, unicode.IsControl) {
		return catalog.ReadToken{}, "invalid_label"
	}
	i := slices.IndexFunc(readTokenLifetimes, func(l struct{ Value, Label string }) bool { return l.Value == f.Expires })
	if i < 0 {
		return catalog.ReadToken{}, "invalid_expiry"
	}
	life, _ := time.ParseDuration(readTokenLifetimes[i].Value)
	var sessions []string
	for _, uid := range strings.FieldsFunc(f.Sessions, func(r rune) bool { return unicode.IsSpace(r) || r == ',' }) {
		if !slices.Contains(sessions, uid) {
			sessions = append(sessions, uid)
		}
	}
	// An empty list means every session, so only a blank field may ask
	// for it. Separators alone are a mistyped scope, not a wider one.
	if len(sessions) > catalog.MaxReadTokenSessions || (len(sessions) == 0 && strings.TrimSpace(f.Sessions) != "") {
		return catalog.ReadToken{}, "invalid_sessions"
	}
	for _, uid := range sessions {
		if _, ok, err := s.catalog.Session(r.Context(), uid); err != nil || !ok {
			if err != nil {
				s.logError(r, "checking a read token's sessions failed", err)
				return catalog.ReadToken{}, "mint_failed"
			}
			return catalog.ReadToken{}, "unknown_session"
		}
	}
	// A bay list limits the token to sessions in those bays, checked on
	// each read, so a session sorted out of them later is out of reach.
	var bays []string
	for _, ref := range strings.FieldsFunc(f.Bays, func(r rune) bool { return unicode.IsSpace(r) || r == ',' }) {
		b, err := s.catalog.ResolveBay(r.Context(), ref)
		if errors.Is(err, catalog.ErrNoBay) {
			return catalog.ReadToken{}, "unknown_bay"
		}
		if err != nil {
			s.logError(r, "checking a read token's bays failed", err)
			return catalog.ReadToken{}, "mint_failed"
		}
		if !slices.Contains(bays, b.ID) {
			bays = append(bays, b.ID)
		}
	}
	if len(bays) == 0 && strings.TrimSpace(f.Bays) != "" {
		return catalog.ReadToken{}, "invalid_bays"
	}
	id, _ := webauth.Current(r)
	return catalog.ReadToken{Label: f.Label, Permissions: []string{catalog.PermRawRead}, Sessions: sessions, BayScoped: len(bays) > 0, Bays: bays, CreatedBy: actor(id).Audit, Expires: now.Add(life)}, ""
}

func (s *Server) revokeReadTokenPage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	id, _ := webauth.Current(r)
	lake := s.reg.Lake()
	_, err := lake.Catalog.RevokeReadToken(r.Context(), r.PathValue("id"), actor(id).Audit, s.now())
	switch {
	case errors.Is(err, catalog.ErrNoReadToken):
		http.NotFound(w, r)
		return
	case errors.Is(err, catalog.ErrReadTokenRevoked):
		// Someone revoked it first; it is revoked either way.
	case err != nil:
		s.logError(r, "revoking a read token failed", err)
		s.renderReadTokens(w, r, readTokensView{Problem: "Revoking failed. Operator logs hold the details."}, http.StatusInternalServerError)
		return
	default:
		if err := lake.Catalog.FlushAudit(r.Context(), lake.Dir); err != nil {
			// The revocation stands, and its event is queued in the same
			// transaction, so the next flush writes it. Say so rather than
			// answer as if nothing happened.
			s.logError(r, "revoked a read token but the audit line failed", err)
			s.renderReadTokens(w, r, readTokensView{Problem: "The token is revoked and no longer works. Writing that to the audit log failed; the line stays queued and is written at the next flush. Operator logs hold the details."}, http.StatusOK)
			return
		}
	}
	http.Redirect(w, r, adminReadTokensPath, http.StatusSeeOther)
}

// rawByToken serves a raw artifact to a bearer read token. A missing,
// unknown, expired or revoked token is 401. A token whose scope leaves
// out the session is 404, the same answer as a session or digest that
// is not there.
func (s *Server) rawByToken(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	now := s.now()
	t, ok, err := s.readTokenOf(r, now)
	if err != nil {
		// The token may be fine; a 401 would tell the tool to drop it.
		s.logError(r, "looking up a read token failed", err)
		apiError(w, http.StatusInternalServerError, "read_failed")
		return
	}
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="lampi-raw"`)
		apiError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	uid := r.PathValue("uid")
	if !t.Allows(catalog.PermRawRead, uid, now) {
		apiError(w, http.StatusNotFound, "not_found")
		return
	}
	reach, err := s.catalog.ReadTokenReaches(r.Context(), t, uid)
	if err != nil {
		s.logError(r, "checking a read token's bays failed", err)
		apiError(w, http.StatusInternalServerError, "read_failed")
		return
	}
	if !reach {
		apiError(w, http.StatusNotFound, "not_found")
		return
	}
	if err := s.reg.Lake().Catalog.TouchReadToken(r.Context(), t.ID, now); err != nil {
		// Last-used is a convenience; the read is still audited.
		s.logError(r, "recording a read token's use failed", err)
	}
	s.serveRaw(w, r, uid, r.PathValue("digest"), "token:"+t.ID+" ("+t.Label+")")
}

// readTokenOf finds the active read token in the request's
// Authorization header. ok is false for a missing, malformed, unknown,
// expired or revoked token; err is set only when the lookup failed.
func (s *Server) readTokenOf(r *http.Request, now time.Time) (catalog.ReadToken, bool, error) {
	secret, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !strings.HasPrefix(secret, ReadTokenPrefix) || len(secret) > 128 {
		return catalog.ReadToken{}, false, nil
	}
	t, found, err := s.reg.Lake().Catalog.ReadTokenBySecret(r.Context(), hashReadToken(secret))
	if err != nil {
		return catalog.ReadToken{}, false, err
	}
	return t, found && t.State(now) == "active", nil
}

func newReadTokenSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return ReadTokenPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func hashReadToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/webauth"
)

// codesView is the registrations page: codes by state, the mint form,
// and, right after a mint, the code shown once.
type codesView struct {
	Groups   []codeGroup
	Fresh    bool
	FreshURL string
	Profiles []string
	// Form holds what the operator typed when a mint is refused.
	Form mintRequest
	// Attempt names this form's mint, so a resubmission is recognised.
	Attempt string
	Problem string
	Minted  *mintedView
}

type codeGroup struct {
	State string
	Codes []codeView
}

// codeStates is the order the page lists codes in.
var codeStates = []string{"pending", "used", "expired", "revoked"}

// codeLifetimes are the expiries the mint form offers, shortest first.
// The first is the default.
var codeLifetimes = []struct{ Value, Label string }{
	{"1h", "1 hour"}, {"24h", "1 day"}, {"72h", "3 days"}, {"168h", "7 days"}, {"720h", "30 days"},
}

func (s *Server) registrationPages(m *http.ServeMux) {
	op := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.OperatorOnly(h)) }
	m.Handle("GET "+adminRegistrationsPath, op(s.codesPage))
	m.Handle("POST "+adminRegistrationsPath, op(s.mintPage))
	m.Handle("POST "+adminRegistrationsPath+"/{id}/revoke", op(s.revokePage))
}

// renderCodes lists the codes around v, which carries the form, a
// problem or a minted code.
func (s *Server) renderCodes(w http.ResponseWriter, r *http.Request, v codesView, status int) {
	now := s.now()
	items, err := s.codes(r, now)
	if err != nil {
		s.logError(r, "listing registration codes failed", err)
		// A minted code is shown however the list fares: it cannot be
		// shown again.
		if v.Minted == nil {
			fail(w, r, err)
			return
		}
		v.Problem = "The code was minted, but the list of codes could not be read. Operator logs hold the details."
	}
	for _, state := range codeStates {
		g := codeGroup{State: state}
		for _, c := range items {
			if c.State == state {
				g.Codes = append(g.Codes, c)
			}
		}
		v.Groups = append(v.Groups, g)
	}
	v.Fresh = webauth.Fresh(r, now)
	v.Attempt = s.attempts.issue(now)
	v.FreshURL = webauth.FreshLoginURL(adminRegistrationsPath)
	v.Profiles, err = s.reg.Lake().Catalog.ProfileNames(r.Context())
	if err != nil {
		// The form still offers the default, which is always there.
		s.logError(r, "listing profiles failed", err)
		v.Profiles = []string{config.DefaultProfile}
	}
	title := "Registration codes"
	view := "registrations"
	if v.Minted != nil {
		title, view = "Code for "+v.Minted.Registration.Name, "minted"
	}
	renderStatus(w, r, pageData{Title: title, View: view, Codes: v, AsOf: stampOf(now)}, status)
}

func (s *Server) codesPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, r, catalog.ErrPage)
		return
	}
	s.renderCodes(w, r, codesView{Form: mintRequest{Expires: codeLifetimes[0].Value}}, http.StatusOK)
}

// readForm parses a form POST and checks its CSRF token.
func (s *Server) readForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil || !s.auth.CheckWrite(r, r.PostForm.Get("csrf")) {
		renderStatus(w, r, pageData{Title: "Request refused", View: "refused"}, http.StatusForbidden)
		return false
	}
	return true
}

// mintProblems says what to do about each refusal the form can meet.
var mintProblems = map[string]string{
	"invalid_name":    "A device name is lowercase letters, digits, '.', '-' and '_', at most 64 characters.",
	"invalid_expiry":  "Choose one of the listed expiries.",
	"unknown_profile": "That profile is not in the lake any more. Choose another.",
	"name_taken":      "A device or a pending code already has that name. Cancel the pending code first, or choose another name.",
	"rate_limited":    "Too many codes were minted just now. Wait a minute and try again.",
	"lake_not_ready":  "The lake cannot mint yet: it needs an identity and a public URL that reaches it. Operator logs hold the details.",
	"mint_failed":     "Minting failed. Operator logs hold the details.",
}

func (s *Server) mintPage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	req := mintRequest{Name: strings.TrimSpace(r.PostForm.Get("name")), Profile: r.PostForm.Get("profile"), Expires: r.PostForm.Get("expires")}
	// A reload of the page that showed a code posts the same form again.
	// It must not mint a second code, and it cannot show the first again,
	// so it says what happened.
	if !webauth.Fresh(r, s.now()) {
		http.Redirect(w, r, webauth.FreshLoginURL(adminRegistrationsPath), http.StatusSeeOther)
		return
	}
	attempt := r.PostForm.Get("attempt")
	if prior, ok := s.attempts.claim(attempt, s.now()); !ok {
		problem := "This form was already sent, or is out of date. Mint again from the form below."
		if prior != "" {
			problem = "This form already minted " + prior + ", and its code was shown once. It is not shown again. If it was not copied, cancel " + prior + " below and mint another."
		}
		s.renderCodes(w, r, codesView{Form: mintRequest{Expires: codeLifetimes[0].Value}, Problem: problem}, http.StatusConflict)
		return
	}
	if !slices.ContainsFunc(codeLifetimes, func(l struct{ Value, Label string }) bool { return l.Value == req.Expires }) {
		req.Expires = "invalid"
	}
	v, err := s.mint(r, req)
	if err != nil {
		// A refused form can be corrected and sent again.
		s.attempts.forget(attempt)
		status, code := mintStatus(err)
		if code == "fresh_login_required" {
			http.Redirect(w, r, webauth.FreshLoginURL(adminRegistrationsPath), http.StatusSeeOther)
			return
		}
		if status >= 500 {
			s.logError(r, "minting a registration code failed", err)
		}
		if req.Expires == "invalid" {
			req.Expires = codeLifetimes[0].Value
		}
		s.renderCodes(w, r, codesView{Form: req, Problem: mintProblems[code]}, status)
		return
	}
	s.attempts.done(attempt, v.Registration.ID)
	s.renderCodes(w, r, codesView{Form: mintRequest{Expires: codeLifetimes[0].Value}, Minted: &v}, http.StatusOK)
}

func (s *Server) revokePage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	ref := r.PathValue("id")
	if !strings.HasPrefix(ref, "reg_") {
		http.NotFound(w, r)
		return
	}
	_, status, code := s.revoke(r, ref)
	// A code someone else cancelled first is cancelled either way.
	if code == "" || code == "already_revoked" {
		http.Redirect(w, r, adminRegistrationsPath, http.StatusSeeOther)
		return
	}
	problem := map[string]string{
		"not_found":     "There is no such code.",
		"already_used":  "That code was already used. Revoke the device it made from Devices.",
		"audit_failed":  "The code is cancelled, but writing it to the audit log failed. Operator logs hold the details.",
		"revoke_failed": "Cancelling failed. Operator logs hold the details.",
	}[code]
	s.renderCodes(w, r, codesView{Form: mintRequest{Expires: codeLifetimes[0].Value}, Problem: problem}, status)
}

// mintAttempts issues a token for each mint form and remembers which
// were sent, and the code each minted. A token names the process that
// issued it and when, and is refused once it is older than attemptTTL or
// from another process, so a form reloaded after its record is gone is
// out of date rather than new. It holds no secret.
type mintAttempts struct {
	once sync.Once
	boot string
	mu   sync.Mutex
	seen map[string]mintAttempt
}

type mintAttempt struct {
	id      string
	expires time.Time
}

const (
	attemptTTL  = time.Hour
	maxAttempts = 1024
)

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *mintAttempts) init() { a.once.Do(func() { a.boot = randomHex(8) }) }

// issue makes the token for one mint form.
func (a *mintAttempts) issue(now time.Time) string {
	a.init()
	return a.boot + "." + strconv.FormatInt(now.Unix(), 10) + "." + randomHex(16)
}

// claim records attempt as in flight. It fails for a token already
// sent, returning the code it minted if any, and for a token that is
// malformed, expired or from another process.
func (a *mintAttempts) claim(attempt string, now time.Time) (string, bool) {
	a.init()
	parts := strings.Split(attempt, ".")
	if len(parts) != 3 || parts[0] != a.boot || len(parts[2]) != 32 {
		return "", false
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", false
	}
	expires := time.Unix(unix, 0).Add(attemptTTL)
	if !now.Before(expires) || time.Unix(unix, 0).After(now.Add(time.Minute)) {
		return "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen == nil {
		a.seen = map[string]mintAttempt{}
	}
	if prior, ok := a.seen[attempt]; ok {
		return prior.id, false
	}
	if len(a.seen) >= maxAttempts {
		for k, v := range a.seen {
			if !now.Before(v.expires) {
				delete(a.seen, k)
			}
		}
	}
	if len(a.seen) >= maxAttempts {
		return "", false
	}
	// The record lives as long as the token is valid, so a token is
	// never both unrecorded and accepted twice.
	a.seen[attempt] = mintAttempt{expires: expires}
	return "", true
}

func (a *mintAttempts) done(attempt, id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e, ok := a.seen[attempt]; ok {
		e.id = id
		a.seen[attempt] = e
	}
}

func (a *mintAttempts) forget(attempt string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.seen, attempt)
}

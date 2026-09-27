package web

import (
	"net/http"
	"slices"
	"strings"

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
	Form    mintRequest
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
		fail(w, err)
		return
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
	v.FreshURL = webauth.FreshLoginURL(adminRegistrationsPath)
	v.Profiles = []string{config.DefaultProfile}
	for name := range s.reg.Lake().Profiles {
		if name != config.DefaultProfile {
			v.Profiles = append(v.Profiles, name)
		}
	}
	slices.Sort(v.Profiles[1:])
	title := "Registration codes"
	view := "registrations"
	if v.Minted != nil {
		title, view = "Code for "+v.Minted.Registration.Name, "minted"
	}
	renderStatus(w, r, pageData{Title: title, View: view, Codes: v, AsOf: stampOf(now)}, status)
}

func (s *Server) codesPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, catalog.ErrPage)
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
	"unknown_profile": "That profile is not in the lake's profiles file any more. Choose another.",
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
	if !slices.ContainsFunc(codeLifetimes, func(l struct{ Value, Label string }) bool { return l.Value == req.Expires }) {
		req.Expires = "invalid"
	}
	v, err := s.mint(r, req)
	if err != nil {
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
		"already_used":  "That code was already used. Revoke the device it made with serve devices revoke.",
		"audit_failed":  "The code is cancelled, but writing it to the audit log failed. Operator logs hold the details.",
		"revoke_failed": "Cancelling failed. Operator logs hold the details.",
	}[code]
	s.renderCodes(w, r, codesView{Form: mintRequest{Expires: codeLifetimes[0].Value}, Problem: problem}, status)
}

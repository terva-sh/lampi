package web

import (
	"errors"
	"net/http"
	"slices"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/webauth"
)

// The admin's bays page (TKT-01M3NNF2K3): each bay with its session
// count, the inbox with why each session is in it, and the two changes
// the dashboard makes to membership, moving one session and releasing
// a hold. Both add a session to a bay, so each needs a sign-in in the
// last 10 minutes as well as the form's CSRF token, and each is audited
// with the admin as its actor. Bulk moves and rules stay on the host
// CLI (docs/bays-inbox.md).

const adminBaysPath = "/admin/bays"

func (s *Server) bayRoutes(m *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.AdminOnly(h)) }
	m.Handle("GET "+adminBaysPath, admin(s.baysPage))
	m.Handle("POST "+adminBaysPath+"/move", admin(s.moveSessionPage))
	m.Handle("POST "+adminBaysPath+"/release", admin(s.releaseHoldPage))
}

// baysView is the admin's bays page.
type baysView struct {
	Bays     []bayRow
	Inbox    []inboxRow
	Fresh    bool
	FreshURL string
	Problem  string
	Done     string
}

type bayRow struct {
	ID, Name string
	Aliases  []string
	Default  bool
	Off      bool
	Sessions int
}

type inboxRow struct {
	UID, Harness, NativeID, Where string
	Bays                          []bayRow // the bays it is in, id and name
	Reasons                       []string
	Held                          bool
}

func (s *Server) renderBays(w http.ResponseWriter, r *http.Request, v baysView, status int) {
	ctx, cancel := readContext(r)
	defer cancel()
	bays, err := s.catalog.Bays(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	counts, err := s.catalog.BaySessionCounts(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	byID := map[string]bayRow{}
	for _, b := range bays {
		row := bayRow{ID: b.ID, Name: b.Name, Aliases: b.Aliases, Default: b.Default, Off: b.Disabled, Sessions: counts[b.ID]}
		byID[b.ID] = row
		v.Bays = append(v.Bays, row)
	}
	entries, err := s.catalog.Inbox(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	holds, err := s.catalog.Holds(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	for _, e := range entries {
		row := inboxRow{UID: e.SessionUID, Harness: e.Harness, NativeID: e.NativeID, Where: e.CWD, Reasons: e.Reasons}
		if e.GitRemote != "" {
			row.Where += " " + e.GitRemote
		}
		for _, id := range e.Bays {
			row.Bays = append(row.Bays, byID[id])
		}
		row.Held = slices.ContainsFunc(holds, func(h catalog.Hold) bool { return h.SessionUID == e.SessionUID })
		v.Inbox = append(v.Inbox, row)
	}
	now := s.now()
	v.Fresh = webauth.Fresh(r, now)
	v.FreshURL = webauth.FreshLoginURL(adminBaysPath)
	renderStatus(w, r, pageData{Title: "Bays", View: "bays", Bays: v, AsOf: stampOf(now)}, status)
}

func (s *Server) baysPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, r, catalog.ErrPage)
		return
	}
	s.renderBays(w, r, baysView{}, http.StatusOK)
}

// bayProblems says what to do about each refusal.
var bayProblems = map[string]string{
	"unknown_bay":   "That bay is not in the lake any more. Reload the page and choose another.",
	"not_a_member":  "The session is not in that bay any more. Reload the page.",
	"same_bay":      "Choose a bay other than the one the session is leaving.",
	"not_held":      "The session is not held any more. Reload the page.",
	"held":          "This session is held for review. Release it to place it; a move would skip the review.",
	"not_found":     "The session is not in the lake any more.",
	"change_failed": "The change failed. Operator logs hold the details.",
	"audit_failed":  "The change was made, but writing it to the audit log failed; the line stays queued. Operator logs hold the details.",
}

// bayChange runs change for the signed-in admin, after the CSRF and
// recent sign-in checks, and answers with the page. change reads the
// form, which readForm has parsed under its size cap, and says what it
// did.
func (s *Server) bayChange(w http.ResponseWriter, r *http.Request, change func(actor string) (string, error)) {
	if !s.readForm(w, r) {
		return
	}
	now := s.now()
	if !webauth.Fresh(r, now) {
		http.Redirect(w, r, webauth.FreshLoginURL(adminBaysPath), http.StatusSeeOther)
		return
	}
	ident, _ := webauth.Current(r)
	done, err := change(actor(ident).Audit)
	code := ""
	switch {
	case err == nil:
	case errors.Is(err, catalog.ErrNoBay):
		code = "unknown_bay"
	case errors.Is(err, catalog.ErrNotAMember):
		code = "not_a_member"
	case errors.Is(err, catalog.ErrSameBay):
		code = "same_bay"
	case errors.Is(err, catalog.ErrNoHold):
		code = "not_held"
	case errors.Is(err, catalog.ErrSessionHeld):
		code = "held"
	case errors.Is(err, catalog.ErrNoSession):
		code = "not_found"
	default:
		s.logError(r, "changing a session's bays failed", err)
		code = "change_failed"
	}
	if code != "" {
		s.renderBays(w, r, baysView{Problem: bayProblems[code]}, http.StatusBadRequest)
		return
	}
	lake := s.reg.Lake()
	if err := lake.Catalog.FlushAudit(r.Context(), lake.Dir); err != nil {
		s.logError(r, "changed a session's bays but the audit line failed", err)
		s.renderBays(w, r, baysView{Problem: bayProblems["audit_failed"]}, http.StatusInternalServerError)
		return
	}
	s.renderBays(w, r, baysView{Done: done}, http.StatusOK)
}

func (s *Server) moveSessionPage(w http.ResponseWriter, r *http.Request) {
	s.bayChange(w, r, func(who string) (string, error) {
		uid, from, to := r.PostForm.Get("uid"), r.PostForm.Get("from"), r.PostForm.Get("to")
		return "Moved " + uid + ".", s.catalog.MoveSession(r.Context(), uid, from, to, who, catalog.ViaWeb, s.now())
	})
}

func (s *Server) releaseHoldPage(w http.ResponseWriter, r *http.Request) {
	s.bayChange(w, r, func(who string) (string, error) {
		uid := r.PostForm.Get("uid")
		return "Released " + uid + "; it is in the bays it asked for.", s.catalog.ReleaseHold(r.Context(), uid, who, catalog.ViaWeb, s.now())
	})
}

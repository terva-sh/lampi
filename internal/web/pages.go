package web

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/webauth"
)

//go:embed templates/*.html assets/*
var files embed.FS
var pages = template.Must(template.New("page").Funcs(template.FuncMap{
	"sliceHarnesses":  func() []string { return []string{"terva", "claude", "codex", "opencode", "cursor", "cursor-cli"} },
	"sliceStates":     func() []string { return []string{"pending", "failed", "ready", "unknown"} },
	"sliceEventTypes": func() []string { return recall.EventTypes },
	"sliceActors":     func() []string { return recall.Actors },
	"short": func(s string) string {
		if len(s) > 14 {
			return s[:10] + "…"
		}
		return s
	},
	"human": func(n int64) string {
		s := fmt.Sprint(n)
		for i := len(s) - 3; i > 0; i -= 3 {
			s = s[:i] + "," + s[i:]
		}
		return s
	},
	"profileURL": profileURL,
	"projectKey": func(p protocol.InventoryProject) string {
		if k, ok := catalog.ProjectKeyOf(p); ok {
			return k.String()
		}
		return ""
	},
	"reviewTab": func(f reviewFilter, hidden bool) string {
		f.Hidden = hidden
		return f.URL()
	},
	"reviewTable": func(v reviewView, rows []reviewRow, caption string, allow bool) reviewTableView {
		return reviewTableView{View: v, Rows: rows, Caption: caption, Allow: allow}
	},
	"deviceURL":     deviceURL,
	"allowable":     allowable,
	"denied":        func(reason string) bool { return reason == config.RefusedByDeny },
	"ruleText":      ruleText,
	"ownerOf":       ownerOf,
	"sessionURL":    func(uid string) string { return "/sessions/" + url.PathEscape(uid) },
	"transcriptURL": func(uid string) string { return "/sessions/" + url.PathEscape(uid) + "/transcript" },
	"fromURL": func(uid string, from int64) string {
		return "/sessions/" + url.PathEscape(uid) + "/transcript?from=" + strconv.FormatInt(from, 10)
	},
	"cursorURL": func(uid, cursor string) string {
		return "/sessions/" + url.PathEscape(uid) + "/transcript?cursor=" + url.QueryEscape(cursor)
	},
	"excerptURL": func(uid string, gen, from int64, count int) string {
		return "/sessions/" + url.PathEscape(uid) + "/excerpt?gen=" + strconv.FormatInt(gen, 10) + "&from=" + strconv.FormatInt(from, 10) + "&count=" + strconv.Itoa(count)
	},
	"str": func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	},
	"count": func(p *int) string {
		if p == nil {
			return "unknown"
		}
		return fmt.Sprint(*p)
	},
	"deref":       func(p *bool) bool { return p != nil && *p },
	"derefInt":    func(p *int64) int64 { return *p },
	"int64":       func(n int) int64 { return int64(n) },
	"sub":         func(a, b float64) float64 { return a - b },
	"signedBytes": signedBytes,
	"bytes":       bytesIEC,
	"permille":    func(n int64) string { return fmt.Sprintf("%.1f%%", float64(n)/10) },
	"kib":         func(n int) string { return fmt.Sprintf("%d KiB", (n+1023)/1024) },
	"lifetimes":   func() []struct{ Value, Label string } { return codeLifetimes },
	"revokeURL":   func(id string) string { return adminRegistrationsPath + "/" + url.PathEscape(id) + "/revoke" },
	"collectionURL": func(uid, kind string) string {
		return "/sessions/" + url.PathEscape(uid) + "?collection=" + url.QueryEscape(kind)
	},
}).ParseFS(files, "templates/*.html"))

type pageData struct {
	Title, View, Display, CSRF string
	Overview                   catalog.Overview
	Sessions                   catalog.Page[catalog.SessionSummary]
	Session                    catalog.SessionSummary
	Records                    catalog.Page[catalog.Record]
	Filters                    catalog.PageRequest
	Collection, NextURL, AsOf  string
	Poll                       bool
	Transcript                 recall.EventPage
	// Unavailable names why a transcript cannot be shown: a
	// normalization state, "missing", or "stale" for a link to a
	// generation that is no longer published.
	Unavailable string
	StaleGen    int64
	Target      int64
	HasTarget   bool
	Search      searchView
	Activity    activityView
	Ops         opsView
	// Operator shows the operator's navigation. Codes is the
	// registrations page.
	Operator bool
	Codes    codesView
	Devices  devicesView
	// Device is one device's page.
	Device   deviceView
	Profiles profilesView
	Profile  profileView
	// ProfileEdit is the operator's profile editor.
	ProfileEdit profileEditView
	// Review is the review queue's page, and Allow its confirm page.
	Review reviewView
	Allow  allowView
	// ReviewCount is how many projects need review, for the header;
	// -1 when unknown.
	ReviewCount int
	// Urgent names active devices whose agent matches an urgent
	// advisory. The overview and devices pages fill it.
	Urgent []string
}

// searchView is the search form and its results. Hits carry the
// snippet split around the match so the template marks it without
// building HTML.
type searchView struct {
	Enabled  bool
	Asked    bool
	Invalid  bool
	Form     url.Values
	Hits     []hitView
	Coverage recall.Coverage
}

type hitView struct {
	recall.Hit
	Before, Match, After string
}

func splitHit(h recall.Hit) hitView {
	v := hitView{Hit: h, Before: h.Snippet}
	if h.MatchLen > 0 && h.MatchStart >= 0 && h.MatchStart+h.MatchLen <= len(h.Snippet) {
		v.Before = h.Snippet[:h.MatchStart]
		v.Match = h.Snippet[h.MatchStart : h.MatchStart+h.MatchLen]
		v.After = h.Snippet[h.MatchStart+h.MatchLen:]
	}
	return v
}

func (s *Server) pageRoutes(m *http.ServeMux) {
	for path, h := range map[string]http.HandlerFunc{"/{$}": s.homePage, "/sessions": s.sessionsPage, "/sessions/{uid}": s.detailPage, "/conflicts": s.conflictsPage, "/sessions/{uid}/transcript": s.transcriptPage, "/search": s.searchPage, "/sessions/{uid}/excerpt": s.excerptPage, "/activity": s.activityPage, "/operations": s.operationsPage, "/devices": s.devicesPage, "/devices/{id}": s.devicePage, "/profiles": s.profilesPage, "/profiles/{name}": s.profilePage, reviewPath: s.reviewPage} {
		m.Handle("GET "+path, s.guardRead(h))
	}
	assets, _ := fs.Sub(files, "assets")
	m.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
}
func render(w http.ResponseWriter, r *http.Request, d pageData) {
	renderStatus(w, r, d, http.StatusOK)
}
func renderStatus(w http.ResponseWriter, r *http.Request, d pageData, status int) {
	id, csrf := webauth.Current(r)
	d.Display = id.Display
	d.CSRF = csrf
	d.Operator = id.Operator
	d.ReviewCount = -1
	if s, ok := r.Context().Value(serverKey{}).(*Server); ok && (id.Viewer || id.Operator) {
		d.ReviewCount = s.reviewCount(r)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = pages.ExecuteTemplate(w, "layout", d)
}
func nextURL(r *http.Request, cursor string) string {
	if cursor == "" {
		return ""
	}
	q := r.URL.Query()
	q.Set("cursor", cursor)
	return r.URL.Path + "?" + q.Encode()
}
func pageError(w http.ResponseWriter, r *http.Request, err error) { fail(w, r, err) }
func (s *Server) homePage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		pageError(w, r, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	overview, err := s.catalog.DashboardOverview(ctx)
	if err != nil {
		pageError(w, r, err)
		return
	}
	recent, err := s.catalog.DashboardSessions(ctx, catalog.PageRequest{Limit: 8})
	if err != nil {
		pageError(w, r, err)
		return
	}
	// The overview is where an urgent agent is seen first. Reading the
	// devices failing does not hide the rest of the page.
	var urgent []string
	if dv, err := s.readDevices(ctx, s.now()); err != nil {
		s.logError(r, "reading devices for the overview failed", err)
	} else {
		urgent = dv.Urgent
	}
	render(w, r, pageData{Title: "Overview", View: "overview", Overview: overview, Sessions: recent, AsOf: overview.AsOf, Poll: true, Urgent: urgent})
}
func (s *Server) sessionsPage(w http.ResponseWriter, r *http.Request) {
	p, err := parsePage(r.URL.Query(), true, false)
	if err != nil {
		pageError(w, r, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.catalog.DashboardSessions(ctx, p)
	if err != nil {
		pageError(w, r, err)
		return
	}
	render(w, r, pageData{Title: "Sessions", View: "sessions", Sessions: v, Filters: p, AsOf: v.AsOf, NextURL: nextURL(r, v.NextCursor), Poll: p.Cursor == ""})
}
func (s *Server) detailPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("collection")
	if kind == "" {
		kind = "artifacts"
	}
	if len(q["collection"]) > 1 {
		pageError(w, r, catalog.ErrPage)
		return
	}
	q.Del("collection")
	p, err := parsePage(q, false, kind == "artifacts")
	if err != nil {
		pageError(w, r, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	uid := r.PathValue("uid")
	summary, err := s.catalog.DashboardSession(ctx, uid)
	if err != nil {
		pageError(w, r, err)
		return
	}
	records, err := s.catalog.DashboardRecords(ctx, uid, kind, p)
	if err != nil {
		pageError(w, r, err)
		return
	}
	render(w, r, pageData{Title: "Session details", View: "detail", Session: summary, Records: records, Filters: p, Collection: strings.Title(kind), AsOf: records.AsOf, NextURL: nextURL(r, records.NextCursor)})
}
func (s *Server) conflictsPage(w http.ResponseWriter, r *http.Request) {
	p, err := parsePage(r.URL.Query(), false, false)
	if err != nil {
		pageError(w, r, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.catalog.DashboardRecords(ctx, "", "conflicts", p)
	if err != nil {
		pageError(w, r, err)
		return
	}
	render(w, r, pageData{Title: "Conflicts", View: "conflicts", Records: v, AsOf: v.AsOf, NextURL: nextURL(r, v.NextCursor)})
}

// transcriptPage shows one page of a session's published events. at
// is a deep link target: the page starts a few events before it and
// marks it. A gen that is no longer published is reported, not
// replaced with whatever now sits at that position.
func (s *Server) transcriptPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req, err := parseEvents(q, "at")
	if err != nil {
		pageError(w, r, err)
		return
	}
	d := pageData{Title: "Transcript", View: "transcript"}
	if q.Has("at") {
		at, err := strconv.ParseInt(q.Get("at"), 10, 64)
		if err != nil || at < 0 || q.Has("cursor") {
			pageError(w, r, recall.ErrInvalid)
			return
		}
		d.Target, d.HasTarget = at, true
		if !q.Has("from") {
			req.From = max(0, at-targetLead)
		}
	}
	ctx, cancel := readContext(r)
	defer cancel()
	uid := r.PathValue("uid")
	d.Session, err = s.catalog.DashboardSession(ctx, uid)
	if errors.Is(err, sql.ErrNoRows) {
		// A link to a purged session, or one this lake never had.
		d.Unavailable = "gone"
		renderStatus(w, r, d, http.StatusNotFound)
		return
	}
	if err != nil {
		pageError(w, r, err)
		return
	}
	d.Transcript, err = s.events.Events(ctx, uid, req)
	var unavailable recall.UnavailableError
	switch {
	case err == nil:
		d.AsOf = d.Transcript.AsOf
		render(w, r, d)
	case errors.As(err, &unavailable):
		d.Unavailable = unavailable.State
		renderStatus(w, r, d, http.StatusConflict)
	case errors.Is(err, recall.ErrGenerationChanged):
		d.Unavailable, d.StaleGen = "stale", req.Gen
		renderStatus(w, r, d, http.StatusConflict)
	default:
		pageError(w, r, err)
	}
}

// targetLead is how many events a deep link shows before its target.
const targetLead = 5

// searchPage is the GET search form and its results. An empty query
// shows the form only; it is not a way to list the corpus.
func (s *Server) searchPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := pageData{Title: "Search", View: "search"}
	d.Search.Enabled = s.index != nil
	d.Search.Form = q
	d.Filters.Harness = q.Get("harness")
	if !d.Search.Enabled {
		renderStatus(w, r, d, http.StatusServiceUnavailable)
		return
	}
	d.Search.Coverage = s.index.Coverage()
	// Nothing filled in: show the form. Session filters alone are
	// refused by Search, as a corpus listing.
	blank := true
	for k := range q {
		if q.Get(k) != "" {
			blank = false
		}
	}
	if blank {
		render(w, r, d)
		return
	}
	d.Search.Asked = true
	req, err := parseSearch(q)
	if err == nil {
		ctx, cancel := readContext(r)
		defer cancel()
		var page recall.SearchPage
		page, err = s.index.Search(ctx, req)
		if err == nil {
			for _, h := range page.Items {
				d.Search.Hits = append(d.Search.Hits, splitHit(h))
			}
			d.Search.Coverage = page.Coverage
			d.AsOf = page.AsOf
			d.NextURL = nextURL(r, page.NextCursor)
			render(w, r, d)
			return
		}
	}
	if errors.Is(err, recall.ErrInvalid) {
		d.Search.Invalid = true
		renderStatus(w, r, d, http.StatusBadRequest)
		return
	}
	pageError(w, r, err)
}

// excerptPage serves a copy-out span as plain text, for a browser
// without JavaScript and for anyone who would rather select text
// than press a button. text/plain with nosniff is never rendered as
// HTML, whatever the transcript holds.
func (s *Server) excerptPage(w http.ResponseWriter, r *http.Request) {
	req, err := parseExcerpt(r.URL.Query())
	if err == nil {
		ctx, cancel := readContext(r)
		defer cancel()
		req.Origin = s.origin
		var ex recall.Excerpt
		ex, err = s.events.Excerpt(ctx, r.PathValue("uid"), req)
		if err == nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(ex.Text))
			return
		}
	}
	status, msg := http.StatusInternalServerError, "The excerpt could not be read."
	var unavailable recall.UnavailableError
	switch {
	case errors.Is(err, recall.ErrInvalid):
		status, msg = http.StatusBadRequest, "That span is not valid: from must be a position in the transcript and count 1 to 200."
	case errors.Is(err, recall.ErrNotFound):
		status, msg = http.StatusNotFound, "This session is not in the lake."
	case errors.Is(err, recall.ErrGenerationChanged):
		status, msg = http.StatusConflict, "This transcript has changed since the span was chosen. Open the current transcript and choose it again."
	case errors.As(err, &unavailable):
		status, msg = http.StatusConflict, "This transcript is not available: "+unavailable.State+"."
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		status, msg = http.StatusServiceUnavailable, "The excerpt took too long to read. Try a shorter span."
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg + "\n"))
}

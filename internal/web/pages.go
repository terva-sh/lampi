package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/webauth"
)

//go:embed templates/*.html assets/*
var files embed.FS
var pages = template.Must(template.New("page").Funcs(template.FuncMap{
	"sliceHarnesses": func() []string { return []string{"terva", "claude", "codex", "opencode", "cursor", "cursor-cli"} },
	"sliceStates":    func() []string { return []string{"pending", "failed", "ready", "unknown"} },
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
	"sessionURL": func(uid string) string { return "/sessions/" + url.PathEscape(uid) },
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
}

func (s *Server) pageRoutes(m *http.ServeMux) {
	for path, h := range map[string]http.HandlerFunc{"/{$}": s.homePage, "/sessions": s.sessionsPage, "/sessions/{uid}": s.detailPage, "/conflicts": s.conflictsPage} {
		m.Handle("GET "+path, s.auth.Guard(h))
	}
	assets, _ := fs.Sub(files, "assets")
	m.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
}
func render(w http.ResponseWriter, r *http.Request, d pageData) {
	id, csrf := webauth.Current(r)
	d.Display = id.Display
	d.CSRF = csrf
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
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
func pageError(w http.ResponseWriter, err error) { fail(w, err) }
func (s *Server) homePage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		pageError(w, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	overview, err := s.catalog.DashboardOverview(ctx)
	if err != nil {
		pageError(w, err)
		return
	}
	recent, err := s.catalog.DashboardSessions(ctx, catalog.PageRequest{Limit: 8})
	if err != nil {
		pageError(w, err)
		return
	}
	render(w, r, pageData{Title: "Overview", View: "overview", Overview: overview, Sessions: recent, AsOf: overview.AsOf, Poll: true})
}
func (s *Server) sessionsPage(w http.ResponseWriter, r *http.Request) {
	p, err := parsePage(r.URL.Query(), true, false)
	if err != nil {
		pageError(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.catalog.DashboardSessions(ctx, p)
	if err != nil {
		pageError(w, err)
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
		pageError(w, catalog.ErrPage)
		return
	}
	q.Del("collection")
	p, err := parsePage(q, false, kind == "artifacts")
	if err != nil {
		pageError(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	uid := r.PathValue("uid")
	summary, err := s.catalog.DashboardSession(ctx, uid)
	if err != nil {
		pageError(w, err)
		return
	}
	records, err := s.catalog.DashboardRecords(ctx, uid, kind, p)
	if err != nil {
		pageError(w, err)
		return
	}
	render(w, r, pageData{Title: "Session details", View: "detail", Session: summary, Records: records, Filters: p, Collection: strings.Title(kind), AsOf: records.AsOf, NextURL: nextURL(r, records.NextCursor)})
}
func (s *Server) conflictsPage(w http.ResponseWriter, r *http.Request) {
	p, err := parsePage(r.URL.Query(), false, false)
	if err != nil {
		pageError(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.catalog.DashboardRecords(ctx, "", "conflicts", p)
	if err != nil {
		pageError(w, err)
		return
	}
	render(w, r, pageData{Title: "Conflicts", View: "conflicts", Records: v, AsOf: v.AsOf, NextURL: nextURL(r, v.NextCursor)})
}

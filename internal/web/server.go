// Package web serves the read-only browser surface. It owns no device tokens.
package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/webauth"
	"terva.sh/lampi/internal/webconfig"
)

type Server struct {
	catalog *catalog.Catalog
	events  *recall.Reader
	auth    *webauth.Browser
}

// New builds a handler mounted inside api.Server's request accounting.
// normalized is the lake's derived JSONL directory, read only through
// recall. client is nil in production; tests supply the trust pool of
// their synthetic HTTPS IdP.
func New(cfg webconfig.Config, cat *catalog.Catalog, normalized string, client *http.Client, loggers ...*slog.Logger) (http.Handler, error) {
	auth, err := webauth.New(cfg, client)
	if err != nil {
		return nil, err
	}
	if len(loggers) > 0 {
		auth.Logger = loggers[0]
	}
	s := &Server{catalog: cat, events: recall.NewReader(cat, normalized), auth: auth}
	m := http.NewServeMux()
	auth.Routes(m)
	get := func(path string, h http.HandlerFunc) { m.Handle("GET "+path, s.guardRead(h)) }
	get("/api/web/v1/overview", s.overview)
	get("/api/web/v1/sessions", s.sessions)
	get("/api/web/v1/sessions/{uid}", s.session)
	get("/api/web/v1/sessions/{uid}/{collection}", s.records)
	get("/api/web/v1/sessions/{uid}/events", s.sessionEvents)
	get("/api/web/v1/conflicts", s.conflicts)
	s.pageRoutes(m)
	return webauth.Headers(m), nil
}
func readContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 5*time.Second)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	status, code := 500, "read_failed"
	body := map[string]string{}
	var unavailable recall.UnavailableError
	switch {
	case errors.As(err, &unavailable):
		status, code = 409, "transcript_unavailable"
		body["state"] = unavailable.State
	case errors.Is(err, recall.ErrGenerationChanged):
		status, code = 409, "generation_changed"
	case errors.Is(err, recall.ErrInvalid):
		status, code = 400, "invalid_request"
	case errors.Is(err, recall.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, catalog.ErrPage):
		status, code = 400, "invalid_filters_or_cursor"
	case errors.Is(err, sql.ErrNoRows):
		status, code = 404, "not_found"
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		status, code = 503, "read_unavailable"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body["error"] = code
	_ = json.NewEncoder(w).Encode(body)
}
func parsePage(q url.Values, sessionFilters bool, artifacts bool) (catalog.PageRequest, error) {
	var p catalog.PageRequest
	for k, v := range q {
		if len(v) != 1 {
			return p, catalog.ErrPage
		}
		switch k {
		case "limit", "cursor":
		case "harness", "project", "unlinked", "state":
			if !sessionFilters {
				return p, catalog.ErrPage
			}
		case "current":
			if !artifacts {
				return p, catalog.ErrPage
			}
		default:
			return p, catalog.ErrPage
		}
	}
	p.Harness = q.Get("harness")
	p.Project = q.Get("project")
	p.State = q.Get("state")
	p.Cursor = q.Get("cursor")
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > 200 {
			return p, catalog.ErrPage
		}
		p.Limit = n
	}
	for _, entry := range []struct {
		key  string
		dest *bool
	}{{"unlinked", &p.Unlinked}, {"current", &p.Current}} {
		if !q.Has(entry.key) {
			continue
		}
		raw := q.Get(entry.key)
		if raw == "true" {
			*entry.dest = true
		} else if raw != "false" {
			return p, catalog.ErrPage
		}
	}
	return p, nil
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		fail(w, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.catalog.DashboardOverview(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	p, err := parsePage(r.URL.Query(), true, false)
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.catalog.DashboardSessions(ctx, p)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		fail(w, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.catalog.DashboardSession(ctx, r.PathValue("uid"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}
func (s *Server) records(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("collection")
	p, err := parsePage(r.URL.Query(), false, kind == "artifacts")
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	uid := r.PathValue("uid")
	if _, err := s.catalog.DashboardSession(ctx, uid); err != nil {
		fail(w, err)
		return
	}
	v, err := s.catalog.DashboardRecords(ctx, uid, kind, p)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}
func (s *Server) conflicts(w http.ResponseWriter, r *http.Request) {
	p, err := parsePage(r.URL.Query(), false, false)
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.catalog.DashboardRecords(ctx, "", "conflicts", p)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) guardRead(next http.HandlerFunc) http.Handler {
	return s.auth.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(r.URL.RawQuery) > 16384 {
			fail(w, catalog.ErrPage)
			return
		}
		next(w, r)
	}))
}

// parseEvents reads from, limit, cursor and gen. Every key appears at
// most once and nothing else is accepted.
func parseEvents(q url.Values, extra ...string) (recall.EventRequest, error) {
	var req recall.EventRequest
	for k, v := range q {
		if len(v) != 1 {
			return req, recall.ErrInvalid
		}
		switch k {
		case "from", "limit", "cursor", "gen":
		default:
			ok := false
			for _, e := range extra {
				ok = ok || k == e
			}
			if !ok {
				return req, recall.ErrInvalid
			}
		}
	}
	for _, f := range []struct {
		key  string
		dest *int64
	}{{"from", &req.From}, {"gen", &req.Gen}} {
		if !q.Has(f.key) {
			continue
		}
		n, err := strconv.ParseInt(q.Get(f.key), 10, 64)
		if err != nil || n < 0 {
			return req, recall.ErrInvalid
		}
		*f.dest = n
	}
	req.Pinned = q.Has("gen")
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > recall.MaxLimit {
			return req, recall.ErrInvalid
		}
		req.Limit = n
	}
	req.Cursor = q.Get("cursor")
	if q.Has("cursor") && req.Cursor == "" {
		return req, recall.ErrInvalid
	}
	return req, nil
}

func (s *Server) sessionEvents(w http.ResponseWriter, r *http.Request) {
	req, err := parseEvents(r.URL.Query())
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.events.Events(ctx, r.PathValue("uid"), req)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}

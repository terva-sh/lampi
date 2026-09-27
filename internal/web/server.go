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
	// index is the search index. Nil turns search off.
	index *recall.Index
	auth  *webauth.Browser
	// origin is the configured base URL, used to make copied links
	// absolute.
	origin string
	// reg serves registration codes to operators. Nil leaves the routes
	// out.
	reg   *Registrations
	mints mintLimit
	// attempts recognises a mint form sent twice.
	attempts mintAttempts
	// clock replaces time.Now in tests.
	clock func() time.Time
	log   *slog.Logger
}

// New builds a handler mounted inside api.Server's request accounting.
// Transcript text is read only through reader, and search only through
// index, which may be nil. client is nil in production; tests supply
// the trust pool of their synthetic HTTPS IdP. reg, when not nil, lets
// operators manage registration codes.
func New(cfg webconfig.Config, cat *catalog.Catalog, reader *recall.Reader, index *recall.Index, reg *Registrations, client *http.Client, loggers ...*slog.Logger) (http.Handler, error) {
	auth, err := webauth.New(cfg, client)
	if err != nil {
		return nil, err
	}
	if len(loggers) > 0 {
		auth.Logger = loggers[0]
	}
	s := &Server{catalog: cat, events: reader, index: index, auth: auth, origin: cfg.BaseURL, reg: reg, log: auth.Logger}
	m := http.NewServeMux()
	auth.Routes(m)
	get := func(path string, h http.HandlerFunc) { m.Handle("GET "+path, s.guardRead(h)) }
	get("/api/web/v1/overview", s.overview)
	get("/api/web/v1/sessions", s.sessions)
	get("/api/web/v1/sessions/{uid}", s.session)
	get("/api/web/v1/sessions/{uid}/{collection}", s.records)
	get("/api/web/v1/sessions/{uid}/events", s.sessionEvents)
	get("/api/web/v1/sessions/{uid}/excerpt", s.sessionExcerpt)
	get("/api/web/v1/conflicts", s.conflicts)
	get("/api/web/v1/search", s.search)
	s.pageRoutes(m)
	s.registrationRoutes(m)
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
	case errors.Is(err, errSearchOff):
		status, code = 503, "search_unavailable"
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

// errSearchOff is a search request to a lake with no index.
var errSearchOff = errors.New("web: search is not enabled")

// parseSearch reads a search request. Empty filter values mean no
// filter, so the plain HTML form can submit every field. Dates are
// RFC 3339 or YYYY-MM-DD in UTC; a date-only until covers that whole
// day.
func parseSearch(q url.Values) (recall.SearchRequest, error) {
	var req recall.SearchRequest
	for k, v := range q {
		if len(v) != 1 {
			return req, recall.ErrInvalid
		}
		switch k {
		case "q", "harness", "project", "unlinked", "since", "until", "limit", "cursor", "event_type", "actor", "tool", "tool_error", "raw_type":
		default:
			return req, recall.ErrInvalid
		}
	}
	req.Query = q.Get("q")
	req.Harness = q.Get("harness")
	req.Project = q.Get("project")
	req.Cursor = q.Get("cursor")
	req.EventType = q.Get("event_type")
	req.Actor = q.Get("actor")
	req.ToolName = q.Get("tool")
	req.RawType = q.Get("raw_type")
	switch q.Get("tool_error") {
	case "":
	case "true", "false":
		v := q.Get("tool_error") == "true"
		req.ToolError = &v
	default:
		return req, recall.ErrInvalid
	}
	if q.Has("unlinked") {
		switch q.Get("unlinked") {
		case "true":
			req.Unlinked = true
		case "false", "":
		default:
			return req, recall.ErrInvalid
		}
	}
	if q.Has("limit") && q.Get("limit") != "" {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > recall.SearchMaxLimit {
			return req, recall.ErrInvalid
		}
		req.Limit = n
	}
	for _, f := range []struct {
		key  string
		dest **time.Time
		end  bool
	}{{"since", &req.Since, false}, {"until", &req.Until, true}} {
		raw := q.Get(f.key)
		if raw == "" {
			continue
		}
		t, err := parseWhen(raw, f.end)
		if err != nil {
			return req, recall.ErrInvalid
		}
		*f.dest = &t
	}
	return req, nil
}

func parseWhen(raw string, end bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return t, err
	}
	if end {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	if s.index == nil {
		fail(w, errSearchOff)
		return
	}
	req, err := parseSearch(r.URL.Query())
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.index.Search(ctx, req)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}

// parseExcerpt reads from, count and gen for a copy-out span.
func parseExcerpt(q url.Values) (recall.ExcerptRequest, error) {
	var req recall.ExcerptRequest
	for k, v := range q {
		if len(v) != 1 {
			return req, recall.ErrInvalid
		}
		switch k {
		case "from", "count", "gen":
		default:
			return req, recall.ErrInvalid
		}
	}
	for _, f := range []struct {
		key string
		dst *int64
	}{{"from", &req.From}, {"gen", &req.Gen}} {
		if !q.Has(f.key) {
			continue
		}
		n, err := strconv.ParseInt(q.Get(f.key), 10, 64)
		if err != nil || n < 0 {
			return req, recall.ErrInvalid
		}
		*f.dst = n
	}
	req.Pinned = q.Has("gen")
	if q.Has("count") {
		n, err := strconv.Atoi(q.Get("count"))
		if err != nil || n < 1 || n > recall.ExcerptMaxEvents {
			return req, recall.ErrInvalid
		}
		req.Count = n
	} else {
		req.Count = recall.ExcerptMaxEvents
	}
	return req, nil
}

func (s *Server) sessionExcerpt(w http.ResponseWriter, r *http.Request) {
	req, err := parseExcerpt(r.URL.Query())
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	req.Origin = s.origin
	v, err := s.events.Excerpt(ctx, r.PathValue("uid"), req)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, v)
}

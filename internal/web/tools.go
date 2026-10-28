package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
)

// Tool usage statistics (TKT-01M61MZNC): the browser route, the read
// token route and the MCP tool_stats tool answer the same parameters
// with the same body, recall.ToolStats. It holds counts, sizes, times,
// tool names and event positions, never content.

const readToolsPath = "/api/read/v1/tools"

// toolStatsDefaultRange is the range read when a request names none.
// The whole lake is a few seconds of reading (docs/performance.md), so
// it is asked for with all=true rather than given by default.
const toolStatsDefaultRange = 7 * 24 * time.Hour

// parseToolStats reads a tool statistics request. Unknown and repeated
// parameters are refused, and an empty value means no filter, so a
// plain form can submit every field. Dates are as for search. A refused
// value is a recall.FilterError naming its parameter; an unknown name is
// not echoed back.
func parseToolStats(q url.Values) (req recall.ToolStatsRequest, all bool, err error) {
	for k, v := range q {
		switch k {
		case "harness", "project", "unlinked", "model", "tool", "origin", "origin_name", "since", "until", "all", "group_by", "split", "bucket", "largest", "context":
		default:
			return req, false, recall.ErrInvalid
		}
		if len(v) != 1 {
			return req, false, &recall.FilterError{Param: k}
		}
	}
	req.Harness = q.Get("harness")
	req.Project = q.Get("project")
	req.Model = q.Get("model")
	req.Tool = q.Get("tool")
	req.OriginKind = q.Get("origin")
	req.OriginName = q.Get("origin_name")
	req.GroupBy = q.Get("group_by")
	req.Split = q.Get("split")
	req.Bucket = q.Get("bucket")
	for _, f := range []struct {
		key  string
		dest *bool
	}{{"unlinked", &req.Unlinked}, {"context", &req.Context}, {"all", &all}} {
		switch q.Get(f.key) {
		case "true":
			*f.dest = true
		case "false", "":
		default:
			return req, false, &recall.FilterError{Param: f.key}
		}
	}
	if v := q.Get("largest"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > recall.ToolStatsMaxLargest {
			return req, false, &recall.FilterError{Param: "largest"}
		}
		req.Largest = n
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
		t, err := recall.ParseWhen(raw, f.end)
		if err != nil {
			return req, false, &recall.FilterError{Param: f.key}
		}
		*f.dest = &t
	}
	if all && (req.Since != nil || req.Until != nil || req.Bucket != "") {
		return req, false, &recall.FilterError{Param: "all"}
	}
	return req, all, nil
}

// toolStats answers a tool statistics request for scope, as the
// browser route, the read route and the MCP tool ask for it. With no
// since, until or bucket it reads the last seven days, unless all asks
// for every recorded time.
func (s *Server) toolStats(ctx context.Context, scope catalog.Scope, q url.Values) (recall.ToolStats, error) {
	if s.index == nil {
		return recall.ToolStats{}, errSearchOff
	}
	req, all, err := parseToolStats(q)
	if err != nil {
		return recall.ToolStats{}, err
	}
	req.Scope, req.Now = scope, s.now()
	if !all && req.Since == nil && req.Until == nil && req.Bucket == "" {
		since := req.Now.Add(-toolStatsDefaultRange)
		req.Since = &since
	}
	return s.index.ToolStats(ctx, req)
}

func (s *Server) tools(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.toolStats(ctx, scopeOf(r), r.URL.Query())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) readToolRoutes(m *http.ServeMux) {
	if s.reg == nil {
		return
	}
	// Not behind Guard: the bearer token is the only credential it takes.
	m.HandleFunc("GET "+readToolsPath, s.readTools)
}

// readTools is the tool statistics for a read token holding
// events:read, over the sessions its scope reaches.
func (s *Server) readTools(w http.ResponseWriter, r *http.Request) {
	s.readTokenAnswer(w, r, "tools", func(ctx context.Context, scope catalog.Scope, q url.Values) (any, error) {
		return s.toolStats(ctx, scope, q)
	})
}

// readTokenAnswer answers a read token holding events:read with
// answer's value for the token's scope. The audit line names what and
// the parameters, which hold no content, and is durable before the
// answer leaves, as for the event stream.
func (s *Server) readTokenAnswer(w http.ResponseWriter, r *http.Request, what string, answer func(context.Context, catalog.Scope, url.Values) (any, error)) {
	now := s.now()
	t, ok := s.tokenFor(w, r, catalog.PermEventsRead, "lampi-events", now)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		fail(w, r, recall.ErrInvalid)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	lake := s.reg.Lake()
	if err := lake.Catalog.QueueAudit(ctx, now, audit.Event{Kind: audit.EventsRead, Actor: "token:" + t.ID + " (" + t.Label + ")", Detail: what + " query=" + q.Encode()}); err != nil {
		s.logError(r, "queueing a read's audit line failed", err)
		apiError(w, http.StatusInternalServerError, "audit_failed")
		return
	}
	if err := lake.Catalog.FlushAudit(ctx, lake.Dir); err != nil {
		s.logError(r, "a read's audit line stays queued", err)
	}
	if err := lake.Catalog.TouchReadToken(ctx, t.ID, now); err != nil {
		s.logError(r, "recording a read token's use failed", err)
	}
	scope, err := s.catalog.ReadTokenScope(ctx, t)
	if err != nil {
		s.logError(r, "reading a read token's bays failed", err)
		fail(w, r, err)
		return
	}
	v, err := answer(ctx, scope, q)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

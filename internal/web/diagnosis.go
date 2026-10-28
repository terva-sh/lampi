package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/diagnose"
	"terva.sh/lampi/internal/recall"
)

// Session diagnosis (TKT-01M61MZNR): the browser routes, the read
// token routes and the MCP diagnose_session and diagnose_lake tools
// answer the same parameters with the same bodies. Findings are
// computed on request from the published events; nothing is cached.
// A body holds codes, counts, sizes, tool names and event positions,
// never content.

const (
	readDiagnosisPath        = "/api/read/v1/diagnosis"
	readSessionDiagnosisPath = "/api/read/v1/sessions/{uid}/diagnosis"
)

// diagnosisDefaultRange is the lake-wide view's range when a request
// names no since, as for tool statistics.
const diagnosisDefaultRange = 7 * 24 * time.Hour

// diagnosisReport is the engine's report with a viewer link for each
// finding's positions, in the same order. The links are relative on
// the HTTP routes and absolute on MCP, as for tool statistics.
type diagnosisReport struct {
	diagnose.Report
	Findings []diagnosisFinding `json:"findings"`
}

type diagnosisFinding struct {
	diagnose.Finding
	Links []string `json:"links"`
}

// diagnosis is uid's report for scope, its links under origin. The
// session is the only parameter, so any query is refused.
func (s *Server) diagnosis(ctx context.Context, scope catalog.Scope, uid string, q url.Values, origin string) (diagnosisReport, error) {
	if len(q) > 0 {
		return diagnosisReport{}, recall.ErrInvalid
	}
	rep, err := s.events.Diagnose(ctx, scope, uid)
	if err != nil {
		return diagnosisReport{}, err
	}
	out := diagnosisReport{Report: rep, Findings: make([]diagnosisFinding, len(rep.Findings))}
	for i, f := range rep.Findings {
		out.Findings[i] = diagnosisFinding{Finding: f, Links: make([]string, len(f.Positions))}
		for j, pos := range f.Positions {
			out.Findings[i].Links[j] = origin + recall.EventLink(uid, rep.Generation, pos)
		}
	}
	return out, nil
}

// diagnosisLake answers a lake-wide diagnosis request for scope.
// Unknown and repeated parameters are refused, and an empty value
// means the default, as for tool statistics.
func (s *Server) diagnosisLake(ctx context.Context, scope catalog.Scope, q url.Values) (recall.DiagnosisGroups, error) {
	req := recall.DiagnoseLakeRequest{Scope: scope, Now: s.now(), Limit: recall.DiagnoseLakeLimit}
	for k, v := range q {
		switch k {
		case "since", "limit", "harness", "project":
		default:
			return recall.DiagnosisGroups{}, recall.ErrInvalid
		}
		if len(v) != 1 {
			return recall.DiagnosisGroups{}, &recall.FilterError{Param: k}
		}
	}
	req.Harness, req.Project = q.Get("harness"), q.Get("project")
	req.Since = req.Now.Add(-diagnosisDefaultRange)
	if raw := q.Get("since"); raw != "" {
		t, err := recall.ParseWhen(raw, false)
		if err != nil {
			return recall.DiagnosisGroups{}, &recall.FilterError{Param: "since"}
		}
		req.Since = t
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > recall.DiagnoseLakeMaxLimit {
			return recall.DiagnosisGroups{}, &recall.FilterError{Param: "limit"}
		}
		req.Limit = n
	}
	return s.events.DiagnoseLake(ctx, req)
}

func (s *Server) sessionDiagnosis(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.diagnosis(ctx, scopeOf(r), r.PathValue("uid"), r.URL.Query(), "")
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) lakeDiagnosis(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.diagnosisLake(ctx, scopeOf(r), r.URL.Query())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) readDiagnosisRoutes(m *http.ServeMux) {
	if s.reg == nil || s.events == nil {
		return
	}
	// Not behind Guard: the bearer token is the only credential it takes.
	m.HandleFunc("GET "+readSessionDiagnosisPath, func(w http.ResponseWriter, r *http.Request) {
		uid := r.PathValue("uid")
		if !auditSafe.MatchString(uid) {
			uid = "invalid"
		}
		s.readTokenAnswer(w, r, "diagnosis session="+uid, func(ctx context.Context, scope catalog.Scope, q url.Values) (any, error) {
			return s.diagnosis(ctx, scope, r.PathValue("uid"), q, "")
		})
	})
	m.HandleFunc("GET "+readDiagnosisPath, func(w http.ResponseWriter, r *http.Request) {
		s.readTokenAnswer(w, r, "diagnosis", func(ctx context.Context, scope catalog.Scope, q url.Values) (any, error) {
			return s.diagnosisLake(ctx, scope, q)
		})
	})
}

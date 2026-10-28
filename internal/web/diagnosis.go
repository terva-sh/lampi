package web

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
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

// transcriptDiagnosisBudget bounds the findings panel's pass within
// the transcript page's read context. Diagnosis reads about 150,000
// events a second (docs/performance.md), so a session too long for it
// shows a notice rather than slowing every page of it further.
var transcriptDiagnosisBudget = time.Second

// diagnosisView is the transcript page's findings panel. Notice says
// why there are no findings to show when diagnosis did not finish.
type diagnosisView struct {
	Notice   string
	Findings []findingRow
	Summary  string
}

type findingRow struct {
	diagnose.Finding
	// Class is the severity's badge class, and Measured the values as
	// one line.
	Class, Measured string
	Events          []findingEvent
	// More is the events the finding covers beyond those listed.
	More int64
}

type findingEvent struct {
	Position int64
	Link     string
}

// transcriptDiagnosis diagnoses uid for the page showing generation
// gen. A failure is a notice on the panel, never a failed page.
func (s *Server) transcriptDiagnosis(ctx context.Context, r *http.Request, uid string, gen int64) diagnosisView {
	ctx, cancel := context.WithTimeout(ctx, transcriptDiagnosisBudget)
	defer cancel()
	rep, err := s.diagnosis(ctx, scopeOf(r), uid, nil, "")
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return diagnosisView{Notice: "Diagnosing this session takes longer than the page allows. terva-lampi query diagnose and the diagnosis API have up to five seconds."}
	case err != nil:
		s.logError(r, "diagnosing a session for its transcript page failed", err)
		return diagnosisView{Notice: "This session could not be diagnosed."}
	case rep.Generation != gen:
		return diagnosisView{Notice: "The transcript changed while it was diagnosed. Reload the page for its findings."}
	}
	v := diagnosisView{Summary: "no findings"}
	counts := map[diagnose.Severity]int{}
	for _, f := range rep.Findings {
		counts[f.Severity]++
		row := findingRow{Finding: f.Finding, Class: map[diagnose.Severity]string{diagnose.Error: "failed", diagnose.Warn: "pending"}[f.Severity], More: f.Count - int64(len(f.Positions))}
		row.Class = cmp.Or(row.Class, "unknown")
		var measured []string
		for _, k := range slices.Sorted(maps.Keys(f.Values)) {
			n, value := f.Values[k], humanCount(f.Values[k])
			if strings.HasSuffix(k, "_bytes") && !strings.HasPrefix(k, "over_") {
				value = bytesIEC(n)
			}
			measured = append(measured, strings.ReplaceAll(k, "_", " ")+" "+value)
		}
		row.Measured = strings.Join(measured, " · ")
		for i, pos := range f.Positions {
			row.Events = append(row.Events, findingEvent{Position: pos, Link: f.Links[i]})
		}
		v.Findings = append(v.Findings, row)
	}
	var parts []string
	for _, sev := range []diagnose.Severity{diagnose.Error, diagnose.Warn, diagnose.Info} {
		if counts[sev] > 0 {
			parts = append(parts, strconv.Itoa(counts[sev])+" "+string(sev))
		}
	}
	if len(parts) > 0 {
		v.Summary = strings.Join(parts, ", ")
	}
	return v
}

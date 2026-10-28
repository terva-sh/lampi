package recall

import (
	"cmp"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/diagnose"
)

// Diagnose reads uid's published events once, from one pinned
// generation, and returns the diagnosis under the shipped rules. A
// session outside scope is ErrNotFound; one with no readable output is
// UnavailableError. The report carries counts, sizes and positions,
// never content, so a route can serialize it as it is and link each
// position with EventLink.
func (r *Reader) Diagnose(ctx context.Context, scope catalog.Scope, uid string) (diagnose.Report, error) {
	if err := r.inScope(ctx, scope, uid); err != nil {
		return diagnose.Report{}, err
	}
	snap, err := r.open(ctx, uid)
	if err != nil {
		return diagnose.Report{}, err
	}
	defer snap.Close()
	br, _, _, err := snap.lines(ctx, 0, 0)
	if err != nil {
		return diagnose.Report{}, err
	}
	a := diagnose.New(nil)
	for n := 0; ; n++ {
		if n%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return diagnose.Report{}, err
			}
		}
		line, read, rerr := readLine(br, MaxLine)
		if rerr == io.EOF && read == 0 {
			break
		}
		if rerr != nil && rerr != io.EOF {
			return diagnose.Report{}, rerr
		}
		a.Line(line)
		if rerr == io.EOF {
			break
		}
	}
	rep := a.Report()
	rep.SessionUID, rep.Generation, rep.Head = uid, snap.pub.Gen, snap.pub.Head
	return rep, nil
}

// The lake-wide diagnosis walks the most recently updated sessions and
// diagnoses each on request; nothing is cached (TKT-01M61MZNR). The
// limits bound one request's work, and the walk stops before the read
// context's deadline rather than failing at it.
const (
	DiagnoseLakeLimit    = 200
	DiagnoseLakeMaxLimit = 1000
	// diagnoseLakeExamples is how many session uids a group names.
	diagnoseLakeExamples = 3
	// diagnoseLakeMargin is the time left for answering when the walk
	// stops early.
	diagnoseLakeMargin = 500 * time.Millisecond
)

// Reasons a lake-wide diagnosis stopped before the sessions ran out.
const (
	TruncatedLimit = "limit"
	TruncatedTime  = "time"
)

// DiagnoseLakeRequest selects the sessions a lake-wide diagnosis walks:
// ready ones in Scope whose head last changed at or after Since, newest
// first, at most Limit of them.
type DiagnoseLakeRequest struct {
	Scope            catalog.Scope
	Since            time.Time
	Harness, Project string
	Limit            int
	Now              time.Time
}

// DiagnosisGroups is a lake-wide diagnosis: the findings of the walked
// sessions, grouped by code, harness and project.
type DiagnosisGroups struct {
	Since        string `json:"since"`
	Limit        int    `json:"limit"`
	RulesVersion int    `json:"rules_version"`
	// Sessions is how many were diagnosed, and Skipped how many were
	// purged, normalized again or not ready by the time they were read.
	Sessions int `json:"sessions"`
	Skipped  int `json:"skipped"`
	// Truncated says sessions in range were left unread, and
	// TruncatedBy why: TruncatedLimit or TruncatedTime.
	Truncated   bool             `json:"truncated"`
	TruncatedBy string           `json:"truncated_by,omitempty"`
	Groups      []DiagnosisGroup `json:"groups"`
	AsOf        string           `json:"as_of"`
}

// DiagnosisGroup is one code's findings in one harness and project.
// Findings can exceed Sessions where a session has one per tool, and
// Events sums the findings' counts.
type DiagnosisGroup struct {
	Code       diagnose.Code       `json:"code"`
	Severity   diagnose.Severity   `json:"severity"`
	Harness    string              `json:"harness"`
	ProjectID  string              `json:"project_id"`
	Sessions   int64               `json:"sessions"`
	Findings   int64               `json:"findings"`
	Events     int64               `json:"events"`
	Examples   []string            `json:"example_sessions"`
	Suggestion diagnose.Suggestion `json:"suggestion"`
}

// DiagnoseLake diagnoses the sessions req selects, newest first, and
// groups their findings. Groups come most severe first, then by
// sessions, most first.
func (r *Reader) DiagnoseLake(ctx context.Context, req DiagnoseLakeRequest) (DiagnosisGroups, error) {
	out := DiagnosisGroups{Since: req.Since.UTC().Format(time.RFC3339), Limit: req.Limit, RulesVersion: diagnose.Shipped.Version,
		Groups: []DiagnosisGroup{}, AsOf: req.Now.UTC().Format(time.RFC3339Nano)}
	sessions, err := r.catalog.RecentReadySessions(ctx, req.Scope, req.Since, req.Harness, req.Project, req.Limit+1)
	if err != nil {
		return out, err
	}
	if len(sessions) > req.Limit {
		sessions = sessions[:req.Limit]
		out.Truncated, out.TruncatedBy = true, TruncatedLimit
	}
	walk := ctx
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		walk, cancel = context.WithDeadline(ctx, deadline.Add(-diagnoseLakeMargin))
		defer cancel()
	}
	// By code, harness and project.
	groups := map[[3]string]*DiagnosisGroup{}
	for _, s := range sessions {
		rep, err := r.Diagnose(walk, req.Scope, s.UID)
		var unavailable UnavailableError
		switch {
		case err == nil:
		case walk.Err() != nil:
			out.Truncated, out.TruncatedBy = true, TruncatedTime
			return out.sorted(groups), nil
		case errors.Is(err, ErrNotFound) || errors.Is(err, ErrGenerationChanged) || errors.As(err, &unavailable):
			out.Skipped++
			continue
		default:
			return out, err
		}
		out.Sessions++
		seen := map[diagnose.Code]bool{}
		for _, f := range rep.Findings {
			k := [3]string{string(f.Code), s.Harness, s.ProjectID}
			g := groups[k]
			if g == nil {
				g = &DiagnosisGroup{Code: f.Code, Severity: f.Severity, Harness: s.Harness, ProjectID: s.ProjectID, Suggestion: f.Suggestion}
				groups[k] = g
			}
			g.Findings++
			g.Events += f.Count
			if !seen[f.Code] {
				seen[f.Code] = true
				g.Sessions++
				if len(g.Examples) < diagnoseLakeExamples {
					g.Examples = append(g.Examples, s.UID)
				}
			}
		}
	}
	return out.sorted(groups), nil
}

func (out DiagnosisGroups) sorted(groups map[[3]string]*DiagnosisGroup) DiagnosisGroups {
	for _, g := range groups {
		out.Groups = append(out.Groups, *g)
	}
	slices.SortFunc(out.Groups, func(a, b DiagnosisGroup) int {
		return cmp.Or(b.Severity.Rank()-a.Severity.Rank(), cmp.Compare(b.Sessions, a.Sessions),
			strings.Compare(string(a.Code), string(b.Code)), strings.Compare(a.Harness, b.Harness), strings.Compare(a.ProjectID, b.ProjectID))
	})
	return out
}

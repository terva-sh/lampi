package web

import (
	"context"
	"strconv"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
)

// errorsView is the Usage page's Errors tab
// (TKT-01M61NTM0K2TQHX28T2R4HN0TN).
type errorsView struct {
	Errors, Failed, ToolCalls, FailRate string
	Sessions                            []errorSessionRow
	Kinds, Tools                        []nameRow
}

type errorSessionRow struct {
	UID, NativeID, ProjectLabel, Harness string
	Errors, Failed                       string
}

// nameRow is one name of a breakdown with its count and its share of
// the largest, for a meter.
type nameRow struct {
	Harness, Name, Count string
	N, Max               int64
}

// errorTopSessions is how many sessions the Errors tab lists.
const errorTopSessions = 10

// errorTopNames is how many rows each breakdown lists.
const errorTopNames = 15

func (s *Server) buildErrors(ctx context.Context, v *usagePageView, scope catalog.Scope, series []recall.SeriesBucket, from, until time.Time, hourly bool) error {
	starts := make([]time.Time, len(series))
	errs := make([]*int64, len(series))
	failed := make([]*int64, len(series))
	var nErr, nFailed, nCalls int64
	for i, b := range series {
		starts[i] = b.Start
		e, f := b.Errors, b.ToolsFailed
		errs[i], failed[i] = &e, &f
		nErr, nFailed, nCalls = nErr+e, nFailed+f, nCalls+b.ToolCalls
	}
	ev := errorsView{Errors: humanCount(nErr), Failed: humanCount(nFailed), ToolCalls: humanCount(nCalls), FailRate: "—"}
	if nCalls > 0 {
		ev.FailRate = percent(nFailed, nCalls)
	}
	v.Charts = []chartView{
		buildChart("errors", "Error events", starts, errs, hourly, humanCount, niceCeil),
		buildChart("failed", "Failed tool results", starts, failed, hourly, humanCount, niceCeil),
	}
	top, err := s.index.TopErrorSessions(ctx, scope, v.Harness, from, until, errorTopSessions)
	if err != nil {
		return err
	}
	for _, t := range top {
		ev.Sessions = append(ev.Sessions, errorSessionRow{UID: t.SessionUID, NativeID: t.NativeID, ProjectLabel: t.ProjectLabel, Harness: t.Harness, Errors: humanCount(t.Errors), Failed: humanCount(t.ToolsFailed)})
	}
	for _, b := range []struct {
		kind string
		dst  *[]nameRow
	}{{recall.StatError, &ev.Kinds}, {recall.StatToolFailed, &ev.Tools}} {
		counts, err := s.index.Breakdown(ctx, scope, v.Harness, from, until, b.kind)
		if err != nil {
			return err
		}
		*b.dst = nameRows(counts, errorTopNames)
	}
	v.Errors = ev
	return nil
}

func nameRows(counts []recall.NameCount, limit int) []nameRow {
	var out []nameRow
	for i, c := range counts {
		if i == limit {
			break
		}
		name := c.Name
		if name == "" {
			name = "(unnamed)"
		}
		out = append(out, nameRow{Harness: c.Harness, Name: name, Count: humanCount(c.N), N: c.N, Max: counts[0].N})
	}
	return out
}

// percent is part of whole to one decimal place: "2.4%".
func percent(part, whole int64) string {
	p := float64(part) * 100 / float64(whole)
	if p > 0 && p < 0.1 {
		return "<0.1%"
	}
	return strconv.FormatFloat(p, 'f', 1, 64) + "%"
}

// nameTableView is one breakdown table on a Usage tab.
type nameTableView struct {
	Title, Label string
	Rows         []nameRow
}

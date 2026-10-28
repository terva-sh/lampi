package recall

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/diagnose"
	"terva.sh/lampi/internal/normalize"
)

func TestDiagnoseOneSession(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "diagnose")
	evs := events(30, func(i int) string { return fmt.Sprint("line ", i) })
	// Positions 10 and 11: a call and a result too large for context.
	big := strings.Repeat("x", 64<<10)
	evs[10].EventType, evs[10].Tool = normalize.EventToolCall, normalize.Tool{Name: ptr("exec"), CallID: ptr("c1")}
	evs[11].EventType, evs[11].Tool, evs[11].ContentText = normalize.EventToolResult, normalize.Tool{Name: ptr("exec"), CallID: ptr("c1")}, &big
	gen := publish(t, s, uid, evs)
	r := NewReader(s.Catalog, s.Normalized)
	rep, err := r.Diagnose(t.Context(), catalog.AllBays(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if rep.SessionUID != uid || rep.Generation != gen || rep.Head == "" || rep.Events != 30 || rep.Harness != "codex" {
		t.Fatalf("%+v", rep)
	}
	if len(rep.Findings) != 1 || rep.Findings[0].Code != diagnose.LargeToolResult || !slices.Equal(rep.Findings[0].Positions, []int64{11}) {
		t.Fatalf("%+v", rep.Findings)
	}

	// Outside scope reads as not stored, as does an unknown session.
	if _, err := r.Diagnose(t.Context(), catalog.InBays(nil), uid); !errors.Is(err, ErrNotFound) {
		t.Fatal("out of scope", err)
	}
	if _, err := r.Diagnose(t.Context(), catalog.AllBays(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown", err)
	}
	// A session with no published output is unavailable.
	pending := ingest(t, s, "pending")
	var un UnavailableError
	if _, err := r.Diagnose(t.Context(), catalog.AllBays(), pending); !errors.As(err, &un) {
		t.Fatal("pending", err)
	}
}

func ptr[T any](v T) *T { return &v }

// largeResult is a short session whose result at position 2 is too
// large for context.
func largeResult() []normalize.Event {
	evs := events(4, func(i int) string { return fmt.Sprint("line ", i) })
	big := strings.Repeat("x", 64<<10)
	evs[1].EventType, evs[1].Tool = normalize.EventToolCall, normalize.Tool{Name: ptr("exec"), CallID: ptr("c1")}
	evs[2].EventType, evs[2].Tool, evs[2].ContentText = normalize.EventToolResult, normalize.Tool{Name: ptr("exec"), CallID: ptr("c1")}, &big
	return evs
}

// TKT-01M61MZNR: the lake-wide walk reads the newest ready sessions in
// range, groups their findings, and says when it left some unread.
func TestDiagnoseLake(t *testing.T) {
	s := lake(t)
	r := NewReader(s.Catalog, s.Normalized)
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var uids []string
	for i := range 3 {
		uid := ingestAt(t, s, "codex", fmt.Sprint("lake-", i), t0.Add(time.Duration(i)*time.Hour))
		publish(t, s, uid, largeResult())
		uids = append(uids, uid)
	}
	clean := ingestAt(t, s, "codex", "clean", t0.Add(4*time.Hour))
	publish(t, s, clean, events(3, func(i int) string { return "fine" }))
	ingestAt(t, s, "codex", "pending", t0.Add(5*time.Hour))

	req := DiagnoseLakeRequest{Scope: catalog.AllBays(), Since: t0.Add(30 * time.Minute), Limit: 10, Now: t0}
	got, err := r.DiagnoseLake(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	// The pending session is not ready, and the first is before since.
	if got.Sessions != 3 || got.Skipped != 0 || got.Truncated || len(got.Groups) != 1 {
		t.Fatalf("%+v", got)
	}
	g := got.Groups[0]
	if g.Code != diagnose.LargeToolResult || g.Harness != "codex" || g.Sessions != 2 || g.Findings != 2 || g.Events != 2 ||
		!slices.Equal(g.Examples, []string{uids[2], uids[1]}) || g.Suggestion.Text == "" {
		t.Errorf("%+v", g)
	}

	req.Since, req.Limit = t0, 2
	if got, _ := r.DiagnoseLake(t.Context(), req); got.Sessions != 2 || !got.Truncated || got.TruncatedBy != TruncatedLimit || got.Groups[0].Sessions != 1 {
		t.Errorf("limit: %+v", got)
	}
	req.Scope = catalog.InBays(nil)
	if got, _ := r.DiagnoseLake(t.Context(), req); got.Sessions != 0 || got.Truncated || len(got.Groups) != 0 {
		t.Errorf("empty scope: %+v", got)
	}
	// Under the margin before the deadline, it stops and says so.
	ctx, cancel := context.WithTimeout(t.Context(), diagnoseLakeMargin/2)
	defer cancel()
	req.Scope = catalog.AllBays()
	if got, err := r.DiagnoseLake(ctx, req); err != nil || got.Sessions != 0 || got.TruncatedBy != TruncatedTime {
		t.Errorf("time: %+v %v", got, err)
	}
}

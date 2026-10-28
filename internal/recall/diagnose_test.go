package recall

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

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

package recall

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"terva.sh/lampi/internal/catalog"
)

func TestEventNavigationHonorsBytesAndGeneration(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "pages")
	publish(t, s, uid, events(110, func(int) string { return strings.Repeat("x", ContentPreview) }))
	r := NewReader(s.Catalog, s.Normalized)
	p, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{})
	if err != nil || len(p.Items) >= DefaultLimit {
		t.Fatal("fixture should hit byte limit", len(p.Items), err)
	}
	nav, err := r.EventNavigation(t.Context(), catalog.AllBays(), p, 0)
	if err != nil || len(nav.Cursors) < 3 {
		t.Fatal("navigation", nav, err)
	}
	position := int64(0)
	for i, cursor := range nav.Cursors {
		p, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Cursor: cursor})
		if err != nil || p.From != position {
			t.Fatalf("page %d starts at %d, want %d: %v", i, p.From, position, err)
		}
		position += int64(len(p.Items))
		n, err := r.EventNavigation(t.Context(), catalog.AllBays(), p, 0)
		if err != nil || n.Current != i {
			t.Fatal("current page", n, err)
		}
	}
	if position != 110 {
		t.Fatal("events omitted", position)
	}
	publish(t, s, uid, events(3, func(int) string { return "new" }))
	if _, err := r.EventNavigation(t.Context(), catalog.AllBays(), p, 0); !errors.Is(err, ErrGenerationChanged) {
		t.Fatal("stale page accepted", err)
	}
}

func TestNumberedSearchOmitsStaleAndUnreadableSessions(t *testing.T) {
	s := lake(t)
	visible, hidden, stale := ingest(t, s, "visible"), ingest(t, s, "hidden"), ingest(t, s, "stale")
	for _, uid := range []string{visible, hidden, stale} {
		publish(t, s, uid, events(11, func(i int) string { return fmt.Sprint("needle ", i) }))
	}
	x := openIndex(t, s)
	pass(t, x)
	publish(t, s, stale, events(2, func(int) string { return "replacement" }))
	req := SearchRequest{Query: "needle", Limit: 3, Scope: catalog.AllBays().OnlySessions([]string{visible, stale})}
	p, nav, err := x.SearchNumbered(t.Context(), req)
	if err != nil || len(nav.Cursors) != 4 || len(p.Items) != 3 {
		t.Fatal("scoped search", nav, len(p.Items), err)
	}
	seen := map[int64]bool{}
	for i, cursor := range nav.Cursors {
		req.Cursor = cursor
		p, n, err := x.SearchNumbered(t.Context(), req)
		if err != nil || n.Current != i {
			t.Fatal("search page", n, err)
		}
		if (p.NextCursor != "") != (i < len(nav.Cursors)-1) {
			t.Fatal("search continuation", i, p.NextCursor)
		}
		for _, hit := range p.Items {
			if hit.SessionUID != visible || seen[hit.Position] {
				t.Fatal("hidden, stale or duplicate hit", hit)
			}
			seen[hit.Position] = true
		}
	}
	if len(seen) != 11 {
		t.Fatal("missed hits", len(seen))
	}
}

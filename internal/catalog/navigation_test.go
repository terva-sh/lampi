package catalog

import (
	"fmt"
	"testing"
)

func TestDashboardNavigationMatchesKeysetPages(t *testing.T) {
	c := seedDashboard(t, 123)
	req := PageRequest{Harness: "codex", Limit: 7}
	nav, err := c.DashboardNavigation(t.Context(), AllBays(), "", "sessions", req)
	if err != nil || len(nav.Cursors) != 9 {
		t.Fatalf("navigation %+v %v", nav, err)
	}
	seen := map[string]bool{}
	for i, cursor := range nav.Cursors {
		req.Cursor = cursor
		p, err := c.DashboardSessions(t.Context(), AllBays(), req)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range p.Items {
			if seen[row.UID] {
				t.Fatal("overlapping pages", row.UID)
			}
			seen[row.UID] = true
		}
		n, err := c.DashboardNavigation(t.Context(), AllBays(), "", "sessions", req)
		if err != nil || n.Current != i {
			t.Fatal("incorrect current page", n, err)
		}
	}
	if len(seen) != 61 {
		t.Fatal("missing filtered rows", len(seen))
	}
	req.Cursor = ""
	nav, err = c.DashboardNavigation(t.Context(), AllBays().OnlySessions([]string{"session-000001"}), "", "sessions", req)
	if err != nil || len(nav.Cursors) != 1 {
		t.Fatal("scope affected page count", nav, err)
	}
	for _, kind := range []string{"artifacts", "provenance", "conflicts"} {
		nav, err := c.DashboardNavigation(t.Context(), AllBays(), fmt.Sprintf("session-%06d", 1), kind, PageRequest{Limit: 1})
		if err != nil || len(nav.Cursors) != 1 {
			t.Fatal("metadata navigation", kind, nav, err)
		}
	}
	if _, err := c.db.Exec(`UPDATE artifacts SET relation='divergent_copy'`); err != nil {
		t.Fatal(err)
	}
	r := PageRequest{Limit: 7}
	nav, err = c.DashboardNavigation(t.Context(), AllBays(), "", "conflicts", r)
	if err != nil || len(nav.Cursors) != 18 {
		t.Fatal("global conflicts navigation", nav, err)
	}
	seen = map[string]bool{}
	for i, cursor := range nav.Cursors {
		r.Cursor = cursor
		p, err := c.DashboardRecords(t.Context(), AllBays(), "", "conflicts", r)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range p.Items {
			if seen[record.ID] {
				t.Fatal("overlapping conflicts", record.ID)
			}
			seen[record.ID] = true
		}
		n, err := c.DashboardNavigation(t.Context(), AllBays(), "", "conflicts", r)
		if err != nil || n.Current != i {
			t.Fatal("conflicts current page", n, err)
		}
	}
	if len(seen) != 123 {
		t.Fatal("missing conflicts", len(seen))
	}
}

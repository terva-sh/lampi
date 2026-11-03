package catalog

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSelectedBayScopeKeepsAuthorizationAndSessionLimits(t *testing.T) {
	c, _ := openTemp(t)
	ctx, now := t.Context(), time.Now()
	one, other := newSession(t, c, "one"), newSession(t, c, "other")
	a, err := c.CreateBay(ctx, "reviews", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBay(ctx, "private-client", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []Membership{{SessionUID: one, Bay: a.ID}, {SessionUID: one, Bay: b.ID}, {SessionUID: other, Bay: b.ID}} {
		m.Actor, m.Via = "admin", ViaCLI
		if _, err := c.AddToBay(ctx, m, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.AliasBay(ctx, a.ID, "code-reviews", "admin", now); err != nil {
		t.Fatal(err)
	}
	scope := InBays([]string{a.ID})
	for _, ref := range []string{"reviews", a.ID, "code-reviews", "reviews,code-reviews"} {
		selected, err := c.SelectReadBays(ctx, scope, ref)
		if err != nil {
			t.Fatal(err)
		}
		p, err := c.DiscoverSessions(ctx, selected, DiscoveryRequest{})
		if err != nil || len(p.Items) != 1 || len(p.Items[0].Bays) != 1 {
			t.Fatalf("selection %+v %v", p, err)
		}
		if selected.All() {
			t.Fatal("selection optimized as entire lake")
		}
	}
	for _, ref := range []string{"private-client", "unknown", "", ",reviews", strings.Repeat("reviews,", 20) + "reviews"} {
		if _, err := c.SelectReadBays(ctx, scope, ref); !errors.Is(err, ErrBaySelection) {
			t.Fatalf("%q: %v", ref, err)
		}
	}
	// A fixed-session token learns no unrelated bay, even with AllBays.
	if _, err := c.SelectReadBays(ctx, AllBays().OnlySessions([]string{other}), a.ID); !errors.Is(err, ErrBaySelection) {
		t.Fatalf("fixed session selector: %v", err)
	}
	narrowed := scope.OnlySessions([]string{one}).OnlySessions([]string{other})
	p, err := c.DiscoverSessions(ctx, narrowed, DiscoveryRequest{})
	if err != nil || len(p.Items) != 0 {
		t.Fatal("named scope broadened")
	}
	selected := AllBays().SelectBays([]string{a.ID, b.ID})
	p, err = c.DiscoverSessions(ctx, selected, DiscoveryRequest{})
	if err != nil || len(p.Items) != 2 {
		t.Fatalf("any-of dedup: %+v %v", p, err)
	}
	if selected.Fingerprint() != AllBays().SelectBays([]string{b.ID, a.ID, a.ID}).Fingerprint() {
		t.Fatal("selector order changes scope")
	}
	first, err := c.DiscoverSessions(ctx, selected, DiscoveryRequest{Limit: 1})
	if err != nil || first.NextCursor == "" {
		t.Fatal(err)
	}
	if _, err := c.DiscoverSessions(ctx, AllBays(), DiscoveryRequest{Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, ErrPage) {
		t.Fatal("discovery cursor lost selection")
	}
}

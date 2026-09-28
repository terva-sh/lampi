package catalog

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/config"
)

// A catalog with no default profile serves an empty one, as a lake with
// no profiles file did.
func TestResolveProfileWithNoDefaultIsEmpty(t *testing.T) {
	c, _ := openTemp(t)
	e, err := c.ResolveProfile(t.Context(), Device{})
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != config.DefaultProfile || e.Version != (config.Profile{}).Version() || !slices.Equal(e.Layers, []string{"profile:default"}) {
		t.Fatalf("resolved %+v", e)
	}
	if _, err := c.ResolveProfile(t.Context(), Device{Profile: "ci"}); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("unknown profile: %v", err)
	}
	if names, err := c.ProfileNames(t.Context()); err != nil || !slices.Equal(names, []string{"default"}) {
		t.Fatalf("names %v %v", names, err)
	}
}

func TestResolveProfileReadsTheCatalog(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for name, raw := range map[string]string{"default": `{"agent":{"debounce":"5s"}}`, "ci": `{"agent":{"debounce":"9s"}}`} {
		if _, _, err := c.PutProfile(ctx, name, []byte(raw), "op", "", now); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ device, name, debounce string }{{"", "default", "5s"}, {"ci", "ci", "9s"}} {
		e, err := c.ResolveProfile(ctx, Device{Profile: tc.device})
		if err != nil {
			t.Fatal(err)
		}
		if e.Name != tc.name || e.Config.Agent.Debounce != tc.debounce || e.Version != e.Config.Version() || !slices.Equal(e.Layers, []string{"profile:" + tc.name}) {
			t.Fatalf("device profile %q resolved %+v", tc.device, e)
		}
	}
	if names, err := c.ProfileNames(ctx); err != nil || !slices.Equal(names, []string{"default", "ci"}) {
		t.Fatalf("names %v %v", names, err)
	}
	for name, want := range map[string]bool{"": true, "default": true, "ci": true, "nope": false} {
		if ok, err := c.HasProfile(ctx, name); err != nil || ok != want {
			t.Errorf("HasProfile(%q) = %v %v", name, ok, err)
		}
	}
}

// Redeem resolves the code's profile inside its transaction, and a code
// whose profile the catalog does not hold stays unspent.
func TestRedeemResolvesTheProfile(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	secret := strings.Repeat("b", 64)
	if _, err := c.CreateRegistration(ctx, "box", secret, "ci", "", ActorCLI, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var got EffectiveProfile
	finish := func(_ Device, _ Registration, e EffectiveProfile) ([]audit.Event, error) {
		got = e
		return nil, nil
	}
	if _, _, err := c.Redeem(ctx, secret, strings.Repeat("c", 64), "m1", nil, now, finish); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("redeem with no profile: %v", err)
	}
	if _, _, err := c.PutProfile(ctx, "ci", []byte(`{"agent":{"debounce":"9s"}}`), "op", "", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Redeem(ctx, secret, strings.Repeat("c", 64), "m1", nil, now, finish); err != nil {
		t.Fatal(err)
	}
	if got.Name != "ci" || got.Config.Agent.Debounce != "9s" {
		t.Fatalf("finish saw %+v", got)
	}
}

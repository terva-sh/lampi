package catalog

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncTokenFileNamesDevicesUniquely(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := t.Context()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	h := func(b string) string { return strings.Repeat(b, 64) }
	created, err := c.SyncTokenFile(ctx, []TokenEntry{
		{Hash: h("a"), Name: "Drew's Laptop"},
		{Hash: h("b"), Name: "drew-s-laptop"},
		{Hash: h("c")},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range created {
		names = append(names, d.Name)
	}
	if got := strings.Join(names, ","); got != "drew-s-laptop,drew-s-laptop-2,token-3" {
		t.Fatalf("names %s", got)
	}
	// Reloading the same file creates nothing.
	if again, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: h("a")}, {Hash: h("b")}, {Hash: h("c")}}, now); err != nil || len(again) != 0 {
		t.Fatalf("again %v %v", again, err)
	}
	if DeviceName(strings.Repeat("x", 100)) != strings.Repeat("x", 64) {
		t.Fatal("name not capped at 64")
	}
}

func TestBindMachine(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := t.Context()
	created, _ := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("a", 64), Name: "a"}, {Hash: strings.Repeat("b", 64), Name: "b"}}, time.Now())
	a, b := created[0], created[1]
	if bound, err := c.BindMachine(ctx, a.ID, "m1"); err != nil || !bound {
		t.Fatalf("bind %v %v", bound, err)
	}
	if bound, err := c.BindMachine(ctx, a.ID, "m1"); err != nil || bound {
		t.Fatalf("rebind same %v %v", bound, err)
	}
	if _, err := c.BindMachine(ctx, a.ID, "m2"); !errors.Is(err, ErrDeviceBound) {
		t.Fatalf("other machine %v", err)
	}
	if _, err := c.BindMachine(ctx, b.ID, "m1"); !errors.Is(err, ErrMachineTaken) {
		t.Fatalf("taken machine %v", err)
	}
	if _, err := c.RevokeDevice(ctx, "nope", time.Now()); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("revoke unknown %v", err)
	}
}

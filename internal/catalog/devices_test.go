package catalog

import (
	"context"
	"errors"
	"fmt"
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
	if bound, err := c.BindMachine(ctx, a.ID, "m1", time.Now()); err != nil || !bound {
		t.Fatalf("bind %v %v", bound, err)
	}
	if bound, err := c.BindMachine(ctx, a.ID, "m1", time.Now()); err != nil || bound {
		t.Fatalf("rebind same %v %v", bound, err)
	}
	if _, err := c.BindMachine(ctx, a.ID, "m2", time.Now()); !errors.Is(err, ErrDeviceBound) {
		t.Fatalf("other machine %v", err)
	}
	if _, err := c.BindMachine(ctx, b.ID, "m1", time.Now()); !errors.Is(err, ErrMachineTaken) {
		t.Fatalf("taken machine %v", err)
	}
	if _, err := c.RevokeDevice(ctx, "nope", "test", time.Now()); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("revoke unknown %v", err)
	}
}

// Two connections, as serve and a second process would hold, race to
// bind many unbound devices to one machine. Transactions begin
// IMMEDIATE, so each waits for the write lock and then sees the
// winner: one bind succeeds and every other is ErrMachineTaken.
func TestConcurrentBindsOfOneMachineRefuseTheLosers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var entries []TokenEntry
	for i := range 16 {
		entries = append(entries, TokenEntry{Name: fmt.Sprintf("d%d", i), Hash: fmt.Sprintf("%064x", i+1)})
	}
	devs, err := a.SyncTokenFile(t.Context(), entries, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		bound bool
		err   error
	}
	out := make(chan result, len(devs))
	start := make(chan struct{})
	for i, d := range devs {
		c := a
		if i%2 == 1 {
			c = b
		}
		go func(c *Catalog, id string) {
			<-start
			bound, err := c.BindMachine(context.Background(), id, "shared-machine", time.Now())
			out <- result{bound, err}
		}(c, d.ID)
	}
	close(start)
	wins := 0
	for range devs {
		r := <-out
		switch {
		case r.err == nil && r.bound:
			wins++
		case errors.Is(r.err, ErrMachineTaken):
		default:
			t.Fatalf("a losing bind was not ErrMachineTaken: %v", r.err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d binds won", wins)
	}
}

// TKT-01M3MQM6: a change by id reaches that device even when its name
// has since passed to another.
func TestDeviceChangesByIDFollowTheID(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	created, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}, {Hash: strings.Repeat("b", 64), Name: "desk"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	laptop, desk := created[0], created[1]
	if _, err := c.BindMachine(ctx, laptop.ID, "01LAPTOP", now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BindMachine(ctx, desk.ID, "01DESK", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.PutProfile(ctx, "ci", []byte(`{}`), "op", "", now); err != nil {
		t.Fatal(err)
	}
	// The operator saw laptop; then its name passed to desk.
	for _, q := range []string{`UPDATE devices SET name='old' WHERE id='` + laptop.ID + `'`, `UPDATE devices SET name='laptop' WHERE id='` + desk.ID + `'`} {
		if _, err := c.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.SetDeviceProfileByID(ctx, laptop.ID, "ci", "ci", "op", now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UnbindDeviceByID(ctx, laptop.ID, "op", now); err != nil {
		t.Fatal(err)
	}
	if d, err := c.RevokeDeviceByID(ctx, laptop.ID, "op", now); err != nil || d.ID != laptop.ID || d.Revoked.IsZero() {
		t.Fatalf("revoke: %+v %v", d, err)
	}
	got, _ := c.DeviceByID(ctx, laptop.ID)
	other, _ := c.DeviceByID(ctx, desk.ID)
	if got.Profile != "ci" || got.MachineID != "" || got.Revoked.IsZero() {
		t.Errorf("the device seen was not changed: %+v", got)
	}
	if other.Profile != "" || other.MachineID != "01DESK" || !other.Revoked.IsZero() {
		t.Errorf("the device that took its name was changed: %+v", other)
	}
	if _, err := c.RevokeDeviceByID(ctx, "dev_nope", "op", now); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("unknown id: %v", err)
	}
}

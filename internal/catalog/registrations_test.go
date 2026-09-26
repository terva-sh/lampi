package catalog

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistrationRedeemsOnce(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := t.Context()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	secret := strings.Repeat("a", 64)
	token := strings.Repeat("b", 64)
	r, err := c.CreateRegistration(ctx, "laptop", secret, "ci", now, now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if r.State(now) != "pending" || !strings.HasPrefix(r.ID, "reg_") {
		t.Fatalf("%+v", r)
	}
	if _, err := c.CreateRegistration(ctx, "laptop", strings.Repeat("c", 64), "", now, now.Add(time.Hour)); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("second pending code for one name: %v", err)
	}
	if _, err := c.CreateRegistration(ctx, "Bad Name", strings.Repeat("c", 64), "", now, now.Add(time.Hour)); err == nil {
		t.Fatal("bad name accepted")
	}
	if _, _, err := c.Redeem(ctx, strings.Repeat("f", 64), token, "m1", now); !errors.Is(err, ErrRegistrationUnknown) {
		t.Fatalf("unknown secret: %v", err)
	}
	d, r, err := c.Redeem(ctx, secret, token, "m1", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "laptop" || d.Profile != "ci" || d.MachineID != "m1" || d.Source != DeviceFromRegistration || r.DeviceID != d.ID {
		t.Fatalf("device %+v registration %+v", d, r)
	}
	got, found, err := c.DeviceByHash(ctx, token)
	if err != nil || !found || got.ID != d.ID {
		t.Fatalf("device by token hash: %+v %v %v", got, found, err)
	}
	if _, r2, err := c.Redeem(ctx, secret, strings.Repeat("d", 64), "m2", now.Add(2*time.Minute)); !errors.Is(err, ErrRegistrationUsed) || r2.ID != r.ID {
		t.Fatalf("second redeem: %v", err)
	}
	if _, err := c.RevokeRegistration(ctx, r.ID, now); !errors.Is(err, ErrRegistrationUsed) {
		t.Fatalf("revoke a used code: %v", err)
	}
	if _, err := c.CreateRegistration(ctx, "laptop", strings.Repeat("c", 64), "", now, now.Add(time.Hour)); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("code for an existing device name: %v", err)
	}

	// Expired and revoked codes are refused.
	exp := strings.Repeat("1", 64)
	if _, err := c.CreateRegistration(ctx, "old", exp, "", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Redeem(ctx, exp, strings.Repeat("2", 64), "m3", now.Add(time.Hour)); !errors.Is(err, ErrRegistrationExpired) {
		t.Fatalf("expired: %v", err)
	}
	rev := strings.Repeat("3", 64)
	if _, err := c.CreateRegistration(ctx, "desk", rev, "", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RevokeRegistration(ctx, "desk", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Redeem(ctx, rev, strings.Repeat("4", 64), "m4", now); !errors.Is(err, ErrRegistrationRevoked) {
		t.Fatalf("revoked: %v", err)
	}
	// A token or a machine another device holds is refused, and the code
	// stays pending.
	again := strings.Repeat("5", 64)
	if _, err := c.CreateRegistration(ctx, "tab", again, "", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Redeem(ctx, again, token, "m9", now); !errors.Is(err, ErrTokenTaken) {
		t.Fatalf("taken token: %v", err)
	}
	if _, _, err := c.Redeem(ctx, again, strings.Repeat("6", 64), "m1", now); !errors.Is(err, ErrMachineTaken) {
		t.Fatalf("taken machine: %v", err)
	}
	if _, _, err := c.Redeem(ctx, again, strings.Repeat("6", 64), "m9", now); err != nil {
		t.Fatalf("code spent by a refused attempt: %v", err)
	}
	list, _ := c.Registrations(ctx)
	if len(list) != 4 {
		t.Fatalf("list %d", len(list))
	}

	if u, _ := c.PublicURL(ctx); u != "" {
		t.Fatal("url before set")
	}
	for _, u := range []string{"https://a.example", "https://b.example"} {
		if err := c.SetPublicURL(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if u, _ := c.PublicURL(ctx); u != "https://b.example" {
		t.Fatalf("url %q", u)
	}
}

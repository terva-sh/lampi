package catalog

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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
	desk, err := c.RevokeRegistration(ctx, "desk", now)
	if err != nil {
		t.Fatal(err)
	}
	// A second revoke changes nothing and says so.
	if again, err := c.RevokeRegistration(ctx, desk.ID, now.Add(time.Minute)); !errors.Is(err, ErrRegistrationRevoked) || !again.Revoked.Equal(desk.Revoked) {
		t.Fatalf("second revoke: %+v %v", again, err)
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

// serve redeems while the operator revokes, from two processes. One of
// them wins; a revoke never reports success for a code that made a
// device.
func TestRevokeNeverSucceedsOnACodeThatWasRedeemed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	serve, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer serve.Close()
	operator, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	ctx := t.Context()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	for i := range 200 {
		secret := fmt.Sprintf("%064x", i)
		r, err := serve.CreateRegistration(ctx, fmt.Sprintf("box-%d", i), secret, "", now, now.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		var redeemErr, revokeErr error
		var wg sync.WaitGroup
		wg.Go(func() {
			_, _, redeemErr = serve.Redeem(ctx, secret, fmt.Sprintf("%064x", 1000+i), fmt.Sprintf("m%d", i), now)
		})
		wg.Go(func() { _, revokeErr = operator.RevokeRegistration(ctx, r.ID, now) })
		wg.Wait()
		if redeemErr == nil && revokeErr == nil {
			t.Fatalf("round %d: the code was redeemed and revoked", i)
		}
		if redeemErr != nil && !errors.Is(redeemErr, ErrRegistrationRevoked) {
			t.Fatalf("round %d: redeem: %v", i, redeemErr)
		}
		if revokeErr != nil && !errors.Is(revokeErr, ErrRegistrationUsed) {
			t.Fatalf("round %d: revoke: %v", i, revokeErr)
		}
	}
}

func TestANameIsFreeTheMomentItsCodeExpires(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := t.Context()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	half := 500 * time.Millisecond
	// Stamps drop trailing zeros, so as text "12:00:00Z" sorts after
	// "12:00:00.5Z", and each of these would come out the wrong way.
	if _, err := c.CreateRegistration(ctx, "whole", strings.Repeat("a", 64), "", now.Add(-time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateRegistration(ctx, "whole", strings.Repeat("b", 64), "", now.Add(half), now.Add(time.Hour)); err != nil {
		t.Fatalf("name of a code that expired half a second ago: %v", err)
	}
	if _, err := c.CreateRegistration(ctx, "part", strings.Repeat("c", 64), "", now.Add(-time.Hour), now.Add(half)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateRegistration(ctx, "part", strings.Repeat("d", 64), "", now, now.Add(time.Hour)); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("name of a code with half a second left: %v", err)
	}
}

func TestRecordExpiriesHandsEachExpiredCodeOutOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	serve, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer serve.Close()
	operator, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	ctx := t.Context()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	mint := func(name string, secret string, expires time.Time) Registration {
		t.Helper()
		r, err := serve.CreateRegistration(ctx, name, strings.Repeat(secret, 64), "", now.Add(-72*time.Hour), expires)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	old := mint("old", "a", now.Add(-time.Hour))
	// Expires on a whole second, seen half a second later: as text the
	// later stamp sorts first.
	edge := mint("edge", "b", now)
	live := mint("live", "c", now.Add(time.Hour))
	gone := mint("gone", "d", now.Add(-time.Hour))
	if _, err := serve.RevokeRegistration(ctx, gone.ID, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	mint("used", "e", now.Add(-time.Hour))
	if _, _, err := serve.Redeem(ctx, strings.Repeat("e", 64), strings.Repeat("f", 64), "m1", now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// With ids, only those codes are looked at.
	got, err := serve.RecordExpiries(ctx, now.Add(500*time.Millisecond), live.ID, edge.ID)
	if err != nil || len(got) != 1 || got[0].ID != edge.ID {
		t.Fatalf("by id: %+v %v", got, err)
	}
	// serve and the operator race; each code goes to one of them.
	var a, b []Registration
	var aErr, bErr error
	var wg sync.WaitGroup
	wg.Go(func() { a, aErr = serve.RecordExpiries(ctx, now) })
	wg.Go(func() { b, bErr = operator.RecordExpiries(ctx, now) })
	wg.Wait()
	if aErr != nil || bErr != nil {
		t.Fatal(aErr, bErr)
	}
	if all := append(a, b...); len(all) != 1 || all[0].ID != old.ID {
		t.Fatalf("sweep: %+v", all)
	}
	// A recorded expiry is not handed out again; a code that expires
	// later is, once.
	if got, err := operator.RecordExpiries(ctx, now); err != nil || len(got) != 0 {
		t.Fatalf("second sweep: %+v %v", got, err)
	}
	if got, err := operator.RecordExpiries(ctx, now.Add(2*time.Hour)); err != nil || len(got) != 1 || got[0].ID != live.ID {
		t.Fatalf("later sweep: %+v %v", got, err)
	}
}

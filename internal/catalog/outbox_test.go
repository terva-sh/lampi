package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
)

// TKT-01M3GAHSN: an expiry whose line cannot be written stays queued and
// is written, once, when the audit log works again.
func TestAuditOutboxKeepsWhatCannotBeWritten(t *testing.T) {
	c, path := openTemp(t)
	ctx := t.Context()
	dir := filepath.Dir(path)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	if _, err := c.CreateRegistration(ctx, "old", strings.Repeat("a", 64), "", "", ActorCLI, now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// An audit log that cannot be opened for append.
	if err := os.Mkdir(audit.Path(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := c.RecordExpiries(ctx, now, "serve register"); err != nil || len(got) != 1 {
		t.Fatalf("record: %v %v", got, err)
	}
	if err := c.FlushAudit(ctx, dir); err == nil {
		t.Fatal("flush into a directory succeeded")
	}
	if n, err := c.PendingAudit(ctx); err != nil || n != 1 {
		t.Fatalf("pending after a failed flush: %d %v", n, err)
	}
	// The code is marked, so another look queues nothing more.
	if got, err := c.RecordExpiries(ctx, now.Add(time.Minute), "serve register"); err != nil || len(got) != 0 {
		t.Fatalf("second record: %v %v", got, err)
	}
	if err := os.Remove(audit.Path(dir)); err != nil {
		t.Fatal(err)
	}
	if err := c.FlushAudit(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if err := c.FlushAudit(ctx, dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"kind":"registration.expired","device":"old","actor":"serve register"`); n != 1 {
		t.Fatalf("%d expiry lines:\n%s", n, raw)
	}
	if n, _ := c.PendingAudit(ctx); n != 0 {
		t.Fatalf("pending after the flush: %d", n)
	}
}

// A redemption that rolls back queues nothing.
func TestRolledBackRedemptionQueuesNoAudit(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	secret := strings.Repeat("b", 64)
	if _, err := c.CreateRegistration(ctx, "box", secret, "", "", ActorCLI, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	refuse := func(Device, Registration) ([]audit.Event, error) { return nil, os.ErrPermission }
	if _, _, err := c.Redeem(ctx, secret, strings.Repeat("c", 64), "m1", nil, now, refuse); err == nil {
		t.Fatal("redeem went through")
	}
	events := func(d Device, r Registration) ([]audit.Event, error) {
		return []audit.Event{{Kind: audit.RegistrationRedeemed, Device: d.Name, Detail: "registration=" + r.ID}}, nil
	}
	if _, _, err := c.Redeem(ctx, secret, strings.Repeat("c", 64), "m1", nil, now, events); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.PendingAudit(ctx); n != 1 {
		t.Fatalf("pending %d, want 1", n)
	}
}

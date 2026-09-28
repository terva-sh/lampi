package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// Two handles on one catalog, as serve and a serve command would hold,
// flush one queue into one log without losing or reordering a line.
func TestFlushesAcrossHandlesKeepTheOrder(t *testing.T) {
	c, path := openTemp(t)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	ctx := t.Context()
	dir := filepath.Dir(path)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	const n = 60
	for i := range n {
		if err := c.QueueAudit(ctx, now, audit.Event{Kind: "test.event", Detail: fmt.Sprintf("n=%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, h := range []*Catalog{c, other, c, other} {
		wg.Go(func() {
			if err := h.FlushAudit(ctx, dir); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	raw, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var i int
		if _, err := fmt.Sscanf(line[strings.Index(line, "n=")+2:], "%03d", &i); err != nil || i <= last {
			t.Fatalf("line out of order or repeated after %d: %s", last, line)
		}
		last = i
	}
	if last != n-1 {
		t.Fatalf("last line %d, want %d", last, n-1)
	}
}

// Token-file sync, bind, and the operator's device and code commands
// queue their events with the change.
func TestDeviceChangesQueueTheirAudit(t *testing.T) {
	c, path := openTemp(t)
	ctx := t.Context()
	dir := filepath.Dir(path)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	created, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}, {Hash: strings.Repeat("b", 64), Name: "desk"}}, now)
	if err != nil || len(created) != 2 {
		t.Fatalf("sync: %v %v", created, err)
	}
	if _, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BindMachine(ctx, created[0].ID, "m1", now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetDeviceProfile(ctx, "laptop", "ci", "ci", "test", now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UnbindDevice(ctx, "laptop", "test", now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RevokeDevice(ctx, "laptop", "test", now); err != nil {
		t.Fatal(err)
	}
	// A second revoke changes nothing and queues nothing.
	if _, err := c.RevokeDevice(ctx, "laptop", "test", now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateRegistration(ctx, "box", strings.Repeat("c", 64), "", "", ActorCLI, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RevokeRegistration(ctx, "box", ActorCLI, "serve register --revoke", now); err != nil {
		t.Fatal(err)
	}
	if err := c.FlushAudit(ctx, dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(audit.Path(dir))
	for kind, want := range map[string]int{audit.DeviceCreated: 2, audit.DeviceDetached: 1, audit.DeviceBound: 1, audit.DeviceProfile: 1, audit.DeviceUnbound: 1, audit.DeviceRevoked: 1, audit.RegistrationRevoked: 1} {
		if n := strings.Count(string(raw), `"kind":"`+kind+`"`); n != want {
			t.Fatalf("%d %s lines, want %d:\n%s", n, kind, want, raw)
		}
	}
	if !strings.Contains(string(raw), `"kind":"device.detached","device":"desk"`) {
		t.Fatalf("detach names the wrong device:\n%s", raw)
	}
}

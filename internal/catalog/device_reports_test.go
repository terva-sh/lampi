package catalog

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func TestDeviceReportReplacesTheOneBefore(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := t.Context()
	created, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("a", 64), Name: "a"}, {Hash: strings.Repeat("b", 64), Name: "b"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	a, b := created[0], created[1]
	if _, ok, err := c.DeviceReport(ctx, a.ID); err != nil || ok {
		t.Fatalf("report before any: %v %v", ok, err)
	}

	t1 := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	first := protocol.AgentReport{AgentVersion: "v0.1.2", LastError: "lake down", LastErrorAt: t1}
	if err := c.PutDeviceReport(ctx, a.ID, first, t1); err != nil {
		t.Fatal(err)
	}
	t2 := t1.Add(5 * time.Minute)
	second := protocol.AgentReport{
		AgentVersion:   "v0.1.3",
		ProfileVersion: "sha256:8427a42989cbc15f",
		AllowSource:    "lake default",
		LastSync:       &protocol.AgentSyncReport{At: t2, Checked: 221, Refused: 3, Unchanged: 218},
	}
	if err := c.PutDeviceReport(ctx, a.ID, second, t2); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.DeviceReport(ctx, a.ID)
	if err != nil || !ok {
		t.Fatalf("report: %v %v", ok, err)
	}
	if !got.Received.Equal(t2) || got.Report.AgentVersion != "v0.1.3" || got.Report.LastError != "" {
		t.Fatalf("report %+v", got)
	}
	if s := got.Report.LastSync; s == nil || s.Checked != 221 || s.Refused != 3 || s.Unchanged != 218 || !s.At.Equal(t2) {
		t.Fatalf("last sync %+v", got.Report.LastSync)
	}

	// A report that took an earlier time but commits later does not
	// replace the newer one.
	if err := c.PutDeviceReport(ctx, a.ID, protocol.AgentReport{AgentVersion: "v0.1.1"}, t2.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := c.DeviceReport(ctx, a.ID); got.Report.AgentVersion != "v0.1.3" || !got.Received.Equal(t2) {
		t.Fatalf("an older report replaced the newer: %+v", got)
	}

	if err := c.PutDeviceReport(ctx, b.ID, protocol.AgentReport{AgentVersion: "v0.1.2"}, t1); err != nil {
		t.Fatal(err)
	}
	all, err := c.DeviceReports(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("reports %d, want one per device", len(all))
	}
}

func TestDeviceReportNeedsADevice(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.PutDeviceReport(t.Context(), "dev_nosuchdevice", protocol.AgentReport{}, time.Now()); err == nil {
		t.Fatal("report for an unknown device was stored")
	}
}

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
)

func TestAgentReportsItsSyncAndProfileToTheLake(t *testing.T) {
	lake, url := profileLake(t)
	// A device token, so the lake files the report under a device.
	tok := strings.Repeat("c3", 32)
	lake.Allow(tok)
	home, cfg, state, _ := agentFixture(t, url)
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi", "tokens"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "tokens", "work.token"), []byte(tok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k := lake.Identity().Public()[0]
	writeAgentConfig(t, cfg, `{"lakes":{"work":{"server":"`+url+`","lake_id":"`+lake.Identity().LakeID+`","key_id":"`+k.ID+`","public_key":"`+k.PublicKey+`"}},"agent":{"debounce":"100ms"}}`)
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	env := Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}
	go func() { done <- runAgentLoop(ctx, env, "", "") }()

	var got catalog.DeviceReport
	deadline := time.Now().Add(10 * time.Second)
	for {
		all, err := lake.Catalog.DeviceReports(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(all) == 1 && all[0].Report.LastSync != nil && all[0].Report.LastSync.Uploaded == 1 {
			got = all[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no report of the upload: %+v\n%s", all, buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The inventory follows the report, sociable by default: the one
	// session's project, allowed.
	var inv catalog.DeviceInventory
	for {
		i, ok, err := lake.Catalog.DeviceInventoryOf(t.Context(), got.DeviceID)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			inv = i
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no inventory\n%s", buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p := inv.Inventory.Projects; inv.Inventory.Mode != "sociable" || len(p) != 1 || !p[0].Allowed || p[0].Sessions != 1 || p[0].Bytes == 0 {
		t.Fatalf("inventory %+v", inv.Inventory)
	}
	r := got.Report
	if r.Inventory != "sociable" {
		t.Fatalf("report inventory mode %q", r.Inventory)
	}
	if r.AgentVersion != agentVersion() || r.MachineID == "" || r.Profile != "default" || !strings.HasPrefix(r.ProfileVersion, "sha256:") || r.Pinned == nil || !*r.Pinned {
		t.Fatalf("report %+v", r)
	}
	// work has no allow rule of its own, so the lake's rules are in force.
	if r.AllowSource != "lake work" || r.DenySource != "none" || r.LastError != "" {
		t.Fatalf("sources %q %q, error %q", r.AllowSource, r.DenySource, r.LastError)
	}
}

func TestReportKeepsTheLastFinishedSyncThroughAnError(t *testing.T) {
	r := newLakeRunner(Env{}, agentLake{})
	t0 := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	r.noteSync(upload.Result{Checked: 3, Refused: 3}, &upload.Rejected{Reasons: []string{"x"}}, t0)
	r.noteSync(upload.Result{}, errors.New("lake down"), t0.Add(time.Minute))
	rep := r.report()
	if rep.LastSync == nil || rep.LastSync.Refused != 3 || !rep.LastSync.At.Equal(t0) {
		t.Fatalf("last sync %+v", rep.LastSync)
	}
	if rep.LastError != "lake down" || !rep.LastErrorAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("error %q at %v", rep.LastError, rep.LastErrorAt)
	}
	r.noteSync(upload.Result{Checked: 3, Unchanged: 3}, nil, t0.Add(2*time.Minute))
	if rep := r.report(); rep.LastError != "" || rep.LastSync.Unchanged != 3 || rep.AllowSource != "local" || rep.Pinned == nil || *rep.Pinned {
		t.Fatalf("after success %+v", rep)
	}
}

func TestReportToAnOlderLakeIsSaidOnce(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	var buf memBuf
	r := newLakeRunner(Env{Stdout: &buf, Stderr: &buf}, agentLake{opt: upload.Options{ServerURL: srv.URL}})
	close(r.ready)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); watchReports(ctx, r, 20*time.Millisecond) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-stopped
	out := buf.String()
	if hits.Load() < 3 || strings.Count(out, "does not take agent reports") != 1 {
		t.Fatalf("%d tries, output:\n%s", hits.Load(), out)
	}
}

// TKT-01M3M7M0TH: the agent sends its inventory when it changes, not
// with every report, and says once that a lake does not take them,
// without sending that lake the same inventory again.
func TestInventoryIsSentWhenItChanges(t *testing.T) {
	var reports, inventories atomic.Int32
	var missing atomic.Bool
	var last atomic.Pointer[protocol.AgentInventory]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case protocol.AgentReportPath:
			reports.Add(1)
			w.Write([]byte(`{}`))
		case protocol.AgentInventoryPath:
			inventories.Add(1)
			if missing.Load() {
				http.NotFound(w, r)
				return
			}
			var inv protocol.AgentInventory
			if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
				t.Error(err)
			}
			last.Store(&inv)
			w.Write([]byte(`{"kept":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var buf memBuf
	r := newLakeRunner(Env{Stdout: &buf, Stderr: &buf}, agentLake{opt: upload.Options{ServerURL: srv.URL}, inventory: protocol.InventoryStrict})
	close(r.ready)
	// Each sync hands over rows of its own, as Sync does: the report
	// loop reads the last ones while the next are built.
	rows := func(sessions int) []upload.InventoryRow {
		return []upload.InventoryRow{
			{CWD: "/w/app", CWDs: 1, GitRemote: "git@git.example:team/app.git", Harnesses: []string{"claude"}, Sessions: sessions, Bytes: 10},
			{CWD: "/w/secret", CWDs: 1, Harnesses: []string{"codex"}, Sessions: 5, Bytes: 50, Reason: "no allow rule matches"},
		}
	}
	r.noteSync(upload.Result{Inventory: rows(2)}, nil, time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); watchReports(ctx, r, 10*time.Millisecond) }()
	defer func() { cancel(); <-stopped }()
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %d reports, %d inventories\n%s", what, reports.Load(), inventories.Load(), buf.String())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitFor("first inventory", func() bool { return inventories.Load() == 1 })
	n := reports.Load()
	waitFor("more reports", func() bool { return reports.Load() >= n+5 })
	if got := inventories.Load(); got != 1 {
		t.Fatalf("an unchanged inventory was sent %d times", got)
	}
	// Strict: the allowed project, folded, and the refused ones counted.
	inv := last.Load()
	if inv.Mode != "strict" || len(inv.Projects) != 1 || inv.Projects[0].GitRemote != "git.example/team/app" ||
		inv.RefusedSessions != 5 || inv.RefusedBytes != 50 || inv.GeneratedAt.IsZero() {
		t.Fatalf("sent %+v", inv)
	}

	r.noteSync(upload.Result{Inventory: rows(3)}, nil, time.Now())
	waitFor("changed inventory", func() bool { return inventories.Load() == 2 })

	missing.Store(true)
	r.noteSync(upload.Result{Inventory: rows(4)}, nil, time.Now())
	waitFor("to the older lake", func() bool { return inventories.Load() == 3 })
	n = reports.Load()
	waitFor("more reports", func() bool { return reports.Load() >= n+5 })
	if got := inventories.Load(); got != 3 {
		t.Fatalf("an older lake was sent the same inventory again: %d", got)
	}
	if c := strings.Count(buf.String(), "does not take inventories"); c != 1 {
		t.Fatalf("said %d times:\n%s", c, buf.String())
	}
}

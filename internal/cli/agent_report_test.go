package cli

import (
	"context"
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
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	r := got.Report
	if r.AgentVersion != version || r.MachineID == "" || r.Profile != "default" || !strings.HasPrefix(r.ProfileVersion, "sha256:") {
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
	if rep := r.report(); rep.LastError != "" || rep.LastSync.Unchanged != 3 || rep.AllowSource != "local" {
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

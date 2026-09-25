package upload

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/redact"
)

// A session that stays quarantined adds one record, not one per sync.
// Allowing its digest uploads those bytes with an override stamp. The
// same file after it grows is scanned and held again.
func TestQuarantineRecordsOnceAndAllowsOneDigest(t *testing.T) {
	secret := "AKIA" + "Z2X5QW7RT3LK9PMN"
	body := "{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n" + secret + "\n"
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	path := writeSession(t, home, "abcd", "s.jsonl", []byte(body))
	state := t.TempDir()
	opt := allowAll(srv, home, state, "/work/app")
	opt.Client = cap.client

	for range 3 {
		if _, err := Sync(context.Background(), opt); err == nil {
			t.Fatal("quarantined session was not refused")
		}
	}
	recs, err := redact.ReadQuarantine(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("three syncs wrote %d records", len(recs))
	}

	if err := redact.Allow(state, redact.Allowed{SHA256: recs[0].SHA256, RelPath: recs[0].RelPath}); err != nil {
		t.Fatal(err)
	}
	cap.reset()
	res, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Manifests != 1 || len(cap.manifests) != 1 {
		t.Fatalf("allowed digest did not upload: %+v", res)
	}
	red := cap.manifests[0].Artifacts[0].Redaction
	if red.Status != protocol.RedactionOverride || red.Hits < 1 {
		t.Fatalf("stamp %+v", red)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"type\":\"message\"}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	cap.reset()
	res, err = Sync(context.Background(), opt)
	if err == nil || res.Quarantined != 1 || cap.puts != 0 {
		t.Fatalf("grown file was not held again: %+v puts %d err %v", res, cap.puts, err)
	}
	if recs, _ := redact.ReadQuarantine(state); len(recs) != 2 {
		t.Fatalf("grown file records %d, want 2", len(recs))
	}
}

// last_attempt.json records a failed run and keeps its error after a
// later run finishes.
func TestAttemptRecordsTheLastError(t *testing.T) {
	state := t.TempDir()
	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n"))
	opt := Options{
		ServerURL:    "http://127.0.0.1:1",
		TervaHome:    home,
		MachineID:    "machine-1",
		StateDir:     state,
		Projects:     config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
		StallTimeout: time.Second,
	}
	if _, err := Sync(context.Background(), opt); err == nil {
		t.Fatal("a down lake succeeded")
	}
	a, ok, err := ReadAttempt(state)
	if err != nil || !ok || a.Error == "" || a.LastError != a.Error || a.LastErrorAt.IsZero() {
		t.Fatalf("attempt %+v ok %v err %v", a, ok, err)
	}
	failed := a.LastError

	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	opt.ServerURL = srv.URL
	opt.Token = "tok"
	opt.Client = srv.Client()
	if _, err := Sync(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	a, _, _ = ReadAttempt(state)
	if a.Error != "" || a.LastError != failed {
		t.Fatalf("after success %+v", a)
	}

	// A run cut short by shutdown is not a failure.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opt.ServerURL = "http://127.0.0.1:1"
	if _, err := Sync(ctx, opt); err == nil {
		t.Fatal("a cancelled sync succeeded")
	}
	if b, _, _ := ReadAttempt(state); b.Error != "" {
		t.Fatalf("shutdown recorded as a failure: %+v", b)
	}
	if !strings.Contains(failed, "127.0.0.1:1") {
		t.Fatalf("error does not name the lake: %s", failed)
	}
}

package upload

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Cursor CLI session the memo recalls has no export on disk. When the
// upload needs its bytes, here because the first pass never reached
// the lake and left no watermark, Load exports it and it uploads. The
// pass after that finds it unchanged.
func TestSyncCursorCLIMemoLoadsWhenBytesAreNeeded(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	cli := filepath.Join(home, "chats", "ab12", "sid-1", "store.db")
	if err := writeCursorCLIStore(cli, "hello from cli"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(cli), "meta.json"), []byte(`{"cwd":"/work/app"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := allowAll(srv, t.TempDir(), t.TempDir(), "/work/app")
	opt.Client = cap.client
	opt.CursorCLIHome = home
	opt.Memo = NewMemo()

	down := httptest.NewServer(nil)
	down.Close()
	offline := opt
	offline.ServerURL = down.URL
	if _, err := Sync(context.Background(), offline); err == nil {
		t.Fatal("sync to a closed lake succeeded")
	}

	res, err := Sync(context.Background(), opt)
	if err != nil || res.Uploaded != 1 || res.Manifests != 1 {
		t.Fatalf("recalled session did not upload: %+v err %v", res, err)
	}
	if len(cap.manifests) != 1 || cap.manifests[0].Artifacts[0].SHA256 == "" {
		t.Fatalf("manifests %+v", cap.manifests)
	}

	cap.reset()
	res, err = Sync(context.Background(), opt)
	if err != nil || res.Unchanged != 1 || res.Manifests != 0 || cap.puts != 0 {
		t.Fatalf("third pass: %+v puts %d err %v", res, cap.puts, err)
	}
}

// A file whose digest the quarantine log already holds is held again
// without being read or scanned, and still counts and reports as
// quarantined.
func TestKnownQuarantineIsNotScannedAgain(t *testing.T) {
	secret := "AKIA" + "Z2X5QW7RT3LK9PMN"
	body := "{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n" + secret + "\n"
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte(body))
	opt := allowAll(srv, home, t.TempDir(), "/work/app")

	scans := 0
	scanHook = func(string, int64) { scans++ }
	t.Cleanup(func() { scanHook = nil })

	var lines []string
	for range 2 {
		res, err := Sync(context.Background(), opt)
		if err == nil || res.Quarantined != 1 {
			t.Fatalf("not quarantined: %+v err %v", res, err)
		}
		lines = append(lines, err.Error())
	}
	if scans != 1 {
		t.Fatalf("held file was scanned %d times", scans)
	}
	if lines[0] != lines[1] || !strings.Contains(lines[1], "aws-access-key-id") || !strings.Contains(lines[1], "quarantined and not uploaded") {
		t.Fatalf("second report differs:\n%s\n%s", lines[0], lines[1])
	}

	opt.UploadHits = true
	if res, err := Sync(context.Background(), opt); err != nil || res.Uploaded != 1 || scans != 2 {
		t.Fatalf("upload_hits did not scan and upload: %+v scans %d err %v", res, scans, err)
	}
}

// With Settle set, a Cursor CLI session written just now is held: the
// result counts it and says when it may be read, nothing is posted,
// and the inventory still counts the session. Without Settle, the same
// session uploads.
func TestSyncHoldsACursorCLISessionStillBeingWritten(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	db := filepath.Join(home, "acp-sessions", "sid-1", "store.db")
	if err := writeCursorCLIStore(db, "hello from acp"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(db), "meta.json"), []byte(`{"schemaVersion":1,"cwd":"/work/app"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := allowAll(srv, t.TempDir(), t.TempDir(), "/work/app")
	opt.Client = cap.client
	opt.CursorCLIHome = home
	opt.Settle = time.Hour

	before := time.Now()
	res, err := Sync(context.Background(), opt)
	if err != nil || res.Held != 1 || res.Manifests != 0 || len(cap.manifests) != 0 {
		t.Fatalf("held pass: %+v err %v", res, err)
	}
	if res.HeldUntil.Before(before.Add(59*time.Minute)) || res.HeldUntil.After(time.Now().Add(time.Hour)) {
		t.Fatalf("held until %v", res.HeldUntil)
	}
	if len(res.Inventory) != 1 || res.Inventory[0].Sessions != 1 {
		t.Fatalf("inventory %+v", res.Inventory)
	}

	opt.Settle = 0
	res, err = Sync(context.Background(), opt)
	if err != nil || res.Held != 0 || !res.HeldUntil.IsZero() || res.Uploaded != 1 {
		t.Fatalf("unheld pass: %+v err %v", res, err)
	}
	if len(cap.manifests) != 1 || cap.manifests[0].NativeSessionID != "acp-sessions/sid-1" {
		t.Fatalf("manifests %+v", cap.manifests)
	}
}

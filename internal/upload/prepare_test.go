package upload

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/terva"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/watermark"
)

func openQueues(t *testing.T, state string) (*watermark.DB, *outbox.DB) {
	t.Helper()
	wm, err := watermark.Open(watermark.File(state))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { wm.Close() })
	q, err := outbox.Open(outbox.File(state))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { q.Close() })
	return wm, q
}

// Two sessions whose sidecars were identical when hashed. The denied
// one changes before the read. The allowed session must still read its
// own file, never the other path with the same digest.
func TestPrepareReadsOnlyTheSessionsOwnPaths(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "aaaa", "a.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"a\",\"cwd\":\"/work/app\"}}\n"))
	writeSession(t, home, "aaaa", "a.errors.jsonl", nil)
	writeSession(t, home, "zzzz", "z.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"z\",\"cwd\":\"/secret\"}}\n"))
	denied := writeSession(t, home, "zzzz", "z.errors.jsonl", nil)
	bundle, err := terva.Manifests(home, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(denied, []byte("bytes from the denied session\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := t.TempDir()
	wm, q := openQueues(t, state)
	opt := Options{
		MachineID: "machine-1",
		StateDir:  state,
		Projects:  config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}
	work, res, err := prepare(context.Background(), opt, wm, q, []adapter.Bundle{bundle})
	if _, ok := err.(*Rejected); !ok || res.Refused != 1 {
		t.Fatalf("err %v res %+v", err, res)
	}
	if len(work) != 1 || len(work[0].manifest.Artifacts) != 2 {
		t.Fatalf("work %+v", work)
	}
	for _, body := range work[0].bodies {
		if strings.Contains(string(body), "denied session") {
			t.Fatal("the allowed session read the denied session's file")
		}
	}
}

// A file that changed after the digest was taken is not read into this
// round. The session waits for the next one.
func TestPrepareSkipsAFileThatChangedAfterHashing(t *testing.T) {
	home := t.TempDir()
	path := writeSession(t, home, "aaaa", "a.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"a\",\"cwd\":\"/work/app\"}}\n"))
	bundle, err := terva.Manifests(home, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"type\":\"user\"}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	state := t.TempDir()
	wm, q := openQueues(t, state)
	opt := Options{
		MachineID: "machine-1",
		StateDir:  state,
		Projects:  config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}
	work, res, err := prepare(context.Background(), opt, wm, q, []adapter.Bundle{bundle})
	if err != nil || len(work) != 0 || res.Refused != 0 || res.Quarantined != 0 {
		t.Fatalf("work %d res %+v err %v", len(work), res, err)
	}
	assertOutboxEmpty(t, state)

	// The next round hashes the new bytes and sends them.
	bundle, err = terva.Manifests(home, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	work, _, err = prepare(context.Background(), opt, wm, q, []adapter.Bundle{bundle})
	if err != nil || len(work) != 1 {
		t.Fatalf("next round: %d %v", len(work), err)
	}
}

// A token in a relpath is not in the file's bytes, so only a scan of
// the manifest finds it. The manifest is refused and the secret stays
// out of the error and the quarantine log.
func TestSyncScansTheManifest(t *testing.T) {
	// Not a published example: those are skipped by the ruleset.
	secret := "AKIA" + "Z7Q4M2X9K3W8N5R1"
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	writeSession(t, home, "abcd", secret+".jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n"))
	state := t.TempDir()
	opt := allowAll(srv, home, state, "/work/app")
	opt.Client = cap.client
	opt.UploadHits = true

	res, err := Sync(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "in the manifest") || !strings.Contains(err.Error(), "aws-access-key-id") {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error contains the secret: %v", err)
	}
	if res.Quarantined != 1 || cap.puts != 0 || len(cap.manifests) != 0 {
		t.Fatalf("res %+v puts %d manifests %d", res, cap.puts, len(cap.manifests))
	}
	if blobCount(t, filepath.Join(data, "cas")) != 0 {
		t.Fatal("bytes were stored")
	}
	log, err := os.ReadFile(filepath.Join(state, "quarantine.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), secret) || !strings.Contains(string(log), "aws-access-key-id") {
		t.Fatalf("quarantine log:\n%s", log)
	}
	assertOutboxEmpty(t, state)
}

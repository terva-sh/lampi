package upload

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"terva.sh/lampi/internal/api"
)

func TestSyncIdempotentThenGrowth(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Token = "tok"
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opt := Options{
		ServerURL: srv.URL,
		Token:     "tok",
		TervaHome: home,
		MachineID: "machine-1",
		Client:    srv.Client(),
	}
	ctx := context.Background()
	first, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if first.Uploaded != 1 || first.Manifests != 1 || first.Sessions[0] == "" {
		t.Fatalf("first: %+v", first)
	}
	second, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if second.Uploaded != 0 || second.Missing != 0 || second.Sessions[0] != first.Sessions[0] {
		t.Fatalf("second: %+v", second)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"type\":\"message\"}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	third, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	// Whole-file re-upload. Tail-only merge is not implemented; the new
	// digest is a second blob and the session uid stays put.
	if third.Uploaded != 1 || third.Sessions[0] != first.Sessions[0] {
		t.Fatalf("third: %+v", third)
	}
}

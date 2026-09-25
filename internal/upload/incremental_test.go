package upload

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// recordScans sets scanHook for the test and returns what it heard,
// the clean prefix each artifact's scan started after.
func recordScans(t *testing.T) *[]int64 {
	t.Helper()
	var got []int64
	scanHook = func(_ string, clean int64) { got = append(got, clean) }
	t.Cleanup(func() { scanHook = nil })
	return &got
}

// With a memo, a file whose size, mtime, and inode have not moved is
// not opened. The proof is a rewrite in place that keeps all three: it
// is taken as unchanged until the memo's next full pass hashes it.
// Without a memo the same rewrite is read and posted at once.
func TestMemoSkipsUnchangedFilesUntilAFullPass(t *testing.T) {
	for _, memo := range []bool{true, false} {
		lake, _ := openLake(t)
		srv := httptest.NewServer(lake.Handler())
		t.Cleanup(srv.Close)
		cap := wrapClient(srv.Client())
		home := t.TempDir()
		meta := "{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n"
		path := writeSession(t, home, "abcd", "s.jsonl", []byte(meta+"first line of the transcript\n"))
		opt := allowAll(srv, home, t.TempDir(), "/work/app")
		opt.Client = cap.client
		now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
		opt.Now = func() time.Time { return now }
		if memo {
			opt.Memo = NewMemo()
		}
		ctx := context.Background()
		if res, err := Sync(ctx, opt); err != nil || res.Manifests != 1 {
			t.Fatalf("first: %+v %v", res, err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(meta+"FIRST LINE OF THE TRANSCRIPT\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, st.ModTime(), st.ModTime()); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
		cap.reset()
		res, err := Sync(ctx, opt)
		if err != nil {
			t.Fatal(err)
		}
		if memo && (res.Manifests != 0 || res.Unchanged != 1 || len(cap.manifests) != 0) {
			t.Fatalf("memo trusted the stat, want no post: %+v", res)
		}
		if !memo && (res.Manifests != 1 || res.Unchanged != 0) {
			t.Fatalf("no memo, want the rewrite posted: %+v", res)
		}
		if !memo {
			continue
		}
		now = now.Add(FullPassEvery)
		cap.reset()
		res, err = Sync(ctx, opt)
		if err != nil {
			t.Fatal(err)
		}
		if res.Manifests != 1 || res.Unchanged != 0 {
			t.Fatalf("full pass, want the rewrite posted: %+v", res)
		}
	}
}

// An append to a file whose watermarked bytes scanned clean scans only
// what follows them. A file uploaded with a hit is scanned whole.
func TestAppendScansOnlyTheTail(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	home := t.TempDir()
	body := "{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n" + strings.Repeat("{\"type\":\"message\",\"text\":\"clean\"}\n", 20000)
	path := writeSession(t, home, "abcd", "s.jsonl", []byte(body))
	opt := allowAll(srv, home, t.TempDir(), "/work/app")
	opt.Memo = NewMemo()
	ctx := context.Background()
	scans := recordScans(t)
	if res, err := Sync(ctx, opt); err != nil || res.Manifests != 1 {
		t.Fatalf("first: %+v %v", res, err)
	}
	appendFile(t, path, "{\"type\":\"message\",\"text\":\"more\"}\n")
	if res, err := Sync(ctx, opt); err != nil || res.Manifests != 1 {
		t.Fatalf("append: %+v %v", res, err)
	}
	if len(*scans) != 2 || (*scans)[0] != 0 || (*scans)[1] != int64(len(body)) {
		t.Fatalf("scans started after %v, want 0 then %d", *scans, len(body))
	}

	// An override upload leaves a watermark with a hit. Its prefix is
	// not clean, so the next append scans the whole file.
	secret := "AKIA" + "Z2X5QW7RT3LK9PMN"
	appendFile(t, path, secret+"\n")
	opt.UploadHits = true
	if res, err := Sync(ctx, opt); err != nil || res.Manifests != 1 {
		t.Fatalf("override: %+v %v", res, err)
	}
	appendFile(t, path, "{\"type\":\"message\",\"text\":\"after\"}\n")
	*scans = nil
	if res, err := Sync(ctx, opt); err != nil || res.Manifests != 1 {
		t.Fatalf("after override: %+v %v", res, err)
	}
	if len(*scans) != 1 || (*scans)[0] != 0 {
		t.Fatalf("after a hit the scan started at %v, want 0", *scans)
	}
}

// A secret that straddles the old end of the file, and a private-key
// block whose BEGIN line the old end cut, are still quarantined when
// only the tail is scanned.
func TestTailScanQuarantinesAcrossTheBoundary(t *testing.T) {
	pad := strings.Repeat("{\"type\":\"message\",\"text\":\"clean\"}\n", 5000)
	var keyBody strings.Builder
	for range 100 {
		keyBody.WriteString(strings.Repeat("A", 64) + `\n`)
	}
	cases := []struct {
		name, before, after, rule string
	}{
		{"token", `{"type":"message","text":"key sk-ant-api03-abcdef`, strings.Repeat("c", 40) + "\"}\n", "anthropic-key"},
		{"pem", `{"type":"message","text":"-----BEGIN RSA PRIV`, `ATE KEY-----\n` + keyBody.String() + `-----END RSA PRIVATE KEY-----"}` + "\n", "private-key"},
	}
	for _, tc := range cases {
		lake, _ := openLake(t)
		srv := httptest.NewServer(lake.Handler())
		t.Cleanup(srv.Close)
		cap := wrapClient(srv.Client())
		home := t.TempDir()
		first := "{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n" + pad + tc.before
		path := writeSession(t, home, "abcd", "s.jsonl", []byte(first))
		opt := allowAll(srv, home, t.TempDir(), "/work/app")
		opt.Client = cap.client
		opt.Memo = NewMemo()
		ctx := context.Background()
		scans := recordScans(t)
		if res, err := Sync(ctx, opt); err != nil || res.Manifests != 1 {
			t.Fatalf("%s first: %+v %v", tc.name, res, err)
		}
		appendFile(t, path, tc.after)
		cap.reset()
		res, err := Sync(ctx, opt)
		if err == nil || !strings.Contains(err.Error(), tc.rule) || res.Quarantined != 1 || res.Manifests != 0 || cap.puts != 0 {
			t.Fatalf("%s append: %+v %v puts=%d", tc.name, res, err, cap.puts)
		}
		if got := (*scans)[len(*scans)-1]; got != int64(len(first)) {
			t.Fatalf("%s: scan started after %d, want the tail after %d", tc.name, got, len(first))
		}
	}
}

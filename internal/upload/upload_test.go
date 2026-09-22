package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watermark"
)

func TestSyncIdempotentThenGrowth(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	body := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n")
	path := writeSession(t, home, "abcd", "s.jsonl", body)
	opt := allowAll(srv, home, t.TempDir(), "/tmp/p")
	opt.Client = cap.client
	ctx := context.Background()

	first, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if first.Uploaded != 1 || first.Manifests != 1 || first.Sessions[0] == "" {
		t.Fatalf("first: %+v", first)
	}
	if blobCount(t, filepath.Join(data, "cas")) != 1 {
		t.Fatal("expected one blob")
	}
	assertRuleset(t, cap.manifests)
	assertFullFileWire(t, cap.manifests)
	sum := sha256.Sum256(body)
	assertWatermark(t, opt, "sessions/abcd/s.jsonl", int64(len(body)), hex.EncodeToString(sum[:]))
	assertOutboxEmpty(t, opt.StateDir)

	cap.reset()
	second, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if second.Uploaded != 0 || second.Missing != 0 || second.Sessions[0] != first.Sessions[0] {
		t.Fatalf("second: %+v", second)
	}
	if cap.puts != 0 {
		t.Fatalf("re-sync PUT count %d", cap.puts)
	}
	assertFullFileWire(t, cap.manifests)
	if blobCount(t, filepath.Join(data, "cas")) != 1 {
		t.Fatal("re-sync stored another blob")
	}
	assertOutboxEmpty(t, opt.StateDir)

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
	// The lake does not assemble tails, so growth is one new full blob.
	// The watermark still moved only because the manifest was ACKed.
	if third.Uploaded != 1 || third.Sessions[0] != first.Sessions[0] {
		t.Fatalf("third: %+v", third)
	}
	grown, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	grownSum := sha256.Sum256(grown)
	assertWatermark(t, opt, "sessions/abcd/s.jsonl", int64(len(grown)), hex.EncodeToString(grownSum[:]))
	// Growth is a local tail, but the PUT is the new full file.
	// prev stays 0 and tail_sha256 is that full digest, not the suffix.
	assertFullFileWire(t, cap.manifests)
	if cap.manifests[len(cap.manifests)-1].Artifacts[0].SHA256 != hex.EncodeToString(grownSum[:]) {
		t.Fatalf("growth digest: %+v", cap.manifests[len(cap.manifests)-1].Artifacts[0])
	}
}

func TestLastSyncStampSurvivesAFailedRetry(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n"))
	state := t.TempDir()
	opt := allowAll(srv, home, state, "/tmp/p")
	if _, err := Sync(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	st, ok, err := ReadLastSync(state)
	if err != nil || !ok {
		t.Fatalf("stamp ok=%v err=%v", ok, err)
	}
	if st.Uploaded != 1 || st.Manifests != 1 || st.Server != srv.URL || st.At.IsZero() {
		t.Fatalf("stamp %+v", st)
	}
	info, err := os.Stat(LastSyncFile(state))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	leftover, err := filepath.Glob(filepath.Join(state, ".last-sync-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftover) != 0 {
		t.Fatalf("temp stamps left behind: %v", leftover)
	}

	// A lake error is not a finished sync. The previous stamp stays.
	opt.ServerURL = "http://127.0.0.1:1"
	opt.Client = nil
	if _, err := Sync(context.Background(), opt); err == nil {
		t.Fatal("expected lake failure")
	}
	again, ok, err := ReadLastSync(state)
	if err != nil || !ok {
		t.Fatalf("reread ok=%v err=%v", ok, err)
	}
	if !again.At.Equal(st.At) || again.Uploaded != 1 {
		t.Fatalf("stamp moved: %+v", again)
	}

	// A refusal finishes the pass, so the stamp records that run.
	opt.ServerURL = srv.URL
	opt.Client = srv.Client()
	opt.Projects = config.Projects{}
	if _, err := Sync(context.Background(), opt); err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("refuse: %v", err)
	}
	refused, ok, err := ReadLastSync(state)
	if err != nil || !ok || refused.Refused != 1 || refused.Uploaded != 0 {
		t.Fatalf("refusal stamp %+v ok=%v err=%v", refused, ok, err)
	}
}

func TestSyncRefusesNonAllowlisted(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n"))
	opt := Options{
		ServerURL: srv.URL,
		Token:     "tok",
		TervaHome: home,
		MachineID: "machine-1",
		StateDir:  t.TempDir(),
		Client:    cap.client,
	}
	_, err := Sync(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") || !strings.Contains(err.Error(), "/work/app") {
		t.Fatalf("err %v", err)
	}
	if cap.puts != 0 || blobCount(t, filepath.Join(data, "cas")) != 0 {
		t.Fatalf("refused sync uploaded puts=%d", cap.puts)
	}

	opt.Projects = config.Projects{
		Allow: []config.ProjectMatch{{CWDPrefix: "/work"}},
		Deny:  []config.ProjectMatch{{CWDPrefix: "/work/app"}},
	}
	cap.reset()
	_, err = Sync(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("deny: %v", err)
	}
	if cap.puts != 0 {
		t.Fatal("deny list uploaded")
	}
}

func TestSyncAllowsGitRemoteAndRefusesTheRest(t *testing.T) {
	repo := t.TempDir()
	gitDir := filepath.Join(repo, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[remote \"origin\"]\n\turl = git@github.com:terva-sh/lampi.git\n"
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	allowed := "{\"type\":\"meta\",\"meta\":{\"id\":\"ok\",\"cwd\":" + jsonString(repo) + "}}\n"
	writeSession(t, home, "aaaa", "ok.jsonl", []byte(allowed))
	writeSession(t, home, "bbbb", "no.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"no\",\"cwd\":\"/other\"}}\n"))

	opt := Options{
		ServerURL: srv.URL,
		Token:     "tok",
		TervaHome: home,
		MachineID: "machine-1",
		StateDir:  t.TempDir(),
		Client:    srv.Client(),
		Projects:  config.Projects{Allow: []config.ProjectMatch{{GitRemote: "https://github.com/terva-sh/lampi"}}},
	}
	res, err := Sync(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") || !strings.Contains(err.Error(), "/other") {
		t.Fatalf("err %v", err)
	}
	if res.Uploaded != 1 || res.Refused != 1 || res.Manifests != 1 {
		t.Fatalf("res %+v", res)
	}
	if blobCount(t, filepath.Join(data, "cas")) != 1 {
		t.Fatal("expected only the allowlisted blob")
	}
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestSyncQuarantineAndOverride(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	body := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/work/app\"}}\n" + secret + "\n")
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", body)
	state := t.TempDir()
	opt := allowAll(srv, home, state, "/work/app")
	opt.Client = cap.client

	res, err := Sync(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "quarantined") || !strings.Contains(err.Error(), "aws-access-key-id") {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error contains the secret: %v", err)
	}
	if res.Quarantined != 1 || res.Uploaded != 0 || cap.puts != 0 {
		t.Fatalf("res %+v puts %d", res, cap.puts)
	}
	if blobCount(t, filepath.Join(data, "cas")) != 0 {
		t.Fatal("quarantined bytes were stored")
	}
	q, err := os.ReadFile(filepath.Join(state, "quarantine.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(q), secret) {
		t.Fatalf("quarantine log contains the secret:\n%s", q)
	}

	opt.UploadHits = true
	cap.reset()
	res, err = Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || len(cap.manifests) != 1 {
		t.Fatalf("override: %+v manifests %d", res, len(cap.manifests))
	}
	red := cap.manifests[0].Artifacts[0].Redaction
	if red.Status != protocol.RedactionOverride || red.Ruleset != "v1" || red.Hits < 1 {
		t.Fatalf("override stamp: %+v", red)
	}
}

func TestSyncDoesNotCommitWithoutAck(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/manifests" {
			http.Error(w, `{"error":"no ack"}`, http.StatusInternalServerError)
			return
		}
		lake.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n"))
	opt := allowAll(srv, home, t.TempDir(), "/tmp/p")
	if _, err := Sync(context.Background(), opt); err == nil {
		t.Fatal("expected manifest failure")
	}
	wm, err := watermark.Open(watermark.File(opt.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	defer wm.Close()
	_, ok, err := wm.Get(context.Background(), watermark.Mark{
		MachineID: opt.MachineID,
		Harness:   protocol.HarnessTerva,
		Root:      home,
		RelPath:   "sessions/abcd/s.jsonl",
	})
	if err != nil || ok {
		t.Fatalf("watermark stored without ack: ok=%v err=%v", ok, err)
	}
	q, err := outbox.Open(outbox.File(opt.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	pending, err := q.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) == 0 {
		t.Fatal("failed sync acked the outbox")
	}
}

func openLake(t *testing.T) (*api.Server, string) {
	t.Helper()
	data := t.TempDir()
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Token = "tok"
	return lake, data
}

func allowAll(srv *httptest.Server, home, state, prefix string) Options {
	return Options{
		ServerURL: srv.URL,
		Token:     "tok",
		TervaHome: home,
		MachineID: "machine-1",
		StateDir:  state,
		Client:    srv.Client(),
		Projects:  config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: prefix}}},
	}
}

func writeSession(t *testing.T, home, bucket, name string, body []byte) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertFullFileWire(t *testing.T, manifests []protocol.Manifest) {
	t.Helper()
	if len(manifests) == 0 {
		t.Fatal("no manifest")
	}
	for _, m := range manifests {
		for _, a := range m.Artifacts {
			if a.ByteWatermarkPrev != 0 || a.TailSHA256 != a.SHA256 || a.SHA256 == "" {
				t.Fatalf("full-file wire fields: %+v", a)
			}
		}
	}
}

func assertRuleset(t *testing.T, manifests []protocol.Manifest) {
	t.Helper()
	if len(manifests) == 0 {
		t.Fatal("no manifest")
	}
	red := manifests[0].Artifacts[0].Redaction
	if red.Status != protocol.RedactionScanned || red.Ruleset != "v1" || red.Hits != 0 {
		t.Fatalf("redaction: %+v", red)
	}
}

func assertWatermark(t *testing.T, opt Options, rel string, size int64, sum string) {
	t.Helper()
	wm, err := watermark.Open(watermark.File(opt.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	defer wm.Close()
	got, ok, err := wm.Get(context.Background(), watermark.Mark{
		MachineID: opt.MachineID,
		Harness:   protocol.HarnessTerva,
		Root:      opt.TervaHome,
		RelPath:   rel,
	})
	if err != nil || !ok {
		t.Fatalf("watermark: ok=%v err=%v", ok, err)
	}
	if got.Offset != size || got.Size != size || got.SHA256 != sum {
		t.Fatalf("mark %+v", got)
	}
}

func assertOutboxEmpty(t *testing.T, state string) {
	t.Helper()
	q, err := outbox.Open(outbox.File(state))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	pending, err := q.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending: %+v", pending)
	}
}

func blobCount(t *testing.T, root string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

type capture struct {
	client    *http.Client
	base      http.RoundTripper
	mu        sync.Mutex
	puts      int
	manifests []protocol.Manifest
}

func wrapClient(c *http.Client) *capture {
	cap := &capture{client: c, base: c.Transport}
	c.Transport = cap
	return cap
}

func (c *capture) reset() {
	c.mu.Lock()
	c.puts = 0
	c.manifests = nil
	c.mu.Unlock()
}

func (c *capture) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil && (req.URL.Path == "/v1/manifests" || req.Method == http.MethodPut) {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(b))
		req.ContentLength = int64(len(b))
		if req.URL.Path == "/v1/manifests" {
			var m protocol.Manifest
			if err := json.Unmarshal(b, &m); err != nil {
				return nil, err
			}
			c.mu.Lock()
			c.manifests = append(c.manifests, m)
			c.mu.Unlock()
		}
		if req.Method == http.MethodPut {
			c.mu.Lock()
			c.puts++
			c.mu.Unlock()
		}
	}
	return c.base.RoundTrip(req)
}

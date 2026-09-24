package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
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
	"time"

	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watermark"

	_ "modernc.org/sqlite"
)

func TestSyncSidecarsFollowAllowlist(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()
	id := "11111111-2222-3333-4444-555555555555"

	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\""+id+"\",\"cwd\":\"/work/app\"}}\n"))
	mustFile(t, filepath.Join(home, "tasks", "tasks-"+id+".json"), `{"tasks":[{"title":"open"}]}`)
	mustFile(t, filepath.Join(home, "tasks", "tasks-orphan.json"), `{"tasks":[{"title":"nope"}]}`)
	mustFile(t, filepath.Join(home, "raati", "raati-10.json"), `{"units":[{"agent_id":"seat-1"}]}`)
	mustFile(t, filepath.Join(home, "swarm", "agents", "seat-1", "meta.json"), `{"session_id":"`+id+`","origin":"/work/app"}`)

	opt := allowAll(srv, home, t.TempDir(), "/work/app")
	res, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Manifests != 1 || res.Uploaded != 3 {
		t.Fatalf("sync: %+v", res)
	}
	_, arts, ok, err := lake.Catalog.Current(ctx, protocol.HarnessTerva, id)
	if err != nil || !ok {
		t.Fatalf("current %v %v", ok, err)
	}
	kinds := map[string]string{}
	var transcript string
	for _, a := range arts {
		kinds[a.RelPath] = a.SHA256
		if a.Kind == protocol.KindTranscriptJSONL {
			transcript = a.SHA256
		}
		if strings.Contains(a.RelPath, "orphan") {
			t.Fatalf("orphan uploaded: %s", a.RelPath)
		}
	}
	if kinds["tasks/tasks-"+id+".json"] == "" || kinds["raati/raati-10.json"] == "" || transcript == "" {
		t.Fatalf("artifacts: %+v", kinds)
	}

	rewritten := `{"tasks":[],"generations":[{"seq":1}]}`
	mustFile(t, filepath.Join(home, "tasks", "tasks-"+id+".json"), rewritten)
	again, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if again.Manifests != 1 {
		t.Fatalf("rewrite: %+v", again)
	}
	_, arts, ok, err = lake.Catalog.Current(ctx, protocol.HarnessTerva, id)
	if err != nil || !ok {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(rewritten))
	want := hex.EncodeToString(sum[:])
	for _, a := range arts {
		if a.Kind == protocol.KindTranscriptJSONL && a.SHA256 != transcript {
			t.Fatalf("transcript head moved to %s", a.SHA256)
		}
		if a.RelPath == "tasks/tasks-"+id+".json" && a.SHA256 != want {
			t.Fatalf("tasks current %s", a.SHA256)
		}
	}

	denied := t.TempDir()
	secret := "22222222-3333-4444-5555-666666666666"
	writeSession(t, denied, "zzzz", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\""+secret+"\",\"cwd\":\"/secret\"}}\n"))
	mustFile(t, filepath.Join(denied, "tasks", "tasks-"+secret+".json"), `{"tasks":[{"title":"hidden"}]}`)
	mustFile(t, filepath.Join(denied, "raati", "raati-11.json"), `{"units":[{"agent_id":"seat-2"}]}`)
	mustFile(t, filepath.Join(denied, "swarm", "agents", "seat-2", "meta.json"), `{"session_id":"`+secret+`","origin":"/secret"}`)
	opt.TervaHome = denied
	opt.StateDir = t.TempDir()
	if _, err := Sync(ctx, opt); err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("denied sync: %v", err)
	}
	if _, _, ok, err := lake.Catalog.Current(ctx, protocol.HarnessTerva, secret); err != nil || ok {
		t.Fatalf("denied session stored: %v %v", ok, err)
	}
}

func mustFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

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
	cap.reset()
	before := blobCount(t, filepath.Join(data, "cas"))
	third, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	grown, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tail := grown[len(body):]
	tailSum := sha256.Sum256(tail)
	grownSum := sha256.Sum256(grown)
	if third.Uploaded != 1 || third.Sessions[0] != first.Sessions[0] {
		t.Fatalf("third: %+v", third)
	}
	if cap.puts != 1 || len(cap.putBodies) != 1 || !bytes.Equal(cap.putBodies[0], tail) {
		t.Fatalf("tail put: puts=%d bodies=%d", cap.puts, len(cap.putBodies))
	}
	art := cap.manifests[len(cap.manifests)-1].Artifacts[0]
	if art.ByteWatermarkPrev != int64(len(body)) || art.TailSHA256 != hex.EncodeToString(tailSum[:]) || art.SHA256 != hex.EncodeToString(grownSum[:]) {
		t.Fatalf("tail wire: %+v", art)
	}
	// Previous head, the tail object, and the assembled full file.
	if blobCount(t, filepath.Join(data, "cas")) != before+2 {
		t.Fatalf("blobs %d, want %d", blobCount(t, filepath.Join(data, "cas")), before+2)
	}
	assertWatermark(t, opt, "sessions/abcd/s.jsonl", int64(len(grown)), hex.EncodeToString(grownSum[:]))

	cap.reset()
	after := blobCount(t, filepath.Join(data, "cas"))
	fourth, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.Uploaded != 0 || fourth.Missing != 0 || cap.puts != 0 || fourth.Sessions[0] != first.Sessions[0] {
		t.Fatalf("re-sync after tail: %+v puts=%d", fourth, cap.puts)
	}
	if blobCount(t, filepath.Join(data, "cas")) != after {
		t.Fatal("unchanged re-sync stored a blob")
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
	if strings.Contains(err.Error(), "workspace.json") || strings.Contains(err.Error(), "meta.json") || strings.Contains(err.Error(), "empty cwd") {
		t.Fatalf("cwd refusal hint on a session that has a cwd: %v", err)
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

func TestClockSkewWarns(t *testing.T) {
	lake, _ := openLake(t)
	serverNow := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	lake.Now = func() time.Time { return serverNow }
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n"))
	opt := allowAll(srv, home, t.TempDir(), "/tmp/p")
	opt.Now = func() time.Time { return serverNow.Add(10 * time.Minute) }
	res, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Warning, "clock skew") || !strings.Contains(res.Warning, "server_time") {
		t.Fatalf("warning %q", res.Warning)
	}
	if res.Uploaded != 1 {
		t.Fatalf("skew still uploads: %+v", res)
	}

	opt.Now = func() time.Time { return serverNow.Add(time.Minute) }
	res, err = Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Warning != "" {
		t.Fatalf("within five minutes: %q", res.Warning)
	}

	opt.Now = func() time.Time { return serverNow.Add(-10 * time.Minute) }
	res, err = Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Warning, "clock skew") {
		t.Fatalf("client behind: %q", res.Warning)
	}
}

func TestSyncDivergentCopy(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	home := t.TempDir()
	path := writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n"))
	opt := allowAll(srv, home, t.TempDir(), "/tmp/p")
	first, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	// Same native id and cwd, but not a prefix of the stored head.
	if err := os.WriteFile(path, []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\",\"x\":1}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if second.Uploaded != 1 || second.Sessions[0] != first.Sessions[0] {
		t.Fatalf("second: %+v", second)
	}
	arts, err := lake.Catalog.Artifacts(context.Background(), first.Sessions[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 || arts[1].Relation != protocol.RelationDivergentCopy || arts[1].Current || !arts[0].Current {
		t.Fatalf("artifacts: %+v", arts)
	}
	uid, current, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessTerva, "s")
	if err != nil || !ok || uid != first.Sessions[0] || len(current) != 1 || current[0].SHA256 != arts[0].SHA256 {
		t.Fatalf("head moved: uid=%s ok=%v err=%v current=%+v", uid, ok, err, current)
	}
	if blobCount(t, filepath.Join(data, "cas")) != 2 {
		t.Fatal("divergent copies were merged into one blob")
	}
}

func TestSecondMachineSameBytes(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n"))
	optA := allowAll(srv, home, t.TempDir(), "/tmp/p")
	first, err := Sync(context.Background(), optA)
	if err != nil {
		t.Fatal(err)
	}
	optB := allowAll(srv, home, t.TempDir(), "/tmp/p")
	optB.MachineID = "machine-2"
	second, err := Sync(context.Background(), optB)
	if err != nil {
		t.Fatal(err)
	}
	if second.Uploaded != 0 || second.Missing != 0 || second.Sessions[0] != first.Sessions[0] {
		t.Fatalf("second machine: %+v", second)
	}
	if blobCount(t, filepath.Join(data, "cas")) != 1 {
		t.Fatal("same bytes stored a second blob")
	}
	ctx := context.Background()
	for _, id := range []string{optA.MachineID, optB.MachineID} {
		uid, ok, err := lake.Catalog.Alias(ctx, protocol.HarnessTerva, "s", id)
		if err != nil || !ok || uid != first.Sessions[0] {
			t.Fatalf("alias %s: %q ok=%v err=%v", id, uid, ok, err)
		}
	}
	prov, err := lake.Catalog.Provenance(ctx, first.Sessions[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(prov) != 2 {
		t.Fatalf("provenance: %+v", prov)
	}
}

func TestStaleClientAdvancesWatermark(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	full := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n{\"type\":\"message\"}\n")
	prefix := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n")
	homeA := t.TempDir()
	homeB := t.TempDir()
	writeSession(t, homeA, "abcd", "s.jsonl", full)
	writeSession(t, homeB, "abcd", "s.jsonl", prefix)
	optA := allowAll(srv, homeA, t.TempDir(), "/tmp/p")
	first, err := Sync(context.Background(), optA)
	if err != nil {
		t.Fatal(err)
	}
	optB := allowAll(srv, homeB, t.TempDir(), "/tmp/p")
	optB.MachineID = "machine-2"
	second, err := Sync(context.Background(), optB)
	if err != nil {
		t.Fatal(err)
	}
	if second.Sessions[0] != first.Sessions[0] {
		t.Fatalf("stale session: %+v", second)
	}
	fullSum := sha256.Sum256(full)
	prefixSum := sha256.Sum256(prefix)
	wm, err := watermark.Open(watermark.File(optB.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	mark, ok, err := wm.Get(context.Background(), watermark.Mark{
		MachineID: optB.MachineID,
		Harness:   protocol.HarnessTerva,
		Root:      optB.TervaHome,
		RelPath:   "sessions/abcd/s.jsonl",
	})
	wm.Close()
	if err != nil || !ok {
		t.Fatalf("watermark: ok=%v err=%v", ok, err)
	}
	if mark.Size != int64(len(prefix)) || mark.SHA256 != hex.EncodeToString(prefixSum[:]) || mark.Offset != int64(len(full)) {
		t.Fatalf("stale mark %+v", mark)
	}
	arts, err := lake.Catalog.Artifacts(context.Background(), first.Sessions[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 || arts[0].SHA256 != hex.EncodeToString(fullSum[:]) {
		t.Fatalf("stale stored a second head: %+v", arts)
	}

	cap := wrapClient(srv.Client())
	optB.Client = cap.client
	third, err := Sync(context.Background(), optB)
	if err != nil {
		t.Fatal(err)
	}
	if third.Manifests != 0 || len(third.Sessions) != 0 || len(cap.manifests) != 0 || cap.puts != 0 {
		t.Fatalf("stale re-sync posted again: %+v manifests=%d puts=%d", third, len(cap.manifests), cap.puts)
	}
}

func TestTailMismatchFallsBackToFullFile(t *testing.T) {
	lake, _ := openLake(t)
	var rejected int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/manifests" && r.Body != nil {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var m protocol.Manifest
			if json.Unmarshal(b, &m) == nil && len(m.Artifacts) > 0 && m.Artifacts[0].ByteWatermarkPrev > 0 {
				rejected++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"prefix mismatch"}`))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
			r.ContentLength = int64(len(b))
		}
		lake.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())
	home := t.TempDir()
	body := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n")
	path := writeSession(t, home, "abcd", "s.jsonl", body)
	opt := allowAll(srv, home, t.TempDir(), "/tmp/p")
	opt.Client = cap.client
	if _, err := Sync(context.Background(), opt); err != nil {
		t.Fatal(err)
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
	res, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if rejected != 1 || res.Uploaded != 2 {
		t.Fatalf("fallback uploaded=%d rejected=%d res=%+v", res.Uploaded, rejected, res)
	}
	grown, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sawTail := false
	sawFull := false
	for _, b := range cap.putBodies {
		if bytes.Equal(b, grown[len(body):]) {
			sawTail = true
		}
		if bytes.Equal(b, grown) {
			sawFull = true
		}
	}
	if !sawTail || !sawFull {
		t.Fatalf("puts tail=%v full=%v lens=%d", sawTail, sawFull, len(cap.putBodies))
	}
	sum := sha256.Sum256(grown)
	uid, current, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessTerva, "s")
	if err != nil || !ok || uid != res.Sessions[0] || len(current) != 1 || current[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("head: uid=%s ok=%v err=%v current=%+v", uid, ok, err, current)
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
	lake.Allow("tok")
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

func TestBundlesSkipEmptyTervaHome(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.MkdirAll(filepath.Join("sessions", "abcd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("sessions", "abcd", "s.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundles, err := bundlesFor(Options{MachineID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 0 {
		t.Fatalf("empty terva home produced %d bundles", len(bundles))
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
	putBodies [][]byte
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
	c.putBodies = nil
	c.manifests = nil
	c.mu.Unlock()
}

func TestSyncResumesByContentRange(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	body := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"range\",\"cwd\":\"/tmp/p\"}}\n{\"type\":\"message\",\"text\":\"abcdefghijklmnopqrstuvwxyz012345\"}\n")
	writeSession(t, home, "abcd", "range.jsonl", body)
	opt := allowAll(srv, home, t.TempDir(), "/tmp/p")
	opt.Client = cap.client
	opt.PieceBytes = 10

	res, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || res.Manifests != 1 || cap.puts < 2 {
		t.Fatalf("resume %+v puts=%d", res, cap.puts)
	}
	var got []byte
	for _, b := range cap.putBodies {
		if len(b) > 10 {
			t.Fatalf("piece longer than the range: %d", len(b))
		}
		got = append(got, b...)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("ranges %q", got)
	}
	sum := sha256.Sum256(body)
	stored, err := lake.CAS.Read(hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, body) {
		t.Fatalf("assembled %q", stored)
	}
	if blobCount(t, filepath.Join(data, "cas")) != 1 {
		t.Fatalf("partial left behind, blobs %d", blobCount(t, filepath.Join(data, "cas")))
	}

	cap.reset()
	again, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if again.Uploaded != 0 || again.Missing != 0 || cap.puts != 0 {
		t.Fatalf("second range sync %+v puts=%d", again, cap.puts)
	}
}

func TestSyncSplitsFilePastBlobCap(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	path := writeSizedSession(t, home, "abcd", "big.jsonl", protocol.MaxBlobBytes)
	opt := allowAll(srv, home, t.TempDir(), "/tmp/p")
	opt.Client = cap.client
	ctx := context.Background()

	first, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if first.Uploaded != 1 || first.Manifests != 1 || len(cap.manifests) != 1 {
		t.Fatalf("first %+v manifests %d", first, len(cap.manifests))
	}
	art := cap.manifests[0].Artifacts[0]
	if len(art.ChunkSHA256s) != 0 || art.Size != protocol.MaxBlobBytes {
		t.Fatalf("at-cap artifact %+v", art)
	}
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(orig)
	origSHA := hex.EncodeToString(sum[:])
	if art.SHA256 != origSHA {
		t.Fatalf("sha %s", art.SHA256)
	}
	if ok, err := lake.CAS.Has(origSHA); err != nil || !ok {
		t.Fatalf("at-cap blob has %v %v", ok, err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{'b'}); err != nil {
		t.Fatal(err)
	}
	f.Close()
	grown, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	grownSum := sha256.Sum256(grown)
	grownSHA := hex.EncodeToString(grownSum[:])
	extraSum := sha256.Sum256([]byte{'b'})
	extraSHA := hex.EncodeToString(extraSum[:])

	cap.reset()
	second, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if second.Uploaded != 1 || second.Manifests != 1 || cap.puts != 1 {
		t.Fatalf("split %+v puts=%d", second, cap.puts)
	}
	if len(cap.putBodies) != 1 || !bytes.Equal(cap.putBodies[0], []byte{'b'}) {
		t.Fatalf("split put %d bytes", len(cap.putBodies))
	}
	if len(cap.manifests) != 1 || len(cap.manifests[0].Artifacts) != 1 {
		t.Fatalf("manifests %+v", cap.manifests)
	}
	art = cap.manifests[0].Artifacts[0]
	if art.ByteWatermarkPrev != 0 || art.SHA256 != grownSHA || art.Size != protocol.MaxBlobBytes+1 {
		t.Fatalf("logical wire %+v", art)
	}
	if len(art.ChunkSHA256s) != 2 || len(art.ChunkLengths) != 2 {
		t.Fatalf("chunks %+v", art)
	}
	if art.ChunkSHA256s[0] != origSHA || art.ChunkSHA256s[1] != extraSHA {
		t.Fatalf("chunk digests %+v", art.ChunkSHA256s)
	}
	if art.ChunkLengths[0] != protocol.MaxBlobBytes || art.ChunkLengths[1] != 1 {
		t.Fatalf("chunk lengths %+v", art.ChunkLengths)
	}
	if ok, err := lake.CAS.Has(grownSHA); err != nil || ok {
		t.Fatalf("assembled object installed: has %v %v", ok, err)
	}
	if ok, err := lake.CAS.Has(origSHA); err != nil || !ok {
		t.Fatalf("prefix chunk missing: %v %v", ok, err)
	}
	if ok, err := lake.CAS.Has(extraSHA); err != nil || !ok {
		t.Fatalf("tail chunk missing: %v %v", ok, err)
	}
	got, err := lake.CAS.Read(grownSHA)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, grown) {
		t.Fatalf("logical read len %d want %d", len(got), len(grown))
	}

	cap.reset()
	third, err := Sync(ctx, opt)
	if err != nil {
		t.Fatal(err)
	}
	if third.Uploaded != 0 || third.Missing != 0 || cap.puts != 0 {
		t.Fatalf("re-sync %+v puts=%d", third, cap.puts)
	}
}

func writeSizedSession(t *testing.T, home, bucket, name string, size int64) string {
	t.Helper()
	meta := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"big\",\"cwd\":\"/tmp/p\"}}\n")
	if int64(len(meta)) >= size {
		t.Fatalf("meta is %d bytes, size is %d", len(meta), size)
	}
	dir := filepath.Join(home, "sessions", bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(meta); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1<<20)
	for i := range buf {
		buf[i] = 'a'
	}
	left := size - int64(len(meta))
	for left > 0 {
		n := int64(len(buf))
		if n > left {
			n = left
		}
		if _, err := f.Write(buf[:n]); err != nil {
			t.Fatal(err)
		}
		left -= n
	}
	return path
}

func TestSyncAssemblesChunkDigests(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	body := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"chunks\",\"cwd\":\"/tmp/p\"}}\n{\"type\":\"message\",\"text\":\"abcdefghijklmnopqrstuvwxyz012345\"}\n")
	writeSession(t, home, "abcd", "chunks.jsonl", body)
	opt := allowAll(srv, home, t.TempDir(), "/tmp/p")
	opt.Client = cap.client
	opt.ChunkBytes = 16

	res, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || res.Manifests != 1 {
		t.Fatalf("chunks %+v", res)
	}
	if len(cap.manifests) != 1 || len(cap.manifests[0].Artifacts) != 1 {
		t.Fatalf("manifests %+v", cap.manifests)
	}
	art := cap.manifests[0].Artifacts[0]
	if len(art.ChunkSHA256s) < 2 {
		t.Fatalf("chunk list %+v", art.ChunkSHA256s)
	}
	sum := sha256.Sum256(body)
	if art.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha %s", art.SHA256)
	}
	stored, err := lake.CAS.Read(art.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, body) {
		t.Fatalf("assembled %q", stored)
	}
	// Each chunk is its own object, plus the assembled digest.
	if blobCount(t, filepath.Join(data, "cas")) != len(art.ChunkSHA256s)+1 {
		t.Fatalf("blobs %d chunks %d", blobCount(t, filepath.Join(data, "cas")), len(art.ChunkSHA256s))
	}

	before := blobCount(t, filepath.Join(data, "cas"))
	cap.reset()
	again, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if again.Uploaded != 0 || again.Missing != 0 || cap.puts != 0 {
		t.Fatalf("second chunk sync %+v puts=%d", again, cap.puts)
	}
	if blobCount(t, filepath.Join(data, "cas")) != before {
		t.Fatal("re-sync stored another blob")
	}
}

func TestSyncClaudeAndCodex(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	tervaHome := t.TempDir()
	claudeHome := t.TempDir()
	codexHome := t.TempDir()
	claudeBody := []byte("{\"type\":\"user\",\"sessionId\":\"sid-1\",\"cwd\":\"/work/app\",\"future_field\":{\"n\":1}}\n")
	claudePath := filepath.Join(claudeHome, "projects", "-work-app", "sid-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, claudeBody, 0o644); err != nil {
		t.Fatal(err)
	}
	day := filepath.Join(codexHome, "sessions", "2026", "09", "23")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	codexBody := []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"thread-1\",\"cwd\":\"/work/app\",\"future\":true}}\n")
	if err := os.WriteFile(filepath.Join(day, "rollout-2026-09-23T12-00-00-thread-1.jsonl"), codexBody, 0o644); err != nil {
		t.Fatal(err)
	}
	history := []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"hist\",\"cwd\":\"/work/app\"}}\n")
	if err := os.WriteFile(filepath.Join(day, "history.jsonl"), history, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "history.jsonl"), history, 0o644); err != nil {
		t.Fatal(err)
	}

	opt := allowAll(srv, tervaHome, t.TempDir(), "/work/app")
	opt.Client = cap.client
	opt.ClaudeHome = claudeHome
	opt.CodexHome = codexHome
	res, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 2 || res.Manifests != 2 || res.Refused != 0 {
		t.Fatalf("sync %+v", res)
	}
	saw := map[string]string{}
	for _, m := range cap.manifests {
		saw[m.Harness] = m.HarnessVersion
		if m.NativeSessionID == "hist" {
			t.Fatal("history.jsonl was ingested as a rollout")
		}
	}
	if saw[protocol.HarnessClaude] != "1" || saw[protocol.HarnessCodex] != "1" {
		t.Fatalf("harness versions %v", saw)
	}
	if blobCount(t, filepath.Join(data, "cas")) != 2 {
		t.Fatal("expected one blob per harness file")
	}
	uid, arts, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessClaude, "sid-1")
	if err != nil || !ok || uid == "" || len(arts) != 1 {
		t.Fatalf("claude catalog ok=%v err=%v arts=%d", ok, err, len(arts))
	}
	raw, err := lake.CAS.Read(arts[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"future_field"`)) {
		t.Fatalf("stored bytes dropped an unknown field: %s", raw)
	}
	if _, _, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessCodex, "hist"); err != nil || ok {
		t.Fatalf("history session ok=%v err=%v", ok, err)
	}

	cap.reset()
	again, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if again.Uploaded != 0 || again.Missing != 0 || cap.puts != 0 {
		t.Fatalf("re-sync %+v puts=%d", again, cap.puts)
	}
}

func TestSyncOpenCodeExport(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	body := []byte("{\"info\":{\"id\":\"ses_1\",\"directory\":\"/work/app\",\"future_field\":true},\"messages\":[]}\n")
	path := filepath.Join(home, "export", "ses_1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "opencode.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "opencode.db-wal"), []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}

	opt := allowAll(srv, t.TempDir(), t.TempDir(), "/work/app")
	opt.Client = cap.client
	opt.OpenCodeHome = home
	res, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || res.Manifests != 1 || res.Refused != 0 {
		t.Fatalf("sync %+v", res)
	}
	if len(cap.manifests) != 1 {
		t.Fatalf("manifests %d", len(cap.manifests))
	}
	m := cap.manifests[0]
	if m.Harness != protocol.HarnessOpenCode || m.HarnessVersion != "1" || m.NativeSessionID != "ses_1" {
		t.Fatalf("header %+v", m)
	}
	if m.Project.CWD != "/work/app" {
		t.Fatalf("cwd %q", m.Project.CWD)
	}
	for _, a := range m.Artifacts {
		if a.RelPath == "opencode.db" || a.RelPath == "opencode.db-wal" {
			t.Fatalf("read %s", a.RelPath)
		}
	}
	if blobCount(t, filepath.Join(data, "cas")) != 1 {
		t.Fatal("expected the export blob only")
	}
	uid, arts, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessOpenCode, "ses_1")
	if err != nil || !ok || uid == "" || len(arts) != 1 {
		t.Fatalf("catalog ok=%v err=%v arts=%d", ok, err, len(arts))
	}
	raw, err := lake.CAS.Read(arts[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"future_field"`)) {
		t.Fatalf("stored bytes dropped an unknown field: %s", raw)
	}

	cap.reset()
	again, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if again.Uploaded != 0 || again.Missing != 0 || cap.puts != 0 {
		t.Fatalf("re-sync %+v puts=%d", again, cap.puts)
	}
}

func TestSyncOpenCodeDatabaseStaysLocal(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "opencode.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "opencode.db-wal"), []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}

	opt := allowAll(srv, t.TempDir(), t.TempDir(), "/work/app")
	opt.OpenCodeHome = home
	res, err := Sync(context.Background(), opt)
	if err == nil {
		t.Fatal("database with no cwd was uploaded")
	}
	if res.Uploaded != 0 || res.Refused != 1 {
		t.Fatalf("sync %+v err=%v", res, err)
	}
	if !strings.Contains(err.Error(), "opencode.db") || strings.Contains(err.Error(), "opencode.db-wal") {
		t.Fatalf("refusal: %v", err)
	}
	if blobCount(t, filepath.Join(data, "cas")) != 0 {
		t.Fatal("database or wal bytes left the machine")
	}
	if _, _, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessOpenCode, "opencode.db"); err != nil || ok {
		t.Fatalf("catalog session ok=%v err=%v", ok, err)
	}
}

func TestCursorEmptyCWDHint(t *testing.T) {
	cases := []struct {
		name string
		m    protocol.Manifest
		want string
	}{
		{
			name: "global",
			m:    protocol.Manifest{Harness: protocol.HarnessCursor, NativeSessionID: "global"},
			want: "empty cwd",
		},
		{
			name: "workspace",
			m:    protocol.Manifest{Harness: protocol.HarnessCursor, NativeSessionID: "workspace/ws1"},
			want: "workspace.json",
		},
		{
			name: "cli",
			m:    protocol.Manifest{Harness: protocol.HarnessCursorCLI, NativeSessionID: "chats/ab/sid"},
			want: "meta.json",
		},
		{
			name: "cli with cwd",
			m:    protocol.Manifest{Harness: protocol.HarnessCursorCLI, Project: protocol.Project{CWD: "/work/app"}},
			want: "",
		},
		{
			name: "other harness",
			m:    protocol.Manifest{Harness: protocol.HarnessOpenCode},
			want: "",
		},
	}
	for _, tc := range cases {
		got := cursorEmptyCWDHint(tc.m)
		if tc.want == "" {
			if got != "" {
				t.Fatalf("%s: %q", tc.name, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%s: %q", tc.name, got)
		}
		if tc.name == "global" && (!strings.Contains(got, "refused by design") || !strings.Contains(got, "workspace.json")) {
			t.Fatalf("global: %q", got)
		}
		if tc.name == "cli" && !strings.Contains(got, "absolute cwd") {
			t.Fatalf("cli: %q", got)
		}
	}
	line := allowlistRefusal(protocol.Manifest{
		Harness:         protocol.HarnessCursor,
		NativeSessionID: "global",
		Artifacts:       []protocol.Artifact{{RelPath: "User/globalStorage/state.json"}},
	})
	if !strings.Contains(line, "not allowlisted") || !strings.Contains(line, "refused by design") {
		t.Fatalf("refusal line: %s", line)
	}
}

func TestSyncCursorExportFiltersAuth(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	global := filepath.Join(home, "User", "globalStorage", "state.vscdb")
	ws := filepath.Join(home, "User", "workspaceStorage", "ws1", "state.vscdb")
	if err := writeCursorDB(global, "hello from cursor"); err != nil {
		t.Fatal(err)
	}
	if err := writeCursorDB(ws, "hello from cursor"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(ws), "workspace.json"), []byte(`{"folder":"file:///work/app"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	opt := allowAll(srv, t.TempDir(), t.TempDir(), "/work/app")
	opt.Client = cap.client
	opt.CursorHome = home
	res, err := Sync(context.Background(), opt)
	if err == nil || res.Uploaded != 1 || res.Manifests != 1 || res.Refused != 1 {
		t.Fatalf("sync %+v err=%v", res, err)
	}
	if !strings.Contains(err.Error(), "User/globalStorage/state.json") {
		t.Fatalf("global export was not refused: %v", err)
	}
	if !strings.Contains(err.Error(), "empty cwd") || !strings.Contains(err.Error(), "refused by design") || !strings.Contains(err.Error(), "workspace.json") {
		t.Fatalf("global refusal hint: %v", err)
	}
	if strings.Contains(err.Error(), "User/workspaceStorage/ws1/state.json") {
		t.Fatalf("allowlisted workspace was named as refused: %v", err)
	}
	if strings.Contains(err.Error(), "sekret-token") || strings.Contains(err.Error(), "state.vscdb-wal") {
		t.Fatalf("refusal leaked a secret or a sidecar: %v", err)
	}
	if len(cap.manifests) != 1 {
		t.Fatalf("manifests %d", len(cap.manifests))
	}
	m := cap.manifests[0]
	if m.Harness != protocol.HarnessCursor || m.HarnessVersion != cursor.Version || m.NativeSessionID != "workspace/ws1" {
		t.Fatalf("header %+v", m)
	}
	if m.Project.CWD != "/work/app" {
		t.Fatalf("cwd %q", m.Project.CWD)
	}
	if m.Artifacts[0].Kind != protocol.KindCursorStateJSON {
		t.Fatalf("kind %s", m.Artifacts[0].Kind)
	}
	if m.Artifacts[0].RelPath != "User/workspaceStorage/ws1/state.json" {
		t.Fatalf("rel %s", m.Artifacts[0].RelPath)
	}
	for _, body := range cap.putBodies {
		if bytes.Contains(body, []byte("sekret-token")) || bytes.Contains(body, []byte("cursorAuth")) || bytes.HasPrefix(body, []byte("SQLite format 3")) {
			t.Fatalf("uploaded raw or auth bytes: %s", body)
		}
	}
	if blobCount(t, filepath.Join(data, "cas")) != 1 {
		t.Fatal("expected the filtered export only")
	}
	uid, arts, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessCursor, "workspace/ws1")
	if err != nil || !ok || uid == "" || len(arts) != 1 {
		t.Fatalf("catalog ok=%v err=%v arts=%d", ok, err, len(arts))
	}
	raw, err := lake.CAS.Read(arts[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("hello from cursor")) || bytes.Contains(raw, []byte("sekret-token")) {
		t.Fatalf("stored export: %s", raw)
	}
	if _, _, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessCursor, "global"); err != nil || ok {
		t.Fatalf("global session ok=%v err=%v", ok, err)
	}

	if err := writeCursorDB(ws, "hello from cursor again"); err != nil {
		t.Fatal(err)
	}
	cap.reset()
	again, err := Sync(context.Background(), opt)
	if err == nil || again.Uploaded != 1 || again.Manifests != 1 {
		t.Fatalf("rewrite %+v err=%v", again, err)
	}
	_, arts, ok, err = lake.Catalog.Current(context.Background(), protocol.HarnessCursor, "workspace/ws1")
	if err != nil || !ok || len(arts) != 1 {
		t.Fatalf("rewritten catalog ok=%v err=%v arts=%d", ok, err, len(arts))
	}
	raw, err = lake.CAS.Read(arts[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("hello from cursor again")) || bytes.Contains(raw, []byte("sekret-token")) {
		t.Fatalf("rewritten export: %s", raw)
	}
}

func TestSyncCursorCLIIsASeparateCorpus(t *testing.T) {
	lake, data := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	cap := wrapClient(srv.Client())

	home := t.TempDir()
	global := filepath.Join(home, "User", "globalStorage", "state.vscdb")
	ws := filepath.Join(home, "User", "workspaceStorage", "ws1", "state.vscdb")
	if err := writeCursorDB(global, "hello from cursor"); err != nil {
		t.Fatal(err)
	}
	if err := writeCursorDB(ws, "hello from cursor"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(ws), "workspace.json"), []byte(`{"folder":"file:///work/app"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(home, "chats", "ab12", "sid-1", "store.db")
	if err := writeCursorCLIStore(cli, "hello from cli"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(cli), "meta.json"), []byte(`{"cwd":"/work/app","schemaVersion":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	noCWD := filepath.Join(home, "chats", "ab12", "no-cwd", "store.db")
	if err := writeCursorCLIStore(noCWD, "stay local"); err != nil {
		t.Fatal(err)
	}

	opt := allowAll(srv, t.TempDir(), t.TempDir(), "/work/app")
	opt.Client = cap.client
	opt.CursorHome = home
	opt.CursorCLIHome = home
	res, err := Sync(context.Background(), opt)
	if err == nil || res.Uploaded != 2 || res.Manifests != 2 || res.Refused != 2 {
		t.Fatalf("sync %+v err=%v", res, err)
	}
	if !strings.Contains(err.Error(), "User/globalStorage/state.json") || !strings.Contains(err.Error(), "chats/ab12/no-cwd/store.json") {
		t.Fatalf("refusal: %v", err)
	}
	if !strings.Contains(err.Error(), "refused by design") || !strings.Contains(err.Error(), "workspace.json") {
		t.Fatalf("global refusal hint: %v", err)
	}
	if !strings.Contains(err.Error(), "absolute cwd") || !strings.Contains(err.Error(), "meta.json") {
		t.Fatalf("cli refusal hint: %v", err)
	}
	if strings.Contains(err.Error(), "chats/ab12/sid-1/store.json") {
		t.Fatalf("allowlisted cli chat was named as refused: %v", err)
	}
	if strings.Contains(err.Error(), "sekret-token") || strings.Contains(err.Error(), "store.db-wal") || strings.Contains(err.Error(), "state.vscdb-wal") {
		t.Fatalf("refusal leaked a secret or a sidecar: %v", err)
	}
	if len(cap.manifests) != 2 {
		t.Fatalf("manifests %d", len(cap.manifests))
	}
	byHarness := map[string]protocol.Manifest{}
	for _, m := range cap.manifests {
		byHarness[m.Harness] = m
	}
	ide := byHarness[protocol.HarnessCursor]
	if ide.HarnessVersion != cursor.Version || ide.NativeSessionID != "workspace/ws1" || ide.Project.CWD != "/work/app" {
		t.Fatalf("ide %+v", ide)
	}
	if ide.Artifacts[0].Kind != protocol.KindCursorStateJSON {
		t.Fatalf("ide kind %s", ide.Artifacts[0].Kind)
	}
	cliM := byHarness[protocol.HarnessCursorCLI]
	if cliM.HarnessVersion != "1" || cliM.NativeSessionID != "chats/ab12/sid-1" || cliM.Project.CWD != "/work/app" {
		t.Fatalf("cli %+v", cliM)
	}
	if cliM.Artifacts[0].Kind != protocol.KindCursorCLIStoreJSON || cliM.Artifacts[0].RelPath != "chats/ab12/sid-1/store.json" {
		t.Fatalf("cli artifact %+v", cliM.Artifacts)
	}
	for _, body := range cap.putBodies {
		if bytes.Contains(body, []byte("sekret-token")) || bytes.Contains(body, []byte("cursorAuth")) || bytes.HasPrefix(body, []byte("SQLite format 3")) {
			t.Fatalf("uploaded raw or auth bytes: %s", body)
		}
	}
	if blobCount(t, filepath.Join(data, "cas")) != 2 {
		t.Fatal("expected two filtered exports")
	}
	ctx := context.Background()
	ideUID, ideArts, ok, err := lake.Catalog.Current(ctx, protocol.HarnessCursor, "workspace/ws1")
	if err != nil || !ok || ideUID == "" || len(ideArts) != 1 {
		t.Fatalf("ide catalog ok=%v err=%v arts=%d", ok, err, len(ideArts))
	}
	cliUID, cliArts, ok, err := lake.Catalog.Current(ctx, protocol.HarnessCursorCLI, "chats/ab12/sid-1")
	if err != nil || !ok || cliUID == "" || cliUID == ideUID || len(cliArts) != 1 {
		t.Fatalf("cli catalog uid=%s ide=%s ok=%v err=%v arts=%d", cliUID, ideUID, ok, err, len(cliArts))
	}
	if _, _, ok, err := lake.Catalog.Current(ctx, protocol.HarnessCursor, "chats/ab12/sid-1"); err != nil || ok {
		t.Fatalf("cli session visible as ide ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := lake.Catalog.Current(ctx, protocol.HarnessCursorCLI, "workspace/ws1"); err != nil || ok {
		t.Fatalf("ide session visible as cli ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := lake.Catalog.Current(ctx, protocol.HarnessCursorCLI, "chats/ab12/no-cwd"); err != nil || ok {
		t.Fatalf("cwd-less cli session uploaded ok=%v err=%v", ok, err)
	}
	cliRaw, err := lake.CAS.Read(cliArts[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(cliRaw, []byte("hello from cli")) || bytes.Contains(cliRaw, []byte("sekret-token")) {
		t.Fatalf("stored cli export: %s", cliRaw)
	}

	wm, err := watermark.Open(watermark.File(opt.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	defer wm.Close()
	ideRel := "User/workspaceStorage/ws1/state.json"
	cliRel := "chats/ab12/sid-1/store.json"
	if _, ok, err := wm.Get(ctx, watermark.Mark{MachineID: opt.MachineID, Harness: protocol.HarnessCursor, Root: home, RelPath: ideRel}); err != nil || !ok {
		t.Fatalf("ide watermark ok=%v err=%v", ok, err)
	}
	if _, ok, err := wm.Get(ctx, watermark.Mark{MachineID: opt.MachineID, Harness: protocol.HarnessCursorCLI, Root: home, RelPath: cliRel}); err != nil || !ok {
		t.Fatalf("cli watermark ok=%v err=%v", ok, err)
	}
	if _, ok, err := wm.Get(ctx, watermark.Mark{MachineID: opt.MachineID, Harness: protocol.HarnessCursor, Root: home, RelPath: cliRel}); err != nil || ok {
		t.Fatalf("cli path stored on the ide harness ok=%v err=%v", ok, err)
	}
	if _, ok, err := wm.Get(ctx, watermark.Mark{MachineID: opt.MachineID, Harness: protocol.HarnessCursorCLI, Root: home, RelPath: ideRel}); err != nil || ok {
		t.Fatalf("ide path stored on the cli harness ok=%v err=%v", ok, err)
	}

	if err := writeCursorCLIStore(cli, "hello from cli again"); err != nil {
		t.Fatal(err)
	}
	cap.reset()
	again, err := Sync(context.Background(), opt)
	if err == nil || again.Uploaded != 1 || again.Manifests != 2 || again.Refused != 2 {
		t.Fatalf("rewrite %+v err=%v", again, err)
	}
	_, ideArts, ok, err = lake.Catalog.Current(ctx, protocol.HarnessCursor, "workspace/ws1")
	if err != nil || !ok || len(ideArts) != 1 {
		t.Fatalf("ide after cli rewrite ok=%v err=%v", ok, err)
	}
	ideRaw, err := lake.CAS.Read(ideArts[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(ideRaw, []byte("hello from cursor")) || bytes.Contains(ideRaw, []byte("hello from cli again")) {
		t.Fatalf("ide export changed with the cli store: %s", ideRaw)
	}
	_, cliArts, ok, err = lake.Catalog.Current(ctx, protocol.HarnessCursorCLI, "chats/ab12/sid-1")
	if err != nil || !ok || len(cliArts) != 1 {
		t.Fatalf("cli rewrite ok=%v err=%v arts=%d", ok, err, len(cliArts))
	}
	cliRaw, err = lake.CAS.Read(cliArts[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(cliRaw, []byte("hello from cli again")) || bytes.Contains(cliRaw, []byte("sekret-token")) {
		t.Fatalf("rewritten cli export: %s", cliRaw)
	}
}

func writeCursorDB(path, note string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_ = os.Remove(path)
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES ('cursorAuth/accessToken', 'sekret-token'), ('composer.composerData', ?)`, `{"note":"`+note+`"}`); err != nil {
		return err
	}
	_, err = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func writeCursorCLIStore(path, note string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_ = os.Remove(path)
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		return err
	}
	meta := `{"name":"kept-title","accessToken":"sekret-token"}`
	msg := `{"role":"user","content":"` + note + `","accessToken":"sekret-token"}`
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('0', ?), ('cursorAuth/accessToken', 'sekret-token')`, hex.EncodeToString([]byte(meta))); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES ('blob-1', ?)`, msg); err != nil {
		return err
	}
	_, err = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
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
			c.putBodies = append(c.putBodies, append([]byte(nil), b...))
			c.mu.Unlock()
		}
	}
	return c.base.RoundTrip(req)
}

package accept

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
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
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/cli"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"

	_ "modernc.org/sqlite"
)

//go:embed testdata/session.jsonl
var fixtureSession []byte

// knownPrompt is the user text in testdata/session.jsonl. The export
// proof in internal/cli and internal/normalize uses the same string.
const knownPrompt = "normalize-proof prompt: lampi-pond-7f3a"

const (
	nativeID = "20260922-161000-abcd1234"
	machineA = "machine-a"
	machineB = "machine-b"
	cwd      = "/home/drew/src/foo"
	relPath  = "sessions/a1b2c3d4e5f60708/20260922-161000-abcd1234.jsonl"
)

// tailLine is one append-only JSONL record. It does not contain knownPrompt.
const tailLine = `{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"tail-only pond"}],"time":"2026-09-22T16:11:00Z"}}` + "\n"

// TestMVPAcceptance is the architecture section 7 gate. The five
// subtests share one lake and one fixture session, in checklist order.
// CI runs this package inside go test ./...
func TestMVPAcceptance(t *testing.T) {
	if !bytes.Contains(fixtureSession, []byte(knownPrompt)) {
		t.Fatal("fixture is missing the known prompt")
	}
	if !bytes.HasSuffix(fixtureSession, []byte("\n")) {
		t.Fatal("fixture must end in a newline so an append is a new line")
	}
	g := openGate(t)

	t.Run("ingest_records_session_uid_and_blob_sha256", func(t *testing.T) {
		g.ingest(t)
	})
	if t.Failed() {
		return
	}
	t.Run("resync_uploads_zero_new_blobs", func(t *testing.T) {
		g.resync(t)
	})
	if t.Failed() {
		return
	}
	t.Run("append_uploads_tail_only_and_head_sha_updates", func(t *testing.T) {
		g.append(t)
	})
	if t.Failed() {
		return
	}
	t.Run("second_machine_cas_hit_two_provenance_rows", func(t *testing.T) {
		g.copySecondMachine(t)
	})
	if t.Failed() {
		return
	}
	t.Run("query_finds_known_prompt", func(t *testing.T) {
		g.queryPrompt(t)
	})
}

type gate struct {
	lake   *api.Server
	data   string
	srv    *httptest.Server
	wire   *wireTap
	homeA  string
	stateA string
	pathA  string
	uid    string
	orig   string
}

func openGate(t *testing.T) *gate {
	t.Helper()
	data := t.TempDir()
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Allow("tok")
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	home := t.TempDir()
	path := filepath.Join(home, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fixtureSession, 0o644); err != nil {
		t.Fatal(err)
	}
	return &gate{
		lake:   lake,
		data:   data,
		srv:    srv,
		wire:   wrapClient(srv.Client()),
		homeA:  home,
		stateA: t.TempDir(),
		pathA:  path,
		orig:   digest(fixtureSession),
	}
}

func (g *gate) ingest(t *testing.T) {
	t.Helper()
	res, err := upload.Sync(t.Context(), g.opt(g.homeA, g.stateA, machineA))
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || res.Manifests != 1 || len(res.Sessions) != 1 || res.Sessions[0] == "" {
		t.Fatalf("ingest: %+v", res)
	}
	g.uid = res.Sessions[0]
	if blobs(t, g.data) != 1 {
		t.Fatalf("blobs %d, want 1", blobs(t, g.data))
	}
	if g.wire.puts != 1 || len(g.wire.putBodies) != 1 || !bytes.Equal(g.wire.putBodies[0], fixtureSession) {
		t.Fatalf("ingest put: %d bodies", g.wire.puts)
	}
	uid, art, ok := g.head(t)
	if !ok || uid != g.uid || art.SHA256 != g.orig || !art.Current {
		t.Fatalf("catalog head: uid %s ok %v art %+v", uid, ok, art)
	}
	stored, err := g.lake.CAS.Read(g.orig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, fixtureSession) {
		t.Fatal("stored blob is not the fixture")
	}
}

func (g *gate) resync(t *testing.T) {
	t.Helper()
	before := blobs(t, g.data)
	g.wire.reset()
	res, err := upload.Sync(t.Context(), g.opt(g.homeA, g.stateA, machineA))
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 0 || res.Missing != 0 || g.wire.puts != 0 {
		t.Fatalf("re-sync: %+v puts=%d", res, g.wire.puts)
	}
	if blobs(t, g.data) != before {
		t.Fatalf("re-sync stored a blob: %d to %d", before, blobs(t, g.data))
	}
	// The file matches its watermark, so no manifest goes either.
	if len(res.Sessions) != 0 || res.Manifests != 0 || res.Unchanged != 1 || g.wire.manifests != 0 {
		t.Fatalf("re-sync posted: %+v manifests=%d", res, g.wire.manifests)
	}
	uid, art, ok := g.head(t)
	if !ok || uid != g.uid || art.SHA256 != g.orig {
		t.Fatalf("head moved on re-sync: uid %s art %+v", uid, art)
	}
}

func (g *gate) append(t *testing.T) {
	t.Helper()
	f, err := os.OpenFile(g.pathA, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(tailLine); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	grown, err := os.ReadFile(g.pathA)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(grown, fixtureSession) {
		t.Fatal("append did not keep the fixture as a prefix")
	}
	tail := grown[len(fixtureSession):]
	if string(tail) != tailLine {
		t.Fatalf("tail %q", tail)
	}
	full := digest(grown)
	before := blobs(t, g.data)
	g.wire.reset()
	res, err := upload.Sync(t.Context(), g.opt(g.homeA, g.stateA, machineA))
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || len(res.Sessions) != 1 || res.Sessions[0] != g.uid {
		t.Fatalf("append sync: %+v", res)
	}
	if g.wire.puts != 1 || len(g.wire.putBodies) != 1 || !bytes.Equal(g.wire.putBodies[0], tail) {
		t.Fatalf("append uploaded %d bodies, want the tail only", g.wire.puts)
	}
	if digest(g.wire.putBodies[0]) == full {
		t.Fatal("append PUT was the whole file")
	}
	uid, art, ok := g.head(t)
	if !ok || uid != g.uid || art.SHA256 != full || art.SHA256 == g.orig {
		t.Fatalf("head after append: uid %s art %+v", uid, art)
	}
	if art.Relation != protocol.RelationGrownFrom || art.GrownFrom != g.orig {
		t.Fatalf("grown_from: %+v", art)
	}
	// The previous head, the uploaded tail, and the assembled file.
	if blobs(t, g.data) != before+2 {
		t.Fatalf("blobs %d, want %d", blobs(t, g.data), before+2)
	}
	stored, err := g.lake.CAS.Read(full)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, grown) {
		t.Fatal("assembled head is not the grown file")
	}
}

func (g *gate) copySecondMachine(t *testing.T) {
	t.Helper()
	grown, err := os.ReadFile(g.pathA)
	if err != nil {
		t.Fatal(err)
	}
	full := digest(grown)
	homeB := t.TempDir()
	pathB := filepath.Join(homeB, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(pathB), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, grown, 0o644); err != nil {
		t.Fatal(err)
	}
	have, err := g.lake.CAS.Has(full)
	if err != nil {
		t.Fatal(err)
	}
	if !have {
		t.Fatal("grown digest is not in the CAS before machine B syncs")
	}
	before := blobs(t, g.data)
	g.wire.reset()
	res, err := upload.Sync(t.Context(), g.opt(homeB, t.TempDir(), machineB))
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 0 || res.Missing != 0 || g.wire.puts != 0 {
		t.Fatalf("machine B: %+v puts=%d", res, g.wire.puts)
	}
	if blobs(t, g.data) != before {
		t.Fatalf("machine B stored a blob: %d to %d", before, blobs(t, g.data))
	}
	if len(res.Sessions) != 1 || res.Sessions[0] != g.uid {
		t.Fatalf("machine B session: %+v, want %s", res.Sessions, g.uid)
	}
	rows, err := g.lake.Catalog.Provenance(t.Context(), g.uid)
	if err != nil {
		t.Fatal(err)
	}
	var hit []catalog.ProvenanceRow
	for _, row := range rows {
		if row.SHA256 == full {
			hit = append(hit, row)
		}
	}
	if len(hit) != 2 {
		t.Fatalf("provenance for %s: %+v", full, rows)
	}
	got := map[string]bool{}
	for _, row := range hit {
		if row.SessionUID != g.uid || row.SHA256 != full {
			t.Fatalf("provenance row: %+v", row)
		}
		got[row.MachineID] = true
	}
	if !got[machineA] || !got[machineB] {
		t.Fatalf("machines: %+v", hit)
	}
	aliasA, okA, err := g.lake.Catalog.Alias(t.Context(), protocol.HarnessTerva, nativeID, machineA)
	if err != nil {
		t.Fatal(err)
	}
	aliasB, okB, err := g.lake.Catalog.Alias(t.Context(), protocol.HarnessTerva, nativeID, machineB)
	if err != nil {
		t.Fatal(err)
	}
	if !okA || !okB || aliasA != g.uid || aliasB != g.uid {
		t.Fatalf("aliases a=%s %v b=%s %v", aliasA, okA, aliasB, okB)
	}
}

func (g *gate) queryPrompt(t *testing.T) {
	t.Helper()
	// The manifest ACK returns before workers project. The export proof
	// reads the derived view, so wait until that view has caught up.
	if err := g.lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := g.lake.Catalog.NormalizeError(t.Context(), g.uid)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg != "" {
		t.Fatalf("normalize_error: %q ok=%v", msg, ok)
	}
	// export opens the catalog itself. The lake connection has to be
	// closed first; sqlite allows one writer.
	if err := g.lake.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "events.jsonl")
	var stderr bytes.Buffer
	if err := cli.Run([]string{"export", "--data", g.data, "--out", out}, cli.Env{
		Stdout: &bytes.Buffer{},
		Stderr: &stderr,
	}); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("export stderr: %s", stderr.String())
	}
	if got := queryContent(t, out, "%"+knownPrompt+"%"); got != knownPrompt {
		t.Fatalf("query %q", got)
	}
}

func (g *gate) opt(home, state, machine string) upload.Options {
	return upload.Options{
		ServerURL: g.srv.URL,
		Token:     "tok",
		TervaHome: home,
		MachineID: machine,
		StateDir:  state,
		Client:    g.wire.client,
		Projects:  config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: cwd}}},
	}
}

func (g *gate) head(t *testing.T) (string, catalog.ArtifactRow, bool) {
	t.Helper()
	uid, arts, ok, err := g.lake.Catalog.Current(t.Context(), protocol.HarnessTerva, nativeID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return "", catalog.ArtifactRow{}, false
	}
	for _, art := range arts {
		if art.Kind == protocol.KindTranscriptJSONL {
			return uid, art, true
		}
	}
	t.Fatalf("no transcript for %s: %+v", uid, arts)
	return "", catalog.ArtifactRow{}, false
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func blobs(t *testing.T, data string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(filepath.Join(data, "cas"), func(path string, d fs.DirEntry, err error) error {
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

func queryContent(t *testing.T, path, like string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE export_lines (line TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if _, err := db.Exec(`INSERT INTO export_lines (line) VALUES (?)`, string(line)); err != nil {
			t.Fatal(err)
		}
	}
	var got string
	err = db.QueryRow(`
		SELECT json_extract(line, '$.content_text')
		FROM export_lines
		WHERE json_extract(line, '$.content_text') LIKE ?`, like).Scan(&got)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return got
}

type wireTap struct {
	client    *http.Client
	base      http.RoundTripper
	mu        sync.Mutex
	puts      int
	putBodies [][]byte
	manifests int
}

func wrapClient(c *http.Client) *wireTap {
	w := &wireTap{client: c, base: c.Transport}
	c.Transport = w
	return w
}

func (w *wireTap) reset() {
	w.mu.Lock()
	w.puts = 0
	w.putBodies = nil
	w.manifests = 0
	w.mu.Unlock()
}

func (w *wireTap) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/v1/manifests") {
		w.mu.Lock()
		w.manifests++
		w.mu.Unlock()
	}
	if req.Body != nil && req.Method == http.MethodPut {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(b))
		req.ContentLength = int64(len(b))
		w.mu.Lock()
		w.puts++
		w.putBodies = append(w.putBodies, append([]byte(nil), b...))
		w.mu.Unlock()
	}
	return w.base.RoundTrip(req)
}

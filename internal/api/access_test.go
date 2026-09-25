package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// shortLimits stands in for the minutes the lake allows: 100ms floors
// and 1 KiB/s, so a 1 KiB body has about 1.1s.
var shortLimits = deadlines{
	json:  100 * time.Millisecond,
	blob:  100 * time.Millisecond,
	rate:  1 << 10,
	slack: time.Second,
}

func TestSlowBlobPutGetsAck(t *testing.T) {
	body := bytes.Repeat([]byte("slow lake\n"), 100)
	sum := shaOf(t, body)

	s := openServer(t)
	limits := shortLimits
	s.limits = &limits
	logs := &syncBuffer{}
	s.Log = slog.New(slog.NewTextHandler(logs, nil))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// The body takes 600ms: longer than the 100ms floor, inside the
	// budget sized from Content-Length.
	resp, err := slowPut(ts.Listener.Addr().String(), sum, body, 4, 150*time.Millisecond)
	if err != nil {
		t.Fatalf("slow put lost its ACK: %v", err)
	}
	defer resp.Body.Close()
	var put protocol.PutResponse
	if err := json.NewDecoder(resp.Body).Decode(&put); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || put.SHA256 != sum || !put.Complete {
		t.Fatalf("slow put %d %+v", resp.StatusCode, put)
	}
	logs.waitFor(t, "status=200")
}

func TestSlowBlobBodyTimesOut(t *testing.T) {
	s := openServer(t)
	limits := shortLimits
	limits.rate = 1 << 20
	s.limits = &limits
	logs := &syncBuffer{}
	s.Log = slog.New(slog.NewTextHandler(logs, nil))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	body := bytes.Repeat([]byte("x"), 1024)
	resp, err := slowPut(ts.Listener.Addr().String(), shaOf(t, body), body, 2, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusRequestTimeout || !bytes.Contains(raw, []byte("timed out")) {
		t.Fatalf("stalled body %d %s", resp.StatusCode, raw)
	}
	logs.waitFor(t, "status=408")
	// The CAS wraps the read error in its own words. The line carries
	// the request's error, not the lake's.
	if line := logs.String(); !strings.Contains(line, `err="request body timed out"`) || strings.Contains(line, "cas:") {
		t.Fatalf("access line for a stalled body: %s", line)
	}
	if n := countBlobs(t, s); n != 0 {
		t.Fatalf("stalled body stored %d blobs", n)
	}
}

func TestCheckHasItsOwnCap(t *testing.T) {
	s := openServer(t)
	h := s.Handler()

	// 20000 digests is over the 1 MiB other routes allow.
	code, raw := postCheck(t, h, digests(20000))
	if code != http.StatusOK {
		t.Fatalf("20000 digests: %d %.200s", code, raw)
	}
	var out protocol.BlobCheckResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Missing) != 20000 {
		t.Fatalf("missing %d", len(out.Missing))
	}

	code, raw = postCheck(t, h, digests(maxCheckDigests+1))
	if code != http.StatusRequestEntityTooLarge || !bytes.Contains(raw, []byte("at most 100000 digests")) {
		t.Fatalf("count cap: %d %s", code, raw)
	}

	code, raw = postCheck(t, h, digests(int(maxCheckBytes/67)+1000))
	if code != http.StatusRequestEntityTooLarge ||
		!bytes.Contains(raw, []byte("exceeds 8388608 bytes")) ||
		!bytes.Contains(raw, []byte("at most 100000 digests")) {
		t.Fatalf("byte cap: %d %s", code, raw)
	}
}

func TestOversizeManifestIs413(t *testing.T) {
	s := openServer(t)
	m := manifest("machine-a", "sid-big", []byte("x"), shaOf(t, []byte("x")), 0, "")
	m.HarnessVersion = strings.Repeat("v", int(maxJSONBytes))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
	req.Header.Set("Authorization", "Bearer sekret")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge || !bytes.Contains(rr.Body.Bytes(), []byte("exceeds 1048576 bytes")) {
		t.Fatalf("oversize manifest %d %s", rr.Code, rr.Body)
	}
}

func TestManifestMissingFieldsIs400(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	body := []byte("fields\n")
	sum := putRaw(t, h, body)
	for name, m := range map[string]protocol.Manifest{
		"no machine":   manifest("", "sid-f", body, sum, 0, ""),
		"no artifacts": {CaptureProtocol: protocol.Version, MachineID: "m", Harness: protocol.HarnessTerva, NativeSessionID: "sid-f"},
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
		req.Header.Set("Authorization", "Bearer sekret")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", name, rr.Code, rr.Body)
		}
	}
}

func TestAccessLogLine(t *testing.T) {
	s := openServer(t)
	logs := &syncBuffer{}
	s.Log = slog.New(slog.NewTextHandler(logs, nil))
	h := s.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if logs.String() != "" {
		t.Fatalf("healthz logged: %s", logs)
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/hello", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	h.ServeHTTP(rr, req)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer wrong-token-value")
	h.ServeHTTP(rr, req)

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one line per request, got %q", lines)
	}
	for _, want := range []string{"method=POST", "path=/v1/hello", "status=200", "bytes=", "duration=", "xff=203.0.113.9"} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("hello line lacks %q: %s", want, lines[0])
		}
	}
	if !strings.Contains(lines[1], "status=401") || !strings.Contains(lines[1], "err=unauthorized") {
		t.Fatalf("401 line: %s", lines[1])
	}
	for _, secret := range []string{"sekret", "wrong-token-value", "Bearer", "Authorization"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("log carries %q: %s", secret, logs)
		}
	}
}

func TestShutdownBoundsNormalizeDrain(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	s.beforeProject = func() { <-release }
	s.Allow("sekret")
	h := s.Handler()
	// Two jobs hold the workers. The third waits in the queue.
	for i := range 3 {
		native := fmt.Sprintf("sid-drain-%d", i)
		body := transcriptLines(
			`{"type":"meta","meta":{"id":"`+native+`","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
			`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"drain pond"}],"time":"2026-09-22T16:10:01Z"}}`,
		)
		postManifest(t, h, manifest("machine-a", native, body, putRaw(t, h, body), 0, ""))
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	time.AfterFunc(300*time.Millisecond, func() { close(release) })
	start := time.Now()
	left, err := s.Shutdown(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatalf("left %d, want 1", left)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("shutdown took %s", took)
	}

	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.Catalog.ListNormalizeJobs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("job not resumed: %+v", jobs)
	}
}

// slowPut sends a PUT on a raw connection in pieces, pausing after each,
// and reads the response. Write errors are ignored: a server that gave
// up closes the connection.
func slowPut(addr, digest string, body []byte, pieces int, pause time.Duration) (*http.Response, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(conn, "PUT /v1/blobs/%s HTTP/1.1\r\nHost: lake\r\nAuthorization: Bearer sekret\r\nContent-Length: %d\r\n\r\n", digest, len(body))
	step := (len(body) + pieces - 1) / pieces
	for off := 0; off < len(body); off += step {
		_, _ = conn.Write(body[off:min(off+step, len(body))])
		time.Sleep(pause)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body = struct {
		io.Reader
		io.Closer
	}{resp.Body, conn}
	return resp, nil
}

func postCheck(t *testing.T, h http.Handler, ds []string) (int, []byte) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/blobs/check", bytes.NewReader(mustJSON(t, ds)))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	return rr.Code, rr.Body.Bytes()
}

func digests(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%064x", i)
	}
	return out
}

// syncBuffer is a log sink the server goroutine and the test share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor polls because the access line is written after the response.
func (b *syncBuffer) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(b.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("log lacks %q: %s", want, b.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

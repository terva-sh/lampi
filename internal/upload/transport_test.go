package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
)

// slowConn is a client connection on a slow link: it sleeps pause after
// every step bytes written. The lake reads as fast as bytes arrive, so
// socket buffers do not decide when the writer moves.
type slowConn struct {
	net.Conn
	step, sent int
	pause      time.Duration
}

func (c *slowConn) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := min(len(p), c.step-c.sent)
		w, err := c.Conn.Write(p[:n])
		total += w
		if err != nil {
			return total, err
		}
		p = p[n:]
		c.sent += n
		if c.sent == c.step {
			c.sent = 0
			time.Sleep(c.pause)
		}
	}
	return total, nil
}

func slowClient(step int, pause time.Duration) *http.Client {
	client := NewClient()
	client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &slowConn{Conn: c, step: step, pause: pause}, nil
	}
	return client
}

// A 32 MiB blob crosses a link slow enough that one piece takes longer
// than the stall timeout. Bytes keep moving, so nothing is cut off. The
// old whole-request timeout could not tell the two apart.
func TestSlowLinkUploadsBlobAtCap(t *testing.T) {
	lake, _ := openLake(t)
	var mu sync.Mutex
	var slowest time.Duration
	var ranges int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			lake.Handler().ServeHTTP(w, r)
			return
		}
		start := time.Now()
		lake.Handler().ServeHTTP(w, r)
		mu.Lock()
		if d := time.Since(start); d > slowest {
			slowest = d
		}
		if r.Header.Get("Content-Range") != "" {
			ranges++
		}
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)

	// The put path Sync takes for a missing blob, without the 32 MiB scan.
	body := bytes.Repeat([]byte("a"), int(protocol.MaxBlobBytes))
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	opt := Options{
		ServerURL: srv.URL,
		Token:     "tok",
		// About 12 MiB/s: a 4 MiB piece takes over 300ms, and bytes
		// move every few milliseconds.
		Client:       slowClient(64<<10, 5*time.Millisecond),
		StallTimeout: 200 * time.Millisecond,
		PieceBytes:   DefaultPieceBytes,
	}
	put, err := putBlobResume(context.Background(), opt.Client, opt, digest, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !put.Complete || put.Exists {
		t.Fatalf("put %+v", put)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := int(protocol.MaxBlobBytes / DefaultPieceBytes); ranges != want {
		t.Fatalf("range puts %d, want %d", ranges, want)
	}
	if slowest <= opt.StallTimeout {
		t.Fatalf("slowest put %s is not slower than the stall timeout %s", slowest, opt.StallTimeout)
	}
	if ok, err := lake.CAS.Has(digest); err != nil || !ok {
		t.Fatalf("blob not stored: %v", err)
	}
}

func TestDefaultClientHasNoWholeRequestTimeout(t *testing.T) {
	c := NewClient()
	if c.Timeout != 0 {
		t.Fatalf("Client.Timeout %s bounds the body", c.Timeout)
	}
	tr := c.Transport.(*http.Transport)
	if tr.TLSHandshakeTimeout == 0 || tr.ResponseHeaderTimeout == 0 || tr.IdleConnTimeout == 0 || tr.DialContext == nil {
		t.Fatalf("transport %+v", tr)
	}
}

// deadListener accepts connections and never reads or answers.
func deadListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	return "http://" + ln.Addr().String()
}

func TestDeadConnectionIsDetected(t *testing.T) {
	base := deadListener(t)

	t.Run("no response headers", func(t *testing.T) {
		client := NewClient()
		client.Transport.(*http.Transport).ResponseHeaderTimeout = 200 * time.Millisecond
		opt := Options{ServerURL: base, StallTimeout: time.Hour}
		done := make(chan error, 1)
		go func() { _, err := postHello(context.Background(), client, opt); done <- err }()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "timeout awaiting response headers") {
				t.Fatalf("err %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("hello to a dead lake did not fail")
		}
	})

	t.Run("body stops moving", func(t *testing.T) {
		client := NewClient()
		client.Transport.(*http.Transport).ResponseHeaderTimeout = time.Hour
		opt := Options{ServerURL: base, StallTimeout: 300 * time.Millisecond}
		// Larger than the loopback socket buffers, so the writer blocks.
		body := make([]byte, 64<<20)
		done := make(chan error, 1)
		go func() {
			_, err := putBlob(context.Background(), client, opt, strings.Repeat("0", 64), body)
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "no bytes moved for 300ms") {
				t.Fatalf("err %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("put to a dead lake did not fail")
		}
	})
}

func fakeDigests(n int) []string {
	out := make([]string, n)
	for i := range out {
		sum := sha256.Sum256([]byte(fmt.Sprint(i)))
		out[i] = hex.EncodeToString(sum[:])
	}
	return out
}

// checkServer answers blobs/check, reporting every odd-indexed digest
// missing. It answers 413 to a batch longer than limit.
type checkServer struct {
	mu      sync.Mutex
	limit   int
	sizes   []int
	refused int
}

func (c *checkServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var got []string
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.limit > 0 && len(got) > c.limit {
		c.refused++
		http.Error(w, "too many digests", http.StatusRequestEntityTooLarge)
		return
	}
	c.sizes = append(c.sizes, len(got))
	missing := []string{}
	for _, d := range got {
		if d[len(d)-1]%2 == 1 {
			missing = append(missing, d)
		}
	}
	_ = json.NewEncoder(w).Encode(protocol.BlobCheckResponse{Missing: missing})
}

func wantMissing(digests []string) []string {
	var out []string
	for _, d := range digests {
		if d[len(d)-1]%2 == 1 {
			out = append(out, d)
		}
	}
	return out
}

func TestCheckIsBatched(t *testing.T) {
	cs := &checkServer{}
	srv := httptest.NewServer(cs)
	t.Cleanup(srv.Close)
	digests := fakeDigests(2500)
	missing, err := postCheck(context.Background(), srv.Client(), Options{ServerURL: srv.URL}, digests)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(cs.sizes) != "[1000 1000 500]" {
		t.Fatalf("batches %v", cs.sizes)
	}
	if fmt.Sprint(missing) != fmt.Sprint(wantMissing(digests)) {
		t.Fatalf("missing %d, want %d", len(missing), len(wantMissing(digests)))
	}
}

func TestCheckHalvesOn413(t *testing.T) {
	cs := &checkServer{limit: 300}
	srv := httptest.NewServer(cs)
	t.Cleanup(srv.Close)
	digests := fakeDigests(1100)
	missing, err := postCheck(context.Background(), srv.Client(), Options{ServerURL: srv.URL}, digests)
	if err != nil {
		t.Fatal(err)
	}
	// 1000 and 500 are refused, then 250 fits and stays.
	if cs.refused != 2 {
		t.Fatalf("refused %d", cs.refused)
	}
	total := 0
	for _, n := range cs.sizes {
		if n > 250 {
			t.Fatalf("batches %v", cs.sizes)
		}
		total += n
	}
	if total != len(digests) {
		t.Fatalf("checked %d of %d", total, len(digests))
	}
	if fmt.Sprint(missing) != fmt.Sprint(wantMissing(digests)) {
		t.Fatalf("missing %d, want %d", len(missing), len(wantMissing(digests)))
	}
}

func TestCheckStopsAtOneDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "too many digests", http.StatusRequestEntityTooLarge)
	}))
	t.Cleanup(srv.Close)
	_, err := postCheck(context.Background(), srv.Client(), Options{ServerURL: srv.URL}, fakeDigests(3))
	if err == nil || !strings.Contains(err.Error(), "413") {
		t.Fatalf("err %v", err)
	}
}

// rangeGate passes ranges to the current lake. failAt answers 500 to
// the range starting there, once.
type rangeGate struct {
	mu     sync.Mutex
	lake   http.Handler
	failAt int64
	starts []int64
}

func (g *rangeGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	lake := g.lake
	cr := r.Header.Get("Content-Range")
	var start int64 = -1
	if cr != "" {
		fmt.Sscanf(cr, "bytes %d-", &start)
		g.starts = append(g.starts, start)
	}
	fail := start >= 0 && start == g.failAt
	if fail {
		g.failAt = -1
	}
	g.mu.Unlock()
	if fail {
		http.Error(w, "lake hiccup", http.StatusInternalServerError)
		return
	}
	lake.ServeHTTP(w, r)
}

func (g *rangeGate) takeStarts() []int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := g.starts
	g.starts = nil
	return out
}

func TestRangePutResumesAfterFailure(t *testing.T) {
	lake, _ := openLake(t)
	gate := &rangeGate{lake: lake.Handler(), failAt: 20}
	srv := httptest.NewServer(gate)
	t.Cleanup(srv.Close)
	body := []byte(strings.Repeat("0123456789", 5))
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	opt := Options{ServerURL: srv.URL, Token: "tok", PieceBytes: 10}

	if _, err := putRanged(context.Background(), srv.Client(), opt, digest, body); err == nil {
		t.Fatal("first attempt did not fail")
	}
	if got := fmt.Sprint(gate.takeStarts()); got != "[0 10 20]" {
		t.Fatalf("first attempt ranges %s", got)
	}
	put, err := putRanged(context.Background(), srv.Client(), opt, digest, body)
	if err != nil {
		t.Fatal(err)
	}
	if !put.Complete {
		t.Fatalf("put %+v", put)
	}
	// The lake already holds 0-19. The retry does not send them again.
	if got := fmt.Sprint(gate.takeStarts()); got != "[20 30 40]" {
		t.Fatalf("resumed ranges %s", got)
	}
	if resumeAt(resumeKey(opt, digest)) != 0 {
		t.Fatal("offset kept after the blob assembled")
	}
}

func TestRangePutRestartsWhenLakeLostPartial(t *testing.T) {
	first, _ := openLake(t)
	gate := &rangeGate{lake: first.Handler(), failAt: 30}
	srv := httptest.NewServer(gate)
	t.Cleanup(srv.Close)
	body := []byte(strings.Repeat("abcdefghij", 5))
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	opt := Options{ServerURL: srv.URL, Token: "tok", PieceBytes: 10}

	if _, err := putRanged(context.Background(), srv.Client(), opt, digest, body); err == nil {
		t.Fatal("first attempt did not fail")
	}
	gate.takeStarts()
	// A lake without the earlier spans answers the resumed pieces with
	// complete false. The client sends the whole body again.
	second, _ := openLake(t)
	gate.mu.Lock()
	gate.lake = second.Handler()
	gate.mu.Unlock()
	put, err := putRanged(context.Background(), srv.Client(), opt, digest, body)
	if err != nil {
		t.Fatal(err)
	}
	if !put.Complete {
		t.Fatalf("put %+v", put)
	}
	if got := fmt.Sprint(gate.takeStarts()); got != "[30 40 0 10 20]" {
		t.Fatalf("ranges %s", got)
	}
	if ok, err := second.CAS.Has(digest); err != nil || !ok {
		t.Fatalf("not installed: %v", err)
	}
}

func TestCheckToken(t *testing.T) {
	for _, tc := range []struct {
		server, token string
		ok            bool
	}{
		{"http://lake.example:8787", "tok", false},
		{"http://10.0.0.5:8787", "tok", false},
		{"http://[2001:db8::1]:8787", "tok", false},
		{"HTTP://lake.example", "tok", false},
		{"http://lake.example:8787", "", true},
		{"https://lake.example", "tok", true},
		{"http://127.0.0.1:8787", "tok", true},
		{"http://127.8.9.10:8787", "tok", true},
		{"http://[::1]:8787", "tok", true},
		{"http://localhost:8787", "tok", true},
		{"http://LocalHost", "tok", true},
	} {
		err := CheckToken(tc.server, tc.token)
		if (err == nil) != tc.ok {
			t.Errorf("CheckToken(%q, %q) = %v", tc.server, tc.token, err)
		}
		if err != nil && !strings.Contains(err.Error(), "plain http") {
			t.Errorf("message %q", err)
		}
	}
}

func TestSyncRefusesTokenOverPlainHTTP(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n"))
	opt := allowAll(srv, home, t.TempDir(), "/tmp/p")
	// A proxy would carry this to the test server. The token must not go.
	opt.ServerURL = "http://lake.example:8787"
	opt.Client = &http.Client{Transport: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) { return url.Parse(srv.URL) }}}
	_, err := Sync(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "refusing to send the device token to lake.example:8787 over plain http") {
		t.Fatalf("err %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("requests %d", hits)
	}
}

// A lake that is down fails the pass before the scan. Nothing reaches
// the outbox, and no file is read for a push that cannot happen.
func TestSyncHelloBeforeScan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	state := t.TempDir()
	writeSession(t, home, "abcd", "s.jsonl", []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"s\",\"cwd\":\"/tmp/p\"}}\n"))
	opt := allowAll(srv, home, state, "/tmp/p")
	_, err := Sync(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err %v", err)
	}
	q, err := outbox.Open(outbox.File(state))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	depth, err := q.Depth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if depth != 0 {
		t.Fatalf("outbox %d after a failed hello", depth)
	}
}

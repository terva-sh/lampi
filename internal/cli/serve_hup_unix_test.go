//go:build unix

package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/auth"
)

// SIGHUP swaps the token set under a request that already passed the
// check. That request still gets its ACK; the next one sees the new set.
func TestHangupReloadsTokensWithoutDroppingRequests(t *testing.T) {
	oldTok, _ := auth.Generate()
	newTok, _ := auth.Generate()
	path := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(path, []byte("# laptop\n"+oldTok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	devices, err := auth.LoadDevices(path)
	if err != nil {
		t.Fatal(err)
	}
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Devices = devices
	ts := httptest.NewServer(lake.Handler())
	t.Cleanup(ts.Close)

	stderr := &lockedBuffer{}
	env := Env{Stderr: stderr}
	reloadOnHangup(t.Context(), func() { reloadDevices(env, path, devices) })

	body := []byte("in flight at SIGHUP\n")
	sum := sha256.Sum256(body)
	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "PUT /v1/blobs/%s HTTP/1.1\r\nHost: lake\r\nAuthorization: Bearer %s\r\nContent-Length: %d\r\n\r\n", hex.EncodeToString(sum[:]), oldTok, len(body))
	half := len(body) / 2
	if _, err := conn.Write(body[:half]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	if err := os.WriteFile(path, []byte("# desktop\n"+newTok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(stderr.String(), "reloaded 1 device tokens") {
		if time.Now().After(deadline) {
			t.Fatalf("no reload: %s", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := conn.Write(body[half:]); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("in-flight PUT lost its ACK: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("in-flight PUT %d", resp.StatusCode)
	}

	for tok, want := range map[string]int{oldTok: http.StatusUnauthorized, newTok: http.StatusOK} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/stats", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("after reload: %d, want %d", resp.StatusCode, want)
		}
	}

	// A file that no longer loads keeps the set that did.
	if err := os.WriteFile(path, []byte("desktop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloadDevices(env, path, devices)
	if !devices.Match("Bearer "+newTok) || !strings.Contains(stderr.String(), "keeping 1 device tokens") {
		t.Fatalf("bad reload replaced the set: %s", stderr.String())
	}
}

// lockedBuffer is stderr shared by the reload goroutine and the test.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

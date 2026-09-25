package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
)

// A fixed WriteTimeout starts when the headers are read, so a slow blob
// body lost its ACK. The lake handler sets deadlines per request.
func TestServeHasNoFixedBodyTimeouts(t *testing.T) {
	srv := newHTTPServer(http.NotFoundHandler())
	if srv.WriteTimeout != 0 || srv.ReadTimeout != 0 {
		t.Fatalf("write %s read %s: a fixed timeout cuts slow blob bodies", srv.WriteTimeout, srv.ReadTimeout)
	}
	if srv.ReadHeaderTimeout <= 0 {
		t.Fatal("no ReadHeaderTimeout")
	}
}

func TestServeWaitsForRequestInFlight(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stderr bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- serveLake(ctx, Env{Stderr: &stderr}, lake, ln, 5*time.Second, time.Second)
	}()

	body := []byte("in flight at SIGTERM\n")
	sum := sha256.Sum256(body)
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "PUT /v1/blobs/%s HTTP/1.1\r\nHost: lake\r\nContent-Length: %d\r\n\r\n", hex.EncodeToString(sum[:]), len(body))
	half := len(body) / 2
	if _, err := conn.Write(body[:half]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		t.Fatalf("serve returned with a request in flight: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if lake.Catalog == nil {
		t.Fatal("catalog closed under a request")
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
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the request finished")
	}
	if lake.Catalog != nil {
		t.Fatal("catalog left open")
	}
	if !strings.Contains(stderr.String(), "waiting for requests in flight") {
		t.Fatalf("stderr: %s", stderr.String())
	}
}

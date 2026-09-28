package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
)

func TestServeHealthcheckPassesAgainstALake(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer lake.Close()
	srv := httptest.NewServer(lake.Handler())
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	addr := strings.TrimPrefix(srv.URL, "http://")
	if err := Run([]string{"serve", "healthcheck", "--addr", addr}, Env{Stdout: &stdout, Stderr: &stderr}); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("a passing probe printed stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestServeHealthcheckFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"not 200", http.StatusServiceUnavailable, `{"status":"ok"}`, "http 503"},
		{"another body", http.StatusOK, `{"status":"starting"}`, "unexpected answer"},
		{"not json", http.StatusOK, "ok", "unexpected answer"},
		{"redirect", http.StatusFound, "", "http 302"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("probe asked for %s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("probe sent an Authorization header")
				}
				if tc.status == http.StatusFound {
					w.Header().Set("Location", "/elsewhere")
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			addr := strings.TrimPrefix(srv.URL, "http://")
			err := Run([]string{"serve", "healthcheck", "--addr", addr}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestServeHealthcheckNoListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	start := time.Now()
	err = Run([]string{"serve", "healthcheck", "--addr", addr, "--timeout", "2s"}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), "healthcheck:") {
		t.Fatalf("err = %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("a refused connection took %s", took)
	}
}

func TestProbeAddr(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1:8787":     "127.0.0.1:8787",
		"0.0.0.0:8787":       "127.0.0.1:8787",
		":8787":              "127.0.0.1:8787",
		"[::]:8787":          "[::1]:8787",
		"lake.internal:8787": "lake.internal:8787",
	} {
		got, err := probeAddr(in)
		if err != nil || got != want {
			t.Errorf("probeAddr(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := probeAddr("8787"); err == nil {
		t.Error("probeAddr accepted an address with no port")
	}
}

func TestServeHealthcheckArguments(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run([]string{"serve", "healthcheck", "extra"}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err == nil || !strings.Contains(err.Error(), `unexpected argument "extra"`) {
		t.Fatalf("err = %v", err)
	}
	if err := Run([]string{"serve", "healthcheck", "--timeout", "0s"}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err == nil {
		t.Fatal("a zero timeout was accepted")
	}
	stdout.Reset()
	if err := Run([]string{"serve", "healthcheck", "--help"}, Env{Stdout: &stdout, Stderr: ioDiscard()}); err != nil || !strings.Contains(stdout.String(), "serve healthcheck") {
		t.Fatalf("help: %v\n%s", err, stdout.String())
	}
}

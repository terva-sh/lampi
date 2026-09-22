package main

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBinaryHelpAndHealth(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "terva-lampi")
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	build := exec.Command(goBin, "build", "-o", bin, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	help, err := exec.Command(bin, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("help: %v\n%s", err, help)
	}
	text := string(help)
	for _, cmd := range []string{"serve", "agent", "sync", "status", "login"} {
		if !strings.Contains(text, "terva-lampi "+cmd) {
			t.Fatalf("help missing %s:\n%s", cmd, text)
		}
	}

	data := t.TempDir()
	srv := exec.Command(bin, "serve", "--addr", "127.0.0.1:0", "--data", data)
	stderr, err := srv.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	srv.Stdout = io.Discard
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if srv.Process != nil {
			_ = srv.Process.Kill()
		}
		_ = srv.Wait()
	}()

	addrCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := sc.Text()
			const prefix = "terva-lampi serve: listening on "
			if strings.HasPrefix(line, prefix) {
				addrCh <- strings.TrimPrefix(line, prefix)
				return
			}
		}
		addrCh <- ""
	}()

	var addr string
	select {
	case addr = <-addrCh:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for serve")
	}
	if addr == "" {
		t.Fatal("serve exited before listening")
	}
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("health %d %s", resp.StatusCode, body)
	}
}

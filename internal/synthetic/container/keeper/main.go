//go:build synthetic_container

// Command keeper is pid 1 for the distroless synthetic image.
//
// The image contains terva-lampi and no shell. This process starts
// serve and reaps it. The catalog allows one writer, so export cannot
// open /lake until serve has exited. The driver creates
// /state/stop-serve when it wants that handoff. Keeper SIGTERMs serve,
// waits until the process has closed the catalog, writes
// /state/serve.stopped, and stays up so docker exec can run export in
// the same container.
//
// Image CMD is not used. The driver replaces it; arguments here are
// ignored so a leftover "serve ..." does not start a second lake.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

const (
	bin         = "/usr/local/bin/terva-lampi"
	stopFile    = "/state/stop-serve"
	stoppedFile = "/state/serve.stopped"
	serveLog    = "/state/serve.log"
	servePid    = "/state/serve.pid"
)

func main() {
	logf, err := os.OpenFile(serveLog, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		die("serve log: %v", err)
	}
	cmd := exec.Command(bin, "serve", "--addr", "127.0.0.1:8787", "--data", "/lake")
	cmd.Stdout = logf
	cmd.Stderr = logf
	if err := cmd.Start(); err != nil {
		die("start serve: %v", err)
	}
	_ = logf.Close()
	if err := os.WriteFile(servePid, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		die("pid: %v", err)
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	reason := ""
	for {
		select {
		case <-sig:
			if reason == "" {
				reason = "signal"
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		case <-tick.C:
			if reason != "" {
				continue
			}
			if _, err := os.Stat(stopFile); err != nil {
				continue
			}
			reason = "file"
			_ = cmd.Process.Signal(syscall.SIGTERM)
		case <-exited:
			if reason != "file" {
				_ = os.WriteFile(stoppedFile, []byte("exit\n"), 0o644)
				return
			}
			if err := os.WriteFile(stoppedFile, []byte("ok\n"), 0o644); err != nil {
				die("stopped marker: %v", err)
			}
			<-sig
			return
		}
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "keeper: "+format+"\n", args...)
	os.Exit(1)
}

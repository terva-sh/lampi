//go:build unix

package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// TestMain turns the test binary into the hook tests' stand-in agent
// when standInEnv is set: it waits for SIGUSR1, or 30s, and exits. Go
// drops SIGUSR1 until a program asks for it, so the stand-in asks, then
// prints "ready" so a test never signals it before it is listening.
func TestMain(m *testing.M) {
	if os.Getenv(standInEnv) != "" {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGUSR1)
		fmt.Println("ready")
		select {
		case <-ch:
		case <-time.After(30 * time.Second):
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

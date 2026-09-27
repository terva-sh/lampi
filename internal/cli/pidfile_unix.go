//go:build unix

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// lockAgentPID takes an exclusive flock on agent.pid and writes this
// process id into it. A second agent on the same state directory finds
// the lock held and stops, naming the pid that holds it. The lock goes
// with the process, so a crash leaves a file the next agent can take.
func lockAgentPID(path string) (func(), error) {
	for range 3 {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, fmt.Errorf("agent: pid file: %w", err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return nil, errAgentRunning(path)
			}
			return nil, fmt.Errorf("agent: pid file: %w", err)
		}
		// The last agent may have removed the path between our open and
		// our lock. That lock is on a file nobody else will open.
		if !samePath(f, path) {
			f.Close()
			continue
		}
		if err := writePID(f); err != nil {
			f.Close()
			return nil, err
		}
		return func() {
			_ = os.Remove(path)
			f.Close()
		}, nil
	}
	return nil, fmt.Errorf("agent: pid file: %s kept changing under the lock", path)
}

func samePath(f *os.File, path string) bool {
	a, err := f.Stat()
	if err != nil {
		return false
	}
	b, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(a, b)
}

func writePID(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("agent: pid file: %w", err)
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		return fmt.Errorf("agent: pid file: %w", err)
	}
	return nil
}

// reloadAgent asks the agent that holds agent.pid in state to read its
// lakes again, the way the hook asks for a sync with SIGUSR1. It
// returns the line a command that changed the lakes prints.
//
// The lock, not the pid in the file, says whether an agent runs: a pid
// left by a crash may belong to another process by now, and SIGHUP
// would end it.
func reloadAgent(state string) string {
	path := filepath.Join(state, "agent.pid")
	f, err := os.Open(path)
	if err != nil {
		return "no agent is running; the next one to start reads the lakes"
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return "no agent is running; the next one to start reads the lakes"
	}
	raw, _ := io.ReadAll(f)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return fmt.Sprintf("an agent holds %s and names no pid; restart it to use the new lakes", path)
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Sprintf("could not signal the agent (pid %d): %v; restart it to use the new lakes", pid, err)
	}
	return fmt.Sprintf("agent (pid %d) is reloading its lakes", pid)
}

//go:build unix

package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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

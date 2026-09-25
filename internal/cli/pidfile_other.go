//go:build !unix

package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// lockAgentPID creates agent.pid with O_EXCL. A file with no readable
// pid is replaced. A file naming a pid stops this agent: on Windows
// FindProcess succeeds for any pid, so a file left by a crash is not
// told apart from a live agent, and the error says to delete it.
func lockAgentPID(path string) (func(), error) {
	body := []byte(strconv.Itoa(os.Getpid()) + "\n")
	for range 3 {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, werr := f.Write(body)
			cerr := f.Close()
			if err := errors.Join(werr, cerr); err != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("agent: pid file: %w", err)
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("agent: pid file: %w", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("agent: pid file: %w", err)
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
			if p, err := os.FindProcess(pid); err == nil {
				p.Release()
				return nil, fmt.Errorf("%w; if no terva-lampi agent is running, delete %s", errAgentRunning(path), path)
			}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("agent: pid file: %w", err)
		}
	}
	return nil, fmt.Errorf("agent: pid file: %s kept changing", path)
}

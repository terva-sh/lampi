// Package lakelock is the lock file one lake process holds in its data
// directory. serve holds it for as long as it runs. A command that
// writes the catalog or the CAS outside serve takes it too, or reads
// the lake without writing when serve has it.
//
// On unix the lock is flock on the file, so it ends with the process
// even after a crash. Elsewhere the file is created with O_EXCL and
// holds the owner's pid; a file whose pid is not running is stale and
// is replaced.
package lakelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Name is the lock file in the data directory.
const Name = "lake.lock"

// ErrHeld is returned, wrapped in *HeldError, when another process
// holds the lock.
var ErrHeld = errors.New("lakelock: held by another process")

// HeldError names the process that holds the lock. PID is 0 when the
// file does not say.
type HeldError struct {
	Dir string
	PID int
}

func (e *HeldError) Error() string {
	msg := fmt.Sprintf("lake %s is in use by another process", e.Dir)
	if e.PID > 0 {
		msg = fmt.Sprintf("lake %s is in use by pid %d", e.Dir, e.PID)
	}
	if staleCanOutlive {
		msg += fmt.Sprintf("; if no terva-lampi is running, delete %s", filepath.Join(e.Dir, Name))
	}
	return msg
}

func (e *HeldError) Is(target error) bool { return target == ErrHeld }

// Lock is a held lake lock.
type Lock struct {
	f    *os.File
	path string
}

// Acquire takes the lock in dir, creating dir (mode 0700) when it is
// missing. It does not wait. A lock another process holds is an error
// that matches ErrHeld.
func Acquire(dir string) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("lakelock: %w", err)
	}
	path := filepath.Join(dir, Name)
	f, err := acquire(path)
	if err != nil {
		if errors.Is(err, ErrHeld) {
			return nil, &HeldError{Dir: dir, PID: readPID(path)}
		}
		return nil, fmt.Errorf("lakelock: %w", err)
	}
	if err := writePID(f); err != nil {
		release(f, path)
		return nil, fmt.Errorf("lakelock: %w", err)
	}
	return &Lock{f: f, path: path}, nil
}

// Release gives the lock up. It is safe to call more than once.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := release(l.f, l.path)
	l.f = nil
	return err
}

func writePID(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		return err
	}
	return nil
}

func readPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

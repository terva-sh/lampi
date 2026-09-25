//go:build !unix

package lakelock

import (
	"errors"
	"os"
)

// acquire creates path with O_EXCL. A file left by a process that is
// no longer running is removed and the create is tried once more.
func acquire(path string) (*os.File, error) {
	for try := 0; ; try++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if try > 0 || running(readPID(path)) {
			return nil, ErrHeld
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
}

// running reports whether pid names a live process. On Windows
// FindProcess opens the process and fails when there is none. A file
// with no pid is treated as held: it may be mid-write.
func running(pid int) bool {
	if pid <= 0 {
		return true
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}

func release(f *os.File, path string) error {
	err := f.Close()
	if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) && err == nil {
		err = rerr
	}
	return err
}

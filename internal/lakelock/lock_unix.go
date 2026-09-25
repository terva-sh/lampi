//go:build unix

package lakelock

import (
	"errors"
	"os"
	"syscall"
)

// acquire flocks path. The file stays after release: removing it would
// let a second process lock a new inode while a third still holds the
// old one.
func acquire(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrHeld
		}
		return nil, err
	}
	return f, nil
}

func release(f *os.File, path string) error {
	// Closing the descriptor drops the flock.
	return f.Close()
}

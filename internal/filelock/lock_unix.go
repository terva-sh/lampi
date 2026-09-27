//go:build unix

package filelock

import (
	"os"
	"syscall"
)

// Lock waits for an exclusive flock on f. The kernel drops it when f is
// closed or the process dies.
func Lock(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			return err
		}
	}
}

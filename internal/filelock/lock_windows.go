//go:build windows

package filelock

import (
	"os"

	"golang.org/x/sys/windows"
)

// Lock waits for an exclusive LockFileEx on f's first byte. Windows
// drops it when f is closed or the process dies.
func Lock(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
}

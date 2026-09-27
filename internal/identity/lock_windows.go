//go:build windows

package identity

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile waits for an exclusive LockFileEx on f's first byte. Windows
// drops it when f is closed or the process dies.
func lockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
}

//go:build unix

package watch

import (
	"os"
	"syscall"
)

// identity is the device and inode. An editor atomic replace keeps the
// path and changes the inode; an append does not.
func identity(info os.FileInfo) (dev, ino uint64, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), true
}

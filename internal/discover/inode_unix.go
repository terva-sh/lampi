//go:build unix

package discover

import (
	"io/fs"
	"syscall"
)

// Inode is the file's inode number. An atomic replace keeps the path
// and changes it; an append does not.
func Inode(info fs.FileInfo) uint64 {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0
	}
	return uint64(st.Ino)
}

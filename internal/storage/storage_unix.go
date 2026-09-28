//go:build unix

package storage

import (
	"fmt"
	"io/fs"
	"syscall"

	"golang.org/x/sys/unix"
)

// allocated is the blocks a file occupies. st_blocks is in 512-byte
// units on every unix Go supports.
func allocated(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return info.Size()
}

func capacity(dir string) (Filesystem, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return Filesystem{}, fmt.Errorf("storage: %w", err)
	}
	size := uint64(st.Bsize)
	return Filesystem{Total: uint64(st.Blocks) * size, Free: uint64(st.Bavail) * size}, nil
}

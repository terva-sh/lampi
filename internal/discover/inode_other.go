//go:build !unix

package discover

import "io/fs"

// Inode is zero where the platform has no inode. Size and mtime are
// then the whole stat.
func Inode(fs.FileInfo) uint64 { return 0 }

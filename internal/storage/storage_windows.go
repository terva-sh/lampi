//go:build windows

package storage

import (
	"fmt"
	"io/fs"
	"strings"

	"golang.org/x/sys/windows"
)

// allocated is the file size: Windows reports no allocation through
// os.Stat.
func allocated(info fs.FileInfo) int64 { return info.Size() }

func capacity(dir string) (Filesystem, error) {
	// GetDiskFreeSpaceEx takes a directory name ending in a separator;
	// a UNC path without one fails.
	if !strings.HasSuffix(dir, `\`) && !strings.HasSuffix(dir, "/") {
		dir += `\`
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return Filesystem{}, fmt.Errorf("storage: %w", err)
	}
	var free, total uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, nil); err != nil {
		return Filesystem{}, fmt.Errorf("storage: %w", err)
	}
	return Filesystem{Total: total, Free: free}, nil
}

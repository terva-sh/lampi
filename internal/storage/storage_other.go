//go:build !unix && !windows

package storage

import "io/fs"

func allocated(info fs.FileInfo) int64 { return info.Size() }

func capacity(string) (Filesystem, error) { return Filesystem{}, ErrUnsupported }

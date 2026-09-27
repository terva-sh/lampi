//go:build !unix && !windows

package filelock

import "os"

// Lock has no lock to take here.
func Lock(f *os.File) error { return ErrUnsupported }

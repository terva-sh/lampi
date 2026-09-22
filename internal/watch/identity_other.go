//go:build !unix

package watch

import "os"

// identity is unavailable. Replace detection then uses size, mtime, and
// remove/rename events.
func identity(os.FileInfo) (dev, ino uint64, ok bool) {
	return 0, 0, false
}

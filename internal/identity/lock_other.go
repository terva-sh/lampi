//go:build !unix && !windows

package identity

import (
	"errors"
	"os"
)

// lockFile refuses: without a lock two commands could each save over
// the other's change, and a lost retirement would put a compromised key
// back in use.
func lockFile(f *os.File) error {
	return errors.New("this platform has no file lock; change identity.json on a unix or windows host")
}

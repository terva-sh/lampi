package identity

import (
	"errors"
	"os"

	"terva.sh/lampi/internal/filelock"
)

// lockFile waits for an exclusive lock on f. Where there is no file
// lock it refuses: two commands could each save over the other's
// change, and a lost retirement would put a compromised key back in use.
func lockFile(f *os.File) error {
	err := filelock.Lock(f)
	if errors.Is(err, filelock.ErrUnsupported) {
		return errors.New("this platform has no file lock; change identity.json on a unix or windows host")
	}
	return err
}

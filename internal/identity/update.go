package identity

import (
	"fmt"
	"os"
	"path/filepath"
)

// lockName is the lock file beside identity.json. Every command that
// changes identity.json holds it from its read to its rename, so two
// that run at once cannot each save a copy that lacks the other's
// change. serve reads identity.json without it: the rename is atomic,
// so a reader always sees one whole file.
const lockName = "identity.lock"

// Update loads dir's identity, lets edit change it, and saves it, all
// under an exclusive lock that waits for any other Update to finish.
// When edit returns an error nothing is saved. A missing identity.json
// is an error that satisfies errors.Is(err, os.ErrNotExist).
func Update(dir string, edit func(*Identity) error) error {
	// The lock file stays after release: removing it would let one
	// process lock a new inode while another still holds the old one.
	f, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	// Closing the file drops the lock.
	defer f.Close()
	if err := lockFile(f); err != nil {
		return fmt.Errorf("identity: locking %s: %w", f.Name(), err)
	}
	id, err := Load(dir)
	if err != nil {
		return err
	}
	if err := edit(id); err != nil {
		return err
	}
	return save(dir, id)
}

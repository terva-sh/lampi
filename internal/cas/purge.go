package cas

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Stored reports whether digest has an object file and whether it has
// a logical index. chunks is what that index reads from, when it
// parses: a chunk list, or the one base a prefix record names.
// A damaged object still counts as stored.
func (s *Store) Stored(digest string) (object, logical bool, chunks []string, err error) {
	p, err := s.Path(digest)
	if err != nil {
		return false, false, nil, err
	}
	if _, err := os.Lstat(p); err == nil {
		object = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, false, nil, fmt.Errorf("cas: %w", err)
	}
	lp, err := s.logicalPath(digest)
	if err != nil {
		return false, false, nil, err
	}
	if _, err := os.Lstat(lp); err == nil {
		logical = true
		if idx, err := s.readLogical(digest); err == nil {
			chunks = idx.ChunkSHA256s
			if idx.PrefixOf != "" {
				chunks = []string{idx.PrefixOf}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, false, nil, fmt.Errorf("cas: %w", err)
	}
	return object, logical, chunks, nil
}

// Remove deletes digest's object, its logical index, and any partial
// upload of it. The chunks an index names are digests of their own. It
// is for purge, with serve stopped. A digest that is not stored is not
// an error.
func (s *Store) Remove(digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.removeObjectLocked(digest); err != nil {
		return err
	}
	if err := s.removeLogicalLocked(digest); err != nil {
		return err
	}
	if err := os.RemoveAll(s.partialDir(digest)); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	return nil
}

// removeLogicalLocked deletes digest's logical index or prefix record,
// if any. The caller holds s.mu.
func (s *Store) removeLogicalLocked(digest string) error {
	lp, err := s.logicalPath(digest)
	if err != nil {
		return err
	}
	if err := os.Remove(lp); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cas: %w", err)
	}
	return syncDir(filepath.Dir(lp))
}

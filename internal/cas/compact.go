package cas

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// These are the store operations serve compact is built from. Compact
// runs under lake.lock, with serve stopped.

// ErrWouldLoop is a Fold whose base reads from the digest being folded.
var ErrWouldLoop = errors.New("cas: the record would loop")

// Terminal is the file digest reads from in the end: digest itself for
// an object or a chunk list, or the end of a prefix record's chain.
func (s *Store) Terminal(digest string) (string, error) {
	ok, err := s.Has(digest)
	if err != nil || ok {
		return digest, err
	}
	idx, err := s.readLogical(digest)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("cas: blob %s is not in the store", digest)
		}
		return "", err
	}
	if idx.PrefixOf == "" {
		return digest, nil
	}
	return s.resolvePrefix(digest, idx)
}

// Fold records digest as the first length bytes of base and removes
// digest's object, if it has one. freed is that object's size. The
// caller has hashed those bytes of base and found digest. A chunk list
// digest had is replaced by the record; its chunks are left for the
// unreferenced sweep. A base that reads from digest is refused: the
// record would loop.
func (s *Store) Fold(digest, base string, length int64) (freed int64, err error) {
	if digest == base || length <= 0 {
		return 0, fmt.Errorf("cas: fold %s into %d bytes of %s: %w", digest, length, base, ErrRejected)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	loops, err := s.readsFromLocked(base, digest)
	if err != nil {
		return 0, err
	}
	if loops {
		return 0, fmt.Errorf("cas: %s reads from %s: %w", base, digest, ErrWouldLoop)
	}
	size, object, err := s.ObjectSize(digest)
	if err != nil {
		return 0, err
	}
	if err := s.writeLogical(digest, logicalIndex{PrefixOf: base, Length: length}); err != nil {
		return 0, err
	}
	if !object {
		return 0, nil
	}
	if err := s.removeObjectLocked(digest); err != nil {
		return 0, err
	}
	return size, nil
}

// readsFromLocked reports whether reading from reaches target: through
// chunk lists and prefix records, any depth. The caller holds s.mu.
func (s *Store) readsFromLocked(from, target string) (bool, error) {
	seen := map[string]bool{}
	queue := []string{from}
	for len(queue) > 0 {
		d := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if d == target {
			return true, nil
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		ok, err := s.Has(d)
		if err != nil {
			return false, err
		}
		if ok {
			continue
		}
		idx, err := s.readLogical(d)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if idx.PrefixOf != "" {
			queue = append(queue, idx.PrefixOf)
		}
		queue = append(queue, idx.ChunkSHA256s...)
	}
	return false, nil
}

// Entry is one file in the store: an object under sha256/, or a chunk
// list or prefix record under logical/.
type Entry struct {
	Digest   string
	Logical  bool
	Size     int64
	Modified time.Time
}

// Entries calls fn for every object, then every logical entry. Temp
// files and paths that are not digests are skipped; fsck reports those.
func (s *Store) Entries(fn func(Entry) error) error {
	for _, sub := range []string{"sha256", "logical"} {
		err := walkEntries(filepath.Join(s.Root, sub), func(path, digest string) error {
			if digest == "" {
				return nil
			}
			st, err := os.Lstat(path)
			if err != nil {
				return err
			}
			return fn(Entry{Digest: digest, Logical: sub == "logical", Size: st.Size(), Modified: st.ModTime()})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// RemoveEntry deletes one object, or one logical entry, and nothing
// else.
func (s *Store) RemoveEntry(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Logical {
		return s.removeLogicalLocked(e.Digest)
	}
	return s.removeObjectLocked(e.Digest)
}

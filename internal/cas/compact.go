package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// These are the store operations serve compact is built from. Compact
// runs under lake.lock, with serve stopped. FoldGrowth also runs at
// ingest, as Grow does.

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
	base, _, err := s.resolvePrefix(digest, idx)
	return base, err
}

// Fold records digest as the first length bytes of base and removes
// digest's object, if it has one. freed is that object's disk size. The
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
	size, object, err := s.StoredSize(digest)
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
// list or prefix record under logical/. Size is the file's disk size.
// Compressed is an object stored as a zstd frame; one that is not was
// installed before compression, and compact re-encodes it.
type Entry struct {
	Digest     string
	Logical    bool
	Compressed bool
	Size       int64
	Modified   time.Time
}

// Entries calls fn for every object, then every logical entry. Temp
// files and paths that are not digests are skipped; fsck reports those.
func (s *Store) Entries(fn func(Entry) error) error {
	for _, sub := range []string{"sha256", "logical"} {
		err := walkEntries(filepath.Join(s.Root, sub), sub == "sha256", func(path, digest string) error {
			if digest == "" {
				return nil
			}
			st, err := os.Lstat(path)
			if err != nil {
				return err
			}
			_, compressed := objectName(filepath.Base(path))
			return fn(Entry{Digest: digest, Logical: sub == "logical", Compressed: sub == "sha256" && compressed, Size: st.Size(), Modified: st.ModTime()})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// RemoveEntry deletes one object, in both its forms, or one logical
// entry, and nothing else.
func (s *Store) RemoveEntry(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Logical {
		return s.removeLogicalLocked(e.Digest)
	}
	return s.removeObjectLocked(e.Digest)
}

// FoldGrowth keeps once the bytes next shares with prev, the version
// it grew from, when next is a chunk list. A file past the object cap
// is sent whole, as chunks: the chunks prev and next share are one
// object already, and prev's last chunk, which next's chunk at the same
// place extends, is recorded as a prefix of it. freed is the size of
// the objects removed.
//
// prev is an object or a chunk list; a prefix record, or a file that is
// not stored, folds nothing. Chunks are compared in order and folding
// stops at the first that next does not extend, so a file that was
// rewritten rather than appended to costs one hash of one chunk.
func (s *Store) FoldGrowth(prev, next string) (freed int64, err error) {
	if prev == next {
		return 0, nil
	}
	old, oldLen, ok, err := s.parts(prev)
	if err != nil || !ok {
		return 0, err
	}
	idx, err := s.readLogical(next)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil || idx.PrefixOf != "" {
		return 0, err
	}
	for i := 0; i < len(old) && i < len(idx.ChunkSHA256s); i++ {
		c := idx.ChunkSHA256s[i]
		if old[i] == c {
			continue
		}
		if oldLen[i] >= idx.ChunkLengths[i] {
			return freed, nil
		}
		sum, err := s.hashPrefix(c, oldLen[i])
		if err != nil {
			return freed, err
		}
		if sum != old[i] {
			return freed, nil
		}
		n, err := s.Fold(old[i], c, oldLen[i])
		if errors.Is(err, ErrWouldLoop) {
			return freed, nil
		}
		if err != nil {
			return freed, err
		}
		freed += n
	}
	return freed, nil
}

// parts is digest as a list of stored pieces: itself when it is an
// object, or its chunks. ok is false for a prefix record or a digest
// not in the store.
func (s *Store) parts(digest string) (parts []string, lengths []int64, ok bool, err error) {
	if size, object, err := s.ObjectSize(digest); err != nil || object {
		return []string{digest}, []int64{size}, object, err
	}
	idx, err := s.readLogical(digest)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, nil
	}
	if err != nil || idx.PrefixOf != "" {
		return nil, nil, false, err
	}
	return idx.ChunkSHA256s, idx.ChunkLengths, true, nil
}

// hashPrefix is the sha256 of the first n bytes of digest, or "" when
// it holds fewer.
func (s *Store) hashPrefix(digest string, n int64) (string, error) {
	rc, err := s.Open(digest)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	got, err := io.Copy(h, io.LimitReader(rc, n))
	if err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	if got != n {
		return "", nil
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

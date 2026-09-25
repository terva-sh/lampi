// Package cas is a content-addressed blob store on the local filesystem.
//
// The object key is sha256/<ab>/<cdef…>, the hex digest split after two
// characters so one directory does not hold every object. A put of a
// digest that is already present is a success and writes nothing: that
// is the whole of dedup layer A. A present object whose size or hash is
// wrong is replaced by the verified new one.
//
// Every install fsyncs the file before the rename and the directory
// after it, so an object that was acknowledged survives a crash.
package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"terva.sh/lampi/internal/protocol"
)

// ErrRejected is a put the client got wrong: the body digest does not
// match the object key, or the body is over the size cap. Every other
// error from Put is a storage failure (mkdir, rename, read, write).
var ErrRejected = errors.New("cas: rejected blob")

// Store keeps blobs under Root.
//
// mu covers install and partial-upload updates. Has and Read stay
// unlocked: a finished object is renamed into place, and a reader
// either sees the old file or the new one.
type Store struct {
	Root string
	mu   sync.Mutex
}

// Open creates the store root. The directory is owner-only: the blobs are
// raw transcripts.
func Open(root string) (*Store, error) {
	if err := mkdirSynced(filepath.Join(root, "sha256")); err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}

// Path returns the filesystem path for a lowercase sha256 hex digest.
func (s *Store) Path(digest string) (string, error) {
	if !protocol.ValidDigest(digest) {
		return "", fmt.Errorf("cas: invalid digest %q", digest)
	}
	return filepath.Join(s.Root, "sha256", digest[:2], digest[2:]), nil
}

// emptyDigest is the sha256 of zero bytes, the only object that may be
// stored empty.
const emptyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// Has reports whether digest is already stored. It does not read the
// object. An empty file under any other digest is reported missing: that
// is what a crash before the data reached disk leaves, and a missing
// answer makes the client put it again, which replaces it. Other damage
// is still reported, and the next put of that digest replaces it.
func (s *Store) Has(digest string) (bool, error) {
	p, err := s.Path(digest)
	if err != nil {
		return false, err
	}
	fi, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fi.Size() == 0 && digest != emptyDigest {
		return false, nil
	}
	return true, nil
}

// Put stores r under digest. The bytes are hashed while they are written
// to a temp file, with no lock held, so a slow body does not stall other
// writers. If the hash does not equal digest, nothing is kept. If the
// object is already present and intact, the new file is discarded and
// exists is true. A present object whose size or hash is wrong is
// replaced by the new file.
//
// limit is the maximum accepted size. A read one byte past limit fails
// before the object is installed. limit <= 0 means no cap.
func (s *Store) Put(digest string, r io.Reader, limit int64) (exists bool, err error) {
	final, err := s.Path(digest)
	if err != nil {
		return false, err
	}
	if err := mkdirSynced(filepath.Dir(final)); err != nil {
		return false, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(final), ".put-*")
	if err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	w := io.MultiWriter(tmp, h)
	var n int64
	if limit > 0 {
		n, err = io.Copy(w, io.LimitReader(r, limit+1))
		if err != nil {
			return false, fmt.Errorf("cas: %w", err)
		}
		if n > limit {
			return false, fmt.Errorf("cas: blob exceeds %d bytes: %w", limit, ErrRejected)
		}
	} else {
		n, err = io.Copy(w, r)
		if err != nil {
			return false, fmt.Errorf("cas: %w", err)
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != digest {
		return false, fmt.Errorf("cas: body sha256 %s does not match %s: %w", sum, digest, ErrRejected)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	exists, err = s.commitFileLocked(digest, tmpName, n)
	if err != nil {
		return false, err
	}
	if !exists {
		// Rename succeeded, so the deferred Remove must not delete the blob.
		tmpName = ""
	}
	return exists, nil
}

// intactLocked reports whether the object for digest is present, is size
// bytes, and hashes to digest. The size check comes first, so the hash
// reads at most size bytes. size < 0 accepts the stored size: every
// object was installed under the blob cap, and damage truncates or
// zero-fills, so the hash is still bounded. The caller holds s.mu.
func (s *Store) intactLocked(digest string, size int64) (bool, error) {
	p, err := s.Path(digest)
	if err != nil {
		return false, err
	}
	st, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	if !st.Mode().IsRegular() || (size >= 0 && st.Size() != size) {
		return false, nil
	}
	sum, err := hashFile(p)
	if err != nil {
		return false, err
	}
	return sum == digest, nil
}

// Open opens digest for reading. A stored object is that file. A logical
// file, whose chunks are stored and whose concatenation is not installed,
// is those chunks in order. The caller closes the reader.
func (s *Store) Open(digest string) (io.ReadCloser, error) {
	ok, err := s.Has(digest)
	if err != nil {
		return nil, err
	}
	if ok {
		return s.OpenBlob(digest)
	}
	idx, err := s.readLogical(digest)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("cas: blob %s is not in the store", digest)
		}
		return nil, err
	}
	return &logicalReader{s: s, parts: idx.ChunkSHA256s}, nil
}

// Read returns the stored bytes for digest. A logical file is the
// concatenation of its chunks. Has is false for that digest: the
// concatenation was not installed.
func (s *Store) Read(digest string) ([]byte, error) {
	f, err := s.Open(digest)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("cas: %w", err)
	}
	return b, nil
}

// OpenBlob opens a stored object for reading.
func (s *Store) OpenBlob(digest string) (*os.File, error) {
	p, err := s.Path(digest)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("cas: %w", err)
	}
	return f, nil
}

// Hash reads r and returns its lowercase sha256 hex digest and byte count.
func Hash(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

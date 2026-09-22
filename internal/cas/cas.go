// Package cas is a content-addressed blob store on the local filesystem.
//
// The object key is sha256/<ab>/<cdef…>, the hex digest split after two
// characters so one directory does not hold every object. A put of a
// digest that is already present is a success and writes nothing: that
// is the whole of dedup layer A.
package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/protocol"
)

// Store keeps blobs under Root.
type Store struct {
	Root string
}

// Open creates the store root. The directory is owner-only: the blobs are
// raw transcripts.
func Open(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, "sha256"), 0o700); err != nil {
		return nil, fmt.Errorf("cas: %w", err)
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

// Has reports whether digest is already stored.
func (s *Store) Has(digest string) (bool, error) {
	p, err := s.Path(digest)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Put stores r under digest. The bytes are hashed while they are written.
// If the hash does not equal digest, nothing is kept. If the object is
// already present, the new file is discarded and exists is true.
//
// limit is the maximum accepted size. A read one byte past limit fails
// before the object is installed. limit <= 0 means no cap.
func (s *Store) Put(digest string, r io.Reader, limit int64) (exists bool, err error) {
	final, err := s.Path(digest)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return false, fmt.Errorf("cas: %w", err)
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
			return false, fmt.Errorf("cas: blob exceeds %d bytes", limit)
		}
	} else {
		n, err = io.Copy(w, r)
		if err != nil {
			return false, fmt.Errorf("cas: %w", err)
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != digest {
		return false, fmt.Errorf("cas: body sha256 %s does not match %s", sum, digest)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}

	if _, err := os.Stat(final); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.Rename(tmpName, final); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	// Rename succeeded, so the deferred Remove must not delete the blob.
	tmpName = ""
	_ = n
	return false, nil
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

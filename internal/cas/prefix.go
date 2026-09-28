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

// A transcript grows by appending, and every version the lake has seen
// stays readable under its own digest. Storing each version whole costs
// the square of the file's length. A prefix record stores a version as
// the first Length bytes of the version it grew into, PrefixOf, so the
// shared bytes are kept once.
//
// Records form chains: a version is superseded when its successor
// arrives, and the record that pointed at it is left pointing at a
// record. Each link is strictly longer than the one before, which is
// what makes a chain end.

// ErrNotGrown is a Grow whose stored prefix and tail do not make the
// digest: the prefix is not the bytes the client grew from, or the tail
// is not the rest.
var ErrNotGrown = errors.New("cas: not a growth of the stored prefix")

// resolvePrefix follows digest's prefix record, idx, to the first file
// in its chain that is not a prefix record: an object or a chunk list.
func (s *Store) resolvePrefix(digest string, idx logicalIndex) (string, error) {
	base, length := idx.PrefixOf, idx.Length
	seen := map[string]bool{digest: true}
	for {
		if seen[base] {
			return "", fmt.Errorf("cas: prefix records from %s loop at %s", digest, base)
		}
		seen[base] = true
		ok, err := s.Has(base)
		if err != nil {
			return "", err
		}
		if ok {
			return base, nil
		}
		next, err := s.readLogical(base)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("cas: %s is a prefix of %s, which is not in the store", digest, base)
			}
			return "", err
		}
		if next.PrefixOf == "" {
			return base, nil
		}
		if next.Length <= length {
			return "", fmt.Errorf("cas: %s is a prefix of %s, which is not longer", digest, base)
		}
		base = next.PrefixOf
	}
}

// PrefixOf reports digest's prefix record: the file it is the first
// length bytes of. ok is false when digest is not a prefix record.
func (s *Store) PrefixOf(digest string) (base string, length int64, ok bool, err error) {
	idx, err := s.readLogical(digest)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	if idx.PrefixOf == "" {
		return "", 0, false, nil
	}
	return idx.PrefixOf, idx.Length, true, nil
}

// Present reports whether digest can be read: an object, or a prefix
// record whose chain reaches a file that holds at least its length. It
// is what the lake answers a client asking whether a blob is stored, so
// a version that grew is not sent again. A chunk list is not counted,
// as Has does not count it.
//
// Sizes are checked and bytes are not, as Has does for an object: a
// base that was truncated, or a chunk list missing a chunk, makes the
// record missing, and the client's put restores the version.
func (s *Store) Present(digest string) (bool, error) {
	ok, err := s.Has(digest)
	if err != nil || ok {
		return ok, err
	}
	idx, err := s.readLogical(digest)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || idx.PrefixOf == "" {
		// A record that does not parse is missing: the put that
		// follows installs the object, which Open prefers.
		return false, nil
	}
	base, err := s.resolvePrefix(digest, idx)
	if err != nil {
		return false, nil
	}
	return s.holds(base, idx.Length)
}

// holds reports whether base, an object or a chunk list, has at least
// length bytes on disk: the object's size, or every chunk stored at its
// recorded length.
func (s *Store) holds(base string, length int64) (bool, error) {
	if size, object, err := s.ObjectSize(base); err != nil || object {
		return object && size >= length, err
	}
	idx, err := s.readLogical(base)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || idx.PrefixOf != "" {
		return false, nil
	}
	var total int64
	for i, c := range idx.ChunkSHA256s {
		size, object, err := s.ObjectSize(c)
		if err != nil {
			return false, err
		}
		if !object || size != idx.ChunkLengths[i] {
			return false, nil
		}
		total += size
	}
	return total >= length, nil
}

// ObjectSize is the size of digest's object file, and false when it has
// none.
func (s *Store) ObjectSize(digest string) (int64, bool, error) {
	p, err := s.Path(digest)
	if err != nil {
		return 0, false, err
	}
	st, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("cas: %w", err)
	}
	return st.Size(), true, nil
}

// prefixReader is the first left bytes of rc. A base that ends early is
// an error, not a short file.
type prefixReader struct {
	rc     io.ReadCloser
	digest string
	left   int64
}

func (r *prefixReader) Close() error { return r.rc.Close() }

func (r *prefixReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.left {
		p = p[:r.left]
	}
	n, err := r.rc.Read(p)
	r.left -= int64(n)
	if err == io.EOF {
		if r.left > 0 {
			return n, fmt.Errorf("cas: %s: its base ends %d bytes early: %w", r.digest, r.left, io.ErrUnexpectedEOF)
		}
		err = nil
	}
	return n, err
}

// Grow installs digest as the first prefixSize bytes of prefix followed
// by tail, then records prefix as the first prefixSize bytes of digest
// and removes prefix's own object. Both are read once, as one stream,
// and each is hashed: a prefix that is not prefixSize bytes or does not
// hash to its digest, or a result that does not hash to digest, is
// ErrNotGrown and nothing is kept. An intact digest already stored is
// kept, and prefix is still recorded against it.
//
// limit caps the grown length. limit <= 0 means no cap.
func (s *Store) Grow(digest, prefix string, prefixSize int64, tail string, limit int64) (exists bool, err error) {
	for _, d := range []string{digest, prefix, tail} {
		if !protocol.ValidDigest(d) {
			return false, fmt.Errorf("cas: invalid digest %q: %w", d, ErrRejected)
		}
	}
	if prefixSize <= 0 || digest == prefix {
		return false, fmt.Errorf("cas: grow %s from %d bytes of %s: %w", digest, prefixSize, prefix, ErrRejected)
	}
	if limit > 0 && prefixSize >= limit {
		return false, fmt.Errorf("cas: blob exceeds %d bytes: %w", limit, ErrRejected)
	}
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

	whole, head := sha256.New(), sha256.New()
	w := io.MultiWriter(tmp, whole)
	pr, err := s.Open(prefix)
	if err != nil {
		return false, err
	}
	n, err := io.Copy(io.MultiWriter(w, head), io.LimitReader(pr, prefixSize))
	if err == nil && n == prefixSize {
		// A stored prefix longer than its catalog size is not the file
		// the client grew from.
		var one [1]byte
		if m, _ := pr.Read(one[:]); m > 0 {
			n++
		}
	}
	pr.Close()
	if err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	if n != prefixSize || hex.EncodeToString(head.Sum(nil)) != prefix {
		return false, fmt.Errorf("cas: %s is not the %d bytes grown from: %w", prefix, prefixSize, ErrNotGrown)
	}
	tr, err := s.Open(tail)
	if err != nil {
		return false, err
	}
	var rest int64
	if limit > 0 {
		rest, err = io.Copy(w, io.LimitReader(tr, limit-prefixSize+1))
	} else {
		rest, err = io.Copy(w, tr)
	}
	tr.Close()
	if err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	total := prefixSize + rest
	if limit > 0 && total > limit {
		return false, fmt.Errorf("cas: blob exceeds %d bytes: %w", limit, ErrRejected)
	}
	if rest == 0 || hex.EncodeToString(whole.Sum(nil)) != digest {
		return false, fmt.Errorf("cas: %s followed by %s is not %s: %w", prefix, tail, digest, ErrNotGrown)
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
	exists, err = s.commitFileLocked(digest, tmpName, total)
	if err != nil {
		return false, err
	}
	if !exists {
		tmpName = ""
	}
	return exists, s.supersedeLocked(prefix, digest, prefixSize)
}

// supersedeLocked records digest as the first length bytes of base and
// removes digest's own object. The caller has checked that those bytes
// hash to digest and that base is stored. The caller holds s.mu.
//
// The record is durable before the object goes. A reader that opened
// the object keeps reading it. Where the remove fails, as it does on
// Windows while a reader holds the file, the object stays: Open prefers
// it, and compact removes it later.
func (s *Store) supersedeLocked(digest, base string, length int64) error {
	if err := s.writeLogical(digest, logicalIndex{PrefixOf: base, Length: length}); err != nil {
		return err
	}
	_ = s.removeObjectLocked(digest)
	return nil
}

// Materialize installs digest's bytes as its own object and removes its
// prefix record, so it no longer reads from its base. It is for purge,
// which removes a base that another session's version still reads. The
// bytes are hashed on the way; a record that does not read as digest is
// an error and nothing changes.
func (s *Store) Materialize(digest string) error {
	if _, _, ok, err := s.PrefixOf(digest); err != nil || !ok {
		return err
	}
	final, err := s.Path(digest)
	if err != nil {
		return err
	}
	if err := mkdirSynced(filepath.Dir(final)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(final), ".put-*")
	if err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	rc, err := s.Open(digest)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), rc)
	rc.Close()
	if err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if sum := hex.EncodeToString(h.Sum(nil)); sum != digest {
		return fmt.Errorf("cas: prefix record %s reads as %s", digest, sum)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exists, err := s.commitFileLocked(digest, tmpName, n)
	if err != nil {
		return err
	}
	if !exists {
		tmpName = ""
	}
	return s.removeLogicalLocked(digest)
}

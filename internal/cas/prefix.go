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
// Each link must be longer than the one before it. last is the length
// of the final record, which that file has to hold.
func (s *Store) resolvePrefix(digest string, idx logicalIndex) (base string, last int64, err error) {
	base, last = idx.PrefixOf, idx.Length
	seen := map[string]bool{digest: true}
	for {
		if seen[base] {
			return "", 0, fmt.Errorf("cas: prefix records from %s loop at %s", digest, base)
		}
		seen[base] = true
		ok, err := s.Has(base)
		if err != nil {
			return "", 0, err
		}
		if ok {
			return base, last, nil
		}
		next, err := s.readLogical(base)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", 0, fmt.Errorf("cas: %s is a prefix of %s, which is not in the store", digest, base)
			}
			return "", 0, err
		}
		if next.PrefixOf == "" {
			return base, last, nil
		}
		if next.Length <= last {
			return "", 0, fmt.Errorf("cas: %s is a prefix of %s, which is not longer", digest, base)
		}
		base, last = next.PrefixOf, next.Length
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
	return s.checkRecord(digest, idx, 0) == nil, nil
}

// checkRecord reports why digest's prefix record, idx, cannot supply
// its length: a chain that does not resolve, or a file at its end that
// holds fewer bytes. Sizes are checked and bytes are not.
func (s *Store) checkRecord(digest string, idx logicalIndex, depth int) error {
	base, last, err := s.resolvePrefix(digest, idx)
	if err != nil {
		return err
	}
	have, err := s.heldBytes(base, depth+1)
	if err != nil {
		return err
	}
	if have < last {
		return fmt.Errorf("cas: a record in the chain from %s is %d bytes of %s, which holds %d", digest, last, base, have)
	}
	return nil
}

// heldBytes is how many bytes base, an object or a chunk list, can
// supply: the object's size, or the sum of a chunk list whose every
// chunk reads at its recorded length. A chunk may itself be a prefix
// record.
func (s *Store) heldBytes(base string, depth int) (int64, error) {
	if depth > maxNesting {
		return 0, fmt.Errorf("cas: %s nests more than %d records deep", base, maxNesting)
	}
	if size, object, err := s.ObjectSize(base); err != nil || object {
		return size, err
	}
	idx, err := s.readLogical(base)
	if errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("cas: blob %s is not in the store", base)
	}
	if err != nil {
		return 0, err
	}
	if idx.PrefixOf != "" {
		if err := s.checkRecord(base, idx, depth); err != nil {
			return 0, err
		}
		return idx.Length, nil
	}
	var total int64
	for i, c := range idx.ChunkSHA256s {
		n, err := s.heldBytes(c, depth+1)
		if err != nil {
			return 0, err
		}
		if n != idx.ChunkLengths[i] {
			return 0, fmt.Errorf("cas: chunk %s of %s holds %d bytes, index says %d", c, base, n, idx.ChunkLengths[i])
		}
		total += n
	}
	return total, nil
}

// ObjectSize is the number of bytes digest's object holds, once
// decompressed, and false when it has none. A frame whose header does
// not decode reports errDamaged.
func (s *Store) ObjectSize(digest string) (int64, bool, error) {
	o, ok, err := s.object(digest)
	if err != nil || !ok {
		return 0, false, err
	}
	n, err := o.logicalSize()
	// Grow removes a superseded object with no lock held here, so one
	// found a moment ago may be gone. It has no object now.
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, true, err
	}
	return n, true, nil
}

// StoredSize is the disk size of digest's object files, the frame and
// any raw copy beside it, and false when it has neither. It is what
// removing the object frees.
func (s *Store) StoredSize(digest string) (int64, bool, error) {
	raw, err := s.Path(digest)
	if err != nil {
		return 0, false, err
	}
	var size int64
	found := false
	for _, p := range []string{raw + zstSuffix, raw} {
		st, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, false, fmt.Errorf("cas: %w", err)
		}
		size += st.Size()
		found = true
	}
	return size, found, nil
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
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	// The grown file is one frame, so the bytes it shares with every
	// earlier version compress together rather than as small tails.
	z, err := s.seal(tmpName, digest, total)
	if err != nil {
		return false, err
	}
	defer func() {
		if z != "" {
			os.Remove(z)
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()
	exists, err = s.commitFileLocked(digest, z, total)
	if err != nil {
		return false, err
	}
	if !exists {
		z = ""
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	z, err := s.seal(tmpName, digest, n)
	if err != nil {
		return err
	}
	defer func() {
		if z != "" {
			os.Remove(z)
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	exists, err := s.commitFileLocked(digest, z, n)
	if err != nil {
		return err
	}
	if !exists {
		z = ""
	}
	return s.removeLogicalLocked(digest)
}

// GrowParts is Grow for a file past the object cap, which is stored as a
// list of pieces rather than one object. prev is the version grown
// from: an object or a chunk list. It returns the pieces of the grown
// file: prev's pieces, with the last one extended by tail up to limit
// and the rest of tail after it. Only the last piece and the tail are
// read. The last piece is hashed on the way and must be the bytes its
// digest names, or it is ErrNotGrown. A last piece that is extended is
// kept as a prefix record of its extension, so its bytes are stored
// once.
//
// The caller binds the grown digest to the pieces, which checks the
// whole file's hash. A tail that is not the rest of that file leaves
// pieces nothing names, for compact to remove.
func (s *Store) GrowParts(prev, tail string, limit int64) (parts []string, lengths []int64, err error) {
	if limit <= 0 {
		return nil, nil, fmt.Errorf("cas: grow %s: no piece limit: %w", prev, ErrRejected)
	}
	old, oldLen, ok, err := s.parts(prev)
	if err != nil {
		return nil, nil, err
	}
	if !ok || len(old) == 0 {
		return nil, nil, fmt.Errorf("cas: %s is not an object or a chunk list: %w", prev, ErrNotGrown)
	}
	tailLen, err := s.Size(tail)
	if err != nil {
		return nil, nil, err
	}
	if tailLen <= 0 || tailLen > limit {
		return nil, nil, fmt.Errorf("cas: tail %s is %d bytes, not 1..%d: %w", tail, tailLen, limit, ErrRejected)
	}
	n := len(old) - 1
	last, lastLen := old[n], oldLen[n]
	if lastLen >= limit {
		// A full last piece stays as it is; the tail is a piece of its own.
		return append(append([]string(nil), old...), tail), append(append([]int64(nil), oldLen...), tailLen), nil
	}

	// Temp files sit beside the objects, as a put's do, so a crash
	// leaves them where the start-up sweep and fsck expect them.
	final, err := s.Path(last)
	if err != nil {
		return nil, nil, err
	}
	dir := filepath.Dir(final)
	if err := mkdirSynced(dir); err != nil {
		return nil, nil, err
	}
	type piece struct {
		tmp  string
		sum  string
		size int64
	}
	var made []piece
	defer func() {
		for _, p := range made {
			if p.tmp != "" {
				os.Remove(p.tmp)
			}
		}
	}()
	// write copies r into a new temp file, compresses it beside its
	// object, and records it as a piece.
	write := func(r io.Reader) error {
		f, err := os.CreateTemp(dir, ".put-*")
		if err != nil {
			return fmt.Errorf("cas: %w", err)
		}
		raw := f.Name()
		defer os.Remove(raw)
		made = append(made, piece{})
		h := sha256.New()
		size, err := io.Copy(io.MultiWriter(f, h), r)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("cas: %w", err)
		}
		sum := hex.EncodeToString(h.Sum(nil))
		z, err := s.seal(raw, sum, size)
		if err != nil {
			return err
		}
		made[len(made)-1] = piece{tmp: z, sum: sum, size: size}
		return nil
	}

	lr, err := s.Open(last)
	if err != nil {
		return nil, nil, err
	}
	defer lr.Close()
	tr, err := s.Open(tail)
	if err != nil {
		return nil, nil, err
	}
	defer tr.Close()
	head := sha256.New()
	fill := limit - lastLen
	first := io.MultiReader(io.TeeReader(io.LimitReader(lr, lastLen), head), io.LimitReader(tr, fill))
	if err := write(first); err != nil {
		return nil, nil, err
	}
	if hex.EncodeToString(head.Sum(nil)) != last || made[0].size < lastLen {
		return nil, nil, fmt.Errorf("cas: %s is not the %d bytes grown from: %w", last, lastLen, ErrNotGrown)
	}
	if tailLen > fill {
		if err := write(tr); err != nil {
			return nil, nil, err
		}
	}
	var wrote int64
	for _, p := range made {
		wrote += p.size
	}
	if wrote-lastLen != tailLen {
		return nil, nil, fmt.Errorf("cas: tail %s read %d bytes of %d: %w", tail, wrote-lastLen, tailLen, ErrRejected)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	parts = append([]string(nil), old[:n]...)
	lengths = append([]int64(nil), oldLen[:n]...)
	for i := range made {
		p := &made[i]
		if p.size == 0 {
			continue
		}
		exists, err := s.commitFileLocked(p.sum, p.tmp, p.size)
		if err != nil {
			return nil, nil, err
		}
		if !exists {
			p.tmp = ""
		}
		parts = append(parts, p.sum)
		lengths = append(lengths, p.size)
	}
	if made[0].sum != last {
		if err := s.supersedeLocked(last, made[0].sum, lastLen); err != nil {
			return nil, nil, err
		}
	}
	return parts, lengths, nil
}

package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/protocol"
)

// logicalIndex is the on-disk record of a file whose chunks are stored
// and whose concatenation is not installed as one object. The digest of
// that concatenation is the index's name, not a blob key.
type logicalIndex struct {
	ChunkSHA256s []string `json:"chunk_sha256s"`
	ChunkLengths []int64  `json:"chunk_lengths"`
}

// BindLogical records digest as the concatenation of parts without
// installing that concatenation. parts are digests already in the store,
// in order. lengths[i] is the size of parts[i]. The hash of the
// concatenation must be digest.
//
// A digest that is already installed as a single intact object is left
// untouched and the parts are not read. exists is then true. A damaged
// object is removed once the parts verify, so Read opens the chunks. A
// logical file has no such object: Has stays false, and Read opens the
// chunks.
func (s *Store) BindLogical(digest string, parts []string, lengths []int64) (exists bool, err error) {
	if !protocol.ValidDigest(digest) {
		return false, fmt.Errorf("cas: invalid digest %q: %w", digest, ErrRejected)
	}
	if len(parts) == 0 {
		return false, fmt.Errorf("cas: chunk list is empty: %w", ErrRejected)
	}
	if len(lengths) != len(parts) {
		return false, fmt.Errorf("cas: chunk_lengths does not match chunk_sha256s: %w", ErrRejected)
	}
	for _, p := range parts {
		if !protocol.ValidDigest(p) {
			return false, fmt.Errorf("cas: invalid chunk digest %q: %w", p, ErrRejected)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ok, err := s.intactLocked(digest, -1)
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}

	for i, p := range parts {
		if lengths[i] <= 0 {
			return false, fmt.Errorf("cas: chunk %d is empty: %w", i, ErrRejected)
		}
		f, openErr := s.OpenBlob(p)
		if openErr != nil {
			return false, openErr
		}
		st, statErr := f.Stat()
		f.Close()
		if statErr != nil {
			return false, fmt.Errorf("cas: %w", statErr)
		}
		if st.Size() != lengths[i] {
			return false, fmt.Errorf("cas: chunk %d is %d bytes, index says %d: %w", i, st.Size(), lengths[i], ErrRejected)
		}
	}

	h := sha256.New()
	for _, p := range parts {
		f, openErr := s.OpenBlob(p)
		if openErr != nil {
			return false, openErr
		}
		_, copyErr := io.Copy(h, f)
		f.Close()
		if copyErr != nil {
			return false, fmt.Errorf("cas: %w", copyErr)
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != digest {
		return false, fmt.Errorf("cas: assembled sha256 %s does not match %s: %w", sum, digest, ErrRejected)
	}

	if err := s.removeObjectLocked(digest); err != nil {
		return false, err
	}

	idx := logicalIndex{
		ChunkSHA256s: append([]string(nil), parts...),
		ChunkLengths: append([]int64(nil), lengths...),
	}
	if prev, readErr := s.readLogical(digest); readErr == nil && sameLogical(prev, idx) {
		return true, nil
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return false, readErr
	}
	if err := s.writeLogical(digest, idx); err != nil {
		return false, err
	}
	return false, nil
}

// removeObjectLocked deletes the installed object for digest, if any. The
// caller has found it damaged and holds s.mu.
func (s *Store) removeObjectLocked(digest string) error {
	p, err := s.Path(digest)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cas: %w", err)
	}
	return syncDir(filepath.Dir(p))
}

func sameLogical(a, b logicalIndex) bool {
	if len(a.ChunkSHA256s) != len(b.ChunkSHA256s) || len(a.ChunkLengths) != len(b.ChunkLengths) {
		return false
	}
	for i := range a.ChunkSHA256s {
		if a.ChunkSHA256s[i] != b.ChunkSHA256s[i] || a.ChunkLengths[i] != b.ChunkLengths[i] {
			return false
		}
	}
	return true
}

func (s *Store) logicalPath(digest string) (string, error) {
	if !protocol.ValidDigest(digest) {
		return "", fmt.Errorf("cas: invalid digest %q", digest)
	}
	return filepath.Join(s.Root, "logical", digest[:2], digest[2:]), nil
}

func (s *Store) readLogical(digest string) (logicalIndex, error) {
	p, err := s.logicalPath(digest)
	if err != nil {
		return logicalIndex{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return logicalIndex{}, err
		}
		return logicalIndex{}, fmt.Errorf("cas: %w", err)
	}
	var idx logicalIndex
	if err := json.Unmarshal(b, &idx); err != nil {
		return logicalIndex{}, fmt.Errorf("cas: logical index: %w", err)
	}
	if len(idx.ChunkSHA256s) == 0 || len(idx.ChunkSHA256s) != len(idx.ChunkLengths) {
		return logicalIndex{}, fmt.Errorf("cas: logical index for %s is incomplete", digest)
	}
	return idx, nil
}

func (s *Store) writeLogical(digest string, idx logicalIndex) error {
	p, err := s.logicalPath(digest)
	if err != nil {
		return err
	}
	if err := mkdirSynced(filepath.Dir(p)); err != nil {
		return err
	}
	b, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(p), ".logical-*")
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
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	tmpName = ""
	return syncDir(filepath.Dir(p))
}

// logicalReader reads chunks in order, one open file at a time.
type logicalReader struct {
	s     *Store
	parts []string
	i     int
	cur   *os.File
}

func (r *logicalReader) Close() error {
	if r.cur == nil {
		return nil
	}
	err := r.cur.Close()
	r.cur = nil
	return err
}

func (r *logicalReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if r.cur == nil {
			if r.i >= len(r.parts) {
				return 0, io.EOF
			}
			f, err := r.s.OpenBlob(r.parts[r.i])
			r.i++
			if err != nil {
				return 0, err
			}
			r.cur = f
		}
		n, err := r.cur.Read(p)
		if n > 0 {
			if err == io.EOF {
				r.cur.Close()
				r.cur = nil
				return n, nil
			}
			return n, err
		}
		if err == io.EOF {
			r.cur.Close()
			r.cur = nil
			continue
		}
		if err != nil {
			return 0, err
		}
	}
}

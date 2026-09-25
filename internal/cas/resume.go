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
	"sort"

	"terva.sh/lampi/internal/protocol"
)

// partialMeta is the on-disk record of a Content-Range upload that has
// not yet covered the object. Spans are inclusive byte offsets.
type partialMeta struct {
	Total int64      `json:"total"`
	Spans [][2]int64 `json:"spans"`
}

// PutRange writes the inclusive range [start, end] of an object whose
// final size is total. The object is installed only when the stored
// ranges cover every byte and hash to digest. A digest that is already
// installed and intact is left untouched: exists and complete are both
// true, and the body is not written. A damaged object is replaced once
// the ranges cover it.
//
// limit caps total. limit <= 0 means no cap. A range is rejected when
// its length does not match the reader.
func (s *Store) PutRange(digest string, start, end, total, limit int64, r io.Reader) (exists, complete bool, err error) {
	if start < 0 || end < start || total <= 0 || end >= total {
		return false, false, fmt.Errorf("cas: content-range %d-%d/%d: %w", start, end, total, ErrRejected)
	}
	if limit > 0 && total > limit {
		return false, false, fmt.Errorf("cas: blob exceeds %d bytes: %w", limit, ErrRejected)
	}
	want := end - start + 1
	buf, err := io.ReadAll(io.LimitReader(r, want+1))
	if err != nil {
		return false, false, fmt.Errorf("cas: %w", err)
	}
	if int64(len(buf)) != want {
		return false, false, fmt.Errorf("cas: content-range length %d, want %d: %w", len(buf), want, ErrRejected)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ok, err := s.intactLocked(digest, -1)
	if err != nil {
		return false, false, err
	}
	if ok {
		// The object is already the digest. Drop a leftover partial so
		// it is not a second copy, and do not write this range.
		_ = os.RemoveAll(s.partialDir(digest))
		return true, true, nil
	}

	dir := s.partialDir(digest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, false, fmt.Errorf("cas: %w", err)
	}
	meta, err := readPartial(dir)
	if err != nil {
		return false, false, err
	}
	if meta.Total == 0 {
		meta.Total = total
	} else if meta.Total != total {
		return false, false, fmt.Errorf("cas: content-range total %d does not match %d: %w", total, meta.Total, ErrRejected)
	}

	dataPath := filepath.Join(dir, "data")
	f, err := os.OpenFile(dataPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, false, fmt.Errorf("cas: %w", err)
	}
	if err := f.Truncate(total); err != nil {
		f.Close()
		return false, false, fmt.Errorf("cas: %w", err)
	}
	if _, err := f.WriteAt(buf, start); err != nil {
		f.Close()
		return false, false, fmt.Errorf("cas: %w", err)
	}
	// The data reaches the disk before meta.json claims the span.
	if err := f.Sync(); err != nil {
		f.Close()
		return false, false, fmt.Errorf("cas: %w", err)
	}
	if err := f.Close(); err != nil {
		return false, false, fmt.Errorf("cas: %w", err)
	}

	meta.Spans = addSpan(meta.Spans, start, end)
	if err := writePartial(dir, meta); err != nil {
		return false, false, err
	}
	if !covered(meta.Spans, total) {
		return false, false, nil
	}

	exists, err = s.installCoveredLocked(digest, dataPath, total)
	if err != nil {
		// The ranges already cover the object. Leaving them in place
		// makes every later range fail the same hash check. Drop the
		// partial so the client can send the bytes again. A rename that
		// already succeeded has moved data out of dir, so this does not
		// delete an installed blob.
		_ = os.RemoveAll(dir)
		return false, false, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return exists, true, fmt.Errorf("cas: %w", err)
	}
	return exists, true, nil
}

// installCoveredLocked hashes a fully covered partial and moves it onto
// the object path. The caller holds s.mu and deletes dir if this returns
// an error.
func (s *Store) installCoveredLocked(digest, dataPath string, size int64) (bool, error) {
	sum, err := hashFile(dataPath)
	if err != nil {
		return false, err
	}
	if sum != digest {
		return false, fmt.Errorf("cas: assembled sha256 %s does not match %s: %w", sum, digest, ErrRejected)
	}
	if err := os.Chmod(dataPath, 0o600); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	if err := syncFile(dataPath); err != nil {
		return false, err
	}
	return s.commitFileLocked(digest, dataPath, size)
}

// Concat installs digest as the concatenation of parts, which are
// digests already in the store, in order. A digest that is already
// installed and intact is left untouched and the parts are not read. A
// damaged object is rebuilt from the parts.
//
// limit caps the assembled length. limit <= 0 means no cap. A sum of
// part sizes past limit is rejected and nothing is installed.
func (s *Store) Concat(digest string, parts []string, limit int64) (exists bool, err error) {
	if len(parts) == 0 {
		return false, fmt.Errorf("cas: chunk list is empty: %w", ErrRejected)
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
		_ = os.RemoveAll(s.partialDir(digest))
		return true, nil
	}

	var total int64
	for _, p := range parts {
		f, err := s.OpenBlob(p)
		if err != nil {
			return false, err
		}
		st, statErr := f.Stat()
		f.Close()
		if statErr != nil {
			return false, fmt.Errorf("cas: %w", statErr)
		}
		size := st.Size()
		if limit > 0 && (size < 0 || size > limit-total) {
			return false, fmt.Errorf("cas: blob exceeds %d bytes: %w", limit, ErrRejected)
		}
		total += size
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

	h := sha256.New()
	w := io.MultiWriter(tmp, h)
	for _, p := range parts {
		f, err := s.OpenBlob(p)
		if err != nil {
			return false, err
		}
		_, copyErr := io.Copy(w, f)
		f.Close()
		if copyErr != nil {
			return false, fmt.Errorf("cas: %w", copyErr)
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != digest {
		return false, fmt.Errorf("cas: assembled sha256 %s does not match %s: %w", sum, digest, ErrRejected)
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
	exists, err = s.commitFileLocked(digest, tmpName, total)
	if err != nil {
		return false, err
	}
	if !exists {
		tmpName = ""
	}
	return exists, nil
}

// commitFileLocked moves src, a synced file of size bytes that hashes to
// digest, onto the object path unless an intact object is already there.
// A present object with the wrong size or hash is replaced. The rename is
// flushed to the directory before this returns. The caller holds s.mu.
// On exists, src is left for the caller to delete.
func (s *Store) commitFileLocked(digest, src string, size int64) (exists bool, err error) {
	final, err := s.Path(digest)
	if err != nil {
		return false, err
	}
	if err := mkdirSynced(filepath.Dir(final)); err != nil {
		return false, err
	}
	ok, err := s.intactLocked(digest, size)
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	if err := os.Rename(src, final); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	if err := syncDir(filepath.Dir(final)); err != nil {
		return false, err
	}
	return false, nil
}

func (s *Store) partialDir(digest string) string {
	if !protocol.ValidDigest(digest) {
		return filepath.Join(s.Root, "partial", "invalid")
	}
	return filepath.Join(s.Root, "partial", digest[:2], digest[2:])
}

func readPartial(dir string) (partialMeta, error) {
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if errors.Is(err, os.ErrNotExist) {
		return partialMeta{}, nil
	}
	if err != nil {
		return partialMeta{}, fmt.Errorf("cas: %w", err)
	}
	var meta partialMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return partialMeta{}, fmt.Errorf("cas: partial meta: %w", err)
	}
	return meta, nil
}

func writePartial(dir string, meta partialMeta) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, ".meta-*")
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
	if err := os.Rename(tmpName, filepath.Join(dir, "meta.json")); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	tmpName = ""
	// The span is claimed once the rename is in the directory.
	return syncDir(dir)
}

func addSpan(spans [][2]int64, start, end int64) [][2]int64 {
	spans = append(spans, [2]int64{start, end})
	sort.Slice(spans, func(i, j int) bool {
		if spans[i][0] == spans[j][0] {
			return spans[i][1] < spans[j][1]
		}
		return spans[i][0] < spans[j][0]
	})
	out := [][2]int64{spans[0]}
	for _, sp := range spans[1:] {
		last := &out[len(out)-1]
		if sp[0] <= last[1]+1 {
			if sp[1] > last[1] {
				last[1] = sp[1]
			}
			continue
		}
		out = append(out, sp)
	}
	return out
}

func covered(spans [][2]int64, total int64) bool {
	return len(spans) == 1 && spans[0][0] == 0 && spans[0][1] == total-1
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

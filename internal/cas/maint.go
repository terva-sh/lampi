package cas

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// tempFile reports a file an install writes before its rename: a put
// or concat (.put-*), a logical index (.logical-*), or partial meta
// (.meta-*). A crash can leave one behind. It is never an object.
func tempFile(name string) bool {
	return strings.HasPrefix(name, ".put-") || strings.HasPrefix(name, ".logical-") || strings.HasPrefix(name, ".meta-")
}

// Sweep removes temp files last written before cutoff, and partial
// uploads whose files were all last written before cutoff. It is for
// start-up, before any request: a put in flight has a fresh temp file,
// and an upload a client is still resuming has a fresh span. removed
// counts files and partial uploads.
func (s *Store) Sweep(cutoff time.Time) (removed int, err error) {
	for _, sub := range []string{"sha256", "logical"} {
		n, err := sweepTemps(filepath.Join(s.Root, sub), cutoff)
		removed += n
		if err != nil {
			return removed, err
		}
	}
	shards, err := os.ReadDir(filepath.Join(s.Root, "partial"))
	if errors.Is(err, os.ErrNotExist) {
		return removed, nil
	}
	if err != nil {
		return removed, fmt.Errorf("cas: %w", err)
	}
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}
		shardDir := filepath.Join(s.Root, "partial", shard.Name())
		uploads, err := os.ReadDir(shardDir)
		if err != nil {
			return removed, fmt.Errorf("cas: %w", err)
		}
		for _, up := range uploads {
			dir := filepath.Join(shardDir, up.Name())
			newest, err := newestModTime(dir)
			if err != nil {
				return removed, err
			}
			if !newest.Before(cutoff) {
				continue
			}
			if err := os.RemoveAll(dir); err != nil {
				return removed, fmt.Errorf("cas: %w", err)
			}
			removed++
		}
	}
	return removed, nil
}

func sweepTemps(root string, cutoff time.Time) (int, error) {
	removed := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !tempFile(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed++
		return nil
	})
	if err != nil {
		return removed, fmt.Errorf("cas: %w", err)
	}
	return removed, nil
}

func newestModTime(path string) (time.Time, error) {
	var newest time.Time
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// A directory's time moves when a temp file comes and goes.
		// The files say when bytes last arrived.
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("cas: %w", err)
	}
	return newest, nil
}

// Problem is one damaged entry fsck found. Logical is false for an
// object under sha256/ and true for an index under logical/.
type Problem struct {
	Digest  string
	Logical bool
	Reason  string
}

func (p Problem) String() string {
	if p.Logical {
		return "logical " + p.Digest + ": " + p.Reason
	}
	return "object " + p.Digest + ": " + p.Reason
}

// Verify re-hashes every object and reads every logical index. Each
// object whose bytes do not hash to its name, and each index that does
// not parse or names a chunk that is not stored, is passed to bad.
// Temp files are skipped. A file whose path is not a digest is a
// problem too, named by its path under the store. checked counts the
// entries read.
func (s *Store) Verify(bad func(Problem)) (checked int, err error) {
	err = walkEntries(filepath.Join(s.Root, "sha256"), func(path, digest string) error {
		checked++
		if digest == "" {
			bad(Problem{Digest: s.rel(path), Reason: "not a digest path"})
			return nil
		}
		sum, err := hashFile(path)
		if err != nil {
			return err
		}
		if sum != digest {
			bad(Problem{Digest: digest, Reason: "bytes hash to " + sum})
		}
		return nil
	})
	if err != nil {
		return checked, err
	}
	err = walkEntries(filepath.Join(s.Root, "logical"), func(path, digest string) error {
		checked++
		if digest == "" {
			bad(Problem{Digest: s.rel(path), Logical: true, Reason: "not a digest path"})
			return nil
		}
		idx, err := s.readLogical(digest)
		if err != nil {
			bad(Problem{Digest: digest, Logical: true, Reason: err.Error()})
			return nil
		}
		for _, c := range idx.ChunkSHA256s {
			ok, err := s.Has(c)
			if err != nil {
				return err
			}
			if !ok {
				bad(Problem{Digest: digest, Logical: true, Reason: "chunk " + c + " is not stored"})
			}
		}
		return nil
	})
	return checked, err
}

// Repair removes what p names, so Has reports it missing and the next
// put stores it again. A logical index that does not parse is removed
// the same way. An index whose chunk is missing is kept: the chunk's
// own put restores it. fixed is false when there was nothing to remove.
func (s *Store) Repair(p Problem) (fixed bool, err error) {
	if !protocol.ValidDigest(p.Digest) {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !p.Logical {
		// Hash again under the lock: a put since Verify may have
		// replaced the object with good bytes.
		ok, err := s.intactLocked(p.Digest, -1)
		if err != nil || ok {
			return false, err
		}
		return true, s.removeObjectLocked(p.Digest)
	}
	if _, err := s.readLogical(p.Digest); err == nil || errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	path, err := s.logicalPath(p.Digest)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("cas: %w", err)
	}
	return true, syncDir(filepath.Dir(path))
}

func (s *Store) rel(path string) string {
	if r, err := filepath.Rel(s.Root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}

// walkEntries calls fn for every file under root/<ab>/<rest> except
// temp files. digest is empty when the path is not a valid digest.
// A missing root has no entries.
func walkEntries(root string, fn func(path, digest string) error) error {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && path == root {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || tempFile(d.Name()) {
			return nil
		}
		digest := ""
		if rel, err := filepath.Rel(root, path); err == nil {
			shard, rest, ok := strings.Cut(filepath.ToSlash(rel), "/")
			if ok && len(shard) == 2 && protocol.ValidDigest(shard+rest) {
				digest = shard + rest
			}
		}
		return fn(path, digest)
	})
	if err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	return nil
}

// Backup copies every object and logical index into the store layout
// under dest: sha256/ first, then logical/, so an index is not copied
// before its chunks. Temp files and partial uploads are left out. An
// entry already in dest with the same size is kept, so a second backup
// into the same directory copies only what is new. Each copy is synced
// and renamed into place.
func (s *Store) Backup(dest string) (copied int, err error) {
	for _, sub := range []string{"sha256", "logical"} {
		root := filepath.Join(s.Root, sub)
		err := walkEntries(root, func(path, _ string) error {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out := filepath.Join(dest, sub, rel)
			n, err := copyIfMissing(path, out)
			copied += n
			return err
		})
		if err != nil {
			return copied, err
		}
	}
	return copied, nil
}

func copyIfMissing(src, dst string) (int, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if have, err := os.Stat(dst); err == nil && have.Size() == st.Size() {
		return 0, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".put-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return 0, err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return 0, err
	}
	tmpName = ""
	return 1, nil
}

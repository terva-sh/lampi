package cas

import (
	"bytes"
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

// TempFile reports a file an install writes before its rename: a put
// or concat (.put-*), a logical index (.logical-*), or partial meta
// (.meta-*). A crash can leave one behind. It is never an object.
func TempFile(name string) bool {
	return strings.HasPrefix(name, ".put-") || strings.HasPrefix(name, ".logical-") || strings.HasPrefix(name, ".meta-")
}

// Sweep removes temp files last written before cutoff, and partial
// uploads whose files were all last written before cutoff. It is for
// start-up and live maintenance with a cutoff older than any request's
// lifetime: a put in flight has a fresh temp file, and an upload a client
// is still resuming has a fresh span. removed counts files and partial uploads.
func (s *Store) Sweep(cutoff time.Time) (removed int, err error) {
	// A resumed upload and its age check/removal must not interleave.
	s.mu.Lock()
	defer s.mu.Unlock()
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
		if d.IsDir() || !TempFile(d.Name()) {
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

// Verify re-hashes every object, decompressing a compressed one, and
// reads every logical index. Each object whose bytes do not hash to its
// name or whose frame does not decode, each index that does not
// parse or names a chunk that is not stored, and each prefix record
// whose chain does not reach a file holding its length, is passed to
// bad. A prefix record's bytes are not re-hashed here: they are its
// base's.
// Temp files are skipped. A file whose path is not a digest is a
// problem too, named by its path under the store. checked counts the
// entries read.
func (s *Store) Verify(bad func(Problem)) (checked int, err error) {
	err = walkEntries(filepath.Join(s.Root, "sha256"), true, func(path, digest string) error {
		checked++
		if digest == "" {
			bad(Problem{Digest: s.rel(path), Reason: "not a digest path"})
			return nil
		}
		_, compressed := objectName(filepath.Base(path))
		sum, err := storedObject{path: path, compressed: compressed}.sum()
		if errors.Is(err, errDamaged) {
			bad(Problem{Digest: digest, Reason: err.Error()})
			return nil
		}
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
	err = walkEntries(filepath.Join(s.Root, "logical"), false, func(path, digest string) error {
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
		if idx.PrefixOf != "" {
			// An object under the same digest is what Open reads, so a
			// record whose base is gone is no longer a problem once a
			// put has restored the object.
			if ok, err := s.Has(digest); err != nil || ok {
				return err
			}
			if err := s.checkRecord(digest, idx, 0); err != nil {
				bad(Problem{Digest: digest, Logical: true, Reason: err.Error()})
			}
			return nil
		}
		for _, c := range idx.ChunkSHA256s {
			ok, err := s.Present(c)
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
// own put restores it. So is a prefix record whose base is missing: a
// put of the digest installs an object, which Open prefers. fixed is false when there was nothing to remove.
func (s *Store) Repair(p Problem) (fixed bool, err error) {
	if !protocol.ValidDigest(p.Digest) {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !p.Logical {
		// Hash again under the lock: a put since Verify may have
		// replaced the object with good bytes.
		return s.repairObjectLocked(p.Digest)
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

// repairObjectLocked removes each of digest's object files, compressed
// and raw, whose bytes are not digest, and keeps any that are. Judging
// each form on its own keeps an intact raw copy when the frame beside
// it is damaged. fixed is false when every file there was intact. The
// caller holds s.mu.
func (s *Store) repairObjectLocked(digest string) (fixed bool, err error) {
	raw, err := s.Path(digest)
	if err != nil {
		return false, err
	}
	for _, o := range []storedObject{{path: raw + zstSuffix, compressed: true}, {path: raw}} {
		st, err := os.Lstat(o.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fixed, fmt.Errorf("cas: %w", err)
		}
		if st.Mode().IsRegular() {
			sum, err := o.sum()
			if err != nil && !errors.Is(err, errDamaged) {
				return fixed, err
			}
			if err == nil && sum == digest {
				continue
			}
		}
		if err := os.Remove(o.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fixed, fmt.Errorf("cas: %w", err)
		}
		fixed = true
	}
	if !fixed {
		return false, nil
	}
	return true, syncDir(filepath.Dir(raw))
}

// removeRawCopyLocked removes digest's raw object when a compressed one
// is beside it. removed is false when there was no such pair. The caller
// holds s.mu.
func (s *Store) removeRawCopyLocked(digest string) (removed bool, err error) {
	raw, err := s.Path(digest)
	if err != nil {
		return false, err
	}
	if _, err := os.Lstat(raw + zstSuffix); err != nil {
		return false, nil
	}
	if err := os.Remove(raw); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("cas: %w", err)
	}
	return true, syncDir(filepath.Dir(raw))
}

func (s *Store) rel(path string) string {
	if r, err := filepath.Rel(s.Root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}

// walkEntries calls fn for every file under root/<ab>/<rest> except
// temp files. digest is empty when the path is not a valid digest.
// Under objects, <rest> may end in .zst, the compressed form. A missing
// root has no entries.
func walkEntries(root string, objects bool, fn func(path, digest string) error) error {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && path == root {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || TempFile(d.Name()) {
			return nil
		}
		digest := ""
		if rel, err := filepath.Rel(root, path); err == nil {
			shard, rest, ok := strings.Cut(filepath.ToSlash(rel), "/")
			if objects {
				rest, _ = objectName(rest)
			}
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
// before its chunks or its base. An object is copied as it is stored,
// compressed or not. A raw object with a compressed one beside it is
// left out, and a raw copy dest holds of an object now compressed is
// removed once the compressed one is there. A frame dest holds of an
// object the store keeps only raw, one repair removed, is removed too. Temp files and partial
// uploads are left out. An entry already in dest with the same size is kept, so a
// second backup into the same directory copies only what is new. Each
// copy is synced and renamed into place.
//
// Backup runs while serve runs, and ingest replaces a grown head's
// object with a prefix record of the new object. The walk can pass the
// new object's directory before it is written and reach the record
// after, and an object can go between listing and copying. So an
// object that has gone is skipped, and afterwards every prefix record
// in dest is followed and whatever it reads from that dest lacks is
// copied from the store.
func (s *Store) Backup(dest string) (copied int, err error) {
	for _, sub := range []string{"sha256", "logical"} {
		root := filepath.Join(s.Root, sub)
		objects := sub == "sha256"
		err := walkEntries(root, objects, func(path, _ string) error {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out := filepath.Join(dest, sub, rel)
			compressed := false
			if objects {
				if _, compressed = objectName(filepath.Base(path)); !compressed {
					if _, err := os.Lstat(path + zstSuffix); err == nil {
						return nil
					}
					// The store has no frame for this object, so one
					// dest holds is stale, a damaged frame repair has
					// removed here, and readers of dest would prefer it.
					if err := os.Remove(out + zstSuffix); err != nil && !errors.Is(err, os.ErrNotExist) {
						return err
					}
				}
			}
			// An index is rewritten in place when a prefix record is
			// pointed further along its chain, at the same size, so it
			// is compared by content.
			n, err := copyIfMissing(path, out, sub == "logical")
			copied += n
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if compressed {
				raw := strings.TrimSuffix(out, zstSuffix)
				if err := os.Remove(raw); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return copied, err
		}
	}
	n, err := s.closeRecords(dest)
	return copied + n, err
}

// closeRecords copies into dest what dest's logical entries read from
// and dest lacks: a prefix record's next link, or a chunk list's
// chunk, an object or a record, until every entry resolves. An entry
// that is damaged rather than short is left for fsck.
func (s *Store) closeRecords(dest string) (copied int, err error) {
	d := &Store{Root: dest}
	// Each pass copies the next missing link of every entry, so a chain
	// that grew many times during the walk takes as many passes. A pass
	// that copies nothing new cannot be followed by one that does.
	for {
		want := map[string]bool{}
		err := walkEntries(filepath.Join(dest, "logical"), false, func(_, digest string) error {
			if digest == "" {
				return nil
			}
			if m := d.firstMissing(digest); m != "" {
				want[m] = true
			}
			return nil
		})
		if err != nil {
			return copied, err
		}
		if len(want) == 0 {
			return copied, nil
		}
		progress := 0
		for m := range want {
			n, err := s.copyEntry(m, dest)
			copied += n
			progress += n
			if err != nil {
				return copied, err
			}
		}
		if progress == 0 {
			return copied, fmt.Errorf("cas: backup: logical entries in %s still stop short", dest)
		}
	}
}

// firstMissing follows what base reads from, through prefix records and
// the chunks of chunk lists, and returns the first digest the store has
// neither an object nor a logical entry for. It is empty when every
// file base reads from is there.
func (s *Store) firstMissing(base string) string {
	seen := map[string]bool{}
	queue := []string{base}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if seen[d] {
			continue
		}
		seen[d] = true
		if ok, err := s.Has(d); err != nil || ok {
			continue
		}
		idx, err := s.readLogical(d)
		if errors.Is(err, os.ErrNotExist) {
			return d
		}
		if err != nil {
			// Damage, not a gap: fsck reports it.
			continue
		}
		if idx.PrefixOf != "" {
			queue = append(queue, idx.PrefixOf)
		}
		queue = append(queue, idx.ChunkSHA256s...)
	}
	return ""
}

// copyEntry copies digest's object, compressed or else raw, or else its
// logical entry, from the store into dest.
func (s *Store) copyEntry(digest, dest string) (int, error) {
	if !protocol.ValidDigest(digest) {
		return 0, fmt.Errorf("cas: invalid digest %q", digest)
	}
	rel := filepath.Join(digest[:2], digest[2:])
	for _, name := range []string{rel + zstSuffix, rel} {
		n, err := copyIfMissing(filepath.Join(s.Root, "sha256", name), filepath.Join(dest, "sha256", name), false)
		if !errors.Is(err, os.ErrNotExist) {
			return n, err
		}
	}
	n, err := copyIfMissing(filepath.Join(s.Root, "logical", rel), filepath.Join(dest, "logical", rel), true)
	if errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("cas: backup: %s is in neither sha256/ nor logical/", digest)
	}
	return n, err
}

func copyIfMissing(src, dst string, compare bool) (int, error) {
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
		if !compare {
			return 0, nil
		}
		same, err := sameBytes(in, dst)
		if err != nil {
			return 0, err
		}
		if same {
			return 0, nil
		}
		if _, err := in.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
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

// sameBytes reports whether in, read from its start, holds the bytes of
// the file at path. It is for the small index files.
func sameBytes(in *os.File, path string) (bool, error) {
	a, err := io.ReadAll(in)
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return bytes.Equal(a, b), nil
}

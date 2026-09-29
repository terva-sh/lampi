package cas

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Export streams every stored file to emit, in the store's layout: rel
// is sha256/<ab>/<rest>[.zst] or logical/<ab>/<rest>, with its size
// and its bytes as stored. It is Backup for a destination that cannot
// be read back, an archive: objects first, then logical entries, and
// then whatever the logical entries emitted read from and were not
// emitted, until every chain closes, as closeRecords does for a
// directory. Chains are followed through the entries as emitted, so a
// record repointed while Export ran still finds what its emitted copy
// names.
//
// A raw object with a frame beside it is left out. An object gone
// between listing and opening is skipped; the closing pass finds it if
// anything emitted needs it. n counts the files emitted.
func (s *Store) Export(emit func(rel string, size int64, r io.Reader) error) (n int, err error) {
	objects := map[string]bool{}
	logical := map[string]logicalIndex{}
	emitFile := func(rel, path string) (bool, error) {
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("cas: %w", err)
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return false, fmt.Errorf("cas: %w", err)
		}
		if err := emit(rel, st.Size(), io.LimitReader(f, st.Size())); err != nil {
			return false, err
		}
		n++
		return true, nil
	}
	emitLogical := func(digest, path string) (bool, error) {
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("cas: %w", err)
		}
		if err := emit(filepath.ToSlash(filepath.Join("logical", digest[:2], digest[2:])), int64(len(b)), bytes.NewReader(b)); err != nil {
			return false, err
		}
		n++
		var idx logicalIndex
		// One that does not parse is copied as it is, for fsck to
		// report from the archive as it would from the lake.
		if json.Unmarshal(b, &idx) == nil {
			logical[digest] = idx
		} else {
			logical[digest] = logicalIndex{}
		}
		return true, nil
	}

	root := filepath.Join(s.Root, "sha256")
	err = walkEntries(root, true, func(path, digest string) error {
		if digest == "" {
			return nil
		}
		if !strings.HasSuffix(path, zstSuffix) {
			if _, err := os.Lstat(path + zstSuffix); err == nil {
				return nil
			}
		}
		rel, err := filepath.Rel(s.Root, path)
		if err != nil {
			return err
		}
		ok, err := emitFile(filepath.ToSlash(rel), path)
		if ok {
			objects[digest] = true
		}
		return err
	})
	if err != nil {
		return n, err
	}
	err = walkEntries(filepath.Join(s.Root, "logical"), false, func(path, digest string) error {
		if digest == "" {
			return nil
		}
		_, err := emitLogical(digest, path)
		return err
	})
	if err != nil {
		return n, err
	}

	// Close every chain: each pass emits the first link each emitted
	// entry reaches and the archive lacks.
	for {
		want := map[string]bool{}
		for d := range logical {
			if m := firstMissingOf(d, objects, logical); m != "" {
				want[m] = true
			}
		}
		if len(want) == 0 {
			return n, nil
		}
		progress := 0
		for d := range want {
			ok, err := s.exportEntry(d, emitFile, emitLogical, objects)
			if err != nil {
				return n, err
			}
			if !ok {
				return n, fmt.Errorf("cas: export: %s is in neither sha256/ nor logical/", d)
			}
			progress++
		}
		if progress == 0 {
			return n, errors.New("cas: export: logical entries still stop short")
		}
	}
}

// exportEntry emits digest's object, compressed or else raw, or else
// its logical entry. ok is false when the store has none of them.
func (s *Store) exportEntry(digest string, emitFile func(rel, path string) (bool, error), emitLogical func(digest, path string) (bool, error), objects map[string]bool) (bool, error) {
	raw, err := s.Path(digest)
	if err != nil {
		return false, err
	}
	for _, p := range []string{raw + zstSuffix, raw} {
		rel, err := filepath.Rel(s.Root, p)
		if err != nil {
			return false, err
		}
		ok, err := emitFile(filepath.ToSlash(rel), p)
		if err != nil || ok {
			if ok {
				objects[digest] = true
			}
			return ok, err
		}
	}
	lp, err := s.logicalPath(digest)
	if err != nil {
		return false, err
	}
	return emitLogical(digest, lp)
}

// firstMissingOf follows what base reads from through the emitted
// logical entries and returns the first digest with neither an emitted
// object nor an emitted entry. It is empty when base closes.
func firstMissingOf(base string, objects map[string]bool, logical map[string]logicalIndex) string {
	seen := map[string]bool{}
	queue := []string{base}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if seen[d] || objects[d] {
			continue
		}
		seen[d] = true
		idx, ok := logical[d]
		if !ok {
			return d
		}
		if idx.PrefixOf != "" {
			queue = append(queue, idx.PrefixOf)
		}
		queue = append(queue, idx.ChunkSHA256s...)
	}
	return ""
}

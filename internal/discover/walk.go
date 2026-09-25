package discover

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WalkFiles calls fn for each entry under base that is not a directory.
// base may be a symlink to a directory. The target is walked and each
// path is still reported under base, so a watcher and a walk name the
// same file the same way. A missing base is an empty walk.
//
// An entry that vanished during the walk, or that this user cannot
// read, is left out and its error is returned in skipped. One
// unreadable directory or one deleted file does not stop the rest of
// the tree. fn returning such an error skips that entry the same way.
// Any other error, or one on base itself, stops the walk.
func WalkFiles(base string, fn func(path string, d fs.DirEntry) error) (skipped []error, err error) {
	return walk(base, false, fn)
}

// WalkDirs calls fn for each directory under base, not base itself,
// with the same symlink and skip rules as WalkFiles.
func WalkDirs(base string, fn func(path string) error) (skipped []error, err error) {
	return walk(base, true, func(p string, _ fs.DirEntry) error { return fn(p) })
}

func walk(base string, dirs bool, fn func(path string, d fs.DirEntry) error) (skipped []error, err error) {
	target, err := filepath.EvalSymlinks(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("discover: %s is not a directory", base)
	}
	err = filepath.WalkDir(target, func(p string, d fs.DirEntry, err error) error {
		rel, relErr := filepath.Rel(target, p)
		if relErr != nil {
			return relErr
		}
		p = filepath.Join(base, rel)
		if err != nil {
			if rel != "." && Skippable(err) {
				skipped = append(skipped, relabel(err, p))
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			return relabel(err, p)
		}
		if d.IsDir() != dirs || rel == "." {
			return nil
		}
		if err := fn(p, d); err != nil {
			if Skippable(err) {
				skipped = append(skipped, relabel(err, p))
				if dirs {
					return fs.SkipDir
				}
				return nil
			}
			return err
		}
		return nil
	})
	return skipped, err
}

// Skippable reports an error that belongs to one file: it is gone, or
// this user cannot read it. The caller leaves that file out and goes
// on with the rest.
func Skippable(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission)
}

// relabel names p in a path error that named the resolved target.
func relabel(err error, p string) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: pe.Op, Path: p, Err: pe.Err}
	}
	return fmt.Errorf("%s: %w", p, err)
}

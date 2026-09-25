package cas

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// syncDir fsyncs dir, so a rename or create inside it survives a crash.
// Windows cannot flush a directory handle, and NTFS journals the rename,
// so there it does nothing. It is a variable so a test can see the calls.
var syncDir = func(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("cas: sync %s: %w", dir, err)
	}
	return nil
}

// mkdirSynced creates dir and its missing parents, mode 0700, and fsyncs
// the parent of each directory it created. A directory that already
// exists costs one stat.
func mkdirSynced(dir string) error {
	var created []string
	for d := dir; ; d = filepath.Dir(d) {
		_, err := os.Stat(d)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cas: %w", err)
		}
		created = append(created, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	if len(created) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	// Deepest first: each new entry is flushed into its parent.
	for _, d := range created {
		if err := syncDir(filepath.Dir(d)); err != nil {
			return err
		}
	}
	return nil
}

// syncFile fsyncs the file at path. It opens the file for writing
// because Windows flushes only a handle with write access.
func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("cas: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	return nil
}

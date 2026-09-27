package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/filelock"
)

// LockSuffix names the lock file beside config.json.
const LockSuffix = ".lock"

// Tx edits config.json while its lock is held. Locked hands one to its
// function; the edits made through it do not lock again.
type Tx struct {
	getenv func(string) string
}

// Locked runs fn while this process holds config.json's lock, a file
// beside it. Every read-edit-write of config.json takes it, so two
// processes, such as register beside an agent moving its pin, do not
// lose each other's write. A caller that must keep another file in step
// with config.json, such as a lake's cached profile, writes it inside
// fn. Where the platform has no file lock, fn runs without one, as every
// write did before.
func Locked(getenv func(string) string, fn func(Tx) error) error {
	path, err := ConfigPath(getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path+LockSuffix, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	defer f.Close()
	if err := filelock.Lock(f); err != nil && !errors.Is(err, filelock.ErrUnsupported) {
		return fmt.Errorf("config: locking %s: %w", f.Name(), err)
	}
	return fn(Tx{getenv: getenv})
}

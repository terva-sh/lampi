// Package filelock waits for an exclusive lock on an open file. The
// lock ends when the file is closed or the process dies, so a crash
// leaves nothing to clean up.
package filelock

import "errors"

// ErrUnsupported is returned where the platform has no file lock.
var ErrUnsupported = errors.New("filelock: this platform has no file lock")

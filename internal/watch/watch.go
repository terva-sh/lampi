// Package watch is the long-running observation of session files.
//
// Unimplemented blocks until the context ends. fsnotify, with a poll
// fallback for macOS, Windows, and network filesystems, is the intended
// body. A hook that nudges the agent is an accelerator only; the walk
// in internal/discover remains the source of truth.
package watch

import "context"

// Watcher runs until ctx is cancelled.
type Watcher interface {
	Run(ctx context.Context) error
}

// Unimplemented is the scaffold watcher.
type Unimplemented struct{}

// Run waits for cancellation. It returns nil on a normal stop so a
// SIGINT from `terva-lampi agent` is not an error.
func (Unimplemented) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

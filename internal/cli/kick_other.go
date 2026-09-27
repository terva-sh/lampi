//go:build !unix

package cli

import "context"

// watchKick is a no-op where SIGUSR1 does not exist. The filesystem
// watch is the source of truth on every platform.
func watchKick(context.Context, func()) {}

// reloadHint is what the agent says a new lake needs. There is no
// SIGHUP here, so the lakes are read at start only.
const reloadHint = "Add a lake, then restart the agent."

// watchReload is a no-op where SIGHUP does not exist: a change to the
// lakes needs a restart.
func watchReload(context.Context, func()) {}

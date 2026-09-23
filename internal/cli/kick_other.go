//go:build !unix

package cli

import "context"

// watchKick is a no-op where SIGUSR1 does not exist. The filesystem
// watch is the source of truth on every platform.
func watchKick(context.Context, func()) {}

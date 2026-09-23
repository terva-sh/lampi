//go:build unix

package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// watchKick turns SIGUSR1 into a sync wake. The filesystem watch is
// still what decides that a file changed. The signal only asks the
// loop to run that check now, which is what an example hook does.
// Cancelling ctx stops listening. The default terminate action is
// not used for this signal.
func watchKick(ctx context.Context, wake func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				wake()
			}
		}
	}()
}

package cli

import (
	"errors"
	"fmt"

	"terva.sh/lampi/internal/lakelock"
)

// lockLake takes the lake lock in data for cmd, a command that writes
// the lake with serve stopped. Only a lock another process holds says
// to stop serve, naming readOnly as the way to run without the lock
// when the command has one. Any other failure names the lake directory
// it tried: that is usually a --data that was left off, so the default
// directory of whoever runs the command was used (TKT-01M3M54QK).
func lockLake(cmd, data, readOnly string) (*lakelock.Lock, error) {
	lock, err := lakelock.Acquire(data)
	switch {
	case err == nil:
		return lock, nil
	case errors.Is(err, lakelock.ErrHeld):
		if readOnly != "" {
			return nil, fmt.Errorf("%s: %w; stop serve first, or pass %s", cmd, err, readOnly)
		}
		return nil, fmt.Errorf("%s: %w; stop serve first", cmd, err)
	default:
		return nil, fmt.Errorf("%s: cannot lock the lake at %s: %w; pass --data with the lake's directory", cmd, data, err)
	}
}

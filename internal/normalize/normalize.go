// Package normalize projects a raw harness blob into schema_version 1
// events. The projection is a derived view. Normalize does not write
// the caller's bytes. The lake records a failure on the session as
// normalize_error and leaves the CAS object where it was.
package normalize

import "context"

// Normalizer turns raw harness bytes into schema_version 1 events.
// A non-nil error means no derived view. Callers discard any partial
// result and leave the raw blob untouched.
type Normalizer interface {
	Normalize(ctx context.Context, raw []byte) ([]Event, error)
}

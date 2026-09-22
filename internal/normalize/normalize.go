// Package normalize will project a raw blob into the shared event schema
// (schema_version 1: harness, actor, event_type, content_text, usage).
//
// The worker is async on the server. A failure marks the session and
// leaves the raw blob untouched. Nothing here reads a blob yet.
package normalize

import "context"

// Normalizer turns raw harness bytes into a derived view.
type Normalizer interface {
	Normalize(ctx context.Context, raw []byte) error
}

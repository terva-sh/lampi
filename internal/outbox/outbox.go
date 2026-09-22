// Package outbox will be the durable queue of blob digests and manifest
// versions that survives sleep and reboot.
//
// The sync command in this scaffold uploads immediately and keeps no
// queue. A crash mid-sync is recovered by running sync again: puts are
// idempotent and the catalog upserts.
package outbox

import "context"

// Item is one pending upload. Digest may be empty when only a manifest
// is waiting.
type Item struct {
	Digest   string
	Manifest []byte
}

// Queue is the disk-backed outbox. Not implemented.
type Queue interface {
	Enqueue(ctx context.Context, item Item) error
	Pending(ctx context.Context) ([]Item, error)
	Ack(ctx context.Context, item Item) error
}

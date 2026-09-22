// Package adapter is the seam between a harness on disk and the lake.
//
// terva is the only implementation in this tree. Claude, Codex, OpenCode,
// and Cursor stay behind this interface until a later phase. Cursor in
// particular is deliberately absent: its store is undocumented SQLite.
package adapter

import (
	"context"
	"io"
	"time"
)

// Ref is one artifact a harness can see without parsing it.
type Ref struct {
	Kind    string
	AbsPath string
	RelPath string
	Size    int64
	ModTime time.Time
}

// Harness discovers artifacts and reads a byte range. ReadSlice is how a
// later tail-only upload avoids resending a prefix. Callers pass offset 0
// to read the whole file.
type Harness interface {
	Name() string
	Discover(ctx context.Context, root string) ([]Ref, error)
	ReadSlice(ctx context.Context, absPath string, offset int64) (io.ReadCloser, error)
}

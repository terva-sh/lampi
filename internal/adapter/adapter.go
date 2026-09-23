// Package adapter is the seam between a harness on disk and the lake.
//
// terva, Claude Code, Codex CLI, OpenCode, the Cursor IDE, and the
// Cursor CLI are the implementations. The IDE reader snapshots
// state.vscdb. The CLI reader snapshots store.db. They are separate
// corpora and do not share a harness name. Both upload a filtered
// export.
package adapter

import (
	"context"
	"io"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// Ref is one artifact a harness can see without parsing it.
type Ref struct {
	Kind    string
	AbsPath string
	RelPath string
	Size    int64
	ModTime time.Time
}

// Bundle is a set of manifests plus the local path for each digest the
// client still has to PUT. Two files with the same bytes share one path;
// either file's bytes satisfy the put. Root is the harness home those
// paths were walked from, and it is the watermark root.
type Bundle struct {
	Root      string
	Manifests []protocol.Manifest
	Paths     map[string]string
	// Cleanup removes temporary files this bundle created. Nil does
	// nothing. Call it after Paths have been read. A second call is safe.
	Cleanup func()
}

// Harness discovers artifacts, reads a byte range, and builds manifests.
// ReadSlice is how a tail-only upload avoids resending a prefix. Callers
// pass offset 0 to read the whole file.
//
// Home resolves the producer directory. WatchDir is the directory under
// that home that holds artifacts. Match reports whether a slash path
// relative to the home is one of them.
type Harness interface {
	Name() string
	Home(getenv func(string) string) (string, error)
	WatchDir() string
	Match(rel string) (kind string, ok bool)
	Discover(ctx context.Context, root string) ([]Ref, error)
	ReadSlice(ctx context.Context, absPath string, offset int64) (io.ReadCloser, error)
	Manifests(root, machineID string) (Bundle, error)
}

// WatchRoots is the optional extension for a harness whose artifacts
// are not all under WatchDir. Each string is a directory under Home.
// The agent starts one watcher per directory. A missing directory is
// an empty tree.
type WatchRoots interface {
	WatchDirs() []string
}

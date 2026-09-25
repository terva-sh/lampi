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
	"terva.sh/lampi/internal/redact"
)

// Ref is one artifact a harness can see without parsing it.
type Ref struct {
	Kind    string
	AbsPath string
	RelPath string
	Size    int64
	ModTime time.Time
}

// Bundle is a set of manifests plus the local path for each artifact,
// keyed by its relpath. Two files with the same bytes keep their own
// paths, so one session never reads another's file. The file can change
// after the digest was taken; the reader checks the bytes against the
// artifact digest. Root is the harness home those paths were walked
// from, and it is the watermark root. A manifest a Permit refused is
// kept so the caller can report it, but its artifacts carry no digest
// and have no entry in Paths.
type Bundle struct {
	Root      string
	Manifests []protocol.Manifest
	Paths     map[string]string
	// Hidden is what the ruleset found in bytes that a file in Paths
	// holds only in encoded form, keyed by the same digest: a Cursor
	// value exported as base64. A scan of the file cannot see those
	// bytes, so the upload adds this to it. A digest with no entry had
	// nothing hidden.
	Hidden map[string]redact.Result
	// Skipped is each file this harness left out of the bundle, as an
	// error that names it: gone, unreadable, or with a line too long
	// to find its session. The other files still upload.
	Skipped []error
	// Cleanup removes temporary files this bundle created. Nil does
	// nothing. Call it after Paths have been read. A second call is safe.
	Cleanup func()
}

// Permit reports whether a session may leave the machine. A reader that
// builds an export asks it before that work, with a manifest that has
// the project and the artifact relpaths but no digests. The upload
// passes the allowlist check it applies again to the finished manifest.
// Nil permits every session.
type Permit func(protocol.Manifest) bool

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

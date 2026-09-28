// Package storage measures how much disk a lake directory uses, split
// by the part of the lake that owns each file.
//
// Use is what the files occupy on disk, the allocated blocks that du
// counts, where the platform reports them, and the file size where it
// does not. A sparse or compressed file can occupy less than its size.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"terva.sh/lampi/internal/cas"
)

// Components of a lake directory. Every file under it is counted in
// exactly one.
const (
	// CAS is stored objects, chunks and logical indexes.
	CAS = "cas"
	// Uploads is partial uploads and install temp files in the CAS: an
	// upload in flight, or a leftover start-up has not swept yet.
	Uploads = "uploads"
	// Catalog is catalog.db with its WAL and shared memory.
	Catalog = "catalog"
	// Normalized is the derived JSONL projections.
	Normalized = "normalized"
	// Parquet is the partitioned export.
	Parquet = "parquet"
	// Search is the search index, search.db with its WAL.
	Search = "search"
	// Audit is audit.jsonl.
	Audit = "audit"
	// Other is everything else: identity, tokens, profiles, locks.
	Other = "other"
)

// Components lists every component in display order.
var Components = []string{CAS, Uploads, Catalog, Normalized, Parquet, Search, Audit, Other}

// Use is what one component occupies.
type Use struct {
	Bytes int64 `json:"bytes"`
	Files int64 `json:"files"`
}

// Measure walks dir and returns the use of each component. A symlink
// to the lake directory is resolved, and nothing under it is followed:
// a symlink inside the lake occupies nothing here. A directory's own
// blocks count toward the component it holds, and Files counts
// regular files only. A file removed
// during the walk is skipped: the lake keeps running while it is
// measured. Cancelling ctx stops the walk.
func Measure(ctx context.Context, dir string) (map[string]Use, error) {
	out := make(map[string]Use, len(Components))
	for _, c := range Components {
		out[c] = Use{}
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path != root {
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() && !d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// A directory belongs with what it holds.
			rel += "/-"
		}
		c := classify(rel)
		u := out[c]
		u.Bytes += allocated(info)
		if !d.IsDir() {
			u.Files++
		}
		out[c] = u
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	return out, nil
}

// classify names the component of a file at rel, a slash-separated
// path relative to the lake directory.
func classify(rel string) string {
	top, rest, nested := strings.Cut(rel, "/")
	if !nested {
		switch {
		case strings.HasPrefix(top, "catalog.db"):
			return Catalog
		case strings.HasPrefix(top, "search.db"):
			return Search
		case strings.HasPrefix(top, "audit.jsonl"):
			return Audit
		}
		return Other
	}
	switch top {
	case "cas":
		if strings.HasPrefix(rest, "partial/") || cas.TempFile(path.Base(rest)) {
			return Uploads
		}
		return CAS
	case "normalized":
		return Normalized
	case "parquet":
		return Parquet
	}
	return Other
}

// Filesystem is the capacity of the filesystem holding a directory.
// Free is what an unprivileged writer can still use.
type Filesystem struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
}

// ErrUnsupported is returned by Capacity where the platform has no way
// to ask.
var ErrUnsupported = errors.New("storage: filesystem capacity is not available on this platform")

// Capacity reports the filesystem holding dir.
func Capacity(dir string) (Filesystem, error) {
	if _, err := os.Stat(dir); err != nil {
		return Filesystem{}, fmt.Errorf("storage: %w", err)
	}
	return capacity(dir)
}

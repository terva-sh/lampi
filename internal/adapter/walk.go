package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/discover"
)

// Walk lists files under root/dir. A missing directory is an empty list:
// this machine may not have that harness installed. rel paths are slash
// separated and relative to root. match receives that path. An entry
// that cannot be read is left out; WalkSkipped reports it.
func Walk(root, dir string, match func(rel string) (kind string, ok bool)) ([]Ref, error) {
	refs, _, err := WalkSkipped(root, dir, match)
	return refs, err
}

// WalkSkipped is Walk plus the entries it left out, each an error that
// names the path. A symlinked root or dir is followed.
func WalkSkipped(root, dir string, match func(rel string) (kind string, ok bool)) ([]Ref, []error, error) {
	if match == nil {
		return nil, nil, fmt.Errorf("adapter: match is nil")
	}
	var out []Ref
	skipped, err := discover.WalkFiles(filepath.Join(root, dir), func(path string, d fs.DirEntry) error {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		kind, ok := match(rel)
		if !ok {
			return nil
		}
		info, err := discover.FileInfo(path, d)
		if err != nil {
			return err
		}
		out = append(out, Ref{
			Kind:    kind,
			AbsPath: path,
			RelPath: rel,
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
			Inode:   discover.Inode(info),
		})
		return nil
	})
	if err != nil {
		return nil, skipped, err
	}
	return out, skipped, nil
}

// OpenSlice opens absPath at offset. Offset 0 reads the whole file.
// Closing the reader is the caller's job.
func OpenSlice(ctx context.Context, absPath string, offset int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

// HashFile is the sha256 of the whole file, lowercase hex.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

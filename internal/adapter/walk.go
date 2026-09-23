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
)

// Walk lists files under root/dir. A missing directory is an empty list:
// this machine may not have that harness installed. rel paths are slash
// separated and relative to root. match receives that path.
func Walk(root, dir string, match func(rel string) (kind string, ok bool)) ([]Ref, error) {
	if match == nil {
		return nil, fmt.Errorf("adapter: match is nil")
	}
	base := filepath.Join(root, dir)
	st, err := os.Stat(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("adapter: %s is not a directory", base)
	}
	var out []Ref
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		kind, ok := match(rel)
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, Ref{
			Kind:    kind,
			AbsPath: path,
			RelPath: rel,
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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

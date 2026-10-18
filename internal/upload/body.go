package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime"
	"sync"
)

// bodySource is a replayable immutable byte range. Production artifacts use
// private snapshots; small protocol JSON and test fixtures use memory.
type bodySource struct {
	file         *os.File
	data         []byte
	offset, size int64
}

func memoryBody(b []byte) bodySource { return bodySource{data: b, size: int64(len(b))} }

func (b bodySource) slice(from, to int64) bodySource {
	b.offset += from
	b.size = to - from
	return b
}

func (b bodySource) open() (io.ReadCloser, error) {
	if b.file != nil {
		// Each request has its own cursor, including GetBody replays. The
		// session owns the file and closes it after all requests finish.
		return io.NopCloser(io.NewSectionReader(b.file, b.offset, b.size)), nil
	}
	return io.NopCloser(bytes.NewReader(b.data[b.offset : b.offset+b.size])), nil
}

const bodyBufferSize = 64 << 10

var bodyBuffers = sync.Pool{New: func() any {
	b := make([]byte, bodyBufferSize)
	return &b
}}

type contextBodyReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextBodyReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

func copyBody(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := bodyBuffers.Get().(*[]byte)
	defer bodyBuffers.Put(buf)
	return io.CopyBuffer(dst, contextBodyReader{ctx, src}, *buf)
}

func (b bodySource) digest(ctx context.Context) (string, error) {
	r, err := b.open()
	if err != nil {
		return "", err
	}
	defer r.Close()
	h := sha256.New()
	n, err := copyBody(ctx, h, r)
	if err != nil {
		return "", err
	}
	if n != b.size {
		return "", io.ErrUnexpectedEOF
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// snapshotBody never uses the original path after copying. Even a same-size
// rewrite or replacement during HTTP retries cannot change the verified bytes.
func snapshotBody(ctx context.Context, path, dir string, size int64, digest string) (bodySource, error) {
	src, err := os.Open(path)
	if err != nil {
		return bodySource{}, err
	}
	defer src.Close()
	dst, err := os.CreateTemp(dir, "artifact-*")
	if err != nil {
		return bodySource{}, err
	}
	defer dst.Close()
	h := sha256.New()
	// An actively growing source cannot consume unbounded temporary disk.
	n, err := copyBody(ctx, io.MultiWriter(dst, h), io.LimitReader(src, size+1))
	if err != nil {
		return bodySource{}, err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != digest {
		return bodySource{}, errFileChanged
	}
	if err := dst.Close(); err != nil {
		return bodySource{}, err
	}
	f, err := os.Open(dst.Name())
	if err != nil {
		return bodySource{}, err
	}
	// Unix releases the storage on close even after SIGKILL. Windows
	// retains the private name until the session closes and removes it.
	if runtime.GOOS != "windows" {
		if err := os.Remove(dst.Name()); err != nil {
			f.Close()
			return bodySource{}, err
		}
	}
	return bodySource{file: f, size: n}, nil
}

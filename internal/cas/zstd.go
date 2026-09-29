package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Objects are stored as zstd frames, at sha256/<ab>/<rest>.zst. The key
// is still the digest of the bytes before compression, and every reader
// decompresses, so nothing outside this package sees the frame.
//
// An object installed before compression is the raw bytes at the name
// without the suffix. Both forms are read, and compact re-encodes the
// raw ones. The form is named by the suffix, not sniffed from the first
// bytes: a chunk or a tail can begin at any byte of a file, including a
// zstd magic number.
//
// A frame of 256 bytes or more records its content size, so Size reads
// a header rather than the object; the encoder leaves the size out of a
// smaller one, which is decoded and counted instead. Every frame carries
// a checksum of its content.
//
// Has and Present check sizes, not bytes. A raw object cut short reads
// short, but a frame cut short still records its whole size, so its
// damage shows only when it is read or when fsck decodes it. Every
// install syncs the frame before renaming it into place, so a crash
// leaves an empty file or none, and Has still reports an empty one
// missing.

// zstSuffix names a compressed object.
const zstSuffix = ".zst"

// errDamaged is an object whose frame does not decode: truncated,
// zero-filled, or not zstd at all. It is damage to report and replace,
// not a failure of the filesystem.
var errDamaged = errors.New("cas: object does not decompress")

// encoderLevel is zstd's "better" level. On the hosted lake's
// transcripts it stored 544 MiB in 100 MiB (5.4x), against 108 MiB at
// the default level and 97 MiB at the best, which costs several times
// the CPU (TKT-01M3K45MQ).
const encoderLevel = zstd.SpeedBetterCompression

// maxWindow bounds the memory a frame can ask the decoder for. Frames
// this package writes use the level's window, far below it, so a frame
// that asks for more is damaged.
const maxWindow = 64 << 20

// storedObject is the file holding digest's bytes.
type storedObject struct {
	path       string
	compressed bool
	// size is the file's size on disk.
	size int64
	mode os.FileMode
}

// zstPath is where digest's compressed object is kept.
func (s *Store) zstPath(digest string) (string, error) {
	p, err := s.Path(digest)
	if err != nil {
		return "", err
	}
	return p + zstSuffix, nil
}

// object finds digest's object file, preferring the compressed form.
// ok is false when neither is there.
func (s *Store) object(digest string) (o storedObject, ok bool, err error) {
	raw, err := s.Path(digest)
	if err != nil {
		return o, false, err
	}
	for _, c := range []bool{true, false} {
		p := raw
		if c {
			p += zstSuffix
		}
		st, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return o, false, fmt.Errorf("cas: %w", err)
		}
		return storedObject{path: p, compressed: c, size: st.Size(), mode: st.Mode()}, true, nil
	}
	return o, false, nil
}

// ObjectPath is the file holding digest's object, compressed or raw,
// and false when it has none. It is for tools and tests that look at
// the store's files; reading goes through Open.
func (s *Store) ObjectPath(digest string) (string, bool, error) {
	o, ok, err := s.object(digest)
	return o.path, ok, err
}

// logicalSize is the number of bytes o holds once decompressed, read
// from the frame header, or else by decoding the frame and counting.
func (o storedObject) logicalSize() (int64, error) {
	if !o.compressed {
		return o.size, nil
	}
	f, err := os.Open(o.path)
	if err != nil {
		return 0, fmt.Errorf("cas: %w", err)
	}
	defer f.Close()
	var buf [zstd.HeaderMaxSize]byte
	n, err := io.ReadFull(f, buf[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		if errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("%w: %s is empty", errDamaged, o.path)
		}
		return 0, fmt.Errorf("cas: %w", err)
	}
	var h zstd.Header
	if err := h.Decode(buf[:n]); err != nil {
		return 0, fmt.Errorf("%w: %s: %v", errDamaged, o.path, err)
	}
	if h.HasFCS {
		return int64(h.FrameContentSize), nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("cas: %w", err)
	}
	r, err := getDecoder(f)
	if err != nil {
		return 0, err
	}
	defer putDecoder(r)
	size, err := io.Copy(io.Discard, r)
	if err != nil {
		return 0, damaged(o.path, err)
	}
	return size, nil
}

// sum is the sha256 of o's bytes once decompressed. A frame that does
// not decode is errDamaged.
func (o storedObject) sum() (string, error) {
	if !o.compressed {
		return hashFile(o.path)
	}
	rc, err := openObjectFile(o.path, true)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", damaged(o.path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// openObjectFile opens an object file for reading its bytes.
func openObjectFile(path string, compressed bool) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cas: %w", err)
	}
	if !compressed {
		return f, nil
	}
	r, err := getDecoder(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &frameReader{f: f, d: r, path: path}, nil
}

// Decoders and encoders are pooled. Each holds a window of history, a
// few MiB for a large object, and allocating one per read or install
// would churn that much memory for every chunk streamed.
var decoders, encoders sync.Pool

// getDecoder returns a decoder reading r, one from the pool when there
// is one. It decodes on the calling goroutine: a lake serves many reads
// at once, and the decoder's own parallelism would multiply its memory
// by them.
func getDecoder(r io.Reader) (*zstd.Decoder, error) {
	if d, ok := decoders.Get().(*zstd.Decoder); ok {
		if err := d.Reset(r); err != nil {
			d.Close()
			return nil, fmt.Errorf("cas: %w", err)
		}
		return d, nil
	}
	d, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(maxWindow))
	if err != nil {
		return nil, fmt.Errorf("cas: %w", err)
	}
	return d, nil
}

// putDecoder returns d to the pool, dropping its reader.
func putDecoder(d *zstd.Decoder) {
	if err := d.Reset(nil); err != nil {
		d.Close()
		return
	}
	decoders.Put(d)
}

// frameReader is a compressed object's bytes. A frame that fails to
// decode is errDamaged, so a reader can tell damage from a failing disk.
type frameReader struct {
	f    *os.File
	d    *zstd.Decoder
	path string
}

func (r *frameReader) Read(p []byte) (int, error) {
	n, err := r.d.Read(p)
	if err != nil && err != io.EOF {
		err = damaged(r.path, err)
	}
	return n, err
}

func (r *frameReader) Close() error {
	if r.d != nil {
		putDecoder(r.d)
		r.d = nil
	}
	return r.f.Close()
}

// damaged wraps a decode error as errDamaged. An error from the file
// itself is passed through.
func damaged(path string, err error) error {
	var pe *os.PathError
	if errors.Is(err, errDamaged) || errors.As(err, &pe) {
		return err
	}
	return fmt.Errorf("%w: %s: %v", errDamaged, path, err)
}

// seal compresses src, a finished file of size bytes, into a new temp
// file beside digest's object, synced and owner-only, and returns its
// path. src is left for the caller to remove. The encode runs without
// s.mu, except where the caller already holds it.
func (s *Store) seal(src, digest string, size int64) (string, error) {
	final, err := s.Path(digest)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(final)
	if err := mkdirSynced(dir); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, ".put-*")
	if err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	name := out.Name()
	ok := false
	defer func() {
		if !ok {
			out.Close()
			os.Remove(name)
		}
	}()
	if err := encode(out, in, size); err != nil {
		return "", err
	}
	if err := out.Chmod(0o600); err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("cas: %w", err)
	}
	ok = true
	return name, nil
}

// emptyFrame is a zstd frame of no bytes: one segment, a content size
// of zero, and one empty raw block. The encoder writes nothing at all
// for empty input, which is not a frame.
var emptyFrame = []byte{0x28, 0xb5, 0x2f, 0xfd, 0x20, 0x00, 0x01, 0x00, 0x00}

// encode writes r, which is size bytes, to w as one zstd frame that
// records its content size. A reader that ends early or runs long is an
// error.
func encode(w io.Writer, r io.Reader, size int64) error {
	if size == 0 {
		var one [1]byte
		if n, _ := r.Read(one[:]); n > 0 {
			return fmt.Errorf("cas: compressing 0 bytes read more")
		}
		_, err := w.Write(emptyFrame)
		if err != nil {
			return fmt.Errorf("cas: %w", err)
		}
		return nil
	}
	enc, ok := encoders.Get().(*zstd.Encoder)
	if !ok {
		var err error
		enc, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(encoderLevel), zstd.WithEncoderConcurrency(1))
		if err != nil {
			return fmt.Errorf("cas: %w", err)
		}
	}
	// An encoder that failed part-way is dropped rather than pooled.
	enc.ResetContentSize(w, size)
	n, err := io.Copy(enc, io.LimitReader(r, size+1))
	if err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	if n != size {
		return fmt.Errorf("cas: compressing %d bytes read %d", size, n)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("cas: %w", err)
	}
	enc.Reset(nil)
	encoders.Put(enc)
	return nil
}

// objectName splits an object file's base name under sha256/<ab>/ into
// the rest of its digest and whether it is the compressed form.
func objectName(name string) (rest string, compressed bool) {
	if r, ok := strings.CutSuffix(name, zstSuffix); ok {
		return r, true
	}
	return name, false
}

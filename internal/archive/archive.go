// Package archive is the encrypted backup format: a tar stream,
// compressed with zstd, encrypted with age to one or more recipients.
// Compression comes before encryption, which leaves nothing to
// compress. The first entry is a manifest, and a reader refuses an
// archive whose manifest it does not know.
//
// Writing needs only public recipients. Reading needs an identity that
// matches one of them, and the lake that writes an archive never holds
// one (TKT-01M3FBQX).
package archive

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
)

// ManifestName is the archive's first entry.
const ManifestName = "lampi-backup.json"

// Format is the archive layout this package writes and reads.
const Format = 1

// Manifest describes an archive. It is written in the clear inside the
// encryption, so it names nothing a reader of the ciphertext could see.
type Manifest struct {
	Format        int    `json:"format"`
	LakeID        string `json:"lake_id,omitempty"`
	Created       string `json:"created"`
	LampiVersion  string `json:"lampi_version"`
	CatalogSchema int    `json:"catalog_schema"`
}

// ErrFormat is an archive this package cannot read: no manifest first,
// or a format it does not know.
var ErrFormat = errors.New("archive: not a lampi backup this version can read")

// Writer adds files to an archive. Close finishes the tar stream, the
// zstd frame and the age payload, in that order; an archive whose
// Close did not return nil is not whole.
type Writer struct {
	enc  io.WriteCloser
	zw   *zstd.Encoder
	tw   *tar.Writer
	when time.Time
}

// NewWriter starts an archive on w encrypted to recipients and writes
// m as its first entry.
func NewWriter(w io.Writer, recipients []age.Recipient, m Manifest) (*Writer, error) {
	if len(recipients) == 0 {
		return nil, errors.New("archive: no recipients")
	}
	enc, err := age.Encrypt(w, recipients...)
	if err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	zw, err := zstd.NewWriter(enc, zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	aw := &Writer{enc: enc, zw: zw, tw: tar.NewWriter(zw), when: time.Now().UTC().Truncate(time.Second)}
	m.Format = Format
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	b = append(b, '\n')
	if err := aw.Add(ManifestName, int64(len(b)), strings.NewReader(string(b))); err != nil {
		return nil, err
	}
	return aw, nil
}

// Add writes a file of size bytes read from r under name, a relative
// slash path. A reader that ends early or runs long is an error.
func (w *Writer) Add(name string, size int64, r io.Reader) error {
	if !validName(name) {
		return fmt.Errorf("archive: entry name %q", name)
	}
	hdr := &tar.Header{Typeflag: tar.TypeReg, Name: name, Size: size, Mode: 0o600, ModTime: w.when, Format: tar.FormatPAX}
	if err := w.tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	n, err := io.Copy(w.tw, io.LimitReader(r, size))
	if err != nil {
		return fmt.Errorf("archive: %s: %w", name, err)
	}
	if n != size {
		return fmt.Errorf("archive: %s: read %d of %d bytes", name, n, size)
	}
	return nil
}

// Close finishes the archive.
func (w *Writer) Close() error {
	if err := w.tw.Close(); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	if err := w.zw.Close(); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	if err := w.enc.Close(); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	return nil
}

// validName is a relative slash path that stays below the directory it
// is extracted into: no empty, absolute, dot-dot or dot segment.
func validName(name string) bool {
	if name == "" || strings.Contains(name, "\\") || path.Clean(name) != name || !filepath.IsLocal(filepath.FromSlash(name)) {
		return false
	}
	return true
}

// Extract decrypts r with identities and writes its files under dest,
// which must exist. Files are 0600 and directories 0700. Only regular
// files are accepted, each at a relative path below dest that is not
// there already; anything else is an error, as is an archive cut
// short, one that fails authentication, or one whose manifest is not
// first or names another format. On an error, what Extract wrote is
// left for the caller to remove. files counts the entries written,
// the manifest included.
func Extract(r io.Reader, identities []age.Identity, dest string) (m Manifest, files int, err error) {
	dec, err := age.Decrypt(r, identities...)
	if err != nil {
		return m, 0, fmt.Errorf("archive: %w", err)
	}
	zr, err := zstd.NewReader(dec, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(64<<20))
	if err != nil {
		return m, 0, fmt.Errorf("archive: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	root, err := os.OpenRoot(dest)
	if err != nil {
		return m, 0, fmt.Errorf("archive: %w", err)
	}
	defer root.Close()
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if files == 0 {
				return m, 0, ErrFormat
			}
			// The tar end marker is not the end of the age payload:
			// read the rest, so a tail cut short or altered after it
			// fails authentication here rather than going unread.
			if _, err := io.Copy(io.Discard, zr); err != nil {
				return m, files, fmt.Errorf("archive: %w", err)
			}
			return m, files, nil
		}
		if err != nil {
			return m, files, fmt.Errorf("archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !validName(hdr.Name) || hdr.Size < 0 {
			return m, files, fmt.Errorf("archive: refusing entry %q (type %c)", hdr.Name, hdr.Typeflag)
		}
		if files == 0 {
			if hdr.Name != ManifestName || hdr.Size > 1<<20 {
				return m, 0, ErrFormat
			}
			b, err := io.ReadAll(tr)
			if err != nil {
				return m, 0, fmt.Errorf("archive: %w", err)
			}
			if json.Unmarshal(b, &m) != nil || m.Format != Format {
				return m, 0, fmt.Errorf("%w: manifest format %d", ErrFormat, m.Format)
			}
			if err := extractFile(root, hdr.Name, bytes.NewReader(b)); err != nil {
				return m, 0, err
			}
			files++
			continue
		}
		if err := extractFile(root, hdr.Name, tr); err != nil {
			return m, files, err
		}
		files++
	}
}

// extractFile writes one entry under root, creating its directories.
// os.Root refuses a path that leaves root, through a symlink too.
func extractFile(root *os.Root, name string, r io.Reader) error {
	local := filepath.FromSlash(name)
	if dir := filepath.Dir(local); dir != "." {
		if err := root.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("archive: %w", err)
		}
	}
	f, err := root.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("archive: %s appears twice", name)
		}
		return fmt.Errorf("archive: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("archive: %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("archive: %w", err)
	}
	return f.Close()
}

// ReadRecipients parses age recipients: public keys, one per line, with
// # comments and blank lines, as age's own recipients files are written.
func ReadRecipients(r io.Reader) ([]age.Recipient, error) {
	rs, err := age.ParseRecipients(r)
	if err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	return rs, nil
}

// ReadIdentities parses an age identity file.
func ReadIdentities(r io.Reader) ([]age.Identity, error) {
	ids, err := age.ParseIdentities(r)
	if err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	return ids, nil
}

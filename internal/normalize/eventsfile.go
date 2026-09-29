package normalize

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/klauspost/compress/zstd"
)

// A session's normalized events are stored as <uid>.jsonl.zst: JSONL
// in a run of independent zstd frames, each holding whole lines, and an
// index of where each frame starts in a skippable frame at the end.
// Any zstd reader decodes the file as one stream and skips the index,
// so `zstd -d`, DuckDB's read_ndjson and a plain decoder all see the
// JSONL. A reader that wants event N reads the index, seeks to the
// frame holding it, and decodes at most one frame before reaching it.
//
// A file written before compression is <uid>.jsonl, plain. Readers
// accept both, and the next normalize of the session replaces it.

// EventsExt is the compressed file's extension, and LegacyEventsExt the
// plain one's.
const (
	EventsExt       = ".jsonl.zst"
	LegacyEventsExt = ".jsonl"
)

// frameBytes is where a frame ends: the first line that takes it to
// this many raw bytes closes it. On transcripts, independent frames of
// 64 KiB compressed a third worse than whole files, and frames of a MiB
// lose little, while bounding what a page decodes before its first line
// (TKT-01M3K45MQ).
const frameBytes = 1 << 20

// Index layout: a skippable frame whose data is one entry per frame,
// then the entry count and indexMagic, so a reader finds it from the
// file's last bytes.
const (
	skippableMagic = 0x184D2A5E
	indexMagic     = "lpix"
	entrySize      = 16 // compressed offset, first line: uint64 each
	trailerSize    = 8  // entry count uint32, indexMagic
)

// EventsPath is where uid's compressed events are written.
func EventsPath(dir, uid string) string { return filepath.Join(dir, uid+EventsExt) }

// frameStart is where one frame begins in the file, and the position of
// its first line.
type frameStart struct {
	Off, Line int64
}

// WriteEventsFile writes events to w as indexed zstd frames.
func WriteEventsFile(w io.Writer, events []Event) error {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	defer enc.Close()
	var (
		raw   bytes.Buffer
		frame []byte
		index []frameStart
		off   int64
		line  int64
		// opened is the position of the first line in raw.
		opened int64
	)
	flush := func() error {
		if raw.Len() == 0 {
			return nil
		}
		frame = enc.EncodeAll(raw.Bytes(), frame[:0])
		if _, err := w.Write(frame); err != nil {
			return fmt.Errorf("normalize: %w", err)
		}
		index = append(index, frameStart{Off: off, Line: opened})
		off += int64(len(frame))
		raw.Reset()
		opened = line
		return nil
	}
	lines := lineEncoder(&raw)
	for _, ev := range events {
		if err := lines.Encode(ev); err != nil {
			return fmt.Errorf("normalize: %w", err)
		}
		line++
		if raw.Len() >= frameBytes {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return writeIndex(w, index)
}

func writeIndex(w io.Writer, index []frameStart) error {
	data := make([]byte, 0, len(index)*entrySize+trailerSize)
	for _, e := range index {
		data = binary.LittleEndian.AppendUint64(data, uint64(e.Off))
		data = binary.LittleEndian.AppendUint64(data, uint64(e.Line))
	}
	data = binary.LittleEndian.AppendUint32(data, uint32(len(index)))
	data = append(data, indexMagic...)
	head := binary.LittleEndian.AppendUint32(nil, skippableMagic)
	head = binary.LittleEndian.AppendUint32(head, uint32(len(data)))
	if _, err := w.Write(append(head, data...)); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	return nil
}

// EventsFile is one session's open events file, compressed or plain.
type EventsFile struct {
	f *os.File
	// Compressed is false for a plain file from before compression.
	Compressed bool
	// Size and MTime identify what was opened, for a cursor to check.
	Size  int64
	MTime int64
	index []frameStart
	dec   *zstd.Decoder
}

// OpenEvents opens uid's events under dir: the compressed file, or else
// a plain one. Neither there is an error that wraps os.ErrNotExist.
func OpenEvents(dir, uid string) (*EventsFile, error) {
	base := filepath.Join(dir, uid)
	for _, c := range []bool{true, false} {
		p := base + LegacyEventsExt
		if c {
			p = base + EventsExt
		}
		f, err := os.Open(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		ev := &EventsFile{f: f, Compressed: c, Size: st.Size(), MTime: st.ModTime().UnixNano()}
		if c {
			// An index that does not read leaves the file readable
			// from its start.
			ev.index, _ = readIndex(f, st.Size())
		}
		return ev, nil
	}
	return nil, fmt.Errorf("normalize: no events for %s: %w", uid, os.ErrNotExist)
}

func readIndex(f *os.File, size int64) ([]frameStart, error) {
	if size < 8+trailerSize {
		return nil, errors.New("normalize: no index")
	}
	var tail [trailerSize]byte
	if _, err := f.ReadAt(tail[:], size-trailerSize); err != nil {
		return nil, err
	}
	if string(tail[4:]) != indexMagic {
		return nil, errors.New("normalize: no index")
	}
	n := int64(binary.LittleEndian.Uint32(tail[:4]))
	dataLen := n*entrySize + trailerSize
	start := size - dataLen - 8
	if n > size/entrySize || start < 0 {
		return nil, errors.New("normalize: index does not fit")
	}
	buf := make([]byte, 8+dataLen)
	if _, err := f.ReadAt(buf, start); err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint32(buf) != skippableMagic || int64(binary.LittleEndian.Uint32(buf[4:])) != dataLen {
		return nil, errors.New("normalize: index frame header")
	}
	index := make([]frameStart, n)
	for i := range index {
		e := buf[8+i*entrySize:]
		index[i] = frameStart{Off: int64(binary.LittleEndian.Uint64(e)), Line: int64(binary.LittleEndian.Uint64(e[8:]))}
		if index[i].Off < 0 || index[i].Off >= start || (i > 0 && (index[i].Off <= index[i-1].Off || index[i].Line <= index[i-1].Line)) {
			return nil, errors.New("normalize: index out of order")
		}
	}
	if len(index) > 0 && (index[0].Off != 0 || index[0].Line != 0) {
		return nil, errors.New("normalize: index does not start at the first frame")
	}
	return index, nil
}

// From returns the events as JSONL from line first, which is at most
// pos: the start of the frame holding pos in a compressed file with an
// index, and otherwise the file's start. The caller skips the lines
// from first to pos. off is a byte offset a signed cursor recorded in a
// plain file, at line pos, and is used only there; zero ignores it. The
// reader is valid until the next From or Close.
func (e *EventsFile) From(pos, off int64) (r io.Reader, first int64, err error) {
	if !e.Compressed {
		if off <= 0 || off > e.Size {
			off, pos = 0, 0
		}
		if _, err := e.f.Seek(off, io.SeekStart); err != nil {
			return nil, 0, err
		}
		return e.f, pos, nil
	}
	start := frameStart{}
	if i := sort.Search(len(e.index), func(i int) bool { return e.index[i].Line > pos }); i > 0 {
		start = e.index[i-1]
	}
	if _, err := e.f.Seek(start.Off, io.SeekStart); err != nil {
		return nil, 0, err
	}
	d, err := zstd.NewReader(e.f, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(64<<20))
	if err != nil {
		return nil, 0, err
	}
	e.closeDecoder()
	e.dec = d
	return d, start.Line, nil
}

func (e *EventsFile) closeDecoder() {
	if e.dec != nil {
		e.dec.Close()
		e.dec = nil
	}
}

// Close closes the file.
func (e *EventsFile) Close() error {
	e.closeDecoder()
	return e.f.Close()
}

// ReadEventsFile returns uid's events under dir as JSONL, decompressed.
func ReadEventsFile(dir, uid string) ([]byte, error) {
	ev, err := OpenEvents(dir, uid)
	if err != nil {
		return nil, err
	}
	defer ev.Close()
	r, _, err := ev.From(0, 0)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("normalize: %s: %w", uid, err)
	}
	return b, nil
}

// RemoveEvents removes uid's events under dir, in both forms. Neither
// there is not an error.
func RemoveEvents(dir, uid string) error {
	for _, ext := range []string{EventsExt, LegacyEventsExt} {
		if err := os.Remove(filepath.Join(dir, uid+ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("normalize: %w", err)
		}
	}
	return nil
}

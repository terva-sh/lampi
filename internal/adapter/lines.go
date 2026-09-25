package adapter

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

// ScanLines calls fn with each line of r, without the line ending. A
// line longer than max bytes is not held in memory: it is read past and
// fn gets it as long true with a nil line. fn returns false to stop.
// A torn last line with no newline is still passed to fn.
func ScanLines(r io.Reader, max int, fn func(line []byte, long bool) bool) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var buf []byte
	long := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !long {
			if n := len(buf) + len(bytes.TrimSuffix(chunk, []byte{'\n'})); n > max {
				long = true
				buf = buf[:0]
			} else {
				buf = append(buf, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		end := errors.Is(err, io.EOF)
		switch {
		case long:
			if !fn(nil, true) {
				return nil
			}
		case len(buf) > 0 || !end:
			line := bytes.TrimSuffix(buf, []byte{'\n'})
			line = bytes.TrimSuffix(line, []byte{'\r'})
			if !fn(line, false) {
				return nil
			}
		}
		if end {
			return nil
		}
		buf = buf[:0]
		long = false
	}
}

// LongLineError is a file whose session identity may sit on a line
// past the reader's cap. The file is left out of this pass rather than
// filed under an id that would split the session.
type LongLineError struct {
	Max int
}

func (e *LongLineError) Error() string {
	return fmt.Sprintf("a line over %d bytes came before the session identity; skipped", e.Max)
}

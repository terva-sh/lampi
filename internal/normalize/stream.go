package normalize

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"time"
)

// EventSink consumes an event before the next record is read. A sink must not
// retain events if it needs a working set independent of transcript length.
type EventSink func(Event) error

type StreamNormalizer interface {
	Stream(context.Context, io.Reader, EventSink) error
}

type contextInput struct {
	ctx context.Context
	io.Reader
}

func (r contextInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// streamLines retains one input record and its events. Memory scales with the
// largest record/event, not the full transcript. Offset and malformed-line
// behavior match the slice APIs, including a final line without a newline.
func streamLines(ctx context.Context, input io.Reader, sink EventSink, parse func(int, int, []byte) ([]Event, error), badEvent func(int, int, int) (Event, error)) error {
	r := bufio.NewReaderSize(contextInput{ctx, input}, 64<<10)
	offset, lineNo, objects, unreadable := 0, 0, 0, 0
	var firstBad error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := r.ReadBytes('\n')
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		if len(line) == 0 {
			break
		}
		at := offset
		offset += len(line)
		lineNo++
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(bytes.TrimSpace(line)) != 0 {
			evs, err := parse(lineNo, at, line)
			var bad unreadableLine
			if errors.As(err, &bad) {
				if firstBad == nil {
					firstBad = err
				}
				unreadable++
				if unreadable > maxUnreadable {
					return tooUnreadable(firstBad)
				}
				ev, e := badEvent(lineNo, at, len(line))
				if e != nil {
					return e
				}
				evs = []Event{ev}
			} else if err != nil {
				return err
			} else {
				objects++
			}
			for _, ev := range evs {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := sink(ev); err != nil {
					return err
				}
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	if objects == 0 && firstBad != nil {
		return firstBad
	}
	return nil
}

func (c Claude) Stream(ctx context.Context, r io.Reader, sink EventSink) error {
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
	c.Now = c.Now.UTC()
	var st claudeState
	return streamLines(ctx, r, sink, func(n, off int, b []byte) ([]Event, error) { return c.line(n, off, b, &st) }, func(n, off, size int) (Event, error) {
		return c.emit(&st, "", EventError, "", time.Time{}, off, unreadableText(n, size), Tool{}, Usage{}, unreadableExtra(n, size))
	})
}
func (c Codex) Stream(ctx context.Context, r io.Reader, sink EventSink) error {
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
	c.Now = c.Now.UTC()
	var st codexState
	return streamLines(ctx, r, sink, func(n, off int, b []byte) ([]Event, error) { return c.line(n, off, b, &st) }, func(n, off, size int) (Event, error) {
		return c.emit(&st, "", EventError, "", time.Time{}, off, unreadableText(n, size), Tool{}, Usage{}, unreadableExtra(n, size))
	})
}
func (t Terva) Stream(ctx context.Context, r io.Reader, sink EventSink) error {
	if t.Now.IsZero() {
		t.Now = time.Now()
	}
	t.Now = t.Now.UTC()
	var st metaState
	return streamLines(ctx, r, sink, func(n, off int, b []byte) ([]Event, error) { return t.line(n, off, b, &st) }, func(n, off, size int) (Event, error) {
		return t.emit(&st, "", EventError, "", time.Time{}, off, unreadableText(n, size), Tool{}, Usage{}, unreadableExtra(n, size))
	})
}

package redact

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
)

const readerBuffer = 64 << 10

// ScanReaderAt is ScanAppended for an immutable random-access source. Body
// buffers have fixed sizes, even for a single long line, escape run or key.
// The offset-preserving JSON view is staged privately in scratch and removed
// before returning. scratch must be a directory owned by the caller.
// Artifact bytes and the view must remain unchanged throughout this call.
func (Ruleset) ScanReaderAt(ctx context.Context, raw io.ReaderAt, size, clean int64, scratch string) (Result, error) {
	if size < 0 {
		return Result{}, fmt.Errorf("redact: negative source size")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if clean >= size || size == 0 {
		return result(nil), nil
	}
	from, err := appendedReaderFrom(ctx, raw, max(clean, 0))
	if err != nil {
		return Result{}, err
	}
	view, err := os.CreateTemp(scratch, ".scan-view-*")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(view.Name())
	defer view.Close()
	if runtime.GOOS != "windows" {
		if err := os.Remove(view.Name()); err != nil {
			return Result{}, err
		}
	}
	w := bufio.NewWriterSize(view, readerBuffer)
	if err := writeJSONView(w, contextReader{ctx, io.NewSectionReader(raw, from, size-from)}); err != nil {
		return Result{}, err
	}
	if err := w.Flush(); err != nil {
		return Result{}, err
	}
	st, err := view.Stat()
	if err != nil {
		return Result{}, err
	}
	if st.Size() != size-from {
		return Result{}, io.ErrUnexpectedEOF
	}
	return scanView(ctx, view, st.Size(), max(clean-from, 0))
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

// writeJSONView implements jsonView without buffering an escape run. Each
// escape occupies exactly its original byte count, including nested escapes.
func writeJSONView(w *bufio.Writer, input io.Reader) error {
	r := bufio.NewReaderSize(input, readerBuffer)
	var fill [4096]byte
	repeat := func(c byte, count int64) error {
		if count < 64 {
			for range count {
				if err := w.WriteByte(c); err != nil {
					return err
				}
			}
			return nil
		}
		for i := range fill {
			fill[i] = c
		}
		for count > 0 {
			n := int(min(count, int64(len(fill))))
			if _, err := w.Write(fill[:n]); err != nil {
				return err
			}
			count -= int64(n)
		}
		return nil
	}
	for {
		b, err := r.ReadSlice('\\')
		if err != nil {
			if _, werr := w.Write(b); werr != nil {
				return werr
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
		if _, err := w.Write(b[:len(b)-1]); err != nil {
			return err
		}
		run := int64(1)
		var c byte
		for {
			c, err = r.ReadByte()
			if err != nil {
				if err == io.EOF {
					return repeat('\\', run)
				}
				return err
			}
			if c != '\\' {
				break
			}
			run++
		}
		pad := byte(' ')
		switch c {
		case 'n':
			c = '\n'
		case 't':
			c = '\t'
		case 'r':
			c = '\r'
		case '"':
		case 'b', 'f':
			c = ' '
		case '/':
			pad = '/'
		case 'u':
			b, _ := r.Peek(4)
			cp, ok := hex4(b)
			if ok {
				_, _ = r.Discard(4)
				run += 4
				c = ' '
				if cp == '\n' || cp == '\t' || cp == '\r' || (cp >= 0x20 && cp < 0x7f) {
					c = byte(cp)
				}
			} else {
				pad = '\\'
			}
		default:
			pad = '\\'
		}
		if err := repeat(pad, run); err != nil {
			return err
		}
		if err := w.WriteByte(c); err != nil {
			return err
		}
	}
}

// A tiny backward cache implements the same two runStart walks as the slice
// API, including arbitrarily long runs, without retaining the prefix.
func appendedReaderFrom(ctx context.Context, raw io.ReaderAt, clean int64) (int64, error) {
	from := max(clean-Overlap, 0)
	buf := make([]byte, readerBuffer)
	base, end := int64(-1), int64(-1)
	at := func(i int64) (byte, error) {
		if i < base || i >= end {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			base = i / readerBuffer * readerBuffer
			n, err := raw.ReadAt(buf, base)
			end = base + int64(n)
			if err != nil && err != io.EOF {
				return 0, err
			}
			if i >= end {
				return 0, io.ErrUnexpectedEOF
			}
		}
		return buf[i-base], nil
	}
	for range 2 {
		for from > 0 {
			var last [6]byte
			n := int(min(from, 6))
			for j := range n {
				c, err := at(from - int64(n-j))
				if err != nil {
					return 0, err
				}
				last[6-n+j] = c
			}
			if repeatable(last[5]) || last[5] == '\\' {
				from--
			} else if n >= 2 && last[4] == '\\' && bytes.IndexByte([]byte("ntrbf"), last[5]) >= 0 {
				from -= 2
			} else if n == 6 && last[0] == '\\' && last[1] == 'u' {
				if _, ok := hex4(last[2:]); !ok {
					break
				}
				from -= 6
			} else {
				break
			}
		}
		from = max(from-literalSlack, 0)
	}
	return from, nil
}

type readerSpan struct {
	start, end  int64
	order, rule int
}

type checkedRunes struct {
	*bufio.Reader
	err error
}

func (r *checkedRunes) ReadRune() (rune, int, error) {
	c, n, err := r.Reader.ReadRune()
	if err != nil && err != io.EOF {
		r.err = err
	}
	return c, n, err
}

func scanView(ctx context.Context, view io.ReaderAt, size, clean int64) (Result, error) {
	res := result(nil)
	next := make([]int64, len(literals))
	seen := make([]bool, len(rules))
	maxLead, maxLiteral := 0, 2
	for _, l := range literals {
		maxLiteral = max(maxLiteral, len(l.lit))
		maxLead = max(maxLead, len(rules[l.rule].lead))
	}
	// A span may start maxLead bytes before its literal. Delay choosing that
	// small frontier so earliest/longest/order selection matches choose().
	var pending []readerSpan
	covered := int64(0)
	flush := func(before int64) {
		for len(pending) > 0 {
			best := 0
			for i, sp := range pending {
				b := pending[best]
				if sp.start < b.start || (sp.start == b.start && (sp.end > b.end || sp.end == b.end && sp.order < b.order)) {
					best = i
				}
			}
			sp := pending[best]
			if sp.start >= before {
				return
			}
			pending[best] = pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if sp.start >= covered {
				res.Hits++
				seen[sp.rule] = true
				covered = sp.end
			}
		}
	}
	buf := make([]byte, readerBuffer+maxLiteral)
	var previous byte
	runes := &checkedRunes{Reader: bufio.NewReaderSize(nil, 4096)}
	for base := int64(0); base < size; base += readerBuffer {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		n := int(min(int64(len(buf)), size-base))
		if _, err := view.ReadAt(buf[:n], base); err != nil {
			return Result{}, err
		}
		limit := int(min(int64(readerBuffer), size-base-1))
		for i := 0; i < limit; i++ {
			g := pairs[int(buf[i])<<8|int(buf[i+1])]
			if g == 0 {
				continue
			}
			at := base + int64(i)
			flush(at - int64(maxLead))
			for _, li := range pairGroups[g] {
				l := &literals[li]
				if at < next[li] || !hasLiteral(buf[i:n], l) {
					continue
				}
				r := &rules[l.rule]
				prev := previous
				if i > 0 {
					prev = buf[i-1]
				}
				if r.word && at > 0 && isWord(prev) {
					continue
				}
				runes.Reader.Reset(contextReader{ctx, io.NewSectionReader(view, at, size-at)})
				runes.err = nil
				loc := r.re.FindReaderSubmatchIndex(runes)
				if runes.err != nil {
					return Result{}, runes.err
				}
				if loc == nil {
					continue
				}
				end := at + int64(loc[1])
				if end < size && len(r.notAfter) > 0 {
					var b [1]byte
					if _, err := view.ReadAt(b[:], end); err != nil {
						return Result{}, err
					}
					if bytes.IndexByte([]byte(r.notAfter), b[0]) >= 0 {
						continue
					}
				}
				next[li] = end
				example, err := readerExample(ctx, view, at, r, loc)
				if err != nil {
					return Result{}, err
				}
				if example || end <= clean {
					continue
				}
				start := at
				if n := len(r.lead); n > 0 && at >= int64(n) {
					lead := make([]byte, n)
					if _, err := view.ReadAt(lead, at-int64(n)); err != nil {
						return Result{}, err
					}
					if bytes.EqualFold(lead, r.lead) {
						start -= int64(n)
					}
				}
				pending = append(pending, readerSpan{start, end, li, l.rule})
			}
		}
		previous = buf[min(readerBuffer, n)-1]
	}
	flush(size + 1)
	for i, hit := range seen {
		if hit {
			res.Rules = append(res.Rules, rules[i].name)
		}
	}
	return res, nil
}

func readerExample(ctx context.Context, view io.ReaderAt, at int64, r *rule, loc []int) (bool, error) {
	if len(r.examples) == 0 {
		return false, nil
	}
	start, end := loc[0], loc[1]
	if i := r.re.SubexpIndex("key"); i > 0 && loc[2*i] >= 0 {
		start, end = loc[2*i], loc[2*i+1]
	}
	for _, example := range r.examples {
		b := bufio.NewReaderSize(contextReader{ctx, io.NewSectionReader(view, at+int64(start), int64(end-start))}, 256)
		pos, previous := 0, byte(0)
		for {
			c, err := b.ReadByte()
			if err != nil {
				if err != io.EOF {
					return false, err
				}
				if pos == len(example) {
					return true, nil
				}
				break
			}
			if c == '/' && previous == '/' {
				continue
			}
			previous = c
			if pos >= len(example) || example[pos] != c {
				break
			}
			pos++
		}
	}
	return false, nil
}

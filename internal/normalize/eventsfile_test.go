package normalize

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// manyEvents is enough events for several frames.
func manyEvents(n int) []Event {
	events := make([]Event, n)
	for i := range events {
		text := fmt.Sprintf("event %d %s", i, strings.Repeat("words ", 150))
		events[i] = Event{SessionID: "s", EventType: "message", ContentText: &text}
	}
	return events
}

func plainJSONL(t *testing.T, events []Event) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteJSONL(&buf, events); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The file is JSONL to any zstd decoder, which skips the index, and it
// is far smaller than the JSONL (TKT-01M3K45MX).
func TestEventsFileDecodesAsPlainZstd(t *testing.T) {
	events := manyEvents(3000)
	dir := t.TempDir()
	if err := WriteFile(dir, "s", events); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(EventsPath(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := zstd.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	got, err := io.ReadAll(d)
	if err != nil {
		t.Fatal(err)
	}
	want := plainJSONL(t, events)
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded %d bytes, want %d", len(got), len(want))
	}
	st, _ := f.Stat()
	if st.Size()*5 > int64(len(want)) {
		t.Fatalf("%d bytes of JSONL stored in %d", len(want), st.Size())
	}
	if b, err := ReadEventsFile(dir, "s"); err != nil || !bytes.Equal(b, want) {
		t.Fatalf("ReadEventsFile: %d bytes %v", len(b), err)
	}
}

// From starts at the frame holding pos, no later than pos and within a
// frame of it, so the lines after the skip are the ones asked for.
func TestEventsFileFromSeeksToTheFrame(t *testing.T) {
	events := manyEvents(3000)
	dir := t.TempDir()
	if err := WriteFile(dir, "s", events); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(plainJSONL(t, events), []byte("\n")), []byte("\n"))
	ev, err := OpenEvents(dir, "s")
	if err != nil {
		t.Fatal(err)
	}
	defer ev.Close()
	if !ev.Compressed || len(ev.index) < 3 {
		t.Fatalf("compressed %v, %d frames", ev.Compressed, len(ev.index))
	}
	var starts []int64
	for _, e := range ev.index {
		starts = append(starts, e.Line)
	}
	for _, pos := range append(starts, 0, 1, starts[1]-1, starts[2]+7, 2999, 3000) {
		r, first, err := ev.From(pos, 0)
		if err != nil {
			t.Fatal(err)
		}
		if first > pos {
			t.Fatalf("pos %d: first %d is past it", pos, first)
		}
		br := bufio.NewReader(r)
		for i := first; i < pos; i++ {
			if _, err := br.ReadBytes('\n'); err != nil {
				t.Fatalf("pos %d: skipping line %d: %v", pos, i, err)
			}
		}
		line, err := br.ReadBytes('\n')
		if pos == 3000 {
			if err != io.EOF || len(line) != 0 {
				t.Fatalf("past the end: %q %v", line, err)
			}
			continue
		}
		if err != nil || !bytes.Equal(bytes.TrimSuffix(line, []byte("\n")), lines[pos]) {
			t.Fatalf("pos %d (from %d): read %.40q %v", pos, first, line, err)
		}
	}
}

// A file whose index does not read is still read from its start, and
// an empty session is a file of no frames that reads as nothing.
func TestEventsFileWithoutAnIndexReadsFromTheStart(t *testing.T) {
	events := manyEvents(3000)
	dir := t.TempDir()
	if err := WriteFile(dir, "s", events); err != nil {
		t.Fatal(err)
	}
	p := EventsPath(dir, "s")
	b, _ := os.ReadFile(p)
	b[len(b)-1] ^= 0xff
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	ev, err := OpenEvents(dir, "s")
	if err != nil {
		t.Fatal(err)
	}
	defer ev.Close()
	if ev.index != nil {
		t.Fatalf("a damaged index read: %v", ev.index)
	}
	if _, first, err := ev.From(2500, 0); err != nil || first != 0 {
		t.Fatalf("from without an index: %d %v", first, err)
	}

	if err := WriteFile(dir, "empty", nil); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadEventsFile(dir, "empty"); err != nil || len(got) != 0 {
		t.Fatalf("empty session: %q %v", got, err)
	}
}

// A plain file from before compression reads, from a signed offset
// too, and the next write replaces it.
func TestPlainEventsFileStillReads(t *testing.T) {
	events := manyEvents(10)
	want := plainJSONL(t, events)
	dir := t.TempDir()
	plain := filepath.Join(dir, "s"+LegacyEventsExt)
	if err := os.WriteFile(plain, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadEventsFile(dir, "s"); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("plain file: %d bytes %v", len(got), err)
	}
	ev, err := OpenEvents(dir, "s")
	if err != nil {
		t.Fatal(err)
	}
	third := int64(bytes.LastIndexByte(want[:bytes.Index(want, []byte("event 3 "))], '\n') + 1)
	r, first, err := ev.From(3, third)
	if err != nil || first != 3 {
		t.Fatalf("from an offset: %d %v", first, err)
	}
	line, _ := bufio.NewReader(r).ReadBytes('\n')
	if !bytes.Contains(line, []byte("event 3 ")) {
		t.Fatalf("read %.60q", line)
	}
	ev.Close()

	if err := WriteFile(dir, "s", events); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Fatalf("plain file kept beside the compressed one: %v", err)
	}
	if err := RemoveEvents(dir, "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenEvents(dir, "s"); !os.IsNotExist(err) && !strings.Contains(fmt.Sprint(err), "file does not exist") {
		t.Fatalf("after remove: %v", err)
	}
}

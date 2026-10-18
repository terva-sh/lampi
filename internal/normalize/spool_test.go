package normalize

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestPrivateRowSpoolPreservesNullableAndLargeStrings(t *testing.T) {
	empty := ""
	text := strings.Repeat("quoted \"\\\x00é\n", 9000)
	for _, row := range []ParquetRow{{SchemaVersion: 1, EventID: "id", EventJSON: text}, {SchemaVersion: 1, ContentText: &empty, ProjectID: &empty, EventJSON: "{}"}, {SchemaVersion: 1, ContentText: &text, EventJSON: text}} {
		var raw bytes.Buffer
		out := bufio.NewWriter(&raw)
		if err := writeSpoolRow(out, row); err != nil {
			t.Fatal(err)
		}
		if err := out.Flush(); err != nil {
			t.Fatal(err)
		}
		got, err := readSpoolRow(bufio.NewReader(&raw), int64(raw.Len()))
		if err != nil || !reflect.DeepEqual(got, row) {
			t.Fatalf("row changed: %v", err)
		}
	}
}

func TestPrivateSpoolRejectsTruncatedRows(t *testing.T) {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	if err := writeSpoolRow(w, ParquetRow{SchemaVersion: 1, EventJSON: "a long event", SessionID: "session"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	full := b.Bytes()
	for cut := 1; cut < len(full); cut++ {
		_, err := readSpoolRow(bufio.NewReader(bytes.NewReader(full[:cut])), int64(len(full)))
		if err == nil || err == io.EOF {
			t.Fatalf("truncation at %d accepted as a complete file: %v", cut, err)
		}
	}
}

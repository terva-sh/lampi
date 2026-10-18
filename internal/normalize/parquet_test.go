package normalize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

func TestParquetPartitionedByDateAndHarness(t *testing.T) {
	root := t.TempDir()
	uid := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	day1 := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	day2 := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	text := "partition pond"
	events := []Event{
		sampleEvent("terva", day1, text),
		sampleEvent("terva", day2, "next day"),
		sampleEvent("claude", day1, "other harness"),
	}
	if err := WriteParquet(root, uid, events); err != nil {
		t.Fatal(err)
	}
	terva1 := mustPath(t, root, "2026-09-22", "terva", uid)
	terva2 := mustPath(t, root, "2026-09-23", "terva", uid)
	claude := mustPath(t, root, "2026-09-22", "claude", uid)
	for _, path := range []string{terva1, terva2, claude} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %o", path, info.Mode().Perm())
		}
	}
	rows := readParquet(t, terva1)
	if len(rows) != 1 || rows[0].ContentText == nil || *rows[0].ContentText != text {
		t.Fatalf("day1 rows %+v", rows)
	}
	if rows[0].Harness != "terva" || rows[0].EventJSON == "" || rows[0].SchemaVersion != SchemaVersion {
		t.Fatalf("row %+v", rows[0])
	}

	if err := WriteParquet(root, uid, []Event{sampleEvent("terva", day1, text)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(terva1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(terva2); !os.IsNotExist(err) {
		t.Fatalf("stale date partition: %v", err)
	}
	if _, err := os.Stat(claude); !os.IsNotExist(err) {
		t.Fatalf("stale harness partition: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "date=2026-09-23")); !os.IsNotExist(err) {
		t.Fatalf("empty date directory: %v", err)
	}
}

func TestParquetRejectsUnsafeHarness(t *testing.T) {
	ev := sampleEvent("../terva", time.Now().UTC().Format(time.RFC3339Nano), "x")
	err := WriteParquet(t.TempDir(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", []Event{ev})
	if err == nil {
		t.Fatal("expected an error for a harness that is not a path segment")
	}
}

func sampleEvent(harness, recorded, text string) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventID:       "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SessionID:     harness + ":sid",
		Harness:       harness,
		RecordedAt:    recorded,
		IngestedAt:    recorded,
		EventType:     EventMessage,
		Actor:         ActorUser,
		ContentText:   &text,
		RawType:       "message",
		Redaction:     Redaction{Status: "none", Ruleset: "v1"},
	}
}

func mustPath(t *testing.T, root, date, harness, uid string) string {
	t.Helper()
	path, err := ParquetPath(root, date, harness, uid)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func readParquet(t *testing.T, path string) []ParquetRow {
	t.Helper()
	rows, err := parquet.ReadFile[ParquetRow](path)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestParquetColumnsAreZstdCompressed(t *testing.T) {
	root := t.TempDir()
	uid := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	day := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	var events []Event
	for i := 0; i < 200; i++ {
		events = append(events, sampleEvent("terva", day, "the same pond, described again and again"))
	}
	if err := WriteParquet(root, uid, events); err != nil {
		t.Fatal(err)
	}
	path := mustPath(t, root, "2026-09-22", "terva", uid)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	pf, err := parquet.OpenFile(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, rg := range pf.Metadata().RowGroups {
		for _, c := range rg.Columns {
			if c.MetaData.Codec != format.Zstd {
				t.Fatalf("column %v codec %v", c.MetaData.PathInSchema, c.MetaData.Codec)
			}
		}
	}
	if rows := readParquet(t, path); len(rows) != len(events) {
		t.Fatalf("read %d rows, want %d", len(rows), len(events))
	}
}

func TestParquetInterleavedPartitionsPreserveCompleteEvents(t *testing.T) {
	root, uid := t.TempDir(), "interleaved"
	var events []Event
	for i := 0; i < 600; i++ {
		ev := sampleEvent([]string{"claude", "codex"}[i%2], fmt.Sprintf("2026-10-%02dT00:00:00Z", 17+i%3), fmt.Sprintf("event %d: <tag> & \"quoted\"\nnext", i))
		ev.EventID = fmt.Sprint(i)
		ev.Extra = extraMap{"unknown": map[string]any{"nested": []any{true, "<>&\n"}}}
		events = append(events, ev)
	}
	if err := WriteParquet(root, uid, events); err != nil {
		t.Fatal(err)
	}
	paths, err := SessionParquet(root, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 6 {
		t.Fatalf("partitions=%d", len(paths))
	}
	for _, path := range paths {
		var expected []string
		for _, ev := range events {
			date, err := PartitionDate(ev.RecordedAt, ev.IngestedAt)
			if err != nil {
				t.Fatal(err)
			}
			if mustPath(t, root, date, ev.Harness, uid) != path {
				continue
			}
			var buf bytes.Buffer
			if err := WriteJSONL(&buf, []Event{ev}); err != nil {
				t.Fatal(err)
			}
			expected = append(expected, strings.TrimSuffix(buf.String(), "\n"))
		}
		rows := readParquet(t, path)
		if len(rows) != len(expected) {
			t.Fatalf("%s: rows=%d want=%d", path, len(rows), len(expected))
		}
		for i, row := range rows {
			if row.EventJSON != expected[i] {
				t.Fatalf("%s row %d changed JSON or ordering", path, i)
			}
			var ev Event
			if err := json.Unmarshal([]byte(row.EventJSON), &ev); err != nil {
				t.Fatal(err)
			}
			if ev.ContentText == nil || row.ContentText == nil || *row.ContentText != *ev.ContentText {
				t.Fatalf("%s row %d lost text", path, i)
			}
		}
	}
	if err := WriteParquet(root, uid, nil); err != nil {
		t.Fatal(err)
	}
	if paths, err := SessionParquet(root, uid); err != nil || len(paths) != 0 {
		t.Fatalf("empty projection left partitions: %v, %v", paths, err)
	}
}

func TestParquetBoundsRowGroupsByRowsAndBytes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []Event
		groups int
	}{
		{"rows", func() []Event {
			evs := make([]Event, parquetRowGroupRows+5)
			for i := range evs {
				evs[i] = sampleEvent("claude", "2026-10-17T00:00:00Z", fmt.Sprint(i))
			}
			return evs
		}(), 2},
		{"bytes-and-oversized-event", []Event{
			sampleEvent("claude", "2026-10-17T00:00:00Z", strings.Repeat("a", parquetRowGroupBytes/3)),
			sampleEvent("claude", "2026-10-17T00:00:00Z", strings.Repeat("b", parquetRowGroupBytes/3)),
			sampleEvent("claude", "2026-10-17T00:00:00Z", strings.Repeat("c", parquetRowGroupBytes+32)),
		}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := WriteParquet(root, "bounded", tc.events); err != nil {
				t.Fatal(err)
			}
			path := mustPath(t, root, "2026-10-17", "claude", "bounded")
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			pf, err := parquet.OpenFile(f, st.Size())
			if err != nil {
				t.Fatal(err)
			}
			if len(pf.RowGroups()) != tc.groups {
				t.Fatalf("row groups=%d want=%d", len(pf.RowGroups()), tc.groups)
			}
			rows := readParquet(t, path)
			if len(rows) != len(tc.events) {
				t.Fatalf("rows=%d want=%d", len(rows), len(tc.events))
			}
			for i, row := range rows {
				if row.ContentText == nil || *row.ContentText != *tc.events[i].ContentText {
					t.Fatalf("row %d was truncated or reordered", i)
				}
			}
		})
	}
}

func TestParquetLateFailureKeepsOldPartitionsAndRemovesTemps(t *testing.T) {
	root, uid := t.TempDir(), "failure"
	old := []Event{sampleEvent("claude", "2026-10-17T00:00:00Z", "previous complete generation")}
	if err := WriteParquet(root, uid, old); err != nil {
		t.Fatal(err)
	}
	path := mustPath(t, root, "2026-10-17", "claude", uid)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first := sampleEvent("claude", "2026-10-17T00:00:00Z", "unpublished replacement")
	bad := sampleEvent("codex", "2026-10-18T00:00:00Z", "cannot encode this event")
	bad.Extra = extraMap{"unsupported": make(chan int)}
	if err := WriteParquet(root, uid, []Event{first, bad}); err == nil {
		t.Fatal("accepted an unserializable event")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("replaced old partition before all serialization succeeded: %v", err)
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasPrefix(d.Name(), ".parquet-") || path != mustPath(t, root, "2026-10-17", "claude", uid)) {
			t.Errorf("left a temporary or unpublished partition: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

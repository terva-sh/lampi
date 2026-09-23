package normalize

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
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

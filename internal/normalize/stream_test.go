package normalize

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func TestStreamMatchesCompatibility(t *testing.T) {
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		n    interface {
			Normalizer
			StreamNormalizer
		}
		raw []byte
	}{
		{"claude", Claude{Now: now, Digest: strings.Repeat("a", 64)}, claudeFixture()},
		{"codex", Codex{Now: now, Digest: strings.Repeat("b", 64)}, codexFixture()},
		{"terva", Terva{Now: now, Digest: strings.Repeat("c", 64)}, fixtureTranscript()},
		{"errors", Terva{Now: now, Kind: "errors_jsonl"}, []byte("{\"error\":\"failed\"}\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, raw := range [][]byte{tc.raw, bytes.ReplaceAll(tc.raw, []byte{'\n'}, []byte{'\r', '\n'}), append([]byte("broken\n\n"), bytes.TrimSuffix(tc.raw, []byte{'\n'})...), []byte("broken\n"), []byte("\n\r\n"), bytes.Repeat([]byte("no\n"), 65)} {
				want, we := tc.n.Normalize(t.Context(), raw)
				var got []Event
				ge := tc.n.Stream(t.Context(), bytes.NewReader(raw), func(ev Event) error { got = append(got, ev); return nil })
				if (we == nil) != (ge == nil) || we != nil && we.Error() != ge.Error() {
					t.Fatalf("error mismatch: %v / %v", we, ge)
				}
				if we != nil {
					continue
				}
				for i := range want {
					want[i].EventID = ""
				}
				for i := range got {
					got[i].EventID = ""
				}
				if len(want) != len(got) || len(want) > 0 && !reflect.DeepEqual(got, want) {
					t.Fatalf("stream differs: got %d want %d", len(got), len(want))
				}
			}
		})
	}
}

type failingInput struct{}

func (failingInput) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestStreamFailureAndCancellation(t *testing.T) {
	n := Claude{}
	if err := n.Stream(t.Context(), failingInput{}, func(Event) error { return nil }); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	raw := bytes.Repeat([]byte("{}\n"), 100)
	ctx, cancel := context.WithCancel(t.Context())
	count := 0
	err := n.Stream(ctx, bytes.NewReader(raw), func(Event) error { count++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if err := n.Stream(t.Context(), bytes.NewReader(raw), func(Event) error { return io.ErrClosedPipe }); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
}

func TestStagePreservesEventsAndPartitions(t *testing.T) {
	root := t.TempDir()
	uid := "session"
	events, err := (Terva{Now: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)}).Normalize(t.Context(), fixtureTranscript())
	if err != nil {
		t.Fatal(err)
	}
	// Revisit a partition after another day: spooling must append in order.
	more := events[0]
	more.RecordedAt = "2026-09-24T00:00:00Z"
	events = append(events, more, events[0])
	st, err := Stage(t.Context(), root, uid, func(sink EventSink) error {
		for _, ev := range events {
			if err := sink(ev); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	norm, pq := filepath.Join(root, "normalized"), filepath.Join(root, "parquet")
	if err := st.Publish(norm, pq); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadEventsFile(norm, uid)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	_ = WriteJSONL(&want, events)
	if !bytes.Equal(raw, want.Bytes()) {
		t.Fatal("events changed")
	}
	parts, err := SessionParquet(pq, uid)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, path := range parts {
		rows, err := parquet.ReadFile[ParquetRow](path)
		if err != nil {
			t.Fatal(err)
		}
		total += len(rows)
		for _, row := range rows {
			if row.EventJSON == "" {
				t.Fatal("missing event")
			}
		}
	}
	if total != len(events) {
		t.Fatalf("rows=%d want=%d", total, len(events))
	}
}

func TestStageFailureLeavesPriorPublication(t *testing.T) {
	root := t.TempDir()
	norm := filepath.Join(root, "normalized")
	pq := filepath.Join(root, "parquet")
	uid := "session"
	events, err := (Claude{Now: time.Now()}).Normalize(t.Context(), claudeFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(norm, uid, events); err != nil {
		t.Fatal(err)
	}
	if err := WriteParquet(pq, uid, events); err != nil {
		t.Fatal(err)
	}
	before, _ := ReadEventsFile(norm, uid)
	for _, mode := range []string{"parse", "cancel"} {
		ctx, cancel := context.WithCancel(t.Context())
		st, err := Stage(ctx, root, uid, func(sink EventSink) error {
			if err := sink(events[0]); err != nil {
				return err
			}
			if mode == "cancel" {
				cancel()
				return ctx.Err()
			}
			return io.ErrUnexpectedEOF
		})
		cancel()
		if st != nil || err == nil {
			t.Fatalf("stage=%v err=%v", st, err)
		}
		after, _ := ReadEventsFile(norm, uid)
		if !bytes.Equal(before, after) {
			t.Fatal("failed staging changed publication")
		}
		paths, _ := filepath.Glob(filepath.Join(root, ".projection-*"))
		if len(paths) != 0 {
			t.Fatalf("staging leaked: %v", paths)
		}
	}
	// A failed final rename must never leave a partial pair marked published.
	st, err := Stage(t.Context(), root, uid, func(sink EventSink) error { return sink(events[0]) })
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := os.Remove(filepath.Join(st.root, "events")); err != nil {
		t.Fatal(err)
	}
	if err := st.Publish(norm, pq); err == nil {
		t.Fatal("missing staged events accepted")
	}
	if _, err := os.Stat(EventsPath(norm, uid)); !os.IsNotExist(err) {
		t.Fatal("partial events remain")
	}
	parts, _ := SessionParquet(pq, uid)
	if len(parts) != 0 {
		t.Fatal("partial parquet remains")
	}
}

func TestStagePanicCleanupAndAbandonedRecovery(t *testing.T) {
	root := t.TempDir()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected producer panic")
			}
		}()
		_, _ = Stage(t.Context(), root, "panic", func(EventSink) error { panic("producer") })
	}()
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("panic leaked private stage")
	}
	abandoned := filepath.Join(root, ".projection-abandoned")
	if err := os.Mkdir(abandoned, 0700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(root, "normalized")
	if err := os.Mkdir(keep, 0700); err != nil {
		t.Fatal(err)
	}
	if err := CleanStaging(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
		t.Fatal("abandoned stage retained", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("removed unrelated data", err)
	}
}

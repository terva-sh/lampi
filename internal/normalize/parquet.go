package normalize

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

// ParquetRoot is the directory name under the lake data dir.
const ParquetRoot = "parquet"

// Bound both serialized batches and the column/page buffers retained until a
// row group is flushed. A single event may exceed either byte budget. Only one
// partition writer is open at a time; input events, partition indices and file
// footer metadata are separate from these working buffers.
const (
	parquetBatchRows     = 128
	parquetBatchBytes    = 1 << 20
	parquetRowGroupRows  = 2048
	parquetRowGroupBytes = 8 << 20
)

// ParquetRow is one normalized event inside a partition file.
// EventJSON is the schema_version 1 object, with the same escaping as
// the JSONL export. ContentText and ProjectID are null when the event
// has none.
type ParquetRow struct {
	SchemaVersion int32   `parquet:"schema_version"`
	EventID       string  `parquet:"event_id"`
	SessionID     string  `parquet:"session_id"`
	Harness       string  `parquet:"harness"`
	RecordedAt    string  `parquet:"recorded_at"`
	IngestedAt    string  `parquet:"ingested_at"`
	EventType     string  `parquet:"event_type"`
	Actor         string  `parquet:"actor"`
	ContentText   *string `parquet:"content_text,optional"`
	ProjectID     *string `parquet:"project_id,optional"`
	CWDHash       string  `parquet:"cwd_hash"`
	RawType       string  `parquet:"raw_type"`
	EventJSON     string  `parquet:"event_json"`
}

// ParquetPath is the file for one session on one UTC date and harness.
//
//	<root>/date=YYYY-MM-DD/harness=<harness>/<sessionUID>.parquet
//
// date is the UTC day of recorded_at, or of ingested_at when
// recorded_at is empty. A session that spans more than one day has
// one file in each of those partitions. The file name is the session
// uid, so a re-projection replaces that session and leaves the rest
// of the day in place.
func ParquetPath(root, date, harness, sessionUID string) (string, error) {
	if err := partitionSegment("date", date); err != nil {
		return "", err
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", fmt.Errorf("normalize: date %q is not YYYY-MM-DD", date)
	}
	if err := partitionSegment("harness", harness); err != nil {
		return "", err
	}
	if err := partitionSegment("session", sessionUID); err != nil {
		return "", err
	}
	return filepath.Join(root, "date="+date, "harness="+harness, sessionUID+".parquet"), nil
}

// WriteParquet replaces every parquet file for sessionUID under root
// with one file per UTC date and harness present in events. An empty
// event list removes the session's files and writes nothing. The
// write lands by rename. Directories are mode 0700 and files are 0600.
func WriteParquet(root, sessionUID string, events []Event) error {
	if err := partitionSegment("session", sessionUID); err != nil {
		return err
	}
	// Indices preserve order without serializing a second whole-session copy.
	groups := map[string][]int{}
	var order []string
	for i, ev := range events {
		date, err := PartitionDate(ev.RecordedAt, ev.IngestedAt)
		if err != nil {
			return err
		}
		path, err := ParquetPath(root, date, ev.Harness, sessionUID)
		if err != nil {
			return err
		}
		if _, ok := groups[path]; !ok {
			order = append(order, path)
		}
		groups[path] = append(groups[path], i)
	}
	old, err := SessionParquet(root, sessionUID)
	if err != nil {
		return err
	}
	staged := map[string]string{}
	defer func() {
		for _, tmp := range staged {
			os.Remove(tmp)
		}
	}()
	// Finish all serialization before replacing any existing partition. A bad
	// event in a later partition must not publish an earlier partial result.
	for _, path := range order {
		tmp, err := stageParquetFile(path, events, groups[path])
		if err != nil {
			return err
		}
		staged[path] = tmp
	}
	for _, path := range order {
		if err := os.Rename(staged[path], path); err != nil {
			return fmt.Errorf("normalize: %w", err)
		}
	}
	for _, path := range old {
		if _, ok := staged[path]; ok {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("normalize: %w", err)
		}
		removeEmptyPartition(root, path)
	}
	return nil
}

// RemoveParquet deletes every partition file for sessionUID.
func RemoveParquet(root, sessionUID string) error {
	if root == "" {
		return nil
	}
	paths, err := SessionParquet(root, sessionUID)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("normalize: %w", err)
		}
		removeEmptyPartition(root, path)
	}
	return nil
}

// SessionParquet lists partition files named <sessionUID>.parquet
// under root. A missing root is an empty list.
func SessionParquet(root, sessionUID string) ([]string, error) {
	if err := partitionSegment("session", sessionUID); err != nil {
		return nil, err
	}
	name := sessionUID + ".parquet"
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == name {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("normalize: %w", err)
	}
	return out, nil
}

// PartitionDate is the UTC day used as the date= key. recordedAt wins.
// ingestedAt is the fallback. Both are RFC3339 timestamps.
func PartitionDate(recordedAt, ingestedAt string) (string, error) {
	if d, ok := utcDay(recordedAt); ok {
		return d, nil
	}
	if d, ok := utcDay(ingestedAt); ok {
		return d, nil
	}
	return "", fmt.Errorf("normalize: event has no recorded_at or ingested_at date")
}

func utcDay(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return "", false
		}
	}
	return t.UTC().Format("2006-01-02"), true
}

func parquetRow(ev Event) (ParquetRow, error) {
	body, err := eventJSON(ev)
	if err != nil {
		return ParquetRow{}, err
	}
	return ParquetRow{
		SchemaVersion: int32(ev.SchemaVersion),
		EventID:       ev.EventID,
		SessionID:     ev.SessionID,
		Harness:       ev.Harness,
		RecordedAt:    ev.RecordedAt,
		IngestedAt:    ev.IngestedAt,
		EventType:     ev.EventType,
		Actor:         ev.Actor,
		ContentText:   ev.ContentText,
		ProjectID:     ev.ProjectID,
		CWDHash:       ev.CWDHash,
		RawType:       ev.RawType,
		EventJSON:     body,
	}, nil
}

func eventJSON(ev Event) (string, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(ev); err != nil {
		return "", fmt.Errorf("normalize: %w", err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func stageParquetFile(path string, events []Event, indices []int) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("normalize: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".parquet-*")
	if err != nil {
		return "", fmt.Errorf("normalize: %w", err)
	}
	tmpName := tmp.Name()
	complete := false
	defer func() {
		tmp.Close()
		if !complete {
			os.Remove(tmpName)
		}
	}()
	// The rows repeat their session's fields and carry each event twice,
	// as text and as JSON, so zstd shrinks them several times over.
	if err := writeParquetRows(tmp, events, indices); err != nil {
		return "", fmt.Errorf("normalize: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return "", fmt.Errorf("normalize: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("normalize: %w", err)
	}
	complete = true
	return tmpName, nil
}

func writeParquetRows(out io.Writer, events []Event, indices []int) error {
	return writeParquetStream(out, func(sink func(ParquetRow) error) error {
		for _, i := range indices {
			row, err := parquetRow(events[i])
			if err != nil {
				return err
			}
			if err := sink(row); err != nil {
				return err
			}
		}
		return nil
	})
}

func writeParquetStream(out io.Writer, produce func(func(ParquetRow) error) error) error {
	w := parquet.NewGenericWriter[ParquetRow](out,
		parquet.Compression(&parquet.Zstd),
		parquet.MaxRowsPerRowGroup(parquetRowGroupRows),
	)
	closed := false
	defer func() {
		if !closed {
			_ = w.Close()
		}
	}()
	batch := make([]ParquetRow, 0, parquetBatchRows)
	batchBytes, groupBytes, groupRows := 0, 0, 0
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := w.Write(batch)
		if err == nil && n != len(batch) {
			err = io.ErrShortWrite
		}
		// Write copies values into column buffers. Drop serialized strings
		// immediately rather than keeping them in the reusable batch array.
		clear(batch)
		batch = batch[:0]
		batchBytes = 0
		return err
	}
	if err := produce(func(row ParquetRow) error {
		size := parquetRowBytes(row)
		if groupRows > 0 && (groupRows == parquetRowGroupRows || groupBytes+size > parquetRowGroupBytes) {
			if err := flushBatch(); err != nil {
				return err
			}
			if err := w.Flush(); err != nil {
				return err
			}
			groupBytes, groupRows = 0, 0
		}
		if len(batch) > 0 && (len(batch) == parquetBatchRows || batchBytes+size > parquetBatchBytes) {
			if err := flushBatch(); err != nil {
				return err
			}
		}
		batch = append(batch, row)
		batchBytes += size
		groupBytes += size
		groupRows++
		return nil
	}); err != nil {
		return err
	}
	if err := flushBatch(); err != nil {
		return err
	}
	err := w.Close()
	closed = true
	return err
}

// Include text duplicated in event_json as well as the scalar columns. This
// estimates uncompressed payload, not just row count or compressed file size.
func parquetRowBytes(r ParquetRow) int {
	n := len(r.EventJSON) + len(r.EventID) + len(r.SessionID) + len(r.Harness) + len(r.RecordedAt) + len(r.IngestedAt) + len(r.EventType) + len(r.Actor) + len(r.CWDHash) + len(r.RawType)
	if r.ContentText != nil {
		n += len(*r.ContentText)
	}
	if r.ProjectID != nil {
		n += len(*r.ProjectID)
	}
	return n
}

func partitionSegment(kind, s string) error {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) {
		return fmt.Errorf("normalize: %s %q is not a partition key", kind, s)
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return fmt.Errorf("normalize: %s %q is not a partition key", kind, s)
		}
	}
	return nil
}

// removeEmptyPartition removes empty harness= and date= directories
// left behind when a session's file moves to another day. It stops at
// root and at the first directory that still has an entry.
func removeEmptyPartition(root, file string) {
	dir := filepath.Dir(file)
	root = filepath.Clean(root)
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

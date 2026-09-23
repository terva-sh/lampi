package normalize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

// ParquetRoot is the directory name under the lake data dir.
const ParquetRoot = "parquet"

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
	groups := map[string][]ParquetRow{}
	var order []string
	for _, ev := range events {
		date, err := PartitionDate(ev.RecordedAt, ev.IngestedAt)
		if err != nil {
			return err
		}
		path, err := ParquetPath(root, date, ev.Harness, sessionUID)
		if err != nil {
			return err
		}
		row, err := parquetRow(ev)
		if err != nil {
			return err
		}
		if _, ok := groups[path]; !ok {
			order = append(order, path)
		}
		groups[path] = append(groups[path], row)
	}
	old, err := SessionParquet(root, sessionUID)
	if err != nil {
		return err
	}
	written := map[string]struct{}{}
	for _, path := range order {
		if err := writeParquetFile(path, groups[path]); err != nil {
			return err
		}
		written[path] = struct{}{}
	}
	for _, path := range old {
		if _, ok := written[path]; ok {
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
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(ev); err != nil {
		return "", fmt.Errorf("normalize: %w", err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func writeParquetFile(path string, rows []ParquetRow) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".parquet-*")
	if err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if err := parquet.Write(tmp, rows); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	tmpName = ""
	return nil
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

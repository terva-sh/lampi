package normalize

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// OutputError distinguishes retryable staging/storage failures from invalid
// harness input. Partial staging is always discarded by Stage.
type OutputError struct{ Err error }

func (e OutputError) Error() string { return "normalize output: " + e.Err.Error() }
func (e OutputError) Unwrap() error { return e.Err }

// Staged contains complete private outputs. It owns no open descriptors after
// Stage returns. The caller checks the catalog generation before Publish.
type Staged struct {
	root, uid  string
	partitions []string
}

func (s *Staged) Close() error { return os.RemoveAll(s.root) }

// Stage retains one record/event, indexed-zstd frame buffers, and one Parquet
// row group. Partition/frame metadata scales with their counts. Partition rows
// are spooled to disk so many dates do not retain many compression writers.
func Stage(ctx context.Context, parent, uid string, produce func(EventSink) error) (stage *Staged, err error) {
	if err := partitionSegment("session", uid); err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp(parent, ".projection-*")
	if err != nil {
		return nil, OutputError{err}
	}
	stage = &Staged{root: root, uid: uid}
	complete := false
	defer func() {
		if !complete {
			err = errors.Join(err, stage.Close())
			stage = nil
		}
	}()
	eventFile, err := os.Create(filepath.Join(root, "events"))
	if err != nil {
		return stage, OutputError{err}
	}
	defer eventFile.Close()
	// root is private; files are private too, including on unusual umasks.
	if err := eventFile.Chmod(0600); err != nil {
		return stage, OutputError{err}
	}
	groups := map[string]string{}
	var current string
	var spool *os.File
	var buffered *bufio.Writer
	closeSpool := func() error {
		if spool == nil {
			return nil
		}
		err := buffered.Flush()
		err = errors.Join(err, spool.Close())
		spool = nil
		return err
	}
	defer closeSpool()
	err = WriteEventsStream(eventFile, func(events EventSink) error {
		return produce(func(ev Event) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			date, e := PartitionDate(ev.RecordedAt, ev.IngestedAt)
			if e != nil {
				return e
			}
			path, e := ParquetPath("parquet", date, ev.Harness, uid)
			if e != nil {
				return e
			}
			if spool == nil || current != path {
				if e := closeSpool(); e != nil {
					return OutputError{e}
				}
				name, ok := groups[path]
				if !ok {
					name = filepath.Join(root, fmt.Sprintf("rows-%d", len(groups)))
					groups[path] = name
					stage.partitions = append(stage.partitions, path)
				}
				spool, e = os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if e != nil {
					return OutputError{e}
				}
				current = path
				buffered = bufio.NewWriterSize(spool, 64<<10)
			}
			row, e := parquetRow(ev)
			if e != nil {
				return e
			}
			if e := writeSpoolRow(buffered, row); e != nil {
				return OutputError{e}
			}
			if e := events(ev); e != nil {
				return OutputError{e}
			}
			return nil
		})
	})
	if err != nil {
		return stage, err
	}
	if err := closeSpool(); err != nil {
		return stage, OutputError{err}
	}
	if err := eventFile.Close(); err != nil {
		return stage, OutputError{err}
	}
	for _, rel := range stage.partitions {
		if err := ctx.Err(); err != nil {
			return stage, err
		}
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return stage, OutputError{err}
		}
		if err := func() error {
			input, e := os.Open(groups[rel])
			if e != nil {
				return e
			}
			defer input.Close()
			output, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			defer output.Close()
			stat, e := input.Stat()
			if e != nil {
				return e
			}
			dec := bufio.NewReaderSize(contextInput{ctx, input}, 64<<10)
			e = writeParquetStream(output, func(sink func(ParquetRow) error) error {
				for {
					if e := ctx.Err(); e != nil {
						return e
					}
					row, e := readSpoolRow(dec, stat.Size())
					if e == io.EOF {
						return nil
					}
					if e != nil {
						return e
					}
					if e := sink(row); e != nil {
						return e
					}
				}
			})
			if e != nil {
				return e
			}
			return output.Close()
		}(); err != nil {
			return stage, OutputError{err}
		}
		if err := os.Remove(groups[rel]); err != nil {
			return stage, OutputError{err}
		}
	}
	complete = true
	return stage, nil
}

// Publish renames only completed files. Catalog publication follows this call;
// a rename failure removes derived output so no partial generation is marked
// published. As with the compatibility writer, this is atomic per file.
func (s *Staged) Publish(normalized, parquetRoot string) (err error) {
	defer func() {
		if err != nil {
			err = errors.Join(err, RemoveEvents(normalized, s.uid), RemoveParquet(parquetRoot, s.uid))
		}
	}()
	old, err := SessionParquet(parquetRoot, s.uid)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, rel := range s.partitions {
		dest := filepath.Join(parquetRoot, rel[len("parquet")+1:])
		keep[dest] = true
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(s.root, rel), dest); err != nil {
			return err
		}
	}
	for _, path := range old {
		if !keep[path] {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			removeEmptyPartition(parquetRoot, path)
		}
	}
	if err := os.MkdirAll(normalized, 0700); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(s.root, "events"), EventsPath(normalized, s.uid)); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(normalized, s.uid+LegacyEventsExt)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// CleanStaging removes abandoned private projections. The caller must hold the
// exclusive lake lock and call this before starting any normalization workers.
func CleanStaging(parent string) error {
	entries, err := os.ReadDir(parent)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".projection-") {
			if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

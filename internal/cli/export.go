package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/config"
)

const exportUsage = `terva-lampi export — write normalized events as JSONL

usage:
  terva-lampi export [--data DIR] [--out FILE]

Reads the lake and writes schema_version 1 events, one JSON object per
line. FILE defaults to stdout. The command waits until normalize
workers have caught up with the catalog. A session whose last
normalize failed is skipped and named on stderr. Its raw blob is left
as it was. A session with no derived file yet is projected once.

DuckDB:
  SELECT content_text FROM read_ndjson('events.jsonl')
sqlite3, after loading each line into a table:
  SELECT json_extract(line, '$.content_text') FROM export_lines
`

func runExport(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), exportUsage)
		return nil
	}
	var data, outPath string
	rest, err := parseFlags(env, args, exportUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&outPath, "out", "", "JSONL path (default: stdout)")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), exportUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if data == "" {
		data, err = config.StateDir(env.getenv)
		if err != nil {
			return err
		}
	}
	lake, err := api.Open(data)
	if err != nil {
		return err
	}
	defer lake.Close()

	out := env.stdout()
	if outPath != "" && outPath != "-" {
		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	return writeExport(env, lake, out)
}

func writeExport(env Env, lake *api.Server, out io.Writer) error {
	ctx := context.Background()
	// The manifest ACK returns before workers project. Wait so this
	// snapshot is the head, then read JSONL. A missing file is still
	// rebuilt below.
	if err := lake.WaitNormalized(ctx); err != nil {
		return err
	}
	sessions, err := lake.Catalog.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess.NormalizeError != "" {
			fmt.Fprintf(env.stderr(), "terva-lampi: session %s normalize_error: %s\n", sess.UID, sess.NormalizeError)
			continue
		}
		path := filepath.Join(lake.Normalized, sess.UID+".jsonl")
		body, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			events, nerr := lake.Project(ctx, sess.Manifest)
			if storeErr := lake.StoreEvents(ctx, sess.UID, events, nerr); storeErr != nil {
				return storeErr
			}
			if nerr != nil {
				fmt.Fprintf(env.stderr(), "terva-lampi: session %s normalize_error: %s\n", sess.UID, nerr.Error())
				continue
			}
			body, err = os.ReadFile(path)
		}
		if err != nil {
			return err
		}
		if _, err := out.Write(body); err != nil {
			return err
		}
	}
	return nil
}

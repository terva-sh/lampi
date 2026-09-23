package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

const exportUsage = `terva-lampi export — write normalized events or a training trajectory

usage:
  terva-lampi export [--data DIR] [--out FILE] [--format events|sharegpt|trajectory]

--format events (the default) writes schema_version 1 events, one JSON
object per line. FILE defaults to stdout. The command waits until
normalize workers have caught up with the catalog. A session whose
last normalize failed is skipped and named on stderr. Its raw blob is
left as it was. A session with no derived file yet is projected once.

--format sharegpt and --format trajectory are the same training
projection: one ShareGPT conversation per session. A session is
included only when config.json allowlists its manifest project.
Default deny. Deny wins. A session that is not permitted is named on
stderr and omitted. Each row carries raw_sha256, the current
transcript blob. encrypted_content is copied onto the turn as stored
and is not written into the turn value. Ruleset v1 strips matches
from the plaintext training fields (value, tool name, and call id).
The CAS and the normalized events are not rewritten.

DuckDB, events:
  SELECT content_text FROM read_ndjson('events.jsonl')
sqlite3, after loading each line into a table:
  SELECT json_extract(line, '$.content_text') FROM export_lines
`

func runExport(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), exportUsage)
		return nil
	}
	var data, outPath, format string
	rest, err := parseFlags(env, args, exportUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&outPath, "out", "", "JSONL path (default: stdout)")
		fs.StringVar(&format, "format", "events", "events, sharegpt, or trajectory")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), exportUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	switch format {
	case "", "events", "sharegpt", "trajectory":
	default:
		fmt.Fprint(env.stdout(), exportUsage)
		return fmt.Errorf("unknown export format %q", format)
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
	switch format {
	case "", "events":
		return writeExport(env, lake, out)
	default:
		return writeShareGPT(env, lake, out)
	}
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
		body, ok, err := sessionJSONL(env, lake, sess)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, err := out.Write(body); err != nil {
			return err
		}
	}
	return nil
}

func writeShareGPT(env Env, lake *api.Server, out io.Writer) error {
	ctx := context.Background()
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	if err := lake.WaitNormalized(ctx); err != nil {
		return err
	}
	sessions, err := lake.Catalog.ListSessions(ctx)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for _, sess := range sessions {
		if sess.NormalizeError != "" {
			fmt.Fprintf(env.stderr(), "terva-lampi: session %s normalize_error: %s\n", sess.UID, sess.NormalizeError)
			continue
		}
		project := config.ProjectID{
			CWD:       sess.Manifest.Project.CWD,
			CWDHash:   sess.Manifest.Project.CWDHash,
			GitRemote: sess.Manifest.Project.GitRemote,
		}
		if !file.Projects.Permitted(project) {
			fmt.Fprintf(env.stderr(), "terva-lampi: session %s not allowlisted\n", sess.UID)
			continue
		}
		body, ok, err := sessionJSONL(env, lake, sess)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		events, err := decodeEvents(body)
		if err != nil {
			return fmt.Errorf("session %s: %w", sess.UID, err)
		}
		_, arts, found, err := lake.Catalog.Current(ctx, sess.Harness, sess.NativeID)
		if err != nil {
			return err
		}
		digest := ""
		if found {
			digest = transcriptDigest(arts)
		}
		if digest == "" {
			fmt.Fprintf(env.stderr(), "terva-lampi: session %s has no transcript digest\n", sess.UID)
			continue
		}
		rec, ok := normalize.ShareGPT(sess.UID, digest, events)
		if !ok {
			fmt.Fprintf(env.stderr(), "terva-lampi: session %s has no training turns\n", sess.UID)
			continue
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

func transcriptDigest(arts []catalog.ArtifactRow) string {
	for _, a := range arts {
		if a.Kind == protocol.KindTranscriptJSONL && a.SHA256 != "" {
			return a.SHA256
		}
	}
	return ""
}

func decodeEvents(body []byte) ([]normalize.Event, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	var out []normalize.Event
	for {
		var ev normalize.Event
		err := dec.Decode(&ev)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
}

// sessionJSONL returns the derived JSONL for one session. ok is false
// when the session is skipped because normalize failed. A missing file
// is projected once. The raw blob is not opened for write.
func sessionJSONL(env Env, lake *api.Server, sess catalog.SessionInfo) ([]byte, bool, error) {
	if sess.NormalizeError != "" {
		fmt.Fprintf(env.stderr(), "terva-lampi: session %s normalize_error: %s\n", sess.UID, sess.NormalizeError)
		return nil, false, nil
	}
	ctx := context.Background()
	path := filepath.Join(lake.Normalized, sess.UID+".jsonl")
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		events, nerr := lake.Project(ctx, sess.Manifest)
		if storeErr := lake.StoreEvents(ctx, sess.UID, events, nerr); storeErr != nil {
			return nil, false, storeErr
		}
		if nerr != nil {
			fmt.Fprintf(env.stderr(), "terva-lampi: session %s normalize_error: %s\n", sess.UID, nerr.Error())
			return nil, false, nil
		}
		body, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, false, err
	}
	return body, true, nil
}

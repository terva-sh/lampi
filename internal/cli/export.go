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
	"path"
	"path/filepath"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakelock"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

const exportUsage = `terva-lampi export — write normalized events or a training trajectory

usage:
  terva-lampi export [--data DIR] [--out FILE] [--format events|sharegpt|trajectory]

--format events (the default) writes schema_version 1 events, one JSON
object per line. FILE defaults to stdout. A session whose last
normalize failed is skipped and named on stderr. Its raw blob is left
as it was.

When serve is not running on DIR, export takes the lake lock, lets
the normalize workers catch up with the catalog, and projects once a
session with no derived file yet. While serve runs, export reads the
catalog read-only and starts no worker. It reads the derived files as
they are, and names a session with no derived file yet on stderr as
not yet normalized.

--format sharegpt and --format trajectory are the same training
projection: one ShareGPT conversation per session. A session is
included only when config.json allowlists its manifest project.
Default deny. Deny wins. A session that is not permitted is named on
stderr and omitted. Each row carries raw_sha256, the current
transcript blob. encrypted_content is copied onto the turn as stored
and is not written into the turn value. Ruleset v2 strips matches
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
	lake, live, closeLake, err := openForExport(data)
	if err != nil {
		return err
	}
	defer closeLake()
	x := exporter{env: env, lake: lake, live: live}

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
		return x.writeEvents(out)
	default:
		return x.writeShareGPT(out)
	}
}

// exporter reads one lake. live is true when serve holds the lake: the
// catalog is read-only and nothing here projects or stores.
type exporter struct {
	env  Env
	lake *api.Server
	live bool
}

// openForExport takes the lake lock and opens the lake with its
// workers, as serve would. When serve holds the lock it opens the lake
// read-only instead, so a second process does not publish derived
// files over serve's. closeLake closes the lake, then drops the lock.
func openForExport(data string) (lake *api.Server, live bool, closeLake func() error, err error) {
	lock, err := lakelock.Acquire(data)
	if errors.Is(err, lakelock.ErrHeld) {
		lake, err := api.OpenReadOnly(data)
		if err != nil {
			return nil, false, nil, err
		}
		return lake, true, lake.Close, nil
	}
	if err != nil {
		return nil, false, nil, err
	}
	lake, err = api.Open(data)
	if err != nil {
		lock.Release()
		return nil, false, nil, err
	}
	return lake, false, func() error { return errors.Join(lake.Close(), lock.Release()) }, nil
}

func (x exporter) writeEvents(out io.Writer) error {
	lake := x.lake
	ctx := context.Background()
	// The manifest ACK returns before workers project. Wait so this
	// snapshot is the head, then read JSONL. A missing file is still
	// rebuilt below. A read-only lake has no queue to wait on.
	if err := lake.WaitNormalized(ctx); err != nil {
		return err
	}
	sessions, err := lake.Catalog.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		body, ok, err := x.sessionJSONL(sess)
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

func (x exporter) writeShareGPT(out io.Writer) error {
	env, lake := x.env, x.lake
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
			NoRepo:    sess.Manifest.Project.GitRemote == "" && adapter.OutsideCheckout(sess.Manifest.Project.CWD),
		}
		if !file.Projects.Permitted(project) {
			fmt.Fprintf(env.stderr(), "terva-lampi: session %s not allowlisted\n", sess.UID)
			continue
		}
		body, ok, err := x.sessionJSONL(sess)
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
		view, found, err := lake.Catalog.Head(ctx, sess.Harness, sess.NativeID)
		if err != nil {
			return err
		}
		digest := ""
		if found {
			digest = transcriptDigest(view)
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

// transcriptDigest is the blob a training row points at. That is the
// session head when the head is a transcript or an export, because the
// projector reads the head. Otherwise it is the first such artifact.
func transcriptDigest(v catalog.HeadView) string {
	for _, a := range v.Current {
		if a.SHA256 != "" && a.SHA256 == v.HeadSHA256 && sessionBlob(a) {
			return a.SHA256
		}
	}
	var transcript string
	for _, a := range v.Current {
		if a.SHA256 == "" || !sessionBlob(a) {
			continue
		}
		// The Cursor IDE projector reads cursor_state_json. The Cursor
		// CLI projector reads cursor_cli_store_json. The OpenCode
		// projector reads opencode_export_json. The training row
		// points at that export, which is the blob that was projected.
		if a.Kind != protocol.KindTranscriptJSONL {
			return a.SHA256
		}
		if transcript == "" {
			transcript = a.SHA256
		}
	}
	return transcript
}

// sessionBlob is a transcript or an export a projector reads.
// history.jsonl is Codex prompt history, not a rollout, and is not one.
func sessionBlob(a catalog.ArtifactRow) bool {
	switch a.Kind {
	case protocol.KindCursorStateJSON, protocol.KindCursorCLIStoreJSON, protocol.KindOpenCodeExportJSON:
		return true
	case protocol.KindTranscriptJSONL:
		return path.Base(a.RelPath) != "history.jsonl"
	default:
		return false
	}
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
// when the session is skipped because normalize failed, or, while serve
// runs, because it has no derived file yet. Otherwise a missing file is
// projected once. The raw blob is not opened for write.
func (x exporter) sessionJSONL(sess catalog.SessionInfo) ([]byte, bool, error) {
	env, lake := x.env, x.lake
	if sess.NormalizeError != "" {
		fmt.Fprintf(env.stderr(), "terva-lampi: session %s normalize_error: %s\n", sess.UID, sess.NormalizeError)
		return nil, false, nil
	}
	ctx := context.Background()
	path := filepath.Join(lake.Normalized, sess.UID+".jsonl")
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && x.live {
		fmt.Fprintf(env.stderr(), "terva-lampi: session %s not yet normalized; serve is running\n", sess.UID)
		return nil, false, nil
	}
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

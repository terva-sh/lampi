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
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakelock"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
)

const exportUsage = `terva-lampi export — write normalized events or a training trajectory

usage:
  terva-lampi export [--data DIR] [--out FILE] [--format events|sharegpt|trajectory] [--bay BAY]...
                     [event filters] [--fields PATH,...]

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

--bay, repeated, limits either format to the sessions in those bays,
by id, name or alias (see serve bays). Without it every bay is
exported: export reads the lake directory, which holds them all.

Event filters, with --format events only, keep the events that match
every filter given. They take the names and values of the web search
filters and select the same events:
  --harness H         terva, claude, codex, opencode, cursor, cursor-cli or grok
  --project ID        the session's project id
  --event-type T      message, tool_call, tool_result, usage, compaction,
                      meta, error, unknown, or unreadable for a line that
                      is not an event
  --actor A           user, assistant, system, tool or harness
  --tool NAME         the tool name, exactly
  --tool-error B      true or false; a result whose harness did not
                      record either matches neither
  --raw-type T        the harness's own type for the line, exactly
  --since T, --until T
                      recorded time, RFC 3339 or YYYY-MM-DD in UTC; since
                      is inclusive, until exclusive, and a date-only until
                      covers that day. An event with no recorded time
                      matches neither.

--fields PATH,... writes one JSON object per event holding only those
paths, keyed by the path. A path is an event field (session_id), one
field of a nested object (tool.name, model.id), or a key of extra
(extra.KEY). A path the event lacks is null.

Every tool call, with the tool and its input:
  terva-lampi export --event-type tool_call \
    --fields harness,session_id,tool.name,content_text

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
	var bays bayList
	var ef eventFlags
	rest, err := parseFlags(env, args, exportUsage, func(fs *flag.FlagSet) {
		fs.Var(&bays, "bay", "export only sessions in this bay (repeatable)")
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&outPath, "out", "", "JSONL path (default: stdout)")
		fs.StringVar(&format, "format", "events", "events, sharegpt, or trajectory")
		ef.register(fs)
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
	filter, fields, err := ef.parse()
	if err != nil {
		return err
	}
	counter, err := ef.counter()
	if err != nil {
		return err
	}
	if (!filter.IsZero() || fields != nil || counter != nil) && format != "" && format != "events" {
		return fmt.Errorf("event filters, --fields and --count-by apply to --format events only")
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
	x := exporter{env: env, lake: lake, live: live, scope: catalog.AllBays(), filter: filter, fields: fields, counter: counter}
	if len(bays) > 0 {
		var ids []string
		for _, ref := range bays {
			b, err := lake.Catalog.ResolveBay(context.Background(), ref)
			if err != nil {
				return err
			}
			ids = append(ids, b.ID)
		}
		x.scope = catalog.InBays(ids)
	}

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
	// scope is the bays --bay chose, or every bay.
	scope catalog.Scope
	// filter and fields select events and their paths for --format
	// events. The zero filter and nil fields write sessions as stored.
	filter recall.EventFilter
	fields recall.Fields
	// counter, when set, counts the selected events instead of writing
	// them.
	counter *recall.Counter
}

// inScope reports whether a session is in the bays being exported.
func (x exporter) inScope(ctx context.Context, uid string) (bool, error) {
	if x.scope.All() {
		return true, nil
	}
	return x.lake.Catalog.SessionInScope(ctx, x.scope, uid)
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
		if in, err := x.inScope(ctx, sess.UID); err != nil || !in {
			if err != nil {
				return err
			}
			continue
		}
		if !x.filter.Session(sess.Harness, sess.ProjectID) {
			continue
		}
		body, ok, err := x.sessionJSONL(sess)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if x.filter.IsZero() && x.fields == nil && x.counter == nil {
			_, err = out.Write(body)
		} else {
			err = x.writeSelected(out, body)
		}
		if err != nil {
			return err
		}
	}
	if x.counter == nil {
		return nil
	}
	var buf bytes.Buffer
	for _, row := range x.counter.Rows() {
		buf.Write(row)
		buf.WriteByte('\n')
	}
	_, err = out.Write(buf.Bytes())
	return err
}

// writeSelected writes the lines of one session's JSONL that the filter
// keeps, each cut to the chosen fields when there are any. A line too
// long for the search index to read is read as the index reads it: as
// an unreadable line with no fields.
func (x exporter) writeSelected(out io.Writer, body []byte) error {
	var buf bytes.Buffer
	for len(body) > 0 {
		line, rest, _ := bytes.Cut(body, []byte("\n"))
		body = rest
		read := line
		if len(line) > recall.MaxLine {
			read = nil
		}
		if !x.filter.Line(read) {
			continue
		}
		if x.counter != nil {
			// An oversized line is left out of a count, as the event
			// stream leaves it out of whole-event output.
			if read != nil {
				if err := x.counter.Add(read); err != nil {
					return err
				}
			}
			continue
		}
		if x.fields != nil {
			buf.Write(x.fields.Project(read))
		} else {
			buf.Write(line)
		}
		buf.WriteByte('\n')
	}
	_, err := out.Write(buf.Bytes())
	return err
}

// eventFlags are the event filter and --fields flags, shared by every
// command that reads normalized events.
type eventFlags struct {
	harness, project, eventType, actor, tool, toolError, rawType, since, until, fields, countBy string
}

func (e *eventFlags) register(fs *flag.FlagSet) {
	for _, f := range []struct {
		name  string
		dest  *string
		usage string
	}{
		{"harness", &e.harness, "keep events of sessions from this harness"},
		{"project", &e.project, "keep events of sessions with this project id"},
		{"event-type", &e.eventType, "keep events of this type"},
		{"actor", &e.actor, "keep events by this actor"},
		{"tool", &e.tool, "keep events of this tool name"},
		{"tool-error", &e.toolError, "keep tool results that failed (true) or succeeded (false)"},
		{"raw-type", &e.rawType, "keep events of this harness type"},
		{"since", &e.since, "keep events recorded at or after this time"},
		{"until", &e.until, "keep events recorded before this time"},
		{"fields", &e.fields, "write only these comma-separated event paths"},
		{"count-by", &e.countBy, "count the events by the value at this path instead of writing them"},
	} {
		// An empty value is refused, not read as the flag left out: an
		// empty --fields would otherwise write whole events, and pass
		// the check that keeps --fields to --format events (review 1678).
		fs.Func(f.name, f.usage, func(v string) error {
			if v == "" {
				return errors.New("empty value")
			}
			*f.dest = v
			return nil
		})
	}
}

func (e *eventFlags) parse() (recall.EventFilter, recall.Fields, error) {
	f := recall.EventFilter{Harness: e.harness, Project: e.project, EventType: e.eventType, Actor: e.actor, ToolName: e.tool, RawType: e.rawType}
	switch e.toolError {
	case "":
	case "true", "false":
		v := e.toolError == "true"
		f.ToolError = &v
	default:
		return f, nil, fmt.Errorf("--tool-error %q: want true or false", e.toolError)
	}
	for _, w := range []struct {
		flag, raw string
		dest      **time.Time
		end       bool
	}{{"--since", e.since, &f.Since, false}, {"--until", e.until, &f.Until, true}} {
		if w.raw == "" {
			continue
		}
		t, err := recall.ParseWhen(w.raw, w.end)
		if err != nil {
			return f, nil, fmt.Errorf("%s %q: want RFC 3339 or YYYY-MM-DD", w.flag, w.raw)
		}
		*w.dest = &t
	}
	if err := f.Validate(); err != nil {
		var fe *recall.FilterError
		if errors.As(err, &fe) && fe.Param == "until" {
			return f, nil, errors.New("--until must be after --since")
		}
		if errors.As(err, &fe) {
			flags := map[string][2]string{
				"harness": {"--harness", e.harness}, "project": {"--project", e.project}, "event_type": {"--event-type", e.eventType},
				"actor": {"--actor", e.actor}, "tool": {"--tool", e.tool}, "raw_type": {"--raw-type", e.rawType},
			}
			bad := flags[fe.Param]
			return f, nil, fmt.Errorf("%s %q: not a value it accepts (see --help)", bad[0], bad[1])
		}
		return f, nil, err
	}
	fields, err := recall.ParseFields(e.fields)
	if err != nil {
		return f, nil, fmt.Errorf("--fields: %w", err)
	}
	return f, fields, nil
}

// counter is the --count-by count, or nil without the flag. --fields
// and --count-by are refused together: a count writes no events.
func (e *eventFlags) counter() (*recall.Counter, error) {
	if e.countBy == "" {
		return nil, nil
	}
	if e.fields != "" {
		return nil, errors.New("--count-by writes counts, not events; leave out --fields")
	}
	c, err := recall.NewCounter(e.countBy)
	if err != nil {
		return nil, fmt.Errorf("--count-by: %w", err)
	}
	return c, nil
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
		if in, err := x.inScope(ctx, sess.UID); err != nil || !in {
			if err != nil {
				return err
			}
			continue
		}
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
	body, err := normalize.ReadEventsFile(lake.Normalized, sess.UID)
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
		body, err = normalize.ReadEventsFile(lake.Normalized, sess.UID)
	}
	if err != nil {
		return nil, false, err
	}
	return body, true, nil
}

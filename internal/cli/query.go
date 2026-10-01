package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/upload"
	"terva.sh/lampi/internal/web"
)

const queryUsage = `terva-lampi query events — read normalized events from a lake with a read token

usage:
  terva-lampi query events [--lake NAME | --server URL] [--token-file FILE]
                           [event filters] [--fields PATH,...] [--out FILE]

Reads the events a lake's event stream (/api/read/v1/events) selects,
and writes them as JSONL: whole events, or the --fields paths. The
filters and --fields are export's, with the same names and values, and
select the same events (terva-lampi export --help lists them). At least
one filter is required; --fields alone selects everything.

The read token is one an admin minted on the lake's Read tokens page
with the events:read permission. It is read from --token-file, or from
the file LAMPI_READ_TOKEN_FILE names. There is no default path. Keep
the file mode 0600. The token is never printed.

The lake is --server, or the server of the config.json lake --lake
names, or the only lake config.json lists.

FILE defaults to stdout. --out is written with mode 0600, and only once
the stream is complete: a stream the lake or the network cut short
leaves no file. On stdout, a short stream writes what arrived and exits
nonzero.

Every tool call, with its harness, session and input:
  terva-lampi query events --token-file read-token \
    --event-type tool_call --fields harness,session_id,tool.name,content_text
`

// queryClient has no overall timeout: a stream over a large lake runs
// as long as it has events to send. Connecting and the first byte of
// the answer are bounded.
var queryClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout:   30 * time.Second,
	ResponseHeaderTimeout: 2 * time.Minute,
}}

func runQuery(env Env, args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(env.stdout(), queryUsage)
		return nil
	}
	if args[0] != "events" {
		fmt.Fprint(env.stdout(), queryUsage)
		return fmt.Errorf("unknown query %q", args[0])
	}
	var lake, server, tokenFile, outPath string
	var ef eventFlags
	rest, err := parseFlags(env, args[1:], queryUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&lake, "lake", "", "the config.json lake to read")
		fs.StringVar(&server, "server", "", "the lake URL")
		fs.StringVar(&tokenFile, "token-file", "", "file holding the read token (or LAMPI_READ_TOKEN_FILE)")
		fs.StringVar(&outPath, "out", "", "JSONL path (default: stdout)")
		ef.register(fs)
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), queryUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	filter, _, err := ef.parse()
	if err != nil {
		return err
	}
	if filter.IsZero() {
		return errors.New("query events needs at least one event filter; --fields alone selects every event")
	}
	if lake != "" && server != "" {
		return errors.New("--lake and --server both name the lake; pass one")
	}
	if server == "" {
		if server, err = queryServer(env, lake); err != nil {
			return err
		}
	}
	if tokenFile == "" {
		tokenFile = env.getenv("LAMPI_READ_TOKEN_FILE")
	}
	if tokenFile == "" {
		return errors.New("no read token: pass --token-file, or set LAMPI_READ_TOKEN_FILE")
	}
	token, err := readReadToken(tokenFile)
	if err != nil {
		return err
	}
	if err := upload.CheckToken(server, token); err != nil {
		return fmt.Errorf("refusing to send the read token over plain http to %s; use an https URL, or http only to a loopback address", server)
	}

	if outPath == "" || outPath == "-" {
		_, err := streamEvents(env, server, token, ef.query(), env.stdout())
		return err
	}
	// The file appears only when the stream is complete, so a short one
	// never passes for the whole answer.
	tmp, err := os.CreateTemp(filepath.Dir(outPath), "."+filepath.Base(outPath)+".partial-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = streamEvents(env, server, token, ef.query(), tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), outPath)
}

// queryServer is the server of the named config.json lake, or of the
// only one when no name is given.
func queryServer(env Env, name string) (string, error) {
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return "", err
	}
	lakes, err := config.ResolveLakes(file, env.getenv, config.LakeFlags{Lake: name})
	if err != nil {
		return "", err
	}
	var withServer []config.Lake
	for _, l := range lakes {
		if l.Server.Value != "" {
			withServer = append(withServer, l)
		}
	}
	switch len(withServer) {
	case 1:
		return withServer[0].Server.Value, nil
	case 0:
		return "", errors.New("no lake to read: pass --server, or --lake with a lake config.json lists")
	default:
		var names []string
		for _, l := range withServer {
			names = append(names, l.Name)
		}
		return "", fmt.Errorf("config.json lists several lakes (%s); pass --lake or --server", strings.Join(names, ", "))
	}
}

// readReadToken reads a read token from path. Its value never appears
// in an error.
func readReadToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	token := strings.TrimSpace(string(b))
	if !strings.HasPrefix(token, web.ReadTokenPrefix) || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("read token: %s does not hold a read token (one starts with %s)", path, web.ReadTokenPrefix)
	}
	return token, nil
}

// query is the stream's query for the flags given, holding each value
// as typed, so the lake reads a date-only --until as export does.
func (e *eventFlags) query() url.Values {
	q := url.Values{}
	for k, v := range map[string]string{
		"harness": e.harness, "project": e.project, "event_type": e.eventType, "actor": e.actor, "tool": e.tool,
		"tool_error": e.toolError, "raw_type": e.rawType, "since": e.since, "until": e.until, "fields": e.fields,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	return q
}

// streamEvents copies the event stream to out without its end line, and
// fails unless the stream ended with one saying it is complete.
func streamEvents(env Env, server, token string, q url.Values, out io.Writer) (web.ReadEventsEnd, error) {
	var end web.ReadEventsEnd
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(server, "/")+"/api/read/v1/events?"+q.Encode(), nil)
	if err != nil {
		return end, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := queryClient.Do(req)
	if err != nil {
		return end, fmt.Errorf("query: lake unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
		return end, fmt.Errorf("query: lake answered %d %s%s", resp.StatusCode, body.Error, queryHint(resp.StatusCode, body.Error))
	}
	br := bufio.NewReaderSize(resp.Body, 64<<10)
	bw := bufio.NewWriterSize(out, 64<<10)
	var rows int64
	ended := false
	endPrefix := []byte(`{"` + web.ReadEventsEndKey + `":`)
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			if ended {
				return end, errors.New("query: the lake sent events after the end of the stream")
			}
			if bytes.HasPrefix(line, endPrefix) {
				var m map[string]web.ReadEventsEnd
				if err := json.Unmarshal(line, &m); err != nil {
					return end, fmt.Errorf("query: unreadable end of stream: %w", err)
				}
				end, ended = m[web.ReadEventsEndKey], true
			} else {
				if _, err := bw.Write(line); err != nil {
					return end, err
				}
				rows++
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			bw.Flush()
			return end, fmt.Errorf("query: the stream stopped after %d events: %w", rows, rerr)
		}
	}
	if err := bw.Flush(); err != nil {
		return end, err
	}
	switch {
	case !ended:
		return end, fmt.Errorf("query: the stream ended early, after %d events, with no end line", rows)
	case !end.Complete:
		return end, fmt.Errorf("query: the lake stopped partway (%s), after %d events", end.Error, rows)
	case end.Rows != rows:
		return end, fmt.Errorf("query: the lake sent %d events but counted %d", rows, end.Rows)
	}
	fmt.Fprintf(env.stderr(), "terva-lampi: %d events from %d sessions\n", end.Rows, end.Sessions)
	if end.Skipped > 0 {
		fmt.Fprintf(env.stderr(), "terva-lampi: %d sessions changed while being read and were left out; run again to include them\n", end.Skipped)
	}
	return end, nil
}

// queryHint says what to do about the lake's refusals a user can fix.
func queryHint(status int, code string) string {
	switch {
	case status == http.StatusUnauthorized:
		return ": the token is unknown, expired or revoked; ask an admin for a new one"
	case status == http.StatusNotFound:
		return ": the token lacks events:read, or the lake is older than the event stream"
	case code == "invalid_request":
		return ": the lake refused a filter value"
	}
	return ""
}

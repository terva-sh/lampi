package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"terva.sh/lampi/internal/recall"
)

const queryToolsUsage = `terva-lampi query tools — tool usage statistics from a lake with a read token

usage:
  terva-lampi query tools [--lake NAME | --server URL] [--token-file FILE]
                          [--since T] [--until T | --all] [--harness H]
                          [--project ID | --unlinked] [--model M] [--tool NAME]
                          [--origin KIND [--origin-name NAME]]
                          [--by tool|origin] [--split harness|model]
                          [--bucket hour|day] [--largest N] [--context]
                          [--out FILE]

Reads the lake's tool statistics (/api/read/v1/tools) for the sessions
the read token reaches: calls, results, sessions, errors, input and
result sizes, and latency, per tool or per origin. Origin kinds are
builtin, mcp, plugin, extension and unknown. Sizes are bytes of text;
percentiles are approximate, at most 19% above the true value. No
transcript content is read or written.

The range is --since (inclusive) to --until (exclusive), as RFC 3339 or
YYYY-MM-DD in UTC; a date-only --until covers that day. With neither,
and no --bucket, it is the last 7 days. --all reads every recorded time,
which on a large lake takes seconds.

The output is JSONL, one object per line with one key:
  {"summary":{...}}   the range, grouping, totals and bucket starts
  {"group":{...}}     one per tool or origin, most called first
  {"largest":{...}}   with --largest N, one per block, biggest first
  {"context":{...}}   with --context, the starting-context hint
docs/web-api.md describes every field. The totals are also written to
stderr.

--context compares the starting context of sessions that used each
MCP server, plugin or extension with sessions in the same harness that
did not. A Claude Code session used what it was offered, from the tool
list it recorded, or called; other sessions what they called. It is a
correlation hint, not a measurement.

The read token, the lake and --out are as for query events.

The most called tools of the last 7 days, with the five largest blocks:
  terva-lampi query tools --token-file read-token --largest 5

MCP servers by share of result bytes in September:
  terva-lampi query tools --token-file read-token --by origin \
    --origin mcp --since 2026-09-01 --until 2026-09-30
`

// queryToolsTimeout bounds one statistics or diagnosis request. The
// lake gives up on its read after five seconds.
const queryToolsTimeout = time.Minute

func runQueryTools(env Env, args []string) error {
	var lake, server, tokenFile, outPath string
	var all, unlinked, withContext bool
	var largest int
	params := map[string]*string{}
	str := func(fs *flag.FlagSet, name, param, help string) {
		params[param] = fs.String(name, "", help)
	}
	rest, err := parseFlags(env, args, queryToolsUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&lake, "lake", "", "the config.json lake to read")
		fs.StringVar(&server, "server", "", "the lake URL")
		fs.StringVar(&tokenFile, "token-file", "", "file holding the read token (or LAMPI_READ_TOKEN_FILE)")
		fs.StringVar(&outPath, "out", "", "JSONL path (default: stdout)")
		str(fs, "since", "since", "only calls recorded at or after this UTC time")
		str(fs, "until", "until", "only calls recorded before this UTC time")
		str(fs, "harness", "harness", "only sessions from this harness")
		str(fs, "project", "project", "only sessions in this project id")
		str(fs, "model", "model", "only calls by this model id")
		str(fs, "tool", "tool", "only this tool name")
		str(fs, "origin", "origin", "only tools of this origin kind")
		str(fs, "origin-name", "origin_name", "only tools of this origin name")
		str(fs, "by", "group_by", "group by tool or origin")
		str(fs, "split", "split", "split each group by harness or model")
		str(fs, "bucket", "bucket", "calls per hour or day")
		fs.BoolVar(&all, "all", false, "read every recorded time")
		fs.BoolVar(&unlinked, "unlinked", false, "only sessions linked to no project")
		fs.BoolVar(&withContext, "context", false, "add the starting-context hint")
		fs.IntVar(&largest, "largest", 0, "list this many of the largest blocks")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), queryToolsUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	q := url.Values{}
	for k, v := range params {
		if *v != "" {
			q.Set(k, *v)
		}
	}
	for k, v := range map[string]bool{"all": all, "unlinked": unlinked, "context": withContext} {
		if v {
			q.Set(k, "true")
		}
	}
	if largest != 0 {
		q.Set("largest", strconv.Itoa(largest))
	}
	server, token, err := queryTarget(env, lake, server, tokenFile)
	if err != nil {
		return err
	}
	body, err := fetchRead(server, token, "/api/read/v1/tools", q, toolsHint)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	if err := writeToolStats(env, body, &out); err != nil {
		return err
	}
	return writeQueryOut(env, outPath, &out)
}

// writeQueryOut writes out to stdout, or to outPath once it is whole.
func writeQueryOut(env Env, outPath string, out *bytes.Buffer) error {
	if outPath == "" || outPath == "-" {
		_, err := out.WriteTo(env.stdout())
		return err
	}
	// The file appears only when it is whole, with mode 0600.
	tmp, err := os.CreateTemp(filepath.Dir(outPath), "."+filepath.Base(outPath)+".partial-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = out.WriteTo(tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), outPath)
}

// fetchRead is the lake's answer at path to q, or an error saying what
// the lake refused and what hint says to do about it.
func fetchRead(server, token, path string, q url.Values, hint func(status int, code, param string) string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryToolsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(server, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := queryClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query: lake unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, fmt.Errorf("query: lake answered %d, a redirect to %s; pass that URL as --server if it is the lake", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusOK {
		var refusal struct {
			Error string `json:"error"`
			Param string `json:"param"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&refusal)
		return nil, fmt.Errorf("query: lake answered %d %s%s", resp.StatusCode, refusal.Error, hint(resp.StatusCode, refusal.Error, refusal.Param))
	}
	// The answer is bounded by the lake's group and largest limits.
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// toolsHint says what to do about a refused statistics request.
func toolsHint(status int, code, param string) string {
	switch {
	case status == http.StatusNotFound:
		return ": the token lacks events:read, or the lake is older than tool statistics"
	case code == "invalid_request" && param != "":
		return ": the lake refused the " + param + " value"
	case code == "too_many_values":
		return ": too many tool rows or distinct tools to read; narrow the range, or pass --harness or --project"
	case code == "search_unavailable":
		return ": the lake has no search index, which tool statistics read"
	case code == "read_unavailable":
		return ": the lake ran out of time reading; narrow the range, or pass --harness or --project"
	}
	return queryHint(status, code)
}

// writeToolStats writes the lake's answer as JSONL, one key per line,
// and its totals to stderr.
func writeToolStats(env Env, body []byte, out *bytes.Buffer) error {
	var parts map[string]json.RawMessage
	if err := json.Unmarshal(body, &parts); err != nil {
		return fmt.Errorf("query: unreadable answer: %w", err)
	}
	var lists struct {
		Groups  []json.RawMessage   `json:"groups"`
		Largest []json.RawMessage   `json:"largest"`
		Totals  recall.ToolTotals   `json:"totals"`
		Since   *string             `json:"since"`
		Until   *string             `json:"until"`
		Context *recall.ToolContext `json:"context"`
	}
	if err := json.Unmarshal(body, &lists); err != nil || parts["totals"] == nil {
		return errors.New("query: the lake's answer has no totals")
	}
	line := func(key string, v json.RawMessage) {
		b, _ := json.Marshal(map[string]json.RawMessage{key: v})
		out.Write(b)
		out.WriteByte('\n')
	}
	summary := map[string]json.RawMessage{}
	for k, v := range parts {
		switch k {
		case "groups", "largest", "context", "units":
		default:
			summary[k] = v
		}
	}
	b, _ := json.Marshal(summary)
	line("summary", b)
	for _, g := range lists.Groups {
		line("group", g)
	}
	for _, l := range lists.Largest {
		line("largest", l)
	}
	if c := parts["context"]; c != nil {
		line("context", c)
	}
	span := "every recorded time"
	switch {
	case lists.Since != nil && lists.Until != nil:
		span = *lists.Since + " to " + *lists.Until
	case lists.Since != nil:
		span = "since " + *lists.Since
	case lists.Until != nil:
		span = "until " + *lists.Until
	}
	t := lists.Totals
	fmt.Fprintf(env.stderr(), "terva-lampi: %d tool calls and %d results in %d sessions, %s; %d groups\n", t.Calls, t.Results, t.Sessions, span, len(lists.Groups))
	if t.Undated > 0 {
		fmt.Fprintf(env.stderr(), "terva-lampi: %d rows have no recorded time\n", t.Undated)
	}
	if c := lists.Context; c != nil {
		fmt.Fprintf(env.stderr(), "terva-lampi: context is a %s: %s\n", strings.ReplaceAll(c.Kind, "_", " "), c.Note)
	}
	return nil
}

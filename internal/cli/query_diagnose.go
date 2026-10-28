package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"terva.sh/lampi/internal/recall"
)

const queryDiagnoseUsage = `terva-lampi query diagnose — what went wrong in past sessions, from a lake with a read token

usage:
  terva-lampi query diagnose SESSION_UID [--lake NAME | --server URL]
                             [--token-file FILE] [--out FILE]
  terva-lampi query diagnose [--since T] [--limit N] [--harness H] [--project ID]
                             [--lake NAME | --server URL] [--token-file FILE]
                             [--out FILE]

With a session uid, reads that session's diagnosis
(/api/read/v1/sessions/UID/diagnosis): findings such as oversized tool
results, error streaks, retry loops, repeated reads and context
pressure, each with a severity, measured values, a suggestion and up
to 16 event positions with viewer links.

Without one, reads the lake-wide view (/api/read/v1/diagnosis): the
most recently updated sessions since --since (default: 7 days ago), at
most --limit of them (default 200, at most 1000), with their findings
grouped by code, harness and project. The lake diagnoses each session
on request and stops before its five-second read budget; truncated in
the summary says sessions in range were left unread.

The output is JSONL, one object per line with one key:
  {"summary":{...}}   the session's counts, or the walk's range and size
  {"finding":{...}}   one per finding, most severe first
  {"group":{...}}     lake-wide, one per code, harness and project
docs/web-api.md describes every field. A count is also written to
stderr. No transcript content is read or written.

The read token, the lake and --out are as for query events.

One session:
  terva-lampi query diagnose --token-file read-token 01M61...
The last day's sessions, grouped:
  terva-lampi query diagnose --token-file read-token --since 2026-10-27
`

func runQueryDiagnose(env Env, args []string) error {
	var lake, server, tokenFile, outPath, since, harness, project string
	var limit int
	// Flag parsing stops at the uid, so the flags after it are parsed
	// again, each defaulting to the value already read.
	var uid string
	for {
		rest, err := parseFlags(env, args, queryDiagnoseUsage, func(fs *flag.FlagSet) {
			fs.StringVar(&lake, "lake", lake, "the config.json lake to read")
			fs.StringVar(&server, "server", server, "the lake URL")
			fs.StringVar(&tokenFile, "token-file", tokenFile, "file holding the read token (or LAMPI_READ_TOKEN_FILE)")
			fs.StringVar(&outPath, "out", outPath, "JSONL path (default: stdout)")
			fs.StringVar(&since, "since", since, "lake-wide: only sessions updated at or after this UTC time")
			fs.IntVar(&limit, "limit", limit, "lake-wide: read at most this many sessions")
			fs.StringVar(&harness, "harness", harness, "lake-wide: only sessions from this harness")
			fs.StringVar(&project, "project", project, "lake-wide: only sessions in this project id")
		})
		if err != nil {
			return err
		}
		if len(rest) == 0 {
			break
		}
		if uid != "" {
			fmt.Fprint(env.stdout(), queryDiagnoseUsage)
			return fmt.Errorf("unexpected argument %q", rest[0])
		}
		uid, args = rest[0], rest[1:]
	}
	q := url.Values{}
	for k, v := range map[string]string{"since": since, "harness": harness, "project": project} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if limit != 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if uid != "" && len(q) > 0 {
		return fmt.Errorf("--since, --limit, --harness and --project are for the lake-wide view; drop them or the session uid")
	}
	server, token, err := queryTarget(env, lake, server, tokenFile)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	if uid != "" {
		body, err := fetchRead(server, token, "/api/read/v1/sessions/"+url.PathEscape(uid)+"/diagnosis", nil, diagnoseHint)
		if err != nil {
			return err
		}
		var rep struct {
			Events   int64 `json:"events"`
			Gen      int64 `json:"generation"`
			Findings []json.RawMessage
		}
		if err := writeKeyed(body, "findings", "finding", &out, &rep); err != nil {
			return err
		}
		fmt.Fprintf(env.stderr(), "terva-lampi: %d findings in %d events of %s, generation %d\n", len(rep.Findings), rep.Events, uid, rep.Gen)
	} else {
		body, err := fetchRead(server, token, "/api/read/v1/diagnosis", q, diagnoseHint)
		if err != nil {
			return err
		}
		var g recall.DiagnosisGroups
		if err := writeKeyed(body, "groups", "group", &out, &g); err != nil {
			return err
		}
		fmt.Fprintf(env.stderr(), "terva-lampi: %d groups from %d sessions updated since %s\n", len(g.Groups), g.Sessions, g.Since)
		switch g.TruncatedBy {
		case recall.TruncatedLimit:
			fmt.Fprintf(env.stderr(), "terva-lampi: truncated: more than %d sessions in range; raise --limit, or narrow with --since, --harness or --project\n", g.Limit)
		case recall.TruncatedTime:
			fmt.Fprintln(env.stderr(), "terva-lampi: truncated: the lake's read budget ran out; narrow with --since, --limit, --harness or --project")
		}
	}
	return writeQueryOut(env, outPath, &out)
}

// writeKeyed writes body as JSONL: a summary line of every key but
// list, then one line under key per element of list. It decodes body
// into v as well.
func writeKeyed(body []byte, list, key string, out *bytes.Buffer, v any) error {
	var parts map[string]json.RawMessage
	if err := json.Unmarshal(body, &parts); err != nil || parts[list] == nil || json.Unmarshal(body, v) != nil {
		return fmt.Errorf("query: unreadable answer from the lake")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(parts[list], &items); err != nil {
		return fmt.Errorf("query: unreadable answer from the lake")
	}
	delete(parts, list)
	line := func(k string, v any) {
		b, _ := json.Marshal(map[string]any{k: v})
		out.Write(b)
		out.WriteByte('\n')
	}
	line("summary", parts)
	for _, it := range items {
		line(key, it)
	}
	return nil
}

// diagnoseHint says what to do about a refused diagnosis request.
func diagnoseHint(status int, code, param string) string {
	switch {
	case status == http.StatusNotFound:
		return ": no such session in the token's scope, the token lacks events:read, or the lake is older than session diagnosis"
	case code == "transcript_unavailable":
		return ": the session has no readable normalized transcript yet"
	case code == "invalid_request" && param != "":
		return ": the lake refused the " + param + " value"
	case code == "read_unavailable":
		return ": the lake ran out of time reading the session"
	}
	return queryHint(status, code)
}

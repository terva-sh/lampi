package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
)

const queryMetadataUsage = `terva-lampi query — discover reachable lake metadata with events:read

usage:
  terva-lampi query bays [--limit N] [--cursor C]
  terva-lampi query projects [filters] [--limit N] [--cursor C]
  terva-lampi query sessions [filters] [--limit N] [--cursor C]
  terva-lampi query session SESSION_UID

All commands accept --lake NAME or --server URL, --token-file FILE
(or LAMPI_READ_TOKEN_FILE), and --out FILE (mode 0600; default stdout).
Output is one JSON object. Listings carry items, next_cursor and as_of.
Continue until next_cursor is empty before treating an inventory as complete.
--bays ID,NAME focuses on any selected visible bay (up to 20 IDs, names or
aliases), intersected with token scope. Omit for all reachable sessions.
The page limit defaults to 50, maximum 200. Keep filters and limit identical
when continuing. Changes to the token's access scope require a fresh listing.

Filters combine with AND: --harness H, --project ID, --unlinked,
--cwd-prefix PATH, --cwd-hash HASH and --machine ID. Bays accepts only
paging flags. Projects cannot use --unlinked: query sessions --unlinked
shows sessions without project IDs, including some review companions.

Session metadata includes explicit cwd, cwd_hash, git_remote, harness,
machine provenance and visible bays. Unknown workflow provenance is null.
No raw artifacts, device configuration or hidden bay memberships are read.
`

func runQueryMetadata(env Env, kind string, args []string) error {
	var lake, server, tokenFile, outPath, uid string
	var harness, project, cwd, hash, machine, cursor, bays string
	var limit int
	var unlinked bool
	for {
		rest, err := parseFlags(env, args, queryMetadataUsage, func(fs *flag.FlagSet) {
			for _, f := range []struct {
				dest       *string
				name, help string
			}{
				{&lake, "lake", "configured lake name"}, {&server, "server", "lake URL"}, {&tokenFile, "token-file", "read token file"}, {&outPath, "out", "output JSON file"},
				{&harness, "harness", "only this harness"}, {&project, "project", "only this project ID"}, {&cwd, "cwd-prefix", "directory and descendants"}, {&hash, "cwd-hash", "exact directory hash"}, {&machine, "machine", "provenance machine ID"}, {&bays, "bays", "any selected bay IDs, names or aliases, comma-separated"}, {&cursor, "cursor", "continuation from the same filters"},
			} {
				fs.StringVar(f.dest, f.name, *f.dest, f.help)
			}
			fs.IntVar(&limit, "limit", limit, "items per page, 1 to 200")
			fs.BoolVar(&unlinked, "unlinked", unlinked, "only sessions without a project ID")
		})
		if err != nil {
			return err
		}
		if len(rest) == 0 {
			break
		}
		if kind != "session" || uid != "" {
			return fmt.Errorf("unexpected argument %q", rest[0])
		}
		uid, args = rest[0], rest[1:]
	}
	q := url.Values{}
	for k, v := range map[string]string{"harness": harness, "project": project, "cwd_prefix": cwd, "cwd_hash": hash, "machine": machine, "cursor": cursor} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if limit != 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if unlinked {
		q.Set("unlinked", "true")
	}
	path := "/api/read/v1/" + kind
	if kind == "session" {
		if uid == "" || len(q) > 0 {
			return fmt.Errorf("query session needs one SESSION_UID and no listing filters")
		}
		path = "/api/read/v1/sessions/" + url.PathEscape(uid)
	}
	if bays != "" {
		if kind == "bays" {
			return fmt.Errorf("query bays does not accept --bays")
		}
		q.Set("bays", bays)
	}
	server, token, err := queryTarget(env, lake, server, tokenFile)
	if err != nil {
		return err
	}
	body, err := fetchRead(server, token, path, q, queryHintMetadata)
	if err != nil {
		return err
	}
	if !json.Valid(body) {
		return fmt.Errorf("query: unreadable metadata answer from the lake")
	}
	var out bytes.Buffer
	out.Write(body)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		out.WriteByte('\n')
	}
	return writeQueryOut(env, outPath, &out)
}

func queryHintMetadata(status int, code, param string) string {
	if code == "invalid_filters_or_cursor" {
		return ": invalid metadata filters or continuation; keep filters, limit and token scope unchanged, or restart without --cursor"
	}
	if status == 404 {
		return ": the session is unknown/inaccessible, the token lacks events:read, or the lake does not support metadata discovery"
	}
	return queryHint(status, code)
}

---
schema: 3
id: TKT-01M3V3KCRA78QSQTY4SV0JVRAJ
title: "CLI: query events from a remote lake with a read token"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies:
  - TKT-01M3V3JSQXA831MB02BRWW6NHE
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T06:49:31Z
updated_at: 2026-10-01T08:21:03Z
created_by:
  id: agent:claude-code/dae09bda
  name: ""
updated_by:
  id: agent:claude-code/dae09bda
  name: ""
extensions: {}
---

## Description

### Why

The stream in the read API ticket is the transport. Agents need one command
they can run on their own machine, the same way they run `export` on the
lake host, with no `curl` or `jq`.

### Scope

`terva-lampi query events` takes the export filters and `--fields` with
the same names. It also takes:

- `--lake URL`, or the name of a lake entry in `config.json`, whose URL it uses
- `--token-file PATH` for a read token holding `events:read`

It writes NDJSON to stdout, or to `--out` with mode 0600. A stream that
ends early is an error with a nonzero exit, never a short file that looks
complete. The command never prints the token.

The token file has no default path. A guessed default would be read on a
machine where nobody placed it, so the path is a flag or an environment
variable named in the docs.

Documentation gets an agent-facing how-to: how an owner mints the token,
where to keep it, and the tool-call query from the export ticket's example
run against a remote lake.

## Acceptance criteria

- [x] query events takes the export filter and --fields flags and returns the same rows as export against a test lake.
- [x] A cut stream exits nonzero; --out is written with mode 0600; the token never appears in output or errors.
- [x] docs/cli.md and an agent-facing how-to show minting a token and running the tool-call query remotely.

## Implementation plan

internal/cli/query.go: 'terva-lampi query events' reuses export's eventFlags (validated locally, sent as typed so the lake parses dates the same way) and adds --lake/--server, --token-file (or LAMPI_READ_TOKEN_FILE, no default) and --out. The server comes from --server, the named config.json lake, or the only one listed. The token file must hold an lrt_ token; CheckToken refuses plain http off loopback. streamEvents copies lines to the output, strips the lampi:end line, and fails without it, on complete=false, on a row count mismatch, or on lines after it. --out goes to a temp file in the same directory and is renamed only after a complete stream. The HTTP client bounds dial, TLS and first byte, not the stream's length.

## Notes

**agent:claude-code/dae09bda** at 2026-10-01T07:17:23Z

### Decisions
- **--out appears only when complete.** A temp file beside it is renamed after the end line checks out and removed otherwise, so a short file never passes for a whole answer. stdout cannot be taken back, so there the exit status carries it.
- **The row count is checked against the end line.** A proxy that drops data mid-stream without cutting the connection would otherwise go unnoticed.
- **No default token path.** The ticket asked for this: a guessed path would be read on machines where nobody put a token. The how-to suggests ~/.config/terva-lampi/read-token as a place to keep it, and the command still needs the flag or the env var.
- **The lake comes from config.json when it is unambiguous.** The lake mux serves /v1 and /api on one origin, so a lake's ingestion server is also its read API.

### Evidence
- TestQueryEventsMatchesExport runs a real lake with the web routes behind httptest. For three flag sets, query events and export return identical rows. --out is mode 0600. A raw-only token fails with the 404 hint naming events:read, and the token is absent from the error and stderr.
- TestQueryEventsRefusesAShortStream: a cut stream, a partway end, a count mismatch, and lines after the end each fail and leave no --out file; a complete stream passes and sends the bearer header and the query as typed; no filter, fields alone, no token, plain http, a missing token file, --lake with --server, an unknown query and a device-token file are refused, the last without echoing its contents.
- `GOFLAGS=-mod=mod just ci` passes.
- Not run against the live lake: it needs a minted token, which only the owner can mint.

**agent:claude-code/dae09bda** at 2026-10-01T07:50:45Z

Review 1686 on #178: finding-1 (high) accepted, the client follows no redirect, since CheckToken vetted only the given server and a redirect would carry the token elsewhere; a 3xx is an error naming the Location. finding-2 (medium) accepted, the client cancels after two minutes with no bytes (idleReader resets a timer per read), and the stream (#177, ed68aff) now flushes or sends a blank keepalive line every 15 s so a slow selective scan is not mistaken for a dead connection; the client skips blank lines. CI flakes seen on unrelated tests during this stack: internal/web TestAllowSelectedIsAllOrNothing (503 after 38 s, run 1719) and internal/upload TestAttemptRecordsTheLastError (no bytes moved for 1s, run 1723).

## Summary

Landed in #178 (merge 0fbc0a0). terva-lampi query events takes export's filters and --fields, reads the event stream with a read token from --token-file or LAMPI_READ_TOKEN_FILE, and fails on a stream that ends early, stops partway, miscounts, or goes silent for two minutes. --out is written 0600 and only when complete. It follows no redirect. docs/reading-the-lake.md is the agent-facing how-to. Reviews 1686 (redirects; idle reads) and 1688 (idle timeout on error bodies) were accepted and fixed.

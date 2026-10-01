---
schema: 3
id: TKT-01M3V3KCRA78QSQTY4SV0JVRAJ
title: "CLI: query events from a remote lake with a read token"
type: task
status: ready
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
updated_at: 2026-10-01T06:49:32Z
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

- [ ] query events takes the export filter and --fields flags and returns the same rows as export against a test lake.
- [ ] A cut stream exits nonzero; --out is written with mode 0600; the token never appears in output or errors.
- [ ] docs/cli.md and an agent-facing how-to show minting a token and running the tool-call query remotely.

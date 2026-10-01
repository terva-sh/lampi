---
schema: 3
id: TKT-01M3V3J8VZKAZJJAR9VTMDGJGD
title: "Export: filter normalized events and select their fields"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/normalize
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T06:48:54Z
updated_at: 2026-10-01T07:46:16Z
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

An agent evaluating a shell emulation for an iOS port of terva needed every
tool call in the lake. The owner had to run, on the lake host:

    sudo -u terva-lampi terva-lampi export --data /var/lib/terva-lampi --format events \
      | jq -c 'select(.event_type=="tool_call") | {h:.harness, s:.session_id, t:.tool.name, a:.content_text}'

`export` has no filters. It wrote every event of every project, and `jq` did
the selecting. An agent that asks a corpus-wide question should get only the
rows it asked for, from lampi itself.

### Scope

`export --format events` gains filters with the names and vocabulary that
`recall.SearchRequest` already uses:

- event filters: `--event-type`, `--actor`, `--tool`, `--tool-error true|false`, `--raw-type`
- session filters: `--harness`, `--project` (a project id)
- time: `--since` and `--until`, RFC 3339, compared with each event's `recorded_at`

Filters are exact-match and combine with AND. Values are checked against the
same vocabulary and the same 256-byte cap as recall, before any output.

`--fields PATH,PATH` writes one JSON object per matching event, holding only
those paths. A path is a schema_version 1 field name, or one level into a
nested object (`tool.name`, `model.id`). An unknown path is refused before
any output.

The matching and the projection live in `internal/recall` as one type, so
the read API stream (a sibling ticket) applies exactly the same rules.
`SearchRequest` keeps its SQL path; this type works on decoded events.

Filters and `--fields` apply to `--format events` only. With `sharegpt` or
`trajectory` they are refused, because those formats write one training row
per session, not events.

### Decisions

- **Keys are the dotted paths, not nested objects.** `{"tool.name": "Bash"}`
  reads directly in `jq` and DuckDB `read_ndjson` without unnesting.
  Rebuilding the nesting was rejected: it adds code and helps no consumer.
- **A missing value is `null`, and the key is still written.** Every row then
  has the same keys, which a table loader expects.
- **`tool_error` matches the recorded flag exactly**, as recall does: a result
  whose harness did not record success or failure matches neither value.
- **Time filters use the event's `recorded_at`.** An event whose timestamp
  does not parse matches no time filter.

## Acceptance criteria

- [x] Each filter works alone and combined with the others on synthetic data, and an invalid value fails before any output, naming the flag.
- [x] --fields writes only the listed paths, with null for a missing value, and refuses an unknown path before any output.
- [x] Filters and --fields are refused with --format sharegpt and trajectory.
- [x] The matching and projection are one type in internal/recall, tested there, and export calls it.
- [x] docs/cli.md documents the flags and replaces the jq pipeline with the equivalent export command.

## Implementation plan

Add recall.EventFilter (Validate, Session, Line) and recall.Fields (ParseFields, Project) in internal/recall/filter.go. Line reuses docRow.fill, the index's own line reader, so harness/project come from the session and type, actor, tool, tool_error, raw_type and recorded time (excluding recorded_at == ingested_at) are read exactly as search reads them. SearchRequest.Filter() hands its structured part over, and Search validates through EventFilter.Validate so both share one vocabulary. web's parseWhen moves to recall.ParseWhen for the CLI. export gains eventFlags (shared with the coming query events command), skips sessions by Session before reading them, and splits JSONL lines only when a filter or --fields is set; otherwise sessions are written as stored.

## Notes

**agent:claude-code/dae09bda** at 2026-10-01T06:56:13Z

### Evidence
- TestEventFilterAgreesWithSearch runs 14 filter combinations through the index and through EventFilter over the published files and requires identical (session, position) sets; 11 of them must be non-empty.
- TestExportFiltersAndFields covers each flag, combinations, a date-only until, whole events without --fields, and nine refusals, each checked to write no --out file.
- `GOFLAGS=-mod=mod just ci` passes with XDG dirs in scratch.

### Decisions
- A line over recall.MaxLine (16 MiB) is unreadable for filtering and projection, as the index treats it, so export and search agree on it too.
- Session filters alone (--harness, --project) are allowed in export, unlike search. Export already reads the whole lake; the guard in search exists to stop paging the corpus through the index.
- Not run against the live lake: it needs sudo to the service user. The owner can check it with the docs/cli.md example.

**agent:claude-code/dae09bda** at 2026-10-01T06:57:31Z

PR #173 on Forgejo (https://git.local.sothr.com/terva-sh/lampi/pulls/173), rebased onto origin/main 360e460; just ci passes there. terva-review dispatched with request-id ready-review.

## Summary

Landed in #173 (merge 0c428f6). export --format events takes --harness, --project, --event-type, --actor, --tool, --tool-error, --raw-type, --since, --until and --fields. recall.EventFilter reads lines with the search index's reader, so export selects what search selects (TestEventFilterAgreesWithSearch); Search validates through the same function. Review 1678 (medium, empty --fields bypassed the events-only check) was accepted and fixed in 157b387: every filter flag refuses an empty value.

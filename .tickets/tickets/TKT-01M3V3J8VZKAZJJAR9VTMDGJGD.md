---
schema: 3
id: TKT-01M3V3J8VZKAZJJAR9VTMDGJGD
title: "Export: filter normalized events and select their fields"
type: task
status: ready
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
updated_at: 2026-10-01T06:49:12Z
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

- [ ] Each filter works alone and combined with the others on synthetic data, and an invalid value fails before any output, naming the flag.
- [ ] --fields writes only the listed paths, with null for a missing value, and refuses an unknown path before any output.
- [ ] Filters and --fields are refused with --format sharegpt and trajectory.
- [ ] The matching and projection are one type in internal/recall, tested there, and export calls it.
- [ ] docs/cli.md documents the flags and replaces the jq pipeline with the equivalent export command.

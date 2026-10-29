---
schema: 3
id: TKT-01M61SPVY01PVEFT1N9JKBK4HB
title: Project Claude Code's offered tool list for the context hint
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/normalize
  - area/search
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T17:42:44Z
updated_at: 2026-10-29T00:48:27Z
created_by:
  id: agent:claude-code/049834bc-stats
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-offered
  name: ""
extensions: {}
---

## Description

The starting-context hint (TKT-01M61MZNW) can only correlate starting context with the servers a session called, because no normalized event records which tools a session was offered. Claude Code does record this. Recent transcripts carry `attachment` lines of type `deferred_tools_delta`, with `addedNames`, `removedNames` and MCP server status lists such as `pendingMcpServers` and `failedMcpServers`. They also carry `prompt_snapshot` attachments with a `tools` key. The Claude normalizer projects both as unknown events with the payload in `extra`, which the search index does not store.

Codex rollouts record no tool list. `session_meta` and `turn_context` carry no tool or server fields; this was checked on recent rollouts, by key names only.

Projecting the offered tool names would let the hint report each server's offered-tool count per session directly, and would show servers that were configured but never called, which the correlation cannot see. Decide how the names reach the stats engine: an indexed column, a per-session summary row, or a read of the events file. Check the attachment shapes against Claude Code's current transcripts before relying on them, since attachment types change between releases.

## Implementation plan

Shapes checked on current Claude Code transcripts (2.1.280 to 2.1.312), by key names and tool names only:

- `attachment` lines of type `prompt_snapshot` carry `tools`, a list of objects with `name`, `description`, `schema` and, for MCP tools, `server`. Snapshots alternate between an empty list and the full one, so an empty list says nothing.
- `deferred_tools_delta` carries `addedNames`, `readdedNames`, `removedNames`, `wireHiddenNames`, `addedLines` and the MCP status lists `pendingMcpServers`, `failedMcpServers` and `needsAuthMcpServers`. It is rare now (7 lines against 685 snapshots in the 80 newest transcripts).
- `deferred_tools_record` is common but holds definitions and input copies, not a name list.

The normalizer already copies each of these lines whole into `extra.attachment` of an `unknown` event with raw_type `attachment`. The indexer already decodes every event, extra included, when it builds the stats rollup; the Codex running total is read from extra the same way. So no projection change and no renormalize is needed.

### Approach

1. Indexing (internal/recall/stats.go): for a Claude session, collect the set of names in `prompt_snapshot.tools[].name`, `deferred_tools_delta.addedNames` and `readdedNames`. Write one stats row per distinct name, kind `tool_offered`, hour NULL, n 1. Every stats consumer filters by kind, so the new kind is invisible to them.
2. Schema version 8: an in-place upgrade like upgradeV6 that blanks every indexed head, so the next pass reads each session again and writes its offered rows. Rows whose signature did not change are kept, so the full-text index is not rewritten.
3. Context hint (internal/recall/toolcontext.go): with `context`, read the walked sessions' offered rows in one query on the same connection, joined to the scope table, and keep only per-origin offered-tool counts per session. A session that recorded a list is classed by the origins it was offered or called. A session that did not is classed by its calls, as before. Add per row `offered` (sessions whose list offered the origin, how many of those never called it, and the median offered-tool count), and per harness `offered_sessions`. The block stays `kind: correlation_hint`, with a note that says which basis each session used.
4. Surfaces: the CLI and MCP output carry the block whole, so only their help text and docs/web-api.md, docs/cli.md and docs/reading-the-lake.md change.

### Alternatives considered

- **An indexed column on docs**, such as a name list on the attachment row. It loses because a new column enters each row's signature, so the upgrade would rewrite every row and its full-text entry, as version 5 did. It would also store a list in a column for one harness's rows, and the walk would parse it at query time.
- **Reading the events file at query time.** It loses because it re-streams every session's normalized JSONL, about 280 KB per snapshot line, on every stats request. The epic asks for index aggregates and one streaming pass.
- **A new normalized event type or field from the normalizer.** It is cleaner to read, but it changes the projection, so every Claude session would have to be renormalized before the hint saw a list. The payload is already in `extra`.
- **A per-session summary row in the stats rollup (chosen).** It needs no column, and the stats table is replaced whole at each index, so it stays exact. The cost is one re-read of each session after the upgrade.

Offered means offered at some point in the session (the union of every list), not only in the first snapshot. Deferred tools are offered by name before they are loaded, and a session's servers rarely change.

## Notes

**agent:claude-code/049834bc-offered** at 2026-10-29T00:48:18Z

Implemented as planned in 1b4ccb4 and ed265d7, with no deviation from the plan.

### What landed

- internal/recall/stats.go: `claudeOffered` reads `extra.attachment` of a Claude `unknown` event with raw_type `attachment`. It reads `prompt_snapshot.tools[].name` and `deferred_tools_delta.addedNames` and `readdedNames`, and any other type yields nothing. Each distinct name becomes one `tool_offered` stats row with hour NULL and n 1, at most 2,000 per session. Other harnesses are not read.
- internal/recall/index.go: schema 8. `upgradeV7` blanks every indexed head in place.
- internal/recall/toolcontext.go: `loadLists` reads the walked sessions' offered rows in one query through stats_session, before the walk, on the same connection. It keeps per session only a count per non-built-in origin. New fields: `harnesses[].offered_sessions`, and `rows[].offered` `{sessions, uncalled_sessions, median_tools}`. The note now says which basis each session used.
- Docs: web-api.md, cli.md, reading-the-lake.md and web-dashboard.md (the search index section and the schema 8 upgrade). Help text in `query tools` and the MCP `tool_stats` description. The CLI and MCP outputs carry the block whole, so their code did not change.

### Checks

- TestToolStatsContextOffered and TestVersion7IndexUpgradesInPlace fail with stats.go and index.go reverted, and pass with them. TestToolStatsPlans now also asserts that the offered query scans the scope table and searches stats_session.
- A throwaway test, not committed, ran one current local transcript through the Claude normalizer, a JSON round trip and the stats rollup. It found 110 offered names. Only the counts were read.
- `GOFLAGS=-mod=mod just ci` passes, with XDG data, state and cache in a scratch directory. The worktree guard refused an XDG_CONFIG_HOME override, so that one was left at its default. The tests that need it set it themselves.

### Release cost

- No renormalize. The attachment lines are already in every normalized generation's `extra`.
- Index: the first start of the release upgrades search.db from schema 7 to 8 in place, and every session is read once more. The cost is about that of the schema 6 to 7 upgrade: each session's normalized JSONL is read and its stats rows are replaced. docs rows whose signature is unchanged are kept, so the full-text index is not rewritten and search keeps answering. Coverage shows sessions as behind until they are read. Until then the hint classes them by calls, and ranged tool statistics are unaffected.
- The new rows are about one per offered tool per Claude session, around 100 to 110 on this workstation's sessions, untimed. They are short names in the stats table, which is small next to docs and fts.

## Summary

Landed in 1b4ccb4 and ed265d7. The index keeps each Claude Code session's offered tool names as tool_offered stats rows, read from the prompt_snapshot and deferred_tools_delta attachments already in the normalized extra. The starting-context hint uses them for offered_sessions and per-row offered {sessions, uncalled_sessions, median_tools}. Index schema 8 is an in-place upgrade that reads each session again; no renormalize.

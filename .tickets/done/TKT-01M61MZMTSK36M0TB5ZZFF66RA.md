---
schema: 3
id: TKT-01M61MZMTSK36M0TB5ZZFF66RA
title: "Search index: store size, call, model and token columns per event"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
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
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:40:51Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-index
  name: Claude Code local agent
extensions: {}
---

## Description

The search index `docs` table (internal/recall/index.go:71) stores content truncated to `IndexContentMax` = 256 KiB and no size before truncation, so the largest tool blocks report as 256 KiB. It also stores neither `tool.call_id` nor the model id.

Add three columns:

- `content_bytes INTEGER`: the length of `content_text` before truncation, null when the event has none.
- `call_id TEXT`: pairs a call with its result for latency and orphan checks.
- `model_id TEXT`: lets usage be split by model.

Add them together because each one enters `sig()`, which rewrites every row the first time. Index `call_id` within a session if pairing needs it. Measure the one-time rewrite on the internal lake and record the time and WAL growth, given the index maintenance issues in TKT-01M4ET6T and TKT-01M4F4SE.

## Acceptance criteria

- [x] docs rows carry content_bytes (size before truncation), call_id and model_id.
- [x] The three columns land in one change so the signature rewrite happens once.
- [ ] The one-time rewrite time and WAL growth on the internal lake are recorded.
- [x] docs rows carry input_tokens, output_tokens, cache_read_tokens and cache_write_tokens from the event's usage (null stays null, a reported zero stays 0), in the same version bump.

## Implementation plan

Schema version 5, rebuilt rather than migrated, like versions 2 to 4.

1. docs gains `content_bytes INTEGER`, `call_id TEXT` and `model_id TEXT`, placed before `content`. Content stays the last column, so reading a new column never walks the overflow pages of a large content value.
2. `docRow.fill` records `len(*ev.ContentText)` before truncation, `ev.Tool.CallID` and `ev.Model.ID`. `sig()` hashes all three. `docWriter` binds 15 values per row (200 rows is 3000 parameters, well under SQLite's limit). The batch byte accounting still counts the truncated content, which is what the statement carries.
3. Two partial indexes, both `WHERE call_id IS NOT NULL`, so they hold only tool rows and no search query can use them. searchSQL never names call_id, so its plans stay as they are.
   - `docs_call ON docs(session_uid, call_id)` pairs a call with its result.
   - `docs_call_time ON docs(event_type, recorded_ns)` drives per-tool aggregates over a time range.
4. Bump indexVersion to 5 and document it in the comment. A rebuild writes no FTS deletes. ALTER TABLE ADD COLUMN plus a sig change would delete and reinsert every row and leave every old row in the FTS segments until optimize.
5. Tests: the three columns are stored (content_bytes is the untruncated size and null without content; call_id and model_id are null when absent); a change to any of them rewrites the row; a version 4 file is rebuilt; the search plans do not use the new indexes; the pairing and time-range aggregate queries do.
6. Measure a full rebuild on a synthetic lake with tool call and result pairs at v4 and v5: time, file size, peak WAL. Record it in a note.
7. Docs: the "Search index" section of docs/web-dashboard.md names the stored per-event fields and the one-time rebuild.

Alternatives: an ALTER TABLE migration loses because the sig change rewrites every row anyway, and each rewrite is an FTS delete as well as an insert. A plain (tool_name, recorded_ns) index loses because searchSQL filters by tool_name and since, and the planner could pick it and then sort by id.

8. Added at the coordinator's request: input_tokens, output_tokens, cache_read_tokens and cache_write_tokens from ev.Usage, in sig(), with docs_tokens ON docs(session_uid, pos) WHERE input_tokens IS NOT NULL for each session's first token count (TKT-01M61MZNW).

## Notes

**agent:claude-code/049834bc-index** at 2026-10-28T16:40:40Z

Implemented in c383a3e. Scope grew at the coordinator's request: the same version bump also stores token usage, for TKT-01M61MZNW (starting-context cost of MCP servers), which needs each session's first usage row. AC 4 records that, and the title changed to match.

### Schema (indexVersion 5)

- New docs columns, all nullable, placed before `content`: `content_bytes INTEGER` (`len(content_text)` in bytes before truncation, null when content_text is null), `call_id TEXT` (tool.call_id), `model_id TEXT` (model.id), `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens INTEGER` (from usage; null stays null, a reported 0 stays 0; cost_usd is not stored). All seven enter `sig()`.
- content stays the last column. SQLite reaches a column stored after a large value by walking that value's overflow pages, so a stats query reading content_bytes after a 256 KiB content would read it all.
- New partial indexes:
  - `docs_call ON docs(session_uid, call_id) WHERE call_id IS NOT NULL` pairs a call with its result.
  - `docs_call_time ON docs(event_type, recorded_ns) WHERE call_id IS NOT NULL` covers time ranges over tool rows.
  - `docs_tokens ON docs(session_uid, pos) WHERE input_tokens IS NOT NULL` finds each session's first token count.
- Search never names those columns, so it cannot pick these indexes. TestToolUsageIndexes checks six search shapes and the aggregate plans.

### Rebuild rather than migrate

A version 4 file is deleted and rebuilt, the same as versions 2 to 4. ALTER TABLE ADD COLUMN lost: the sig change rewrites every row anyway, and each rewrite is an FTS delete plus an insert. That leaves every old row in the FTS segments until optimize and roughly doubles the work. A plain `(tool_name, recorded_ns)` index lost because searchSQL filters by tool_name and since; the planner could pick it and then sort every match by id in a temp b-tree.

### Measurements (synthetic lake, this workstation)

The scratch test is not committed. It used 200 codex sessions × 100 rounds: a message, a tool_call with call_id and model, and a tool_result, with every 50th result about 290 KiB. That is 60,000 events, with Pass timed from an empty index. Peak WAL was sampled every 2 ms.

| index | rebuild (3 runs) | search.db | peak WAL | WAL after |
|---|---|---|---|---|
| v4 (main) | 11.0 s, 11.2 s, 11.0 s | 327.7 MiB | 8.4 MiB | 0 |
| v5 | 11.5 s, 11.8 s, 12.3 s | 330.9 MiB | 7.8 MiB | 0 |

v5 costs about 5% more rebuild time and 1% more file. The WAL stays at about one session's transaction because each session commits separately and the pass truncates at the end. The fixture has no usage rows, so docs_tokens is empty here; usage rows are a small share of a real lake.

AC 3 is not ticked. The internal lake cannot be read or touched from this session. The upgrade cost there is one full rebuild, the same as deleting search.db, so measure it at the rollout. Record the time from start until Coverage.behind reaches 0, and the peak size of search.db-wal.

### Verification

`go vet ./...`, the gofmt check, the build, and `go test` passed for every package that reads no user config or state directory. That includes recall, web, api, catalog and normalize. The full `just ci` with scratch XDG_CONFIG_HOME and XDG_STATE_HOME did not run, because this session's worktree guard refuses any command that sets XDG_CONFIG_HOME. Still to run: internal/cli, internal/config, internal/discover, internal/adapter/*, cmd/lampi-perf, cmd/terva-lampi, internal/synthetic, internal/testharness and internal/web/smoketest. Run `just ci` with scratch XDG before merging.

### Querying the new columns (for the aggregation tickets)

Always join `indexed i ON i.session_uid=d.session_uid`, as searchSQL does. That hides the rows of pending, failed and purged sessions. Apply read-token scope the way Search does.

- Results with their tool name, size and latency in a time range:
  `SELECT c.tool_name, c.model_id, r.content_bytes, r.tool_error, r.recorded_ns-c.recorded_ns FROM docs r JOIN indexed i ON i.session_uid=r.session_uid JOIN docs c ON c.session_uid=r.session_uid AND c.call_id=r.call_id AND c.event_type='tool_call' WHERE r.event_type='tool_result' AND r.call_id IS NOT NULL AND r.recorded_ns>=? AND r.recorded_ns<?` walks docs_call_time for r and docs_call for c. Add `AND r.harness=?` or `AND r.project_id=?` as residual filters. Keep `r.call_id IS NOT NULL` written out: it is what lets SQLite use the partial index.
- Calls per tool and model: `... FROM docs d WHERE d.event_type='tool_call' AND d.call_id IS NOT NULL AND d.recorded_ns BETWEEN ...`, grouped by tool_name and model_id.
- Use `content_bytes` for sizes, never length(content), which stops at 256 KiB. For p50, p95 and max, read sizes ordered per tool and pick the ranks in Go, since SQLite has no percentile function.
- Orphans: a result whose LEFT JOIN to a call finds nothing, or the reverse.
- First usage per session: `SELECT session_uid, min(pos) FROM docs WHERE input_tokens IS NOT NULL GROUP BY session_uid` walks docs_tokens with no sort. Join back on (session_uid, pos) for input_tokens + cache_read_tokens + cache_write_tokens.
- Tool rows with no call id (a harness that writes none) are outside docs_call_time. Count them through docs_tool or docs_type.
- recorded_ns is null for an event whose source had no time, so a time range never includes those rows.

## Summary

Search index version 5 (c383a3e) stores content_bytes (size before truncation), call_id, model_id and four token counts per docs row, all in sig(), ahead of content. It adds the partial indexes docs_call (session_uid, call_id), docs_call_time (event_type, recorded_ns) and docs_tokens (session_uid, pos). A v4 file is rebuilt once. A synthetic 60k-event rebuild took 11.0 s on v4 and 11.5 to 12.3 s on v5, the file grew from 327.7 to 330.9 MiB, and peak WAL was about 8 MiB. Search plans are unchanged. The internal-lake measurement (AC 3) is still open for the rollout, and full just ci with scratch XDG still needs a run (see note).

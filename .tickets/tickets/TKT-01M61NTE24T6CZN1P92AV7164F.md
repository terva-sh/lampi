---
schema: 3
id: TKT-01M61NTE24T6CZN1P92AV7164F
title: "Usage rollup: lake and harness token totals with cache split"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/d4b24133
  branch: t3/overview-operations-stats
  worktree: /home/sothr/.t3/worktrees/lampi/t3-d4b24133
  commit: 6a552827fe4fa020d8af6710203ee8a125104db8
  session: null
  claimed_at: 2026-10-28T16:41:30Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:34:46Z
updated_at: 2026-10-28T16:41:30Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

Show total tokens for the whole lake and per harness on the overview page.

### Approach

- Add a rollup table keyed by session and generation, written when the indexer projects a session and replaced when the generation changes. Backfill existing sessions.
- Store four token kinds separately: plain input, output, cache read, cache write, plus cost when reported. Never store only a combined input figure (owner requirement, see the epic).
- Normalize the counting so the kinds mean the same thing across harnesses. Codex `input_tokens` includes `cached_input_tokens`, so plain input there is input minus cached. Claude reports cache read and cache creation apart from input already. Keep a field null when a harness does not report it, and show "not reported" rather than zero.
- Deduplicate Claude usage on the message id; Claude Code repeats `message.usage` on each content-block line.
- Start with a spike: compute totals for a sample of real sessions per harness and compare against the harness's own figures before building the page.

### Open question

Codex usage events read `last_token_usage`, falling back to `total_token_usage`. Confirm that summing `last_token_usage` across a session gives the session total, and what happens when only the total is present.

## Acceptance criteria

- [ ] Plain input, output, cache read and cache write are stored and shown separately
- [ ] Claude usage is deduplicated per API message
- [ ] A spike compares totals against harness figures for sample sessions
- [ ] Existing sessions are backfilled

## Implementation plan

### Where the rollup lives

A `stats` table in search.db, written by `indexSession` in the same transaction as the session's docs rows and replaced whole each time the session is indexed. The indexer already parses every line of a session's new generation into a `normalize.Event`, so the rollup costs no extra read. `remove` and `RemoveFromIndex` drop a session's stats with its docs, so purge and failed sessions leave nothing behind.

Rejected:
- Summing usage from the docs table at request time: usage is not in docs, and grouping millions of event rows per page load does not scale.
- A catalog table: the catalog does not read events, and a second reader of every generation would duplicate the indexer's work and its failure handling.
- Request-time reads of the events files: one full read of the lake per page.

### Shape

One row per session, UTC hour (NULL when the event had no recorded time), kind and name: `n` plus nullable `input`, `output`, `cache_read`, `cache_write`, `cost_usd`. Kinds: `event`, `error` (name = raw_type), `tool_call` and `tool_failed` (name = tool name), `usage` (n = model calls after dedup). Charts sum hours into days. The rows of a session are small: hours times distinct tools.

### Counting rules, one per harness

The four token kinds are exclusive: plain input does not include cache read or cache write.

- claude: Claude Code repeats `message.usage` on every content-block line of one response. Keep the last usage per `extra.id` (the message id). On 30 local sessions 25,924 of 46,076 usage lines repeated a message id, and 4 repeats differed (streaming updates), so last wins rather than first.
- codex: sum differences of the running `extra.info.total_token_usage`, not `last_token_usage`. On 30 local sessions 236 of 8,508 token_count lines repeated the previous total; the total never decreased. A decrease is treated as a restart. Plain input is `input_tokens - cached_input_tokens`; cache write is not reported (null).
- terva: one usage line per model call, cache already separate from input (cache_read exceeded input on every sampled line). Summed as is, including usage on compaction lines.
- opencode, cursor, cursor-cli: summed as normalized.

A field no event reported stays NULL, and the page shows "not reported", not 0.

### Migration

indexVersion 5. A version-4 index is upgraded in place: create `stats`, then blank `indexed.head` so the next pass re-reads every session. Docs rows whose signature is unchanged are kept, so FTS is not rewritten and search keeps answering while stats fill in. Other versions are still rebuilt.

### Scope

Index rows carry no bays. A caller that does not read every bay passes the session uids its scope reads, from a new catalog read that takes a Scope, and the query filters with `json_each`.

### PR split

1. This ticket: stats table, counting rules, migration, `Index.Totals` by harness, overview tiles for tokens per harness.
2. Usage over time, error trends, tool usage: query functions and pages on top of the same table.

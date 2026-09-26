---
schema: 3
id: TKT-01M3F2PGHM6VHQBNE5XKDXS407
title: "Search: index current normalized content with durable FTS5 work"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3F2PGA1EFCEPEBPT1JFR3JJ
origin: null
dependencies:
  - TKT-01M3F2PGDY06D7XE12NWQ9EZF4
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:42:52Z
updated_at: 2026-09-26T20:59:21Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

### Scope and rationale

Create a rebuildable SQLite FTS5 search index for current normalized content_text. Index keys include session UID, generation and event position. Indexing must lag ingestion safely, expose pending/failed state and never make stale results look current. Keep unknown/encrypted fields out of indexed text. This ticket owns index schema, durable jobs, maintenance and query functions, not the browser search screen.

### Contract

Follow docs/web-ui-plan.md release B and its pinned sibling references. Use only isolated synthetic data. New work remains draft pending promotion.

## Acceptance criteria

- [x] Durable indexing converges after restart and superseded work cannot publish stale rows; failures leave ingestion functional and lag visible.
- [x] Only current successful normalized generations and content_text are searchable; purged and failed generations are excluded at query time.
- [x] Literal query/filter/date input is validated and result pagination is bounded, deterministic and resistant to SQL/FTS injection.
- [x] Purge, rebuild and backup/restore tests preserve index correctness without concurrent writer violations.
- [x] Indexing and queries have bounded memory/results on large synthetic sessions and report pending/failed coverage.

## Definition of done

- [x] Focused tests and relevant API/operator docs are complete; record evidence and decisions in the ticket.

## Implementation plan

The index lives in internal/recall (index.go, search.go) beside the event reader, so the web adapter and the planned MCP adapter share it.

### Storage
search.db is a separate SQLite file in the data directory with its own connection pool. It holds three tables:
- `docs`: one row per event, with position, harness, project, event_type, actor, tool name and tool error, raw_type, recorded time, and content cut at 256 KiB.
- `fts`: an FTS5 external-content table with the trigram tokenizer, kept in step by triggers.
- `indexed`: the (session, generation, head) currently searchable.

### Durability
The catalog is the work queue. Each pass compares catalog.PublishedSessions with `indexed`:
- It indexes ready sessions whose generation differs.
- It drops sessions that are gone, failed or never published.
- It keeps pending sessions' old rows until the new generation lands.

A crash loses nothing: the next pass sees the same difference, and rows from a half-written generation are deleted before a rewrite. Passes run at start, 200 ms after a publish (api.Server.OnPublished, fired once the job row is cleared), and every five minutes.

### Consistency
Rows are written in batches (500 rows or 8 MiB) under the new generation. They are invisible until one statement flips `indexed`; old rows are deleted afterwards. Queries join docs to indexed on generation, then drop any hit whose session is not ready at that generation in the catalog. A stale, failed or purged result is never returned, even before a pass catches up.

### Queries
A query is a literal string, quoted as a single FTS5 string (quotes doubled), so no FTS syntax is interpreted. It must be 3 to 1024 bytes, valid UTF-8 and free of NUL. Filters are bound parameters: harness, exact project or unlinked, and a UTC recorded-time range where since is inclusive and until exclusive. Events without a recorded time are excluded when a range is set. Results are ordered newest-indexed first with a signed keyset cursor on the FTS rowid, bound to the filters. Snippets are computed in Go with case folding and byte offsets. No HTML comes from FTS or the transcript.

### Lifecycle
serve builds the index only with web config (cli.startWeb). BeforeClose stops the index after the workers and before the catalog closes. serve purge removes a session's index rows first. Backup skips search.db, and a restore rebuilds it. An unknown schema version is deleted and rebuilt.

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-26T20:59:14Z

### Decisions and rejected alternatives

- **Trigram tokenizer over unicode61.** unicode61 drops punctuation, so `git push --force` and file paths would not match literally. Trigram gives case-insensitive substring semantics, which is what "literal text" means for commands and paths. Costs: queries need at least 3 characters, and the index is large, measured at 116 MiB for 50 MiB of text (TestIndexScale).
- **A separate search.db instead of tables in catalog.db.** The catalog runs on one connection. Indexing tens of MB would contend with ingest there, and it would bloat `serve backup` (VACUUM INTO) with derived data. A separate file can be deleted to rebuild and needs no catalog migration. That matters because the onboarding branch adds catalog migration 4, and this work would otherwise collide with it. ATTACHing the file to the catalog connection was also rejected: it keeps the contention and complicates the read-only open.
- **No job table; the catalog is the queue.** A durable job table would duplicate what published_gen/published_head already say, and would need its own crash reconciliation. A full comparison of 20k sessions is one catalog read.
- **Snippets computed in Go, not with FTS5 snippet().** With trigram, snippet() returned odd windows (probe: `"…Then I ran \x01GIT\x02…"` for "git push").
- **Order is newest indexed first, by FTS rowid.** FTS5 walks rowid descending natively, so a page is bounded work (EXPLAIN QUERY PLAN has no temp b-tree; asserted in a test). Ranking by bm25 or recorded time would sort every match. A reindexed session's rows get new rowids and move up, which tracks recent activity.
- **Built only when web config is present.** Nothing else reads it yet. The MCP server (TKT-01M3FPWCFS) will need the same wiring.
- **Rebuild is "stop serve, delete the files, start".** This keeps the lake.lock rule without a new CLI command.

### Evidence
- TestIndexScale (non-race): 38,000 events, 50 MiB of text, indexed in 5.3 s with about 2 MiB of heap growth; the rare-token query took 0.4 ms and 200-hit pages about 5 ms.
- TestSearchShowsOnlyCurrentGenerations, TestIndexRecoversFromPartialWritesAndBadFiles, TestSearchFiltersAndPaging, TestSearchIsLiteralAndCaseInsensitive and TestIndexRebuildsUnknownVersions pass.
- cli TestStartWebIndexesPublishedSessions (upload → normalize → hook → index) and the extended purge test pass.
- `just ci` passes; race runs of recall, web, api and cli pass.

## Summary

Landed on branch t3code/explore-store-ui-search in commit 97eb9c3. recall.Index keeps search.db, a trigram FTS5 index, in step with the catalog's published generations. recall.Index.Search runs literal filtered queries with signed cursors and coverage. serve wires it only with web config. Operator docs: docs/web-dashboard.md#search-index. The browser search screen is TKT-01M3F2PGM. Not yet merged or deployed.

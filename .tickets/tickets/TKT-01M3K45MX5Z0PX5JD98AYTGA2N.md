---
schema: 3
id: TKT-01M3K45MX5Z0PX5JD98AYTGA2N
title: "Derived stores: compress parquet and shrink the search index"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
  - area/search
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/d8436f9f
  branch: normalize/zstd-events
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-d8436f9f
  commit: f78ca7b7f6cef29c22255d334798d05814021dc6
  session: null
  claimed_at: 2026-09-29T00:23:33Z
  expires_at: null
archive: null
created_at: 2026-09-28T04:25:34Z
updated_at: 2026-10-01T06:24:58Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

After the blob fixes, the derived stores dominate the lake: 541 MiB of
parquet and 486 MiB of search index, against 544 MiB of raw sessions.

- Parquet is written with `parquet.Write` defaults, which are
  uncompressed. Use a zstd codec.
- The search index keeps each event's text in `docs.content` and in a
  trigram FTS5 index. Measure the options, such as an external-content
  table over normalized JSONL, detail settings and column pruning,
  against search behaviour.
- Normalized JSONL: measure it, and compress it if it is large.

## Implementation plan

1. **Parquet with zstd:** done in #56.
2. **Normalized JSONL compressed** as `<uid>.jsonl.zst` (this PR):
   - Independent zstd frames of about 1 MiB of whole lines, with an index
     in a trailing skippable frame. Recall pages from the frame holding
     the first event.
   - Readers accept the old plain `.jsonl` until the next normalize
     rewrites the session.
3. **Search index:** measure docs.content against the trigram FTS5 index,
   and try the options (contentless FTS5 with snippets read from the
   events file, detail settings) against search behaviour. A separate
   PR.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T05:27:30Z

### Measured

On this workstation's six lampi Claude Code sessions (54.5 MiB raw),
normalized with the Claude normalizer:

| store | size | against raw |
|---|---|---|
| normalized JSONL | 66.0 MiB | 1.21× |
| parquet as written today (uncompressed) | 75.4 MiB | 1.38× |
| parquet with zstd | 16.2 MiB | 0.30× |

The first PR on this ticket sets the parquet codec. Normalized JSONL is
larger than the raw sessions and is the next target. The web viewer,
the search indexer and export all read it, so compressing it touches
every one of those readers. The search index is measured separately.

The parquet codec landed in #56 and was reviewed clean on v0.5.0.
The recall reader pages normalized JSONL with byte-offset cursors
(`recall/events.go`, `cursor.Off`), so compressing that file needs a
seekable framing or a cursor change.

**agent:claude-code/e4a47e8c** at 2026-09-28T05:54:50Z

`serve normalize --all` queues every session, so the parquet written
uncompressed before #56 can be rewritten with zstd. Running it on the
hosted lake is the owner's call. The worker pool is two, and a viewer
on a session being replaced is asked to reload.

**agent:claude-code/d8436f9f** at 2026-09-29T00:23:33Z

### Normalized JSONL compressed: built

**Format.** `normalize.WriteEventsFile` writes JSONL as independent zstd
frames at the better level, each closed at the first line that takes it
past 1 MiB raw. A skippable frame (magic 0x184D2A5E) follows. It holds
one (compressed offset, first line) entry per frame, the count, and
`lpix`, so a reader finds it from the file's last 8 bytes. Any zstd
decoder yields the JSONL and skips the index.

**Readers.**
- `normalize.OpenEvents` prefers `.jsonl.zst` and falls back to a plain
  `.jsonl`.
- `EventsFile.From(pos, off)` seeks to the frame holding pos. On a plain
  file it seeks to a signed cursor's byte offset, as before.
- Recall's `snapshot.lines` wraps both. Events, Excerpt and the indexer
  use it, and export reads through `ReadEventsFile`.
- A cursor on a compressed file pages by position, and its `o` field is
  0. An index that does not read leaves the file readable from its
  start.

**Writes.** `WriteFile(dir, uid, events)` removes the plain file after
the rename. `removeDerived` removes both forms.

**Measured** on this workstation's eight largest lampi Claude Code
sessions (91.4 MiB raw):

| form | size | against raw |
|---|---|---|
| normalized JSONL, plain | 124.3 MiB | 1.36× |
| framed `.jsonl.zst` | 18.5 MiB | 0.20× |
| one frame per file | 16.3 MiB | 0.18× |

The 1 MiB frames cost 13% against one frame per file, and bound a page
to decoding at most one frame before its first event.

### Alternatives

- **One frame per file, with cursors that count lines.** Rejected: each
  page would decode from the start of the file, O(n) per page.
- **Facebook's seekable zstd format.** Rejected: its seek table maps
  decompressed byte offsets, and paging needs line positions. Frames
  aligned to a fixed line count would avoid a table, but a frame of 256
  large events could be many MiB.
- **Keep the byte offset in the cursor as a compressed offset plus a
  line skip.** Rejected: the index already maps positions, so the cursor
  needs nothing more than the position it is signed with.

Existing plain files stay until their session is normalized again.
`serve normalize --all` rewrites every session, and running it on the
hosted lake is the owner's call, as the earlier note says.

**agent:claude-code/d8436f9f** at 2026-09-29T00:30:51Z

terva-review on #121, head 586672e, run 019176d3: clean, no findings at the failure threshold, and CI is green. The search-index part of this ticket is still to do: measure docs.content and the trigram index, then try contentless FTS5 with snippets read from the events file, and detail settings.

**agent:claude-code/fbqx** at 2026-09-29T02:15:16Z

Search-index part: the hosted index at 4.9 GB is mostly churn, not size. Untimed events were rewritten at every sync (TKT-01M3NENNN8, search index rewrites untimed events at every sync). A one-shot build of 329 local sessions (1.09 GB normalized) is 611 MiB, about 0.56x normalized: docs and their indexes take 236 MiB, and the trigram FTS 380 MiB. Shrinking beyond that is a separate question for after the rebuild lands.

**agent:claude-code/fbqx** at 2026-09-29T02:15:39Z

Owner decision 2026-09-29, for the hosted lake's upgrade to the release with zstd CAS and events:
1. Take a backup first. Older releases cannot read .zst objects or events files, so the backup is the way back.
2. Upgrade.
3. Run serve normalize --all as part of the upgrade, which rewrites every session's events file compressed.
4. With TKT-01M3NENNN8 in the release, search.db is rebuilt once on start (index version 4). That reclaims the 4.9 GB index.

**agent:claude-code/27b21f4b** at 2026-10-01T06:09:45Z

### Search index measured on the internal lake, 2026-10-01

Measured on a copy (`storage-v0.5.1-BKPCeV5d/measure.sh`): **516 MiB** of indexed text in 903,407 rows (445,210 with content). The text by event type:

| event_type | text |
|---|---|
| tool_result | 321 MiB |
| tool_call | 131 MiB |
| message | 47 MiB |
| compaction | 12 MiB |

The index took 2958 MiB on disk: `fts_data` 2102 MiB, `docs` 751 MiB, the rest in indexes.

| layout | fts_data | build time |
|---|---|---|
| trigram, detail=full (now), as found | 2102 MiB | |
| trigram, detail=full, after a full `optimize` | **1263 MiB** | 52 s |
| trigram, **detail=column**, rebuilt and optimized | **581 MiB** | 109 s rebuild, 11 s optimize |

`detail=column` would cut the optimized trigram index by more than half. Whether substring search, phrase queries and snippets still behave was **not** tested: with less than full detail, FTS5 has no term positions, so a substring longer than three characters may have to be checked against the row's content. Measure that before choosing it.

**Dead rows:** deleted rows left in unmerged segments are their own ticket, TKT-01M3V1AJJ5C69T9R3CRQQ06PB6 (Search index: reclaim deleted FTS5 rows with a scheduled optimize).

**WAL:** the 1.5 GiB write-ahead log is TKT-01M3V1AJGSZB7C2C52JCA2SF88 (search.db WAL is never capped or truncated).

**agent:claude-code/27b21f4b** at 2026-10-01T06:24:58Z

Correction to the previous note: the dead-rows ticket is TKT-01M3NPFNMH (Search index: FTS segments hold 0.5 GiB over live size after rebuild), and the WAL ticket is TKT-01M3NPFNJA (Search index WAL keeps its peak size). TKT-01M3V1AJJ5 and TKT-01M3V1AJGS, which that note named, are archived as their duplicates.

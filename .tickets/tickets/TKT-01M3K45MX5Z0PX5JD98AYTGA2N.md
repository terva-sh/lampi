---
schema: 3
id: TKT-01M3K45MX5Z0PX5JD98AYTGA2N
title: "Derived stores: compress parquet and shrink the search index"
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-28T04:25:34Z
updated_at: 2026-09-28T05:27:30Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
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

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
updated_at: 2026-09-28T04:25:34Z
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

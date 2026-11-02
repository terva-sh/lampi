---
schema: 3
id: TKT-01M6EMJKV6ZR1BYAJ8JA3QPZY1
title: "Spike: search by LIKE so the trigram index can use detail=column"
type: spike
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3K45MX5Z0PX5JD98AYTGA2N
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T17:23:09Z
updated_at: 2026-11-02T17:23:09Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

The search index's trigram FTS5 table takes about twice the space at `detail=full` that it would at `detail=column`: on the internal lake on 2026-10-01, 1263 MiB of `fts_data` optimized at `detail=full`, against 581 MiB at `detail=column` (TKT-01M3K45MX5Z0PX5JD98AYTGA2N, note 7).

`detail=column` cannot be dropped in. FTS5 then refuses MATCH queries longer than three characters, phrases, AND and NEAR, and lampi searches with `fts MATCH ?`. `content LIKE '%…%'` still uses the trigram index under `detail=column` and returns correct results (tested 2026-11-02 with SQLite 3.46.1).

### Question

Can recall's query language (terms, quoted phrases, exclusions, harness and project filters) be expressed as LIKE patterns against a `detail=column` trigram table, and how does query time compare with today's MATCH on a copy of a real index? lampi computes snippets in Go, so FTS5's `snippet()` is not needed.

Answer it on an index copy. Record the space saved, query times for short, long and multi-term queries, and anything that cannot be expressed. Do not change the live index.

## Acceptance criteria

- [ ] Query time and index size are measured on a copy for both layouts
- [ ] Every recall query form is shown to work as LIKE, or listed as not

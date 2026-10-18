---
schema: 3
id: TKT-01M56M4CEXFZ0T0JME4959T7G1
title: "Search: prove unchanged prefixes before skipping event decoding"
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-18T04:25:46Z
updated_at: 2026-10-18T04:25:46Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Follow-up to TKT-01M56GVA966MW31426MC7552PC (CPU: evaluate incremental projection and search prefix reuse); see docs/incremental-projection.md. Search already retains unchanged SQL rows by searchable-field signature, yet each changed generation decodes every normalized event and loads every old row signature. Do not skip a prefix based on event count, file size, event IDs or a matching tail: the projector can rewrite earlier text or metadata while preserving them.

Prototype per-frame searchable-field fingerprints and a durable generation-bound prefix proof. Signatures must cover exactly every field docRow.sig indexes, normalizer/search version, harness/project metadata and ordering. Only a proved unchanged prefix may bypass event decoding and old row-signature loading. A missing/invalid proof falls back to the existing complete pass. Keep FTS row IDs and stored search fields unchanged, recheck publication before committing the index transaction, and invalidate proof on rewrite, truncate, sidecar reorder, metadata change and index-version changes. Crash/restart and superseded publication must leave the old searchable generation or a complete new one.

Target at least 50% lower search-pass CPU for a 64 KiB append to a 32 MiB synthetic transcript, with identical query results/highlights/pagination and no increased index growth across repeated appends. Measure three fresh processes and include a rewrite control. This follow-up is outside the memory epic's evaluated release scope.

## Acceptance criteria

- [ ] A durable prefix proof covers all searchable fields, ordering, metadata and semantic versions; absence safely falls back.
- [ ] Append/rewrite/truncate/sidecar/metadata/crash/supersession cases preserve query results and bounded index growth.
- [ ] Three-process append measurements meet or explain the 50 percent search CPU target against the current complete pass.

---
schema: 3
id: TKT-01M61MZMTSK36M0TB5ZZFF66RA
title: "Search index: store content size, call id and model id per event"
type: task
status: draft
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
updated_at: 2026-10-28T16:20:09Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
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

- [ ] docs rows carry content_bytes (size before truncation), call_id and model_id.
- [ ] The three columns land in one change so the signature rewrite happens once.
- [ ] The one-time rewrite time and WAL growth on the internal lake are recorded.

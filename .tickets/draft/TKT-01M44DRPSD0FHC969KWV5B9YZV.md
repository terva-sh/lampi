---
schema: 3
id: TKT-01M44DRPSD0FHC969KWV5B9YZV
title: "Cursor CLI: stream the export so large stores need not be capped"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - area/agent
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-04T21:40:21Z
updated_at: 2026-10-04T21:40:21Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

The Cursor CLI reader builds each export whole in memory, the upload reads it whole again to hash, scan and split it, and `splitAt` copies each chunk. A real 359 MiB store made a 467 MB export with peak RSS near 2.6 GB. TKT-01M44B45JRVQ1W2XF5H0K3MC4X (Cursor CLI: read ACP sessions under acp-sessions/) therefore skips a store over 256 MiB. On the workstation where this was found, that leaves the 359 MiB, 744 MiB and 2.5 GiB ACP sessions on the machine.

TKT-01M44B45MT89CGR6M0HHTWG63P (Cursor CLI: upload store.db blobs as content-addressed objects) cuts the export between blob rows and sends only changed chunks, so the upload and lake storage no longer scale with the session. Memory still does. Writing the export to its temp file row by row, scanning and hashing it as it is written, and reading chunks from the file at upload time would bound memory by the largest chunk. The cap could then go.

## Acceptance criteria

- [ ] Exporting and uploading a Cursor CLI store holds at most a few chunks in memory
- [ ] The 256 MiB store cap is removed

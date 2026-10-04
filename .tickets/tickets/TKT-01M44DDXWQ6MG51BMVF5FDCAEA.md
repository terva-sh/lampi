---
schema: 3
id: TKT-01M44DDXWQ6MG51BMVF5FDCAEA
title: Cursor CLI normalize searches the whole export once per row
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/normalize
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/580cbe08
  branch: cursor/cli-normalize-offsets
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-580cbe08
  commit: f37526edb5a4698048d495d8cecaa766630e23fe
  session: null
  claimed_at: 2026-10-04T21:34:28Z
  expires_at: null
archive: null
created_at: 2026-10-04T21:34:28Z
updated_at: 2026-10-04T21:34:28Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

`normalize.CursorCLI` gives each meta row and blob an offset for its `content_ref` with `cursorOffset`, which runs `bytes.Index` over the whole export from the start for every row. The work is rows times document size. A real ACP export of 284 MB with about 20,000 blobs had not finished normalizing after 9 minutes; the lake's normalize worker sat in `cursorOffset` the whole time.

Before ACP sessions were read, Cursor CLI exports were small and this did not show. With TKT-01M44B45JRVQ1W2XF5H0K3MC4X (Cursor CLI: read ACP sessions under acp-sessions/), exports of hundreds of megabytes reach the lake, and the worker would spend its time on one session.

`cursorOffset` also matches the bare quoted key, so a row whose id an earlier row names as a field value got that earlier position.

The Cursor IDE projector in `internal/normalize/cursor.go` calls `cursorOffset` the same way. This ticket covers the CLI projector.

## Acceptance criteria

- [x] Normalizing a Cursor CLI export reads it about once, not once per row
- [x] A row's content_ref offset points at that row, not at an earlier row that names its id
- [x] A test fails if the offset search goes back to searching from the start

## Implementation plan

`CursorCLI.Normalize` keeps one `offsets` cursor over the export. Each row is found by its own field, `"key":"<key>"` for meta and `"id":"<id>"` for a blob, starting where the previous row was found. The offset still points at the quoted key, as before. A row that is not found that way, in a document written in another order or spacing, falls back to `cursorOffset` and does not move the cursor.

Alternatives considered:

- Record each row's offset while parsing the document with a streaming decoder. Lost: it means replacing `json.Unmarshal` of the whole export with a token walk for one field; the forward search costs one pass and changes nothing else.
- Keep searching for the bare quoted key, only forward. Lost: inside the previous row's data a field value equal to the next id matched first, which a test showed.
- Fix the IDE projector in the same change. Left out: its exports are per workspace and did not show the problem; noted on the ticket.

## Notes

**agent:claude-code/580cbe08** at 2026-10-04T21:34:28Z

Measured on a copy of a real 222 MB ACP store (exported as 284,062,434 bytes): normalize took 1.8 s and produced 13,451 events with the fix. Before it, the lake's worker was still in cursorOffset when the 9-minute test timeout fired. The new test fails when the forward search is disabled. The IDE projector (internal/normalize/cursor.go) still calls cursorOffset per row; not changed here.

---
schema: 3
id: TKT-01M3NQ2RT5G1TEY54R9R8RXG2G
title: "Normalize: a torn line mid-file fails the whole session"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T04:34:31Z
updated_at: 2026-09-29T04:34:31Z
created_by:
  id: agent:claude-code/16ebd168
  name: ""
updated_by:
  id: agent:claude-code/16ebd168
  name: ""
extensions: {}
---

## Description

Two sessions on the internal lake fail to normalize with `normalize: line N is not a JSON object` (seen 2026-09-29 after the v0.3.0 deploy):

- `01M3NK7HRF1KNKVJ0QPS5WG726`, line 139, first seen 03:27Z. The file is not on the workstation, so it came from another device.
- `01M3NNF6B0VSE95KK0V8YX6758`, line 691, first seen 04:06Z. It is a Claude Code transcript in a `terva-sh/tuohi` worktree, uploaded when the workstation's allow rules widened (TKT-01M3NMHDWR).

### Cause

The file is torn, and lampi did not tear it. Line 691 of the tuohi transcript is a `queue-operation` record cut off mid-string, with the next record (`{"type":…`) written on the same line and no newline between them. The lines before and after it parse. Claude Code wrote it that way at 2026-09-28 19:31Z.

### Why the whole session fails

TKT-01M38RJT92 (Claude Code normalize projector) specified that "a line that is not a JSON object fails the blob". The Codex projector does the same. The capture adapters already skip such lines when reading identity (`internal/adapter/claude/claude.go` readIdentity). The raw bytes are kept intact in the CAS, so nothing is lost. But one torn line leaves the whole session with no events, search entries or transcript view.

### Proposal

A line in the middle of a file that is not a JSON object becomes a marker event, and the session keeps going. The marker carries the line number and byte offset, but not the line's content, keeping the rule that the error text does not include the line. The session could be flagged as normalized with warnings. A torn *last* line in a file still being written is a different case, and waiting for the next sync may be right there.

Decide first whether the strict rule still serves a purpose. It may exist to catch a projector bug early rather than paper over it, and a marker event would need to keep that visible.

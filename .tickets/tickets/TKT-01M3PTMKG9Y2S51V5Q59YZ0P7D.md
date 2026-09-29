---
schema: 3
id: TKT-01M3PTMKG9Y2S51V5Q59YZ0P7D
title: "Conflicts: record resolutions, resolve TKT-01M3M5VEQ leftovers"
type: bug
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M3PTMA4C1XD80THS0AXEKK1Y
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T14:55:56Z
updated_at: 2026-09-29T14:56:06Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

All 184 `divergent_copy` rows on the dev lake are subagent transcripts
at `<session>/subagents/agent-*.jsonl`, written between 2026-09-26
18:58 and 2026-09-28 15:08 UTC, before TKT-01M3M5VEQ's fix. That bug
related a new subagent file with no current row at its path to the
session's own transcript, as if the transcript had moved. Every growth
of the file was stored again as a divergent copy. Migration 12 made the
newest copy current but left every row labelled `divergent_copy`, so the
Conflicts page and the overview count show 184 conflicts that are not.

### Approach

- A `conflict_resolutions` table keyed by artifact id: resolution,
  resolved_at, resolved_by, and a note. `relation` is not changed.
- Resolutions: `kept_head` (an operator kept the head),
  `made_head` (an operator made this copy the head), `superseded` (a
  later copy that extends this one was made the head) and
  `not_a_conflict` (the lake compared the copy with another file).
- Catalog calls to resolve and reopen, each queueing an audit event in
  the same transaction.
- A migration resolves the leftovers as `not_a_conflict`. A row
  qualifies when it is a companion of its session's head (under the
  directory named for the head's file) and no earlier artifact at its
  own session and relpath is anything but `divergent_copy`: it had
  nothing at its own path to diverge from, so it was compared with
  another file. A real divergence of a subagent file after the fix has
  an earlier head or grown_from row at its path and is left alone.
- The Conflicts page, the session's Conflicts tab, the overview count,
  `GET /v1/conflicts`, `GET /api/web/v1/conflicts` and
  `terva-lampi conflicts` show unresolved conflicts. Each takes a way
  to include resolved ones, and a resolved row names its resolution.

## Acceptance criteria

- [ ] Resolving and reopening a conflict each queue an audit event in the same transaction
- [ ] A migration resolves as not_a_conflict every divergent copy that is a companion of its session head with no earlier non-divergent row at its path
- [ ] A real divergence of a companion file, after an earlier head or grown_from row at its path, stays unresolved
- [ ] The Conflicts page, session Conflicts tab, overview count, /v1/conflicts, /api/web/v1/conflicts and terva-lampi conflicts show unresolved conflicts, and each can include resolved ones

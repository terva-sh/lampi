---
schema: 3
id: TKT-01M3NNF27AS0NCTG7N8XFDMWMK
title: "Bays: scope every read path by the caller's bays"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies:
  - TKT-01M3NNF24AXMQ130VWR71QHQZ3
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T04:06:17Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Scope every read path by the caller's read bays, before any session can land in a bay other than `default`. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

### Scope

- Recall, search, excerpts, transcripts, activity, overview counts and device views in `internal/recall` and `internal/web` filter by the caller's read bays, joining against catalog membership. An admin reads everything.
- search.db may carry each session's bay set so FTS queries filter without a join. It is refreshed on a membership change without re-projecting the session.
- `terva-lampi export` on the lake host is admin-level and gains `--bay` (repeatable). The training export stays gated by the `projects` allowlist and also by `--bay`.
- A test lists every catalog query that returns session data and fails when one takes no bay scope. It must fail when a new unscoped query is added, not only check the ones known today.
- A single choke point (a query builder that requires a scope) was considered and not chosen, because it needs `internal/recall` refactored first. If that refactor turns out small, prefer it and say so in a note.

## Acceptance criteria

- [ ] A viewer granted one bay sees no session outside it in any dashboard, recall or search response
- [ ] A test fails when a query returning session data takes no bay scope, including one added later
- [ ] export --bay narrows events and training exports

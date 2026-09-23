---
schema: 3
id: TKT-01M3558E1DAWTFQF88M9J6NJR6
title: Conflict dashboard for divergent_copy
type: task
status: in-progress
status_reason: null
priority: low
due_on: null
labels:
  - area/catalog
  - phase/3-export
assignees: []
milestone: phase-3
parent: TKT-01M3558DZ6CBW2HB49WFFY20CP
origin: null
dependencies:
  - TKT-01M3558DHP800VABHKKFYPD4W9
blocks_on: none
references: []
claim:
  actor: agent:cursor/f860
  branch: cursor/divergent-copy-conflicts-f860
  worktree: /workspace
  commit: 7b275a1883fb3751bc357bed8fb3356b7ac8917e
  session: f860
  claimed_at: 2026-09-23T20:15:42Z
  expires_at: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T20:15:48Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/f860
  name: Cursor cloud agent
extensions: {}
---

## Description

Operator-visible listing of divergent_copy relations from the catalog.

## Acceptance criteria

- [ ] An operator can list divergent_copy relations from the catalog
- [ ] Each listed relation names its session_uid and the divergent artifact

## Implementation plan

List divergent_copy rows that Layer B already stored. Do not re-derive them from CAS bytes, and do not add a resolve protocol.

### Catalog

Catalog.DivergentCopies reads artifacts where relation is divergent_copy, joined to the session. Each row carries session_uid, artifact id, harness, native session id, kind, relpath, the divergent digest and size, the head digest that stayed and its size, and the provenance machine ids for each digest.

### Operator surfaces

GET /v1/conflicts returns that list as JSON. It uses the same device-token check as the other /v1 routes. An empty catalog returns conflicts: [].

terva-lampi conflicts prints the same rows. With no --server it reads catalog.db in the lake directory and does not create that file when it is missing. --server URL calls GET /v1/conflicts with the device token from --token-file. Passing both --data and --server is an error.

### Left out

No merge, pick, or delete. An operator inspects the row and decides elsewhere.

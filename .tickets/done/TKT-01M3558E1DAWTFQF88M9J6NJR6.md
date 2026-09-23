---
schema: 3
id: TKT-01M3558E1DAWTFQF88M9J6NJR6
title: Conflict dashboard for divergent_copy
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T20:22:05Z
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

- [x] An operator can list divergent_copy relations from the catalog
- [x] Each listed relation names its session_uid and the divergent artifact

## Implementation plan

List divergent_copy rows that Layer B already stored. Do not re-derive them from CAS bytes, and do not add a resolve protocol.

### Catalog

Catalog.DivergentCopies reads artifacts where relation is divergent_copy, joined to the session. Each row carries session_uid, artifact id, harness, native session id, kind, relpath, the divergent digest and size, the head digest that stayed and its size, and the provenance machine ids for each digest.

### Operator surfaces

GET /v1/conflicts returns that list as JSON. It uses the same device-token check as the other /v1 routes. An empty catalog returns conflicts: [].

terva-lampi conflicts prints the same rows. With no --server it reads catalog.db in the lake directory and does not create that file when it is missing. --server URL calls GET /v1/conflicts with the device token from --token-file. Passing both --data and --server is an error.

### Left out

No merge, pick, or delete. An operator inspects the row and decides elsewhere.

## Summary

Operators list divergent_copy rows from the catalog. Catalog.DivergentCopies reads the stored artifacts and provenance. It does not re-derive the relation from CAS bytes.

terva-lampi conflicts prints the list from the lake directory. A missing catalog.db is an empty list and is not created. --server calls GET /v1/conflicts with the device token. Passing both --data and --server is an error.

GET /v1/conflicts returns the same rows and uses the bearer check as the other /v1 routes. Each row names session_uid, the divergent artifact_id, harness, native session id, kind, relpath, both digests and sizes, and the machines that posted each digest.

Interactive resolve is not implemented. The list does not merge copies or move the head.

make ci is green. Landed on cursor/divergent-copy-conflicts-f860 as https://github.com/terva-sh/lampi/pull/24.

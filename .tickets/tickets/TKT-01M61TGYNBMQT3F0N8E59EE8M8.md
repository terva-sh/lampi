---
schema: 3
id: TKT-01M61TGYNBMQT3F0N8E59EE8M8
title: "Cursor CLI adapter: export blob rowid to order compacted branches"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T17:56:59Z
updated_at: 2026-10-29T00:37:28Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

The Cursor CLI normalizer orders blobs from the lists in the protobuf root blobs (internal/normalize/cursorcliorder.go). After two or more compactions, older branches may share only the messages both kept, and no root says which came first. A probe of local stores found no field that orders roots: field 26 holds one value per store and field 8 never names another root. Since review 2902 on #220, those events carry `extra.order = "unestablished"` instead of a guessed order.

The adapter reads `SELECT id, data FROM blobs ORDER BY id` (internal/adapter/cursorcli/snapshot.go). The `blobs` table is a rowid table, and Cursor writes a new root each time the conversation changes, so root rowid order is likely chronological. Exporting each blob's rowid (for example as `seq`) would let the normalizer order the older branches by their roots' rowids.

### Costs to weigh

- **Re-upload.** The artifact bytes change for every Cursor CLI store, so each one uploads again once. The reader version pin moves too.
- **Older agents.** Agents that do not send `seq` keep today's behaviour, so the normalizer must accept both.
- **An unverified assumption.** Check against real compacted stores that root rowid order matches the growth of the roots' lists before relying on it.

## Acceptance criteria

- [ ] Root rowid order is checked against real compacted stores and found chronological, or the ticket records that it is not.
- [ ] The normalizer orders older branches by root rowid when the export carries it, and keeps extra.order unestablished when it does not.

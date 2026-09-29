---
schema: 3
id: TKT-01M3PTMA4C1XD80THS0AXEKK1Y
title: "Conflicts: explain them and let an operator settle them"
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T14:55:46Z
updated_at: 2026-09-29T14:55:46Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

The Conflicts page lists every artifact stored as `divergent_copy` and
offers nothing to do about one. On the dev lake all 184 rows are
leftovers of TKT-01M3M5VEQ (a subagent transcript compared with its
session's own transcript), so a real fork would be buried, and a person
looking at the page cannot tell what it asks of them.

This epic makes the page mean something and lets a signed-in operator
settle a conflict from the dashboard, without a lake-host command.

### What a conflict is

A post whose bytes neither extend nor are extended by the copy the lake
compared them with. The lake keeps both, leaves the head where it was,
and merges nothing. A real one happens when a harness rewrites a
session file or two machines write the same session differently. When
the machine keeps appending to its rewritten file, every post is
another full divergent copy and the session's head stops moving: the
session looks frozen in the lake.

### Decisions

- **Resolve, do not relabel.** `relation` stays the truth about how the
  bytes compared; a separate resolution records what a person or the
  lake decided. Relabelling a leftover as `grown_from` would claim a
  prefix relation nobody checked, and `compact` and `purge` trust that
  relation.
- **Nothing is deleted.** Every resolution keeps the bytes. Reclaiming
  storage stays with `serve compact` and `serve purge`.
- **Operator, not admin.** Settling a conflict changes which bytes a
  session shows, the same weight as the device actions an operator
  already takes. Reading the bytes stays admin-only (Raw tab).
- **Audited and reversible.** Each resolution writes an audit event in
  the same transaction; a resolution can be reopened.

### Tickets

1. Resolutions in the catalog, and the leftovers of TKT-01M3M5VEQ
   resolved by a migration.
2. The Conflicts page explains what a row means and what to check.
3. An operator keeps the head or reopens a conflict, from a conflict
   page that shows where the copies part.
4. An operator makes a divergent copy the session's head.

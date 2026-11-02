---
schema: 3
id: TKT-01M6F5K429GX4TZFJB35TV3KC7
title: "MCP and CLI: filter recall and analysis by selected bays"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies:
  - TKT-01M6F56Z6QP6C2P3M1T7J923BV
  - TKT-01M6F5K3BTS8X9BTFZTECWYDCR
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:20:32Z
updated_at: 2026-11-02T22:57:43Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

A token may read several bays, but current MCP recall has no argument to focus on one of them. Add a consistent way to select one or more bays in remote CLI recall/event reads, MCP search, tool statistics and diagnosis, using the interaction contract from the usage spike. Preserve existing behavior when the selector is omitted.

The selected bays intersect the caller's current scope and any other filters. Resolve names, IDs and supported aliases through authorized lookup. Specify whether several bays mean any or all and how a session in several selected bays is deduplicated. Single-session event/excerpt/diagnosis reads must enforce an explicitly selected bay when that context is supplied. Return understandable errors without revealing inaccessible names. Bind the selector and its semantics into cursors and re-check current membership as scope changes.

Filtering must happen before counts, aggregates, examples and diagnostics are computed; filtering rendered results afterwards leaks data and produces incorrect statistics. Distinguish no matches from index lag or bounded/truncated work. Add focused scope, multiple-membership, paging and regression tests and update docs/reading-the-lake.md, docs/cli.md and docs/web-api.md. This is read selection only; granting access or modifying membership stays outside it.

## Acceptance criteria

- [ ] Remote CLI and MCP recall/analysis accept documented bay selectors, with explicit multi-bay semantics and backward-compatible omitted selectors.
- [ ] All selected results and aggregates intersect current scope before computation and deduplicate shared sessions as specified.
- [ ] Cursors, inaccessible/unknown bays, membership changes and single-session context checks have meaningful tests.
- [ ] Tool descriptions, CLI help and API examples explain selectors, scope and incomplete coverage.

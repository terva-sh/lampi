---
schema: 3
id: TKT-01M3558D8WN5HTVPM4KRQQCSHP
title: Define project allowlist for off-box raw
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/redact
  - phase/0-policy
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558D72YST7VYN39EFK272P
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T18:15:12Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Config surface (cwd prefix, git remote, or terva CWDHash) gating which projects may upload raw. Default deny outside allowlist. Unblocks redaction upload path.

## Acceptance criteria

- [ ] Allowlist/denylist in agent config
- [ ] Sync refuses non-allowlisted raw with clear error
- [ ] Documented in README/docs

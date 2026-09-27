---
schema: 3
id: TKT-01M3HKYFF5HJ5BD2V1KANNK301
title: Report which projects the allowlist refuses
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T14:22:47Z
updated_at: 2026-09-27T14:22:47Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

`last_sync.json` reports how many files were refused but not which projects, so choosing allow rules means reconstructing cwds and remotes by hand. Add a report, for example `terva-lampi status --refused` or `sync --explain`, listing each refused project once: cwd, normalized remote when known, harness, file count, and the reason (no allow match, deny rule, empty cwd). It prints locally only and uploads nothing.

## Acceptance criteria

- [ ] One line per refused project with reason and file count
- [ ] Nothing leaves the machine

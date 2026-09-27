---
schema: 3
id: TKT-01M3HKYFE1ZHBSSHGV6V5XW4SJ
title: Allow and deny rules by git remote prefix
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - policy
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
updated_at: 2026-09-27T20:52:53Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

`git_remote` allow and deny rules match one repository exactly, and `cwd_prefix` rules name paths that differ per machine. A lake profile meant for several machines therefore has to list every repository, and a new repository is refused until someone adds it everywhere.

### Approach

Add a `git_remote_prefix` field to `ProjectMatch`, compared after `NormalizeRemote` on a path-segment boundary (host plus owner, e.g. everything under one organisation), for both allow and deny. Deny keeps its doubt-is-a-match reading: an unknown remote matches a deny prefix. Update `docs/policy.md`, the profile verifier's accepted fields, and `deploy/profiles.json.example`.

Alternatives: glob syntax inside `git_remote`. Rejected because it changes the meaning of existing rules and makes exact matches ambiguous.

## Acceptance criteria

- [ ] git_remote_prefix matches on a segment boundary after NormalizeRemote
- [ ] Deny prefix matches an unknown remote
- [ ] Lake profiles accept the field and the policy doc describes it

---
schema: 3
id: TKT-01M3HKYFE1ZHBSSHGV6V5XW4SJ
title: Allow and deny rules by git remote prefix
type: task
status: done
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
updated_at: 2026-09-27T21:11:10Z
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

- [x] git_remote_prefix matches on a segment boundary after NormalizeRemote
- [x] Deny prefix matches an unknown remote
- [x] Lake profiles accept the field and the policy doc describes it

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T21:11:10Z

### Implementation

- **The field:** `ProjectMatch.GitRemotePrefix` (`git_remote_prefix`), matched by `remoteHasPrefix` in internal/config/policy.go.
- **Folding:** both sides go through `NormalizeRemote`, which yields host plus path, with scheme, login, port and `.git` dropped and case folded. The match requires equality or a `prefix + "/"` boundary.
- **Refusals:**
  - A prefix that folds to nothing matches nothing.
  - A remote with an empty, `.` or `..` segment matches no prefix. That is defence in depth: the remote comes from the checkout's config and is trusted to the same degree as a `git_remote` rule.
- **Deny rules:** like `git_remote`, a deny prefix reads an unknown remote as a match.

### Compatibility

Profiles decode strictly, so a v0.1.1 lake refuses to load a `profiles.json` that uses the field. A v0.1.1 agent keeps its cached profile and logs the verify error, which fails safe. The docs say to upgrade the lake, then the agents.

### Rejected

Glob syntax inside `git_remote`: it would change the meaning of existing rules.

### Tests

- Allow cases: scp, ssh with port, https, case, nested group, boundary, other host, `..`, `//`, empty.
- Deny cases: under, equal, beside, unknown, no repository.
- JSON decode.
- A profile with allow and deny prefixes.
- `profiles.json.example` still loads.

## Summary

Added git_remote_prefix for allow and deny rules. It is folded by NormalizeRemote and matched on a / boundary, so one rule such as git@host:owner covers every repository of that owner on every machine. Deny prefixes treat an unknown remote as a match. Documented, including the upgrade order (lake first, then agents). Landed in the PR for this ticket.

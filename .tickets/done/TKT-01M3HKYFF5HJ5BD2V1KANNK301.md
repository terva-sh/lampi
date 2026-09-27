---
schema: 3
id: TKT-01M3HKYFF5HJ5BD2V1KANNK301
title: Report which projects the allowlist refuses
type: task
status: done
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
updated_at: 2026-09-27T21:21:50Z
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

- [x] One line per refused project with reason and file count
- [x] Nothing leaves the machine

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T21:21:49Z

### Implementation

- **`config.Projects.Refusal(id)`** returns why `Permitted` refuses. Deny is checked first, then allow, then: empty allow list, no cwd, or no match. A test checks that it agrees with `Permitted` in every case.
- **`upload.Refusals(opt)`** builds manifests with sync's own `bundlesFor`. It opens no outbox, watermarks or network, and the Cursor readers still skip snapshots for refused sessions. It groups by folded remote when there is one, otherwise by cwd.
- **`terva-lampi agent refused [--lake NAME]`** applies each lake's effective rules (config.json plus the cached profile). It prints a section per lake, or uses config.json rules when there is no lake. It uses a fixed placeholder machine id, so the report creates no machine file.

### Pivot

The first version grouped by cwd plus remote. On this workstation that gave 37 lines, 16 of them worktrees of one ledger repository. Allow rules name repositories, so it now groups by remote and shows the number of checkouts: 19 lines. That matches the ticket's "each refused project once".

### Evidence

On the owner's workstation: 79 refused sessions in 19 projects. It also showed a terva-sh repository (tuohi) that a `git_remote_prefix` for terva-sh would admit. Tests cover grouping, the reasons, no writes to the state directory, and the two-lake CLI output with `--lake`.

## Summary

Added terva-lampi agent refused [--lake NAME]. It lists each refused project once, grouped by repository when there is a remote, with session count, harnesses, reason (deny rule, empty allow list, no cwd, no matching allow rule), a cwd with a checkout count, and the remote. It reads sessions the way sync does, sends nothing and writes no sync state.

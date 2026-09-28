---
schema: 3
id: TKT-01M3KA702JPYHHZWAFA8BE5RK3
title: "serve normalize --status: local normalization view"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies:
  - TKT-01M3KA70114Q6WFTAAKN5NKG2M
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: ops/normalize-status-cli
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 5973a320865c2f381b090c1fc7eddc8515f154bd
  session: null
  claimed_at: 2026-09-28T06:23:21Z
  expires_at: null
archive: null
created_at: 2026-09-28T06:11:10Z
updated_at: 2026-09-28T06:23:21Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Add `serve normalize --status`. It reads the catalog directly, as the
service user, so it works with serve stopped, over SSH, and in scripts.
It prints:

- per-state session counts
- queued jobs and the oldest pending age
- the last failure's session and message

With `--json` it prints the same object /v1/stats carries, for scripts.

Follows TKT-01M3KA70114Q6WFTAAKN5NKG2M (the /v1/stats normalization object), whose shape it
reuses.

## Acceptance criteria

- [x] serve normalize --status prints counts, queue, oldest pending age and last failure without serve running
- [x] --json prints the /v1/stats normalization object

## Implementation plan

`api.CatalogNormalization(ctx, cat, now)` holds the catalog half of the
status: sessions by state, plus the job table's size and oldest age.
`Server.NormalizationStatus` builds on it, so the CLI and /v1/stats
share one shape.

`serve normalize --status` opens the catalog read-only and prints:

- sessions by state
- the jobs waiting and the oldest one's age
- each failed session with its `normalize_error`

It runs as the service user, who may read those messages, unlike a
device token.

`--json` prints the `NormalizationStats` object. Its queued, running
and retrying fields are 0, because they belong to a serve process and
this command does not ask one; the usage says so. `--status` refuses
selectors and `--dry-run`, and `--json` needs `--status`.

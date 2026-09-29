---
schema: 3
id: TKT-01M3NPFNP7G07N9TM75GM2D29M
title: "Upgrade docs: run serve compact to compress blobs stored raw"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/cas
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/65ab7244
  branch: search/wal-limit
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-123283bc
  commit: 93998cb107dfcd01c021cf76b22b1a0081fefb09
  session: null
  claimed_at: 2026-09-29T04:24:12Z
  expires_at: null
archive: null
created_at: 2026-09-29T04:24:06Z
updated_at: 2026-09-29T04:24:12Z
created_by:
  id: agent:claude-code/65ab7244
  name: ""
updated_by:
  id: agent:claude-code/65ab7244
  name: ""
extensions: {}
---

## Description

The dev lake upgraded to v0.3.0 (zstd at rest), but its operations page shows compression against raw at 1.09-1.14×: 1.4 GiB of current raw sessions in 1.2 GiB of blobs. The epic measured 5.4× on these transcripts.

Since the upgrade, every new object is stored compressed. Objects stored raw before it stay raw until `serve compact` rewrites them as zstd frames (TKT-01M3K45MVD, #119). The upgrade steps in `docs/vps-bringup.md` and `docs/container.md` say to take a backup before the upgrade, and do not say to run `serve compact` afterwards. So a lake that follows them keeps its old objects raw.

Add the step to both documents: after the upgrade, stop serve, run `serve compact --dry-run`, then `serve compact`, then start serve.

## Acceptance criteria

- [ ] vps-bringup.md and container.md say to run serve compact after upgrading to zstd at rest

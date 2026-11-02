---
schema: 3
id: TKT-01M3NPFNP7G07N9TM75GM2D29M
title: "Upgrade docs: run serve compact to compress blobs stored raw"
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-29T04:24:06Z
updated_at: 2026-11-02T16:58:44Z
created_by:
  id: agent:claude-code/65ab7244
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

The dev lake upgraded to v0.3.0 (zstd at rest), but its operations page shows compression against raw at 1.09-1.14×: 1.4 GiB of current raw sessions in 1.2 GiB of blobs. The epic measured 5.4× on these transcripts.

Since the upgrade, every new object is stored compressed. Objects stored raw before it stay raw until `serve compact` rewrites them as zstd frames (TKT-01M3K45MVD, #119). The upgrade steps in `docs/vps-bringup.md` and `docs/container.md` say to take a backup before the upgrade, and do not say to run `serve compact` afterwards. So a lake that follows them keeps its old objects raw.

Add the step to both documents: after the upgrade, stop serve, run `serve compact --dry-run`, then `serve compact`, then start serve.

## Acceptance criteria

- [x] vps-bringup.md and container.md say to run serve compact after upgrading to zstd at rest

## Notes

**agent:claude-code/d4b24133** at 2026-11-02T16:46:35Z

2026-11-02: claim taken over by agent:claude-code/d4b24133 at the owner's request. The claim's branch search/wal-limit is merged into main. The docs change is open as PR #138 from docs/compact-after-upgrade, untouched since 2026-09-28. No local worktree holds this ticket's branch.

**agent:claude-code/d4b24133** at 2026-11-02T16:46:35Z

claim taken from agent:claude-code/65ab7244 by agent:claude-code/d4b24133

## Summary

The upgrade sections of docs/container.md and docs/vps-bringup.md now say that an upgrade to zstd at rest compresses only new objects, and that serve compact rewrites the old ones once a rollback is no longer needed, linking each guide's own compact procedure. The text is PR #138's, applied to current main because that branch was 671 commits behind; #138 is closed as superseded.

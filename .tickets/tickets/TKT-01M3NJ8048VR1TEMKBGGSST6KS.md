---
schema: 3
id: TKT-01M3NJ8048VR1TEMKBGGSST6KS
title: "Release v0.3.0: notes, tag, and the published archives and image"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/16ebd168
  branch: release/v0.3.0
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-16ebd168
  commit: a43c5ce9142557e3a943e0aa4a8455954cb3d256
  session: null
  claimed_at: 2026-09-29T03:10:04Z
  expires_at: null
archive: null
created_at: 2026-09-29T03:10:00Z
updated_at: 2026-09-29T03:10:04Z
created_by:
  id: agent:claude-code/16ebd168
  name: ""
updated_by:
  id: agent:claude-code/16ebd168
  name: ""
extensions: {}
---

## Description

Cut v0.3.0 from main at a43c5ce, 82 commits past v0.2.0. The owner asked on 2026-09-29 for a release with the recent improvements, then a local deploy (filed separately).

### What it carries, for the notes

- Catalog migration 15 → 16: project sightings and lake-wide hidden projects, for the project review queue.
- CAS objects and normalized events are written zstd-compressed (`.zst`). v0.2.0 and older cannot read either, so rollback means restoring a backup taken before the upgrade.
- The search index moves to version 4 and is rebuilt once at start. Untimed events are no longer rewritten at every sync (TKT-01M3NENNN8).
- New: encrypted backups (`serve backup --archive`, `serve restore`), `serve compact` compresses objects stored raw, the dashboard's project review queue, hide and unhide, and batch Allow.

### Order

1. Rehearse the upgrade on a scratch lake seeded by v0.2.0, with a build of a43c5ce: migrate 15 → 16, `serve normalize --all`, the search rebuild, and fsck.
2. Write the notes, recorded on this ticket.
3. Tag `v0.3.0` at a43c5ce, which is already on both mains, and push the tag to both forges. The pipeline was proven by the v0.2.0 rcs, and step 1 covers the upgrade, so there is no rc.
4. Check the published archives and the image's `--version`, then attach the notes to both releases.

## Acceptance criteria

- [ ] A scratch lake seeded by v0.2.0 upgraded with an a43c5ce build: schema 16, normalize --all, search rebuilt, fsck clean
- [ ] v0.3.0 is tagged on both forges and its archives and image name the tag
- [ ] Release notes state the 15 to 16 migration, the .zst format and its rollback, and lake-before-agents

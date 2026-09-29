---
schema: 3
id: TKT-01M3NJ805R4Q52Z583PENP53BM
title: Deploy v0.3.0 to the internal lake and workstation agent
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3NJ8048VR1TEMKBGGSST6KS
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

The owner asked on 2026-09-29 to deploy the new release locally. That request is this deploy's authorization, as TKT-01M3FP11A (Onboarding rollout: upgrade the hosted lake and register machines) requires for each deploy.

### Starting point

- The lake and the workstation agent run v0.2.0 (3f71211), catalog schema 15 (TKT-01M3N5R5).
- v0.3.0 adds migration 16 and writes CAS objects and events files as `.zst`, which v0.2.0 cannot read.

### Approach

It follows the owner's decision in TKT-01M3K45MX note 6 and the v0.2.0 bundle's pattern. The bundle goes in the external handoff directory, and the owner runs it as root:

1. Check every precondition before stopping anything.
2. Stop the lake and take a verified checkpoint with the installed v0.2.0's `serve backup` and `serve fsck`. This backup is the only way back.
3. Install v0.3.0 and let serve migrate 15 → 16. The search index rebuilds at start.
4. Queue `serve normalize --all` and SIGHUP serve, so every events file is rewritten compressed.
5. Check health, 401s, schema, integrity, counts, lake id and the public URL, then resume the agent.

The workstation agent is then upgraded in place. `serve compact`, which compresses the objects v0.2.0 stored raw, needs serve stopped and is not in the owner's decision, so it is left for the owner to schedule.

## Acceptance criteria

- [ ] A verified checkpoint of the stopped schema-15 lake exists, taken with v0.2.0
- [ ] The lake runs v0.3.0 at schema 16 with integrity ok and counts preserved
- [ ] Every session is normalized again and the search index is rebuilt
- [ ] Health, auth refusals and the public URL answer after the upgrade
- [ ] The workstation agent runs v0.3.0 and its next sync re-uploads nothing

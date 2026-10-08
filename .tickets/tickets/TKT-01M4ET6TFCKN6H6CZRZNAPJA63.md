---
schema: 3
id: TKT-01M4ET6TFCKN6H6CZRZNAPJA63
title: Diagnose failed Operations search-index compaction
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/search
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/t3code-8afe4a1e
  branch: fix/operations-compaction
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-8afe4a1e
  commit: 0616211805ac616729b3a376ec62a1093a5f5da1
  session: null
  claimed_at: 2026-10-08T22:30:47Z
  expires_at: null
archive: null
created_at: 2026-10-08T22:30:11Z
updated_at: 2026-10-08T22:30:47Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
extensions: {}
---

## Description

The dogfooding dashboard accepted Compact search index but finished with: Maintenance did not finish. Check the lake logs before retrying. Lake and capture remain active. Obtain the underlying maintenance log, reproduce the failure safely, and fix the demonstrated cause while preserving searchable data and access controls.

## Acceptance criteria

- [ ] The cause is identified from the maintenance error and reproduced safely.
- [ ] The fix handles the demonstrated failure and has regression coverage.
- [ ] Compaction succeeds on dogfooding with search and capture healthy.

## Implementation plan

Obtain only the filtered maintenance error from the protected system journal. Separate failures during FTS5 optimize, vacuum/checkpoint, and the post-job storage sample. Reproduce the observed cause using isolated data before changing compaction behavior. Preserve the original data, authenticate admin operations, and add regression coverage for the demonstrated failure. Land the fix through Forgejo CI and review, then prepare a backed-up rollout and verify the user-triggered operation.

## Notes

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:30:47Z

The user reports the asynchronous failure message, so the action was accepted and m.run returned an error. Both services remain active; the filesystem has 55 GiB free. Existing web and recall tests pass. Code inspection found no demonstrated cause yet. This account cannot read the system journal and sudo -n requires a password; a filtered root journal command was requested from the user. Do not infer corruption or retry destructive cleanup from the generic UI message.

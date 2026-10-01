---
schema: 3
id: TKT-01M3V52D6SRZG41ZSX4Y05KB01
title: Deploy v0.5.2 to the internal lake and workstation agent
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/27b21f4b
  branch: release/v0.5.2
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-27b21f4b
  commit: 1f2f84eca2ddba4e948354b959cfd89f84c04c93
  session: null
  claimed_at: 2026-10-01T07:16:47Z
  expires_at: null
archive: null
created_at: 2026-10-01T07:15:12Z
updated_at: 2026-10-01T07:16:47Z
created_by:
  id: agent:claude-code/27b21f4b
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-10-01 for a deploy bundle for v0.5.2 (TKT-01M3V3QR0Y, Release v0.5.2: search index and catalog WAL cap). That request is this ticket's promotion. The owner runs the lake step as root, and the agent then upgrades the workstation agent.

### What changes

The installed lake and agent run v0.5.1 (4ef0c50) at catalog schema 21. v0.5.2 adds no migration, and the unit and web drop-in match the v0.5.1 bundle's copies. Only the binary changes.

The point of the deploy is the search index WAL. On 2026-10-01 `search.db-wal` had regrown to 1.5 GiB under v0.5.1 (TKT-01M3K45MX). A clean stop of serve already deletes the WAL, so the stop is not the fix. What v0.5.2 changes is that the WAL is cut back after every index pass, and stays under 64 MiB between them.

## Acceptance criteria

- [ ] The lake runs v0.5.2 with schema 21, integrity ok and counts preserved
- [ ] search.db-wal is at most 64 MiB after the first index pass
- [ ] The workstation agent runs v0.5.2 and its next sync uploads nothing new

## Implementation plan

1. Bundle `~/.local/state/agent-handoffs/lampi/deploy-v0.5.2-cILnCBtq/` (outside the repository): the GitHub release's linux_amd64 binary checked against `release-checksums.txt`, `operator-deploy.sh`, and the README.
2. The owner runs `operator-deploy.sh` as root. It checks preconditions (versions, unit and drop-in, schema 21, the v0.5.1 role_map, space, lake id), prints `search.db-wal`'s size, pauses the agent, stops the lake, and backs it up with the installed v0.5.1. It then verifies the copy, installs v0.5.2, checks health, auth, the admin group, schema, counts, lake id and the public URL, reports the WAL a minute in, and resumes the agent with a forced sync.
3. The agent upgrades `~/.local/bin/terva-lampi` after keeping copies of the binary and config.json, restarts the agent service, and checks `agent config` and the next sync.
4. After hours of uploads the owner runs `sudo stat` on `search.db-wal`. At most 64 MiB satisfies criterion 2.

Alternatives considered:
- Reusing the v0.5.1 script unchanged. It would refuse, because its role_map precondition expects the pre-v0.5.1 map, and it would re-run the normalize retry with nothing to retry.
- Skipping the backup for a binary-only change. The backup is the same cost as v0.5.1's and covers a mistake in the install itself.
- Waiting for the WAL to shrink inside the script. A clean stop deletes the WAL, which was checked on the scratch lake, so it is small after any restart and the wait would prove nothing. The real check is step 4.

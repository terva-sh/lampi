---
schema: 3
id: TKT-01M3V52D6SRZG41ZSX4Y05KB01
title: Deploy v0.5.2 to the internal lake and workstation agent
type: task
status: done
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
claim: null
archive: null
created_at: 2026-10-01T07:15:12Z
updated_at: 2026-10-01T08:05:24Z
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

The point of the deploy is the search index WAL. On 2026-10-01 `search.db-wal` had regrown to 1.5 GiB under v0.5.1 (TKT-01M3K45MX). A clean stop of serve already deletes the WAL, so the stop is not the fix. What v0.5.2 changes is that each index pass ends with a truncating checkpoint, which cuts the WAL back to 0 unless a reader holds it busy. A busy checkpoint is retried at the next pass. The 64 MiB `journal_size_limit` applies only when the WAL resets. It does not bound growth during a pass, which reached 297 MiB on this lake (see the notes).

## Acceptance criteria

- [x] The lake runs v0.5.2 with schema 21, integrity ok and counts preserved
- [x] The workstation agent runs v0.5.2 and its next sync uploads nothing new
- [x] search.db-wal returns to 0 after an index pass whose truncating checkpoint no reader holds busy; a busy one is retried at the next pass

## Implementation plan

1. Bundle `~/.local/state/agent-handoffs/lampi/deploy-v0.5.2-cILnCBtq/` (outside the repository): the GitHub release's linux_amd64 binary checked against `release-checksums.txt`, `operator-deploy.sh`, and the README.
2. The owner runs `operator-deploy.sh` as root. It checks preconditions (versions, unit and drop-in, schema 21, the v0.5.1 role_map, space, lake id), prints `search.db-wal`'s size, pauses the agent, stops the lake, and backs it up with the installed v0.5.1. It then verifies the copy, installs v0.5.2, checks health, auth, the admin group, schema, counts, lake id and the public URL, reports the WAL a minute in, and resumes the agent with a forced sync.
3. The agent upgrades `~/.local/bin/terva-lampi` after keeping copies of the binary and config.json, restarts the agent service, and checks `agent config` and the next sync.
4. After hours of uploads the owner runs `sudo stat` on `search.db-wal`. At most 64 MiB satisfies criterion 2.

Alternatives considered:
- Reusing the v0.5.1 script unchanged. It would refuse, because its role_map precondition expects the pre-v0.5.1 map, and it would re-run the normalize retry with nothing to retry.
- Skipping the backup for a binary-only change. The backup is the same cost as v0.5.1's and covers a mistake in the install itself.
- Waiting for the WAL to shrink inside the script. A clean stop deletes the WAL, which was checked on the scratch lake, so it is small after any restart and the wait would prove nothing. The real check is step 4.

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T07:32:21Z

### Deployed, 2026-10-01

**Lake.** The owner ran `operator-deploy.sh`, and it ended `UPGRADE COMPLETE: terva-lampi v0.5.2 (360e46007ca2)`.
- **Checkpoint:** `/var/lib/terva-lampi-pre-v0.5.2-3gCC9co7`, verified with 692 sessions, 20391 artifacts and 20390 provenance rows at schema 21. The counts were preserved after the install, and integrity was ok.
- **Admin group:** serve logged `web admin groups groups="Brokkr Lampi Admin"`.
- **Lake id:** unchanged, `lake_u3cpc5lo4dwujlk5il3mpjepai`, and the public URL answered.
- **Search index:** under v0.5.1, `search.db` was 2879 MiB and `search.db-wal` 1617 MiB. A minute after the start the WAL was 0 MiB. Right after the script, the owner's `sudo stat` showed `search.db` at 3019833344 bytes and the WAL at 0.
- **Agent:** it resumed, still on v0.5.1, and synced with uploaded 0 and refused 1. The refusal is not from the deploy. It is `upload: refused off-box raw`, and every sync since at least 2026-09-30 18:00 CDT shows refused 1.

**Workstation agent.** The old binary and config.json are kept in `~/.local/state/agent-handoffs/lampi/agent-rollback-v0.5.2-P89B/`. The new binary prints `terva-lampi v0.5.2 (360e46007ca2)`, and the service restarted active.
- **Config:** `agent config` shows the default lake pinned (lake_u3cpc5lo4dwujlk5il3mpjepai) with `allow_source=lake:default` and profile version `sha256:eb5436a95cc6a4ca`.
- **Syncs:** the first after the restart uploaded 0, with 194 unchanged. The next uploaded 2 files that live sessions were still writing.

**Criterion 2 is open.** A WAL of 0 bytes right after a restart is expected under either version, because a clean stop deletes the WAL. It closes when the owner's `sudo stat` after hours of uploads shows `search.db-wal` at most 64 MiB.

**agent:claude-code/27b21f4b** at 2026-10-01T07:35:32Z

### WAL on the live lake after v0.5.2, 2026-10-01

The owner ran `sudo stat` on the lake twice after the deploy. serve started at about 07:30 UTC.

| when | search.db | search.db-wal |
|---|---|---|
| a minute after the start | 3019833344 | 0 |
| about 07:34 UTC, after the agent's first uploads | 3131387904 | 311397872 (297 MiB) |
| a few minutes later | 3118047232 | 0 |

The truncating checkpoint works on the live lake: the WAL went from 297 MiB back to 0 when the pass ended. That fixes the problem in this ticket's title. Under v0.5.1 the WAL stayed at 1617 MiB.

**Correction to the deploy bundle's README.** It said `search.db-wal` should stay at most 64 MiB. That was wrong. `journal_size_limit` only limits what is left after the WAL resets. It does not limit growth within a pass, and one pass here wrote about 300 MiB. Criterion 2 says "after the first index pass", and that holds. The transient peak is the size of one pass's writes, which belongs to TKT-01M3NPFNMH (Search index: FTS segments hold 0.5 GiB over live size after rebuild).

**agent:claude-code/27b21f4b** at 2026-10-01T08:05:24Z

Review 1685 (and 1683/1684) finding: the description claimed the WAL stays under 64 MiB between passes, and criterion 2 required at most 64 MiB with no allowance for a busy checkpoint. Both were wrong: journal_size_limit applies at a reset only, and a reader can hold the truncate busy until the next pass. The description now says so, and criterion 2 is replaced by one that checks truncation after an unblocked checkpoint. That is met by the owner's measurements: 297 MiB during a pass, then 0. The busy retry is covered by TestAReaderOnTheWALLeavesTheReclaimPending, not observed live.

## Summary

v0.5.2 is deployed on the internal lake and the workstation agent. Lake: schema 21, counts preserved (692/20391/20390), lake id and admin group unchanged, checkpoint /var/lib/terva-lampi-pre-v0.5.2-3gCC9co7. search.db-wal went from 1617 MiB under v0.5.1 to 0, peaked at 297 MiB during a pass, and returned to 0 when the pass ended, so the truncation works. The README's claim that it would stay under 64 MiB was wrong. The per-pass write volume is noted on TKT-01M3NPFNMH. Agent: v0.5.2, its rollback copies are in agent-rollback-v0.5.2-P89B, and its first sync uploaded nothing. The single refused upload predates the deploy.

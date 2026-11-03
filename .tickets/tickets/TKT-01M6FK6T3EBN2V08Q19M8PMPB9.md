---
schema: 3
id: TKT-01M6FK6T3EBN2V08Q19M8PMPB9
title: "Release v0.12.0: organize sessions into bays, catalog 23"
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
  actor: agent:claude-code/t3-0270c158
  branch: tickets/v0.12.0
  worktree: /home/sothr/.t3/worktrees/lampi/t3-0270c158
  commit: ccb9e4cc5111a02b9010319c6e13fd97621ac450
  session: null
  claimed_at: 2026-11-03T02:20:56Z
  expires_at: null
archive: null
created_at: 2026-11-03T02:18:28Z
updated_at: 2026-11-03T02:20:56Z
created_by:
  id: agent:claude-code/t3-0270c158
  name: ""
updated_by:
  id: agent:claude-code/t3-0270c158
  name: ""
extensions: {}
---

## Description

Release v0.12.0 from main ccb9e4c: everything merged since v0.11.1, PRs #247 to #256. The owner asked on 2026-11-02 to cut a new release; that authorizes the tag on both forges. Upgrading the live lake is not part of this ticket.

### Scope

- #253 organize sessions across bays: MCP/CLI metadata discovery and bay filters, exact selections, reviewed migration plans with apply, verify and recover, and an advisory bay doctor (TKT-01M6F5K38C9N9Y1TRX4PZB7JSW).
- #251 catalog 22: indexed project remote and folder on sessions, machine index.
- #252 Sessions page Project box with owner and folder suggestions, and a Machine select.
- #254 read token form: bay checkboxes, fill the session list from a project or machine.
- #250 wide list pages, wrapping project cells, machine names, readable times.
- #247 Health panel grouped by kind with a bulk fix and a temporary-folder deny.
- #248 read token form says what each Reads choice is for.

### Upgrade facts

internal/catalog/catalog.go appends two migrations: schema 21 to 23 (22 adds and backfills sessions.project_remote and project_cwd with indexes and provenance_by_machine; 23 adds bay_migrations). serve backs up the catalog to migration-backups/ first; v0.11.1 cannot open schema 23, so rollback means restoring that copy. internal/normalize and internal/recall/index.go are unchanged: no renormalize, index stays 8.

### Alternatives

v0.11.2 was rejected: a schema migration with a restore-only rollback, plus new CLI, MCP and API surface, is a minor-version change, and a patch number would tell operators a binary swap is enough to roll back.

## Acceptance criteria

- [x] An isolated v0.11.1 upgrade rehearsal and the release gates pass on the synchronized main commit
- [ ] The tag is published on both forges, and the archives, checksums and images name the tag
- [ ] Both release bodies describe the features, the migration, and the restore-based rollback

## Notes

**agent:claude-code/t3-0270c158** at 2026-11-03T02:20:56Z

Gates on main ccb9e4c (#256), in a detached scratch worktree (release-v0120.* under the agent-scratch lampi root). The first `GOFLAGS=-mod=mod just ci` failed in one test only, TestTakeIsConsistentUnderAWriter in internal/adapter/sqlitesnap (`no snapshot succeeded`). That package is unchanged since v0.11.1, the test passed 20 of 20 with -count=20 on its own, and a second full `just ci` passed, so it is a load flake: under the parallel suite, every Take in its 2-second window returned ErrChanged. Filed separately. `just release-check` passed; `just release-snapshot` built all five archives and checksums.

Isolated rehearsal (rehearse.sh, rehearse.log): old = GitHub's v0.11.1 linux_amd64 verified against checksums.txt, new = the snapshot's linux_amd64. v0.11.1 serve synced two synthetic Claude sessions (/work/app, /work/review), catalog 21, index 8. Candidate: `migrate --check` reported 21 -> 23, 2 pending; serve wrote migration-backups/catalog-...-v21.db, migrated to 23, filled project_cwd for both rows, index stayed 8, unchanged sync uploaded 0. `serve bays plan` -> `apply-plan --yes --create-proposed` created code-reviews and added the review session while keeping default; `verify-plan` matched the receipt; `recover-plan` wrote an inverse; bay_migrations held 1 receipt. An appended session ingested a new manifest; fsck 3 entries 0 bad. Rollback: v0.11.1 refused schema 23 with the restore message; after copying the backup over catalog.db and removing -wal/-shm, v0.11.1 opened schema 21 with both sessions, fsck 0 bad, no errors in serve logs.

Rollback cost seen in the rehearsal: the head ingested after the upgrade is not in the restored catalog, and the agent's next sync reports it unchanged, so that session stays at its pre-upgrade head on the lake until it changes again. Bays, plans and receipts made after the upgrade are lost as well. The release notes say so. No live config, token, lake or agent touched.

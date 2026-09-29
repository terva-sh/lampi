---
schema: 3
id: TKT-01M3NNF2FEC5VKPV6Q8PKZGBQ9
title: "Bays: inbox, bulk move, apply-rules and the sorting guide"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies:
  - TKT-01M3NNF29WCV5F1D8QSBWPM6M0
blocks_on: none
references: []
claim:
  actor: agent:claude-code/7859b064
  branch: bays/dashboard
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-7859b064
  commit: f6810af55cd8b37b10fcd1aecbd77349fb929e0d
  session: null
  claimed_at: 2026-09-29T16:32:36Z
  expires_at: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T16:32:36Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Tooling for keeping the default bay at zero. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

### Scope

- `serve bays inbox` lists sessions in `default`, held sessions, and sessions with a refused request, each with its reason: no rule matched, request refused (which bay, why), or hold rule X.
- Bulk move by filter (project, git remote, cwd prefix, device, harness) with `--dry-run`, audited.
- `serve bays apply-rules` runs the lake rules again over stored sessions, add-only as at ingest, with `--dry-run`.
- `serve bays release` for a held session restores its requested bays.
- A guide in `docs/` covering how to sort an existing lake after upgrade, how to write rules that keep new sessions out of the inbox, and why the aim is an empty inbox.

## Acceptance criteria

- [ ] serve bays inbox gives a reason for every listed session
- [ ] Bulk move and apply-rules have --dry-run and audit every change
- [ ] The sorting guide is in docs/

## Implementation plan

One PR (bays/inbox), stacked on the agent child (TKT-01M3NNF2CE).

**Catalog** (`internal/catalog/inbox.go`):
- `Inbox` lists every session that is in the default bay, held or flagged, has a refused request, or is in no bay, with one reason line each.
- `MoveSessions` does a filtered bulk move in one transaction. The filters are project, git remote, remote prefix, cwd prefix, device (through the device's bound machine_id in provenance) and harness. With DryRun it rolls back.
- `ApplyRules` routes stored sessions again, add-only; a hold flags. With dryRun it rolls back.
- `BayProblems` checks bay invariants: a session in no bay, dangling bay references, and a missing default.
- `removeFromBay` is extracted for in-transaction use.

**CLI**: `serve bays inbox`, `move BAY [--from BAY] FILTER... [--dry-run]` and `apply-rules [--dry-run]`. `serve fsck` reports BayProblems. This carries AC2 of TKT-01M3NNF21H.

**Docs**: `docs/bays-inbox.md` is the sorting guide, linked from cli.md and policy.md.

## Notes

**agent:claude-code/7859b064** at 2026-09-29T16:32:36Z

Decisions:
- Dry runs execute the change inside the transaction and roll it back. The queued audit lines roll back with it, so a dry run reports exactly what the real run would do. A separate read-only planner lost because it would duplicate the routing logic and could drift from it.
- A move with no filter is refused (ErrNoFilter). Moving a whole bay is serve bays delete, or a filter that says so, so an empty flag set cannot move everything.
- apply-rules does not take sessions out of the default. It adds only, as ingest does, and the guide says to follow it with a move out of the default. Removing from the default automatically lost because a session placed by a rule alone would leave the admin inbox without anyone looking at it.
- fsck --repair does not fix bay problems. It names them, and serve bays inbox and move are the tools that fix them.

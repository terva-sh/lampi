---
schema: 3
id: TKT-01M3NNF2FEC5VKPV6Q8PKZGBQ9
title: "Bays: inbox, bulk move, apply-rules and the sorting guide"
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T19:51:55Z
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

- [x] serve bays inbox gives a reason for every listed session
- [x] Bulk move and apply-rules have --dry-run and audit every change
- [x] The sorting guide is in docs/

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

**agent:claude-code/7859b064** at 2026-09-29T19:51:54Z

Review rounds, PR #162:

- Review 1461: move could not pick a session in no bay, although the inbox told the admin to move it. A move from the default bay now takes a matching session in no bay too. A single-session add command was the alternative, rejected as new surface when move already covers it. Also, a session in the default and another bay was called "nothing placed"; it now gets ReasonAlsoInDefault.
- Review 1464: fsck dropped the identity and bay errors when a CAS entry was bad; it now joins all three. The bay checks now cover holds naming a bay that is gone.
- Review 1467: a session placed in the default bay by an accepted request, or an add rule, was called "nothing placed". placedInDefault names the request or the rule. The reason loops check rows.Err.
- Review 1468: a bulk move could take a held session out of its hold bay. MoveSessions, and the dashboard's single MoveSession, refuse with ErrSessionHeld. Skipping held sessions was rejected, since it leaves part of a bulk move undone without saying so. --cwd-glob and --cwd-hash were accepted and ignored by move; they now filter. The add-rule reason reads "matches add rule N", because the lake does not record which rule placed a membership.
- The fsck AC of TKT-01M3NNF21H (catalog child) is fulfilled here: serve fsck reports sessions in no bay and references to bays that are gone.

## Summary

Merged as #162 (b46d7c5). serve bays inbox lists every session that needs an admin, each with at least one reason (TestInboxGivesEveryEntryAReason and the reason tests). serve bays move BAY [--from BAY] FILTER moves sessions: it needs a filter, a move from the default bay also places sessions in no bay, and it refuses held sessions. serve bays apply-rules routes stored sessions again, add-only. Both take --dry-run, which rolls back the transaction, audit lines included, and every real change is audited. serve fsck checks the bay tables. docs/bays-inbox.md is the sorting guide.

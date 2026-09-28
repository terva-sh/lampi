---
schema: 3
id: TKT-01M3GAHSNWKB00S1RMSF0K693W
title: "Registration codes: audit events after a commit can be lost"
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T02:19:20Z
updated_at: 2026-09-28T00:47:03Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Finding-1 from terva-review 992 on PR #17 (TKT-01M3FHHBJ, Registration codes), deferred at the owner's direction on 2026-09-27 so the onboarding stack could merge.

`RecordExpiries` in internal/catalog/registrations.go sets `expiry_recorded_at` and commits before either caller appends the `registration.expired` line. If that append then fails, for example because the audit file cannot be opened, the code is never returned again, so its expiry line is lost for good. `/v1/register` only logs the failure; `serve register` reports it and says the line will not be retried. That was a known cost when the fix landed, but it breaks the "every expiry reaches the audit log once" guarantee.

Fix: keep the event eligible until the append succeeds. For example, select the rows, append the lines, then mark them, and accept a possible duplicate line after a crash in between: a duplicate is better than a lost event. Alternatively, a two-phase marker (claimed, then recorded) can be reclaimed after a timeout. Test with an audit file that cannot be written: the expiry must be recorded once it can be.

## Implementation plan

Owner chose the outbox (2026-09-28), after PR #40 landed schema 8. Catalog schema 9 adds audit_outbox(seq, event JSON). A catalog change queues its audit events in its own transaction; Catalog.FlushAudit appends them in order and deletes each after its append, stopping at the first failure (at-least-once: a crash between append and delete writes a line twice, nothing is lost). Flushes in one process take turns (mutex). serve flushes after each queued change, before every direct audit line (keeps order, and retries), and at Open.
Two PRs for review size:
1. The outbox, and the events the ticket named first: RecordExpiries queues registration.expired (with an actor parameter), Redeem's finish callback returns the redemption events (redeemed, device created, bound). registrar.AuditExpiries and /v1/register flush.
2. The rest of the post-commit sites: device bind on first upload, token-file sync (created, detached), serve devices revoke/unbind/set-profile, and registration revoke.
Refusal lines record no catalog change and stay direct appends. Mint keeps audit-then-show (it revokes the code when the line fails).
Alternative rejected: writing the line before commit needs no schema change but leaves a line for a change that rolled back when the commit fails.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T02:31:13Z

Widened by terva-review 996 on PR #17 (finding-2), deferred with this ticket at the owner's direction. The redemption itself has the same gap. `/v1/register` commits the device and the spent code, and only then appends `registration.redeemed` and the device-binding events. If that append fails, the handler still returns success, and nothing can recreate those events later, since the code cannot be replayed.

So the general problem is that every audit event written after its catalog commit can be lost when the append fails: expiry, redemption, and binding. The fix should cover all of them. One option is an outbox of pending audit events kept in the catalog and written in the same transaction, then appended and cleared, and retried on the next write or at serve start. That gives at-least-once delivery, where a duplicate line is acceptable and a lost one is not.

**agent:claude-code/e4a47e8c** at 2026-09-27T22:38:20Z

Deferred behind open PR #40 (catalog/head-updates, TKT-01M3F2RKG), which adds catalog schema 8. The outbox this ticket needs is a catalog migration too, and two branches each appending schema 8 would collide. Picking it up once #40 lands, on top of it. Plan: an audit_outbox table written in the same transaction as the catalog change (expiry marks, redemption, device binding), appended and cleared after commit, retried on the next write and at serve start: at-least-once, with a duplicate line accepted and a lost one not.

**agent:claude-code/e4a47e8c** at 2026-09-28T00:47:03Z

Review of part 1 (#47) drove three changes beyond the plan: direct lines (refusals) also go through the outbox so the log keeps its order; FlushAudit takes audit.jsonl.lock (filelock) as well as the in-process mutex, since serve and a serve command are separate processes, and keeps events queued where there is no file lock; a line the catalog cannot queue is appended directly rather than dropped (the one case where order can slip, documented in policy.md; the reviewer's later objection to that was rejected with this reason). Part 2: a flush that fails is a warning (registrar.ErrAuditQueued), not a failure, so serve register --list and the dashboard still list, and a mint still goes ahead and refuses on its own line. serve devices revoke/unbind no longer suggest re-running to retry the record: the line is queued. Filed TKT-01M3JQDHM for a webauth test flake seen in CI on #47.

## Summary

Audit events no longer get lost after a catalog commit. Catalog schema 9 adds audit_outbox. Every catalog change that is audited queues its events in its own transaction: expiry, redemption (redeemed, device created, bound), token-file sync (created, detached), first-upload bind, serve devices revoke/unbind/set-profile, and code revoke. Catalog.FlushAudit then appends them in order, under a lock shared across processes, and deletes each once written. A line that cannot be written stays queued and goes out on the next write or the next serve start. Lines that record no change, such as refusals, take the same queue. PRs: #47 (outbox, expiry, redemption) and this one (devices and revoke). Mint keeps audit-then-show.

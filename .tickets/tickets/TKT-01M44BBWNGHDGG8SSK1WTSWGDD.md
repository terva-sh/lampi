---
schema: 3
id: TKT-01M44BBWNGHDGG8SSK1WTSWGDD
title: "Cursor CLI: skip unchanged and quarantined sessions without re-export"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/adapter
  - area/agent
  - area/redact
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M44B45GTE4ZFV6Y86P9A7RKE
blocks_on: none
references: []
claim:
  actor: agent:claude-code/580cbe08
  branch: cursor/skip-unchanged
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-580cbe08
  commit: c82e0b9d9ebe5df817b1fe9a2a6215031f9771ea
  session: null
  claimed_at: 2026-10-04T20:58:38Z
  expires_at: null
archive: null
created_at: 2026-10-04T20:58:24Z
updated_at: 2026-10-04T20:58:38Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

The Cursor CLI reader takes no memo (`memoized` in `internal/upload/upload.go` says so). Every pass snapshots and exports every permitted `store.db`, and only then does `unchanged` find the digest matches the watermark. A pass runs after any harness's write, which on a busy workstation is every 30 seconds or so. With the ACP sessions of TKT-01M44B45JRVQ1W2XF5H0K3MC4X (Cursor CLI: read ACP sessions under acp-sessions/), that is gigabytes copied, encoded and hashed per pass for sessions that did not change.

A quarantined session has the same problem for every harness. It never gets a watermark, so `unchanged` is false on every pass, and the file is read and scanned again to reach the same quarantine record. For a Claude JSONL that is cheap. For a multi-gigabyte Cursor export it is the full export on every pass.

Found on 2026-10-04 while planning the ACP reader. Filed separately so the ACP reader lands on a reader that is cheap when nothing changed.

## Acceptance criteria

- [ ] A Cursor CLI session whose store.db and WAL stat did not change is not snapshotted or exported on a non-full pass
- [ ] A memo hit that still needs bytes (no watermark, pending outbox) exports then, and the redaction scan still sees hidden bytes
- [ ] An artifact whose digest is already quarantined under the current ruleset and not allowed is not read or scanned again, and is still reported as quarantined
- [ ] Tests count snapshots to prove a second unchanged pass takes none

## Notes

**agent:claude-code/580cbe08** at 2026-10-04T20:58:31Z

Filed and promoted under the owner's instruction of 2026-10-04 to file, promote and complete the Cursor work in order. It sits between the key fix and the ACP reader because the ACP reader multiplies the per-pass cost.

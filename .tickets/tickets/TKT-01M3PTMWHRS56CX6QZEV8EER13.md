---
schema: 3
id: TKT-01M3PTMWHRS56CX6QZEV8EER13
title: "Conflicts: operator keeps the head or reopens a conflict"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3PTMA4C1XD80THS0AXEKK1Y
origin: null
dependencies:
  - TKT-01M3PTMKG9Y2S51V5Q59YZ0P7D
blocks_on: none
references: []
claim:
  actor: agent:claude-code/cd41c9ac
  branch: feat/conflict-actions
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-fdd1a9d1
  commit: b52913940b55fd388850fa5629a1584da9d629f8
  session: null
  claimed_at: 2026-09-29T15:17:59Z
  expires_at: null
archive: null
created_at: 2026-09-29T14:56:05Z
updated_at: 2026-09-29T16:46:16Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

An operator can settle a conflict without a lake-host command.

### Approach

- `/conflicts/{artifact}`: one conflict. Both sides' path, size,
  machines and digest, and where the copies part: the byte offset and
  line of the first difference, read from the CAS with a bounded
  stream. Links to both sides' Raw pages for an admin.
- "Keep the head" resolves the conflict as `kept_head`; "Reopen" removes
  a resolution. Operator only, CSRF-checked, audited in the same
  transaction, flushed before the answer, as the device actions do.
- A JSON form of both under `/api/web/v1/conflicts/{artifact}`.

## Acceptance criteria

- [x] A conflict page shows both sides and the offset and line where they part, read with a bounded stream
- [x] Keep the head and Reopen are operator-only, CSRF-checked and audited before they answer
- [x] A viewer sees the conflict page without the actions and cannot post them

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-29T15:17:59Z

Page /conflicts/{artifact} (viewer) with both sides, device names, and where they part (byte offset + line, up to 64 MiB of each, offsets only, never content); admin gets raw download links. Operator: Keep the head (optional one-line note, 500 chars) and Reopen, as forms and as POST /api/web/v1/conflicts/{id}/{keep-head|reopen}; CSRF-checked, audited in the transaction, flushed before answering. Mounted with the other operator routes, so a lake without Registrations serves the page but no actions. Tests mutation-checked: operator gate, audit flush, line counting, copy-ends branch, API CSRF each fail the suite when removed. Screenshots checked for open (operator/admin) and resolved states.

**agent:claude-code/cd41c9ac** at 2026-09-29T16:36:15Z

terva-review 1406 (run 3dd865a8): two medium findings, both accepted and fixed in 80e6859 (dispositions posted on #153): the page claimed a read failure when the server had no blob store; notes were trimmed before validation and measured in bytes. Tests added for each, mutation-checked.

**agent:claude-code/cd41c9ac** at 2026-09-29T16:46:16Z

terva-review 1411 (run 2003286e): prior two verified resolved; new medium (reopen accepted an empty or null note) and low (first-byte case did not name byte 0/line 1) both accepted and fixed; dispositions posted on #153.

---
schema: 3
id: TKT-01M3PTMWHRS56CX6QZEV8EER13
title: "Conflicts: operator keeps the head or reopens a conflict"
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-29T14:56:05Z
updated_at: 2026-09-29T14:56:06Z
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

- [ ] A conflict page shows both sides and the offset and line where they part, read with a bounded stream
- [ ] Keep the head and Reopen are operator-only, CSRF-checked and audited before they answer
- [ ] A viewer sees the conflict page without the actions and cannot post them

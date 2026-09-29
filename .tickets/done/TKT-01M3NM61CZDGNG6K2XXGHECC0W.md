---
schema: 3
id: TKT-01M3NM61CZDGNG6K2XXGHECC0W
title: Admin role and raw artifact access
type: epic
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: audit:raw-access
    path: .audit/raw-access.tsv
claim: null
archive: null
created_at: 2026-09-29T03:43:53Z
updated_at: 2026-09-29T05:05:00Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Let a lake admin read raw session artifacts: in the dashboard, and
through tokens for tools that have no browser session. Raw bytes are the
least filtered copy in the lake (ruleset v2 quarantines a hit but never
rewrites the file), so everything here is gated on a new `admin` role
above `operator`.

The owner decided on 2026-09-28 to reverse the "raw blobs out of scope"
line in `docs/web-ui-plan.md`, now that the dashboard has roles. The
trigger was a terva session whose normalization failed. The dashboard
showed no transcript and no way to see the input the projector choked
on.

### Children, in order

1. TKT-01M3NKZT6N (Dashboard: admin role above operator)
2. TKT-01M3NKY2V3 (Dashboard: admin-only raw artifact view), which
   depends on 1
3. The raw-read tokens ticket, which depends on 2 because it serves the
   same artifact reads

Each child lands as its own PR, so that terva-review can read every
change.

### Related work elsewhere

- TKT-01M3N8KHW5 (Bays) designs the admin/operator split this epic
  builds. As of 2026-09-28 it is on the unpushed branch
  `t3code/share-ponds-within-sessions`.
- TKT-01M3KAMD1Z (Lake-backed sessions: read path, token scope,
  byte-exact reads) plans read tokens more broadly, on the unpushed
  branch `t3code/session-lake-epic`. Raw-read tokens are the narrow
  first case, built so that they can become one permission in its
  model.

## Acceptance criteria

- [x] All child tickets are done
- [x] docs/web-ui-plan.md, docs/web-dashboard.md and docs/web-api.md describe the admin role, raw view and raw-read tokens

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-29T05:05:00Z

Owner confirmed on 2026-09-29 the first item under 'For the owner to review': operators are not promoted to admin on upgrade. See the note on TKT-01M3NKZT6N.

## Summary

Done. All three children landed on 2026-09-29, each as its own PR,
merged by agent:claude-code/cd41c9ac working unattended on the owner's
instruction to finish the epic:

- #132 (dde83a3): TKT-01M3NKZT6N, the admin role
- #134 (93998cb): TKT-01M3NKY2V3, the raw artifact view
- #135 (227aeb4): TKT-01M3NM6FW7, raw-read tokens

Each PR merged only after green CI, a clean terva-review at its head,
and a disposition for every finding. No person reviewed them before
merge.

### For the owner to review

- **Operators are not promoted to admin on upgrade.** The agent made
  this call; the owner did not answer the question. It is the opposite
  of the migration the Bays epic (TKT-01M3N8KHW5) proposes. The
  reasoning is in TKT-01M3NKZT6N.
- **Raw reads buffer in memory.** Each read holds up to 8 MiB before it
  is sent, so the audit line only ever names bytes that were read
  (terva-review 1323). This is acceptable for a capped, admin-only
  route, but it no longer streams.
- **Read tokens can't be edited.** Changing a token's scope means
  revoking it and minting another.

### Decision trail

The agent's decision log is committed at `.audit/raw-access.tsv`. A
second model (Sonnet) reviewed it against the session and found nothing
serious in the admin gate, the digest and scope checks, or token
minting.

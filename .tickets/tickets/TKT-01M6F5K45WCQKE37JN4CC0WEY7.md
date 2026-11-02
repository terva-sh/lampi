---
schema: 3
id: TKT-01M6F5K45WCQKE37JN4CC0WEY7
title: "Bays: plan and apply selected sessions to multiple destinations"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/server
  - policy
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies:
  - TKT-01M6F56Z6QP6C2P3M1T7J923BV
  - TKT-01M6F5K3BTS8X9BTFZTECWYDCR
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:20:32Z
updated_at: 2026-11-02T22:57:43Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

Current serve bays move accepts metadata filters, one source and one destination. It cannot consume an exact reviewed UID list or add several destination memberships while preserving an explicit set of source memberships. Implement a shared selection and migration-plan service plus host CLI surface according to the usage spike.

### Selection and preview

Accept bounded paginated discovery results, exact UID lists/files and supported structured filters. Users can combine a reproducible candidate set with explicit exclusions. Snapshot the reviewed session IDs rather than silently expanding a live query during apply. An exploratory text/heuristic match is evidence for a selection, not a persistent routing rule. Preview without writes: each session's prior membership, proposed additions/removals, already-satisfied no-ops, held/invalid/inaccessible items, destination availability and the reason for inclusion. Provide the complete paginated/exportable change set, counts and limits; never present a sample as the full preview. Allow proposing a not-yet-created destination without creating it during preview; keep creation and grants explicit at apply.

### Apply contract

Support adding one or more bays while preserving memberships, and moving from explicitly named source memberships into one or more destinations while preserving unrelated memberships. Define duplicate IDs, conflicting add/remove targets, invalid destinations, aliases, last-bay/default fallback and hold constraints. Bind the plan to the relevant membership/rule/destination state, reject or re-preview material changes, and handle new sessions or later manifests without silently changing the reviewed set. Define bounded transaction/batch behavior and partial-failure reporting, ensure retries are idempotent, and audit the operator and each effective change.

Re-check authorization during apply. Reading or generating a plan grants no write authority. Provide post-apply verification and an inverse-plan/recovery path that validates current state rather than overwriting unrelated later edits; explain that removing a membership cannot retract content already read. Rule creation for future manifests is a separate explicit operation with its own match preview. Explain when later requests/rules may re-add removed memberships. Preserve session IDs and artifacts. Update CLI help and bays-inbox documentation.

## Acceptance criteria

- [ ] Selections support exact IDs and exclusions as well as metadata filters, and enumerate all matches with explicit paging and limits.
- [ ] A write-free plan shows complete before/after memberships and access implications for additions or moves to multiple destinations, including proposed new bays.
- [ ] Apply uses the reviewed set, checks current authority and relevant state, and reports no-ops, rejected holds, conflicts and partial failures explicitly.
- [ ] Tests cover multi-bay preservation, stale plans, missing destinations, last-bay/default behavior, retry idempotency and audit output.
- [ ] Verification and state-checked recovery are available, with existing-session moves documented separately from future routing.
- [ ] A proposed future-routing rule can be previewed without installing it, with its metadata-wide catchment and later-manifest effects distinguished from the exact historical selection.

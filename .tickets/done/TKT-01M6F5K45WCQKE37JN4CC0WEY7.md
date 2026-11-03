---
schema: 3
id: TKT-01M6F5K45WCQKE37JN4CC0WEY7
title: "Bays: plan and apply selected sessions to multiple destinations"
type: task
status: done
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
updated_at: 2026-11-03T00:09:39Z
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

- [x] Selections support exact IDs and exclusions as well as metadata filters, and enumerate all matches with explicit paging and limits.
- [x] A write-free plan shows complete before/after memberships and access implications for additions or moves to multiple destinations, including proposed new bays.
- [x] Apply uses the reviewed set, checks current authority and relevant state, and reports no-ops, rejected holds, conflicts and partial failures explicitly.
- [x] Tests cover multi-bay preservation, stale plans, missing destinations, last-bay/default behavior, retry idempotency and audit output.
- [x] Verification and state-checked recovery are available, with existing-session moves documented separately from future routing.
- [x] A proposed future-routing rule can be previewed without installing it, with its metadata-wide catchment and later-manifest effects distinguished from the exact historical selection.

## Implementation plan

Build a bounded shared service that consumes all metadata pages into exact UID selections with exclusions, then previews add/move edits without writes. Plans include complete per-session visible before/after, holds/issues/no-ops, destination/access warnings and state fingerprints. Require current administrator authority for apply; re-derive the reviewed request and compare membership/routing/destination/grant state in one transaction before changes. Use a durable migration receipt for idempotent retries, verification and inverse previews that preserve unrelated later memberships and reject changed touched memberships. New destination creation is explicit and grants remain separate. Add host select/plan/apply/verify/recover CLI commands and an independent hypothetical routing catchment preview, with semantic concurrency/hold/audit/recovery tests and documentation. Limit each atomic operation to 500 sessions; excess selections fail rather than sampling.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-02T23:17:50Z

Implemented complete bounded selection (one metadata-query snapshot, exact UIDs/exclusions, reproducible selector) and 24-hour portable plans. Add/remove edits resolve aliases and preserve unrelated membership; new bays stay hypothetical until explicit creation at apply. Plans include all rows, no-ops/rejections, access warnings and state fingerprints. Apply requires independently established current administrator authority, re-derives the reviewed request inside one transaction and checks membership timestamps, holds/flags, routing metadata/requests and conservative lake/bay/alias/grant/rule state. Atomic batches cap at 500; 8 MiB plan bounds refuse instead of sampling. Durable receipts enable restart-safe retries, verification and inverse previews that preserve unrelated later bays and reject changed touched membership. No grants or routes are created implicitly. Added select/plan/apply-plan/verify-plan/recover-plan/preview-rule host commands, strict files and private atomic JSON output. Hypothetical future routing evaluates its own metadata-wide catchment without writes. Scoped proposed names deliberately do not test availability, preventing hidden-name collision probes; scoped policy state is unavailable and apply requires a full preview. Updated schema upgrade fixtures for the new receipt table. Full catalog/cli/web package tests passed; focused final migration/selection/CLI race tests, vet and whitespace checks passed. Tests cover stale membership/holds/grants/rules/names/routing/deletions, expired/tampered plans, multi-bay/no-op/fallback behavior, rejected-batch atomicity, receipts across reopen, inverse conflicts and audit actor/plan IDs. No live lake changed.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:39:19Z

Final UX review expanded full-administrator access previews from grant counts to bounded named read-grant principals for destinations and removed sources, with retained-membership/token caveats and explicit clipping. Scoped previews still disclose no grant principals. A focused test verifies both sides of that boundary; deployment docs now call out the durable receipt schema migration and backup-based rollback.

**agent:codex/t3-6387cc8a** at 2026-11-03T00:09:39Z

### Keep recovery checks across imported/refined previews

While fixing review 3016's dashboard per-session refinement issue, the caller audit found that only RecoverBayMigration checked the original receipt's touched state. Importing or re-previewing its request through PlanBayMigration could adopt a later remove/re-add as current state and then undo it. That violates the documented state-checked correction contract even when current bay names look unchanged.

The common planner now requires independently established complete administrator authority for recovery provenance, loads its receipt, checks touched membership state for every effective selected UID, and verifies that resolved additions/removals are exactly that receipt item's inverse. RecoverBayMigration delegates to this shared validation. Excluded or omitted sessions do not block an unchanged selected subset. Apply still independently revalidates state and returns an already-existing receipt before attempting any replay.

A catalog regression creates an inverse, removes/re-adds a touched membership with a later timestamp, proves a refreshed inverse is stale, then previews/applies only the unchanged companion subset. It also rejects substituted edits labelled as recovery. Dashboard import/re-preview retains the exact old request when receipt validation refuses it. The full focused migration/recovery/organization suite passes; final normal/race/vet/build gates are running. No schema change, live membership edit or new write authority is involved.

## Summary

Delivered the shared exact-selection and multi-bay plan/apply/receipt/recovery service plus six host CLI commands and docs/bay-migrations.md. Historical batches and hypothetical future-rule catchments are separate. Complete bounded previews, state/authority checks, explicit creation, atomic audited apply, idempotent retries and state-checked inverse plans are tested. MCP/dashboard adapters can now reuse this service.

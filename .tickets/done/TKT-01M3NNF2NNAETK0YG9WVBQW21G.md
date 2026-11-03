---
schema: 3
id: TKT-01M3NNF2NNAETK0YG9WVBQW21G
title: "Bays: guided selection, multi-bay migration and inbox triage"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies:
  - TKT-01M3NNF2K3AJVA7QBH61A18028
  - TKT-01M6F5K45WCQKE37JN4CC0WEY7
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T04:06:18Z
updated_at: 2026-11-03T00:09:39Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

Expand the existing bulk held/refused-session triage follow-up into one dashboard workflow for organizing reachable stored sessions, including those outside the default inbox. The owner explicitly requested migration UX and actual usage patterns on 2026-11-02. Preserve bulk release and refusal explanations from the original scope; share the selection and migration-plan service with CLI and MCP rather than building another interpretation of membership changes.

### Interaction

Discover sessions by project, directory, source/device, harness, time, visible bay and available evidence. Start from a search hit, session page, bay inbox or doctor suggestion. Let users select individual sessions, all filtered results across pages, and explicit exceptions. Show why a session matches and identify companion sessions with no project ID. Related-session suggestions must not silently enlarge selection.

Choose one or more destination bays, then choose add-to-bays (retain existing membership) or move-from-named-bays (remove only chosen sources). Explain default as inbox, preserve unrelated memberships and distinguish a code-review content bay from a hold/review quarantine. An inline proposed-bay flow must not create a bay merely to show a preview. Surface who may gain or lose reach through bay grants and other applicable scope, without promising that moving a session revokes every reader.

Preview the complete before/after membership delta, no-ops, exclusions, holds/refusals and blocked items. Keep selection through paging and recoverable errors; provide accessible labels, keyboard operation and clear empty/stale/loading states. Apply under existing appropriate admin authority, CSRF and recent-sign-in rules, revalidate the plan, show effective changes and link to verification/state-checked correction. Held sessions still need explicit release; moving may not bypass it. Do not silently grant access while migrating.

Offer a separate reviewed step to route future matching sessions. Display exactly which new sessions that rule could catch, supported metadata versus ad hoc evidence, and how existing requests/rules could restore a removed membership. Existing apply-rules only adds; creating a rule does not finish sorting stored sessions. Update dashboard help and the migration walkthrough. Retain the original completed dashboard-bays dependency and add new shared-service/UX dependencies.

## Acceptance criteria

- [x] A guided flow works from inbox, bay/session/search views and doctor suggestions, with exact selection, all filtered pages and exclusions.
- [x] Users can add multiple destination bays or move from explicit sources, with complete membership/access previews and unrelated memberships preserved.
- [x] Bulk held/refused triage retains explanations and explicit release rules; current admin, CSRF and recent-sign-in protections remain enforced.
- [x] Paging, keyboard/screen-reader use, missing project IDs, stale plans, errors, verification and correction are covered by interaction tests.
- [x] Routing future sessions is a separate previewed decision, and documentation distinguishes it from moving historical sessions.

## Implementation plan

Add an admin-only server-rendered organize workflow using shared discovery/selection/migration services. Preserve exact selections across POST paging, filters, explicit exclusions and recoverable errors; offer directory-hash inspection for unlinked companions without auto-inclusion. Support add versus explicit-source moves to multiple existing/proposed bays, complete before/after/access previews, private plan export/import, fresh-sign-in/CSRF-protected apply, receipt verification and inverse previews. Retain held/refused explanations and add explicit bounded bulk hold release with per-item results. Keep future-rule catchment preview separate and provide an exact host installation handoff. Link inbox/bay/session/search and later doctor findings into the flow. Include updated-time metadata selectors, accessible labels/keyboard-native controls, empty/stale states and semantic interaction/auth tests.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-03T00:01:27Z

### Review 3014 finding-1: preserve imported bay reference intent

Published review 3014 at 96b9d88f0a595e3fc2148bc9b40e5798ca55906c confirms the hidden-membership finding is resolved, but finds a real dashboard refinement bug. formFromMigration retained name/alias references while checkbox values were bay IDs, so subsequent selection/exclusion refinements could lose destinations or sources. Review: https://git.local.sothr.com/terva-sh/lampi/pulls/253#issuecomment-37729.

Accepted: form restoration now resolves existing add/remove references to current IDs, deduplicates the controls, and retains proposed names in the separate proposed-destination field. This shared restoration path covers import, re-preview, doctor handoff, apply-result/error retention and recovery. Normalizing only the template comparison was rejected because the editable form should carry canonical existing references and keep hypothetical names separate.

Unavailable references are retained as explicit checked controls, rather than silently discarded. A disappeared bay does not prevent an idempotent receipt retry; a later newly requested preview still refuses the unavailable reference until the user replaces or removes it. No destination, source, selected UID or exclusion is inferred.

Rendered-form regression tests import real name/alias plans, re-preview them, refine exclusions and submit another membership preview in add/move modes with multiple existing and proposed destinations. They also exercise a doctor handoff and a deleted-destination receipt retry followed by refusal of a new invalid preview. Focused organization tests pass. Full normal tests, web race tests, vet/build and formatting checks are running before the final source/ticket push and fresh model review. No live lake state changed.

**agent:codex/t3-6387cc8a** at 2026-11-03T00:09:39Z

### Review 3016 finding-1: retain heterogeneous per-session edits

Review 3016 at 2bbe0822915add18508af8aeaa27ae8212cab63b confirms name/alias restoration is resolved but finds that imported and recovery request.edits were represented by empty uniform Add/Move fields. Refinement therefore discarded the correction. Accepted. Review: https://git.local.sothr.com/terva-sh/lampi/pulls/253#issuecomment-37738.

The dashboard now has a retained per-session mode. It carries the strict typed request in a hidden field, shows each session's retained additions/removals, and refines the exact cohort/exclusions without converting edits to a uniform operation. Browsing and filtered selection are limited to the original edit cohort. New sessions without an existing edit, a missing retained request or conflicting uniform fields are refused; a separate explicit operation is available for new membership intent. Proposed bays unused by the narrowed effective cohort are omitted, and recovery provenance/reason are retained. Existing uniform name/alias restoration remains intact.

A union/intersection of per-session sources/destinations was rejected because it changes intent. Merely disabling refinement was also rejected because the user needs to refine actual migration/correction cohorts. The dedicated mode supports that workflow while keeping each selected session's operation exact.

Rendered-form tests create a heterogeneous original migration and recovery, refine exclusions/exact/filtered selection, re-preview and apply only the selected inverse; unselected and unrelated sessions remain unchanged. They reject newly selected sessions and missing retained state. Another imported-edit test retains alias-based edits while dropping an excluded session's unused proposed bay. Refreshing an old correction after a receipt-touched change is refused while retaining the per-session request in the form. The shared planner now checks that receipt on every recovery preview; the catalog ticket records this related audit fix.

Focused catalog/web migration/recovery/organization tests pass. Full normal tests, catalog/web/CLI race checks, vet/build and formatting checks are running before the single final push and fresh full review. No live lake state changed.

## Summary

Shipped the admin organization workflow with exact cross-page selections, complete add/move previews, proposed bays, plan import/export, protected apply, durable receipt verification/correction, explicit hold release and separate future-rule catchment. Added semantic rendered-form interaction tests and UTC updated-time discovery/selection filters. Web/catalog tests pass. Full CLI run exposed pre-existing trimpath runtime.Caller fixture-path failures; repeat the repository gate without trimpath.

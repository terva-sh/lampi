---
schema: 3
id: TKT-01M3NNF2NNAETK0YG9WVBQW21G
title: "Bays: guided selection, multi-bay migration and inbox triage"
type: task
status: ready
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
updated_at: 2026-11-02T22:57:44Z
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

- [ ] A guided flow works from inbox, bay/session/search views and doctor suggestions, with exact selection, all filtered pages and exclusions.
- [ ] Users can add multiple destination bays or move from explicit sources, with complete membership/access previews and unrelated memberships preserved.
- [ ] Bulk held/refused triage retains explanations and explicit release rules; current admin, CSRF and recent-sign-in protections remain enforced.
- [ ] Paging, keyboard/screen-reader use, missing project IDs, stale plans, errors, verification and correction are covered by interaction tests.
- [ ] Routing future sessions is a separate previewed decision, and documentation distinguishes it from moving historical sessions.

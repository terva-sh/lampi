---
schema: 3
id: TKT-01M6F5K4X6SQP6Y1VXQ77JBYE0
title: "Doctor: suggest bay destinations with supporting evidence"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/server
  - area/search
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies:
  - TKT-01M6F5K4SJPKTM2867HZEHSWR0
  - TKT-01M6F5K45WCQKE37JN4CC0WEY7
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:20:33Z
updated_at: 2026-11-02T22:57:44Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

Extend the profile/bay health tooling to suggest sending sessions or identified cohorts to one or more bays, using the validated heuristic contract. The existing serve profiles doctor and dashboard Health panel share internal/profiledoctor, whose inputs currently cover allow/deny rules and newest project inventory. Its review_group code groups projects waiting for capture permission; it is not a code-review classifier. Add a deliberate session/bay context or shared report rather than reinterpreting that finding.

Expose stable finding codes, heuristic version, evidence/reasons, uncertainty or abstention, candidate session counts/IDs, present memberships and one or more recommended destinations. User mappings can name an existing bay or propose creating one; distinguish missing/unknown/inaccessible destinations from a valid choice. Already-placed sessions should not generate duplicate work, and conflicting alternatives should not be presented as all required destinations. Show gaps in inventory/index coverage and bound content-assisted work under the caller's permissions.

Share the report across CLI human/JSON output, the dashboard Health/bay view and a read-only MCP recommendation tool or documented extension to an existing read surface. Keep profile-wide information restricted to appropriate operators; an events:read report sees only reachable sessions and permitted metadata, not every device taking the profile. Use the same heuristic engine, identifiers and evidence shape on each surface. Link recommendations to an exact migration preview that distinguishes add/move and historical/future actions.

Recommendations change nothing. In particular, ApplySafe and serve profiles doctor --write may continue their existing safe capture-rule fixes but must not apply bay suggestions, widen capture, create bays, change grants or write a route. Preserve existing profile findings and machine-install doctor work. Provide dismiss/ignore feedback as chosen in the heuristic contract, with scope and expiry. Add focused fixtures, authorization tests and reference docs. This ticket implements validated heuristics; it does not assume a learned classifier is needed.

## Acceptance criteria

- [ ] Shared doctor findings recommend one or more destinations with stable codes, evidence, version, uncertainty, current placement and bounded complete-or-truncated candidate reporting.
- [ ] CLI human/JSON, dashboard and read-only MCP expose consistent appropriately scoped recommendations and a handoff to exact migration preview.
- [ ] Tests cover reviewers and companions, ordinary development, mixed repositories, missing metadata/bays, multiple compatible bays, alternatives and already-placed sessions.
- [ ] Doctor can abstain/dismiss as specified, and incomplete or unauthorized evidence is not reported as a confident classification.
- [ ] Bay suggestions never run as safe fixes or --write changes, and existing profile diagnostics retain their behavior.
- [ ] Help and documentation explain heuristics, limitations, permissions and separate historical/future actions.

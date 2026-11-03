---
schema: 3
id: TKT-01M6F5K4X6SQP6Y1VXQ77JBYE0
title: "Doctor: suggest bay destinations with supporting evidence"
type: task
status: done
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
updated_at: 2026-11-03T00:20:26Z
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

- [x] Shared doctor findings recommend one or more destinations with stable codes, evidence, version, uncertainty, current placement and bounded complete-or-truncated candidate reporting.
- [x] CLI human/JSON, dashboard and read-only MCP expose consistent appropriately scoped recommendations and a handoff to exact migration preview.
- [x] Tests cover reviewers and companions, ordinary development, mixed repositories, missing metadata/bays, multiple compatible bays, alternatives and already-placed sessions.
- [x] Doctor can abstain/dismiss as specified, and incomplete or unauthorized evidence is not reported as a confident classification.
- [x] Bay suggestions never run as safe fixes or --write changes, and existing profile diagnostics retain their behavior.
- [x] Help and documentation explain heuristics, limitations, permissions and separate historical/future actions.

## Implementation plan

Implement internal/baydoctor as a shared bounded metadata report. Expose read-only MCP/HTTP, host CLI human/JSON and dashboard doctor entry with exact migration-preview handoff. User supplies mappings and scoped expiring ignores; no purpose guesses, safe fixes, profile writes, grants or routes. Test scope, alternatives, incomplete evidence and no mutation.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-02T23:37:50Z

Final self-review added a destination_limit status when overlapping compatible mappings exceed the migration service total of 20 destinations. The report withholds an unusable preview request; a focused fixture verifies the combined limit.

**agent:codex/t3-6387cc8a** at 2026-11-03T00:20:26Z

### Review 3018 finding-1: separate cohort selection and destination authority

Review 3018 on e5e7abd7abc621b1ff317777306b41ff71d69600 confirms browser paging normalization is resolved but identifies a real doctor bug: exact session selection was reused as credential authority when resolving destination bays. An unrestricted reader could not receive a recommendation to an existing bay unless a selected session was already there. Accepted. Review: https://git.local.sothr.com/terva-sh/lampi/pulls/253#issuecomment-37749.

Recommend now retains the original credential scope for destination resolution and uses a separately narrowed scope for candidate enumeration and report fingerprinting. Both request-local bay selection and exact UIDs leave destination authority intact. Do not relax SelectReadBays itself: fixed-session credentials must still be unable to discover bays outside their authorized sessions, and bay-scoped readers still require destination access.

The administrator dashboard also separates its focus/per-session candidate bounds from administrator authority. Exact selections are intersected with the current organization context; when no explicit selection exists, focused/per-session filters freeze that already-bounded cohort before recommendation. This preserves filtering without misrepresenting focus or imported edits as a credential restriction. An explicit empty session_uids array now means an empty inspected cohort and survives JSON serialization via omitzero; nil remains unspecified. This prevents an empty focused result from expanding to the whole lake. Documentation explains these distinctions.

Regressions exercise HTTP/MCP with unrestricted, bay-scoped, source-only, fixed-session and multi-session credentials, including destination reach outside the selected cohort and refusal to broaden fixed-token session access. Dashboard tests cover exact, focus and retained per-session selection/filtering plus empty results. The executable CLI walkthrough now verifies host and remote exact reviewer recommendations to an existing empty learning bay alongside a hypothetical review bay. Focused tests pass; full normal, affected-package race, vet/build and store checks run before the final source/ticket push and fresh full review. No live lake state changed.

## Summary

Implemented internal/baydoctor with shared versioned metadata reports, explicit mappings, stable finding IDs, uncertainty, compatible destinations, conflicting-alternative abstention, current visible placement, hypothetical/unknown destinations, already-placed suppression, scoped expiring feedback and exact migration-preview handoffs. Exposed host serve bays doctor and remote query recommend-bays in human/JSON output, read-only MCP recommend_bays and HTTP POST, and the administrator organization dashboard with a Health-panel entry. No safe fix or profile --write path consumes this report. Tests exercise the labeled matrix, strict policy/argument handling, no mutation, 500-row truncation without handoffs, reviewer/rater coverage without content/index, scope/privacy, dashboard preview, CLI host/remote parity and private output. Reference contracts and help are in docs/bay-doctor.md, docs/cli.md and docs/web-api.md. Focused tests pass; repository gates are being run before final handoff.

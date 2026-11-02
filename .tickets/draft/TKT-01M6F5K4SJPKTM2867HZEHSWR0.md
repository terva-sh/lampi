---
schema: 3
id: TKT-01M6F5K4SJPKTM2867HZEHSWR0
title: "Spike: validate heuristics for bay recommendations"
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/search
  - question
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies:
  - TKT-01M6F5K3BTS8X9BTFZTECWYDCR
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:20:32Z
updated_at: 2026-11-02T22:20:35Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

Determine evidence and evaluation criteria for doctor suggestions that a session or cohort belongs in one or more bays. Start from observed Vuoro reviewers and rating companions but avoid assuming their folder layout or SDK source proves every session is a review.

Evaluate explicit importer/workflow provenance when available, verified directory layouts across devices, harness metadata, companion relationships, standardized initial prompts and user-defined mappings from evidence to existing/proposed bay destinations. Separate known fields from information the capture protocol does not yet provide. If a needed provenance field is missing, record the smallest follow-up contract and file its own draft; do not silently invent identity metadata or infer intent from an entire repository. Compare metadata-only and bounded opt-in content-assisted approaches, including cost and the permissions needed to inspect content.

Build synthetic/sanitized positive and negative fixtures: Vuoro reviewer and finding rater; development in the same repository/harness; ordinary development discussing reviews; SDK sessions doing other jobs; reused review directories; several machine layouts; missing or contradictory provenance; a session suitable for multiple bays; one already correctly placed; missing destinations and incomplete inventories. Record labeled expected outcomes, false-positive/negative results and why a rule abstains. Decide evidence/confidence categories and acceptance thresholds based on those results, rather than assigning an unsupported confidence score.

Recommend existing destinations only through an explicit mapping or other explainable policy. A guessed bay name does not authorize a grant. Where several destinations are useful, distinguish compatible memberships from mutually exclusive alternatives. Define suppress/dismiss feedback, repeat-run behavior, conflicting signals and heuristic versioning. Findings must distinguish advisory historical selections from metadata selectors safe enough to propose for future routing. No autonomous moves, new grants or capture changes.

## Acceptance criteria

- [ ] A documented evidence hierarchy identifies existing versus missing provenance and distinguishes content permission/cost from metadata-only checks.
- [ ] A sanitized labeled fixture matrix includes companions, mixed development, multi-bay cases, contradictory signals and incomplete data.
- [ ] Evaluation records false positives/negatives, chosen thresholds or evidence categories, abstention rules and rejected heuristics.
- [ ] A recommendation contract covers destination mappings, multiple compatible bays versus alternatives, dismissals, versioning and migration-preview handoff.

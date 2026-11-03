---
schema: 3
id: TKT-01M6HMDAJG6AM55ZER5AG5Z9D4
title: Preview large routing catchments without sampling
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/docs
assignees: []
milestone: null
parent: TKT-01M6F5A01MX2ZCP9J7M3GGD7M0
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-03T21:17:59Z
updated_at: 2026-11-03T21:17:59Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

### Observed dogfood friction

The live Vuoro Linux worktree prefix contains 1,281 reachable sessions. Complete metadata discovery can page through it, but select_sessions and preview_bay_rule deliberately cap candidates at 500. Historical migration can be divided into exact paired batches; a single hypothetical root routing predicate cannot currently return its complete catchment. Narrowing to historical run directories changes the persistent predicate and does not validate the proposed root rule. Doctor correctly reports incomplete=true with no preview handoffs for this case.

### Proposed investigation

Keep apply/migration bounds and current refusal behavior. Design either a resumable read-only routing catchment traversal with unchanged predicate/scope and explicit consistency limits, or a documented complete metadata-based catchment verification followed by a host preview. A report must separate complete enumeration from current-state validity and hypothetical future matches. Record the original predicate, exclusions for historical review only, exact returned UID union, continuation safety and content/metadata limits. Do not silently widen grants or use a sampled report to approve routing.

Evaluate usability for one rule matching more than 500 stored sessions, while retaining explicit operator review, held/unlinked/control coverage and no automatic rule installation. Update MCP/CLI and operator documentation with tested refusal and completion behavior. Synthetic fixtures only; no production transcripts or credentials.

## Acceptance criteria

- [ ] Provide a complete verifiable read-only catchment workflow for an unchanged predicate matching more than 500 sessions.
- [ ] Preserve bounded apply plans, scope privacy, continuation consistency and explicit incomplete/refusal outcomes.
- [ ] Document and test the distinction between historical exact batches and future routing predicates.

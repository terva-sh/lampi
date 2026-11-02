---
schema: 3
id: TKT-01M6F9QAGWW2PCF4ESFVVNN643
title: Capture explicit workflow and companion provenance for session routing
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/protocol
  - area/agent
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T23:32:44Z
updated_at: 2026-11-02T23:32:44Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

TKT-01M6F5K4SJPKTM2867HZEHSWR0 (Spike: validate heuristics for bay recommendations) found that capture protocol 1 lacks explicit workflow/run/role provenance. mapped-metadata-v1 therefore offers uncertain user-mapped candidates rather than classifying reviews. The sanitized matrix includes a development session in a reused review directory, which metadata mapping cannot distinguish.

Design the smallest optional source-backed contract for workflow producer/type, run identifier and role (reviewer versus finding rater). Record whether each value came from a trusted producer or unverified transcript hint. Do not infer purpose from harness=claude, SDK origin, repository, bay name or directory alone. Old manifests retain unknown values. Reuse a run identifier for companion discovery only when the source actually records one; it must not silently enlarge historical selections.

Determine adapter availability, privacy and protocol compatibility before adding persisted fields or route predicates. Document conflict/absence behavior and migration requirements. This is a separate draft follow-up, not a prerequisite for the current advisory metadata doctor; no deployment, capture change or live migration is authorized by filing it.

## Acceptance criteria

- [ ] A versioned optional contract records producer, workflow, run ID and companion role without inventing missing values.
- [ ] Adapters preserve verified source evidence; development in a reused review directory remains distinguishable or explicitly unknown.
- [ ] Discovery and recommendation tests cover old manifests, contradictory evidence, privacy and future-routing semantics.

---
schema: 3
id: TKT-01M6F5K514R3K4KSCPE766K25F
title: "Docs: discover, classify and migrate sessions across bays"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies:
  - TKT-01M6F5K429GX4TZFJB35TV3KC7
  - TKT-01M6F5K49E9JCCE6C80FTTNRP3
  - TKT-01M3NNF2NNAETK0YG9WVBQW21G
  - TKT-01M6F5K4X6SQP6Y1VXQ77JBYE0
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

Write a task-oriented walkthrough for people and agents using the shipped discovery, filtering, recommendation and migration tools. Each implementation ticket owns its reference/help updates; this ticket connects them into complete user workflows and keeps examples runnable.

Update docs/reading-the-lake.md, docs/bays-inbox.md, relevant docs/cli.md and docs/web-api.md sections, docs/web-dashboard.md, registration/routing guidance and the documentation index. Show the actual final command and tool schemas, not speculative names from draft tickets. Clarify capture profiles versus agent bay requests versus lake rules versus stored membership, and default as inbox. Explain one session in multiple bays without duplicate artifacts or changed identity.

Include an agent-first Vuoro exercise: discover metadata, identify reviewers plus finding-rating companions including those without project IDs, inspect supporting evidence and development controls, select exact IDs with exclusions, accept/refine doctor suggestions, preview add versus move into one or more destinations, and hand the same plan to an authorized operator. Use examples covering a separate development bay plus review bay, an overlapping secondary bay and multiple machine layouts. Explain rule creation and historical backfill separately, subsequent uploads, nonempty inbox reasons, held/refused items and coverage/truncation.

Cover read-token setup through a private file, minimum appropriate authority, scoped discovery/filtering, changed readers and token semantics, planned bay creation/grants, stale plan refusal, idempotent retry, post-apply verification and state-checked correction. Clarify that classification is advisory and moving membership cannot retract previously read content. No credentials, live transcript dumps or guessed host coordinates belong in examples.

Validate CLI examples and MCP requests against synthetic fixtures, including zero-match/error paths and multi-bay behavior; record the checks. Reference the Vuoro dogfood outcome without making internal host details a prerequisite. If UX or heuristics require decisions, document the chosen contract with rationale and link the relevant design ticket.

## Acceptance criteria

- [ ] An indexed end-to-end guide covers people and agents from discovery through identification, doctor recommendations, reviewed migration and verification.
- [ ] Examples cover exact selection, companions/missing project IDs, add versus move, multiple bays and future routing separately from existing-session changes.
- [ ] Reference docs and CLI/MCP help agree on permissions, selectors, scope, paging/truncation, heuristic limitations and stale-plan recovery.
- [ ] Executable examples are validated on synthetic data and include failure/empty-result paths without production credentials or transcript dumps.

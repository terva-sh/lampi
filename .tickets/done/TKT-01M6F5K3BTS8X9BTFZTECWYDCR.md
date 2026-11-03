---
schema: 3
id: TKT-01M6F5K3BTS8X9BTFZTECWYDCR
title: "Spike: session identification and multi-bay migration UX"
type: spike
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
  - question
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:20:31Z
updated_at: 2026-11-02T23:02:50Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

Determine the interactions and selection contract before implementing a bulk bay migration. Use the observed Vuoro review workflow as evidence, and cover other concrete user jobs without presenting hypothetical usage as measured behavior.

### Scenarios to work through

Sort an existing default inbox by verified review working directories; identify reviewers plus rating/checking companions across machines; split review and development sessions within one repository; classify sessions without a project ID; select a few search results or an exact UID list; select all filtered sessions across pages and exclude false positives; add a useful secondary bay while retaining current memberships; move sessions out of named source bays into several destinations; correct a mistaken classification after later changes; turn an accepted selection into future routing where available metadata supports it. Include held/refused sessions, overlapping cohorts, new matches arriving during preview, and empty results.

### Deliverable

Record scenario-based CLI transcripts and dashboard interaction specifications with representative sanitized examples. Choose AND/OR semantics, destination any/all semantics for reads, selection persistence and pagination behavior, labels for add versus move, and treatment of default and unrelated memberships. Specify the evidence shown beside each candidate, how users exclude a session, how to keep companion sessions together without silently expanding a selection, and when a content-derived classification needs explicit metadata instead of a wider rule.

Evaluate exact UIDs, project/remote, directory prefix/glob/hash, device, harness, time, search results and provenance as identification patterns. Explicitly say which current fields support persistent routing and which only support an ad hoc historical selection. Include discoverability, keyboard and screen-reader behavior, stale plans, readable errors and operator authorization. Decide how rule previews differ from one-time moves and how to explain who gains or loses access, including readers with dynamic bay scope and fixed-session tokens. Do not claim a bay move revokes every token that can still read the session.

Reuse the existing dashboard triage ticket rather than specifying a competing inbox flow. Record tradeoffs: filter-only moves miss arbitrary reviewed subsets; matching text does not prove purpose; membership replacement can discard useful bays; applying a live query twice can change the selected set. Do not implement or change the live lake in this spike.

## Acceptance criteria

- [x] A scenario matrix distinguishes observed Vuoro behavior from proposed workflows and covers mixed projects, companions, missing project IDs and multiple memberships.
- [x] CLI and dashboard interaction specifications define find, include/exclude, destination choice, preview, apply, verification and recovery.
- [x] A selection/membership contract resolves paging, any/all and AND/OR semantics, add versus move, stale plans, holds and the default inbox.
- [x] Decisions map to the implementation tickets with unresolved research questions and alternatives recorded.

## Implementation plan

Write a scenario-based interaction and API contract from the observed Vuoro reviewer/rater pair and mixed-development controls. Set any-of semantics for multiple selected read bays, AND between field filters, exact reviewed UID sets with explicit exclusions, add versus source-specific move semantics, bounded plans and stale-state checks. Specify CLI and accessible dashboard steps for selection across pages, complete previews, apply/verify/recovery and future-route previews. Record companion/provenance unknowns and the distinction between evidence and routing predicates. Map decisions to the dependent implementation tickets and document why filter-only moves, implicit membership replacement and keyword-only classification lose.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-02T23:02:50Z

The interaction contract is docs/bay-workflow.md. It records the observed Vuoro reviewer/rater pair and proposes scenario coverage for mixed repositories, missing project IDs, several device layouts, exact search-derived IDs, overlapping memberships, holds/refusals and new matches after preview. Decisions: fields AND; multiple read bays any-of with deduplication; selection across every page freezes exact IDs and exclusions; add retains all membership, move removes only named sources; related sessions require explicit inclusion; missing provenance remains unknown. The CLI/dashboard flow is discover → refine → choose add/move and destinations → complete preview → operator apply → verify/correct. Preview binds relevant state; retries do not widen the set; correction preserves later unrelated changes. Future routing has its own hypothetical metadata match preview and is not inferred from a text search. Accessible selection and error states, scoped readership implications, default-inbox fallback and fixed-session token behavior are explicit. Alternatives and why they lost are in the contract. Concrete API schemas/batch limits and heuristic evaluation belong to their dependent tickets, rather than unresolved UX decisions. The linked implementation tickets inherit this contract.

## Summary

Recorded the scenario and interaction contract in docs/bay-workflow.md: exact scoped selection with exclusions, multi-bay add/move semantics, complete/stale previews, operator apply and state-checked correction, accessible dashboard behavior, and separately previewed future routing. The observed Vuoro evidence is distinguished from proposed scenarios and uncertain provenance. Design reviewed against current scope, routing, hold and membership code; implementation tickets own the executable checks.

---
schema: 3
id: TKT-01M6HMDAFCSN0N540295A4Y3FZ
title: Read bounded opening evidence for exact session cohorts
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

The real Vuoro exercise enumerated 1,298 directory/project candidates and controls, including 1,281 Linux Claude worktree sessions. Purpose is not present in workflow metadata; reviewers, unlinked finding raters, draft mergers, and fix editors need explicit opening inspection. read_events supports this correctly one session at a time, but the per-token 30-call burst and two-call-per-second refill make complete inspection require hundreds of paced calls. Opening prompts can include large diffs even when the evidence needed is a short leading template. An eight-event opening span can also end before the first user message.

### Proposed investigation

Design a read-only MCP/CLI opening-evidence operation over exact bounded UIDs. Return per-session generation/head, event positions, byte/content truncation and coverage limits, plus a bounded first user opening excerpt and optional separately bounded role evidence. Preserve access checks and transcript-as-data handling; never infer trusted workflow identity or silently include related sessions. Budget session count, total returned bytes, events searched per session and retries explicitly. Keep current no-sampling semantics: distinguish complete inspection, absent opening within the budget, unavailable transcript, changed generation and rate limiting.

Compare client-side paced read_events composition with a server batch surface. Favor the client if it meets the operational need without a new endpoint; a wider token or removing rate limits is not a solution. Document consent/scope, safe retry/resume, private artifact handling and exact selection/preview handoff. Production transcripts and credentials must not become fixtures.

## Acceptance criteria

- [ ] Define a bounded MCP/CLI workflow that returns generation/head and evidence positions for each exact requested UID.
- [ ] Keep incomplete/truncated/unavailable/changed states explicit and never classify unread sessions as exclusions.
- [ ] Document per-token pacing, retry/resume and content budgets; validate on sanitized reviewer/rater/editor/development controls.

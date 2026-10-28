---
schema: 3
id: TKT-01M61NTE24T6CZN1P92AV7164F
title: "Usage rollup: lake and harness token totals with cache split"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:34:46Z
updated_at: 2026-10-28T16:34:46Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

Show total tokens for the whole lake and per harness on the overview page.

### Approach

- Add a rollup table keyed by session and generation, written when the indexer projects a session and replaced when the generation changes. Backfill existing sessions.
- Store four token kinds separately: plain input, output, cache read, cache write, plus cost when reported. Never store only a combined input figure (owner requirement, see the epic).
- Normalize the counting so the kinds mean the same thing across harnesses. Codex `input_tokens` includes `cached_input_tokens`, so plain input there is input minus cached. Claude reports cache read and cache creation apart from input already. Keep a field null when a harness does not report it, and show "not reported" rather than zero.
- Deduplicate Claude usage on the message id; Claude Code repeats `message.usage` on each content-block line.
- Start with a spike: compute totals for a sample of real sessions per harness and compare against the harness's own figures before building the page.

### Open question

Codex usage events read `last_token_usage`, falling back to `total_token_usage`. Confirm that summing `last_token_usage` across a session gives the session total, and what happens when only the total is present.

## Acceptance criteria

- [ ] Plain input, output, cache read and cache write are stored and shown separately
- [ ] Claude usage is deduplicated per API message
- [ ] A spike compares totals against harness figures for sample sessions
- [ ] Existing sessions are backfilled

---
schema: 3
id: TKT-01M3AQK47R1Z3T2PC6FH7N97VR
title: Cursor 1d promote composer usage and summary
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T22:11:49Z
updated_at: 2026-09-24T22:28:18Z
created_by:
  id: agent:cursor/d660
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/d660
  name: Cursor cloud agent
extensions: {}
---

## Description

Promote composer-level usage and conversation summary in the Cursor IDE normalize projector only.

A `composerData:` object can carry `usageData` and `latestConversationSummary`. Today those fields stay on the composer meta event. This ticket makes a sibling `usage` event and a sibling `compaction` event. The Cursor adapter, its Version, and the cursor-cli projector stay as they are.

### Usage

`usageData` on a `composerData:` object becomes a sibling `event_type=usage` (`EventUsage`). The composer meta event remains.

`usage.cost_usd` is `costInCents / 100` when `costInCents` is present and numeric. Cents become USD. A missing cost leaves `cost_usd` null. A non-numeric `costInCents` is not a cost.

Token counts land on `Usage` (`input`, `output`, `cache_read`, `cache_write`) only when that object already has them under a recognizable token name. Cost, amount, and price are not token counts. Do not invent a count from those fields.

Unknown keys on `usageData` stay on the usage event's `extra`. The promoted object does not stay on the composer meta `extra`.

### Conversation summary

`latestConversationSummary` on a `composerData:` object becomes a sibling `event_type=compaction` (`EventCompaction`).

The human-readable text is `content_text` when the value is a string, or when the object has a string field `summary`. The rest of the object stays on the compaction event's `extra`. The promoted value does not stay on the composer meta `extra`.

### Out of scope

Bubble-level `tokenCount` and bubble-level `usageData` stay on the bubble event `extra`. They are not usage events. Soft-link, deploy, the Cursor CLI projector (`cursorcli`), AgentsView, PlantCursor, and a live Cursor install stay out. Operator docs stay out. There is no `Version` or `harness_version` bump.

## Acceptance criteria

- [x] A composerData usageData object becomes a sibling usage event. cost_usd is costInCents / 100 when that field is present and numeric. Recognizable token fields already on that object map to Usage input, output, cache_read, and cache_write. Unknown usageData keys stay on the usage event extra, not on the composer meta extra.
- [x] A missing cost leaves cost_usd null. A non-object usageData stays on the composer meta extra and is not a usage event. cost, amount, and price do not become token counts.
- [x] A composerData latestConversationSummary becomes a sibling compaction event. A string value, or an object field summary, is content_text. The rest stays on the compaction event extra, not on the composer meta extra.
- [x] Bubble usageData and tokenCount stay on the bubble event extra. They are not usage or compaction events.
- [x] go test ./internal/normalize/... passes. The Cursor adapter and its Version are unchanged.

## Definition of done

- [x] The projector change lives in internal/normalize/cursor.go and internal/normalize/cursor_test.go.
- [x] The Cursor adapter is unchanged, including Version. cursor-cli, soft-link, deploy, and AgentsView are unchanged. Operator docs are unchanged.

## Implementation plan

### Approach

Change `Cursor.projectComposerData` so one `composerData:` row returns the meta event plus siblings. The meta event stays first. `Cursor.emit` is unchanged. `Usage` is set on the usage event after emit.

A `usageData` object promotes only when it has a numeric `costInCents` or a recognizable integer token field. The live shape is a model name mapped to `costInCents` and `amount`. `cost_usd` is the sum of those cents divided by 100. A numeric `costInCents` on the object itself is the same cost. Token fields map only under recognizable names. `amount`, `price`, `cost`, and `tokenCount` are not counts. Leftover keys, including `amount`, stay on the usage event extra. When nothing promotes, `usageData` stays on the meta extra.

A `latestConversationSummary` promotes only when a summary string is present. That string is the value itself, the object field `summary`, or the live nested `summary.summary`. `content_text` is that string. The rest of the object stays on the compaction extra. An empty string, an object with no summary string, and every other shape stay on the meta extra.

Bubble `usageData` and `tokenCount` stay on the bubble extra. `assertNoPromoted` allows `tool_call`, `tool_result`, `usage`, and `compaction`, and still fails when a usage or compaction event comes from a `bubbleId:` row.

### Tests

`TestCursorComposerFixtureMatrix` locks the six QE cases: happy usage (`costInCents` 250 to `cost_usd` 2.5, live model bucket, `amount` is not a token count), happy compaction (live nested summary string, one-level object, and a string value), bubble stay-put, the `assertNoPromoted` flip with a 1c tool still present, malformed and no-invent, and co-promote of usage plus compaction on one composer with the meta event kept.

## Notes

**agent:cursor/d660** at 2026-09-24T22:18:58Z

Operator docs still say composer usageData and latestConversationSummary stay on extra. That sentence is stale after this projector change. Quill owns the docs pass. This ticket does not edit docs/architecture.md or the README.

**agent:cursor/d660** at 2026-09-24T22:28:18Z

Gage locked the fixture matrix after the first 1d tip. TestCursorComposerFixtureMatrix is that matrix. Live usageData is a model key whose value has costInCents and amount. Live latestConversationSummary nests the text at summary.summary. Amount-only, junk keys, and an empty or malformed summary do not promote.

## Summary

Cursor.projectComposerData keeps the composer meta event and adds siblings when there is something to promote.

usageData becomes a usage event when costInCents is numeric or a recognizable token count is present. The live shape is a model name mapped to costInCents and amount. cost_usd is those cents divided by 100, so 250 is 2.5. amount, price, and cost are not token counts. Amount-only and junk usageData stay on the meta extra. A missing or non-numeric cost leaves cost_usd null.

latestConversationSummary becomes a compaction event when a summary string is present. The live text is summary.summary. A one-level summary string and a string value are content_text as well. The rest of the object stays on the compaction extra. An empty or malformed summary stays on the meta extra.

Bubble usageData and tokenCount stay on the bubble extra. assertNoPromoted allows usage and compaction, and still fails when those events come from a bubble row. Tool calls from the 1c promote stay allowed.

The Cursor adapter Version is still 2. cursor-cli, soft-link, deploy, AgentsView, e2e, and operator docs are unchanged. go test ./... passed.
